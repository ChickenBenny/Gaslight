package chain

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- shared test helpers ---

// addr builds a deterministic Address from a label so tests read nicely.
func addr(label string) Address {
	var a Address
	copy(a[:], label)
	return a
}

// deposit is a stand-in "1000 USDC to the exchange hot wallet" transfer.
func deposit(id string) Tx {
	return Tx{
		ID:    id,
		From:  addr("alice"),
		To:    addr("exchange"),
		Value: big.NewInt(1000),
	}
}

// produceEmpty appends n empty blocks.
func produceEmpty(d *Driver, n int) {
	for i := 0; i < n; i++ {
		d.ProduceBlock(nil)
	}
}

// --- Driver: producing blocks ---

func TestProduceBlockAppendsAndChains(t *testing.T) {
	d := NewDriver(1)
	genesis := d.Snapshot().Head()

	b1 := d.ProduceBlock(nil)
	b2 := d.ProduceBlock(nil)

	s := d.Snapshot()
	assert.Equal(t, uint64(2), s.Height())
	assert.Equal(t, uint64(1), b1.Number)
	assert.Equal(t, uint64(2), b2.Number)
	// Parent chaining is what lets a client detect a reorg (a new head whose
	// ParentHash it has never seen).
	assert.Equal(t, genesis.Hash, b1.ParentHash, "b1 should point at genesis")
	assert.Equal(t, b1.Hash, b2.ParentHash, "b2 should point at b1")
	assert.Equal(t, b2.Hash, s.Head().Hash, "head should be the latest block")

	require.NotNil(t, s.ByNumber(2))
	assert.Equal(t, b2.Hash, s.ByNumber(2).Hash)
}

// --- Driver: reorg (the flagship behaviour) ---

// Mirrors the reorg-eats-a-deposit scenario: a deposit is confirmed, then a
// longer competing branch that omits it wins, orphaning the deposit.
func TestReorgReplacesCanonicalAndOrphansDeposit(t *testing.T) {
	d := NewDriver(1)
	produceEmpty(d, 4) // heights 1..4

	depBlk := d.ProduceBlock([]Tx{deposit("alice-deposit")}) // height 5
	depTx := depBlk.Receipts[0].TxHash

	produceEmpty(d, 3) // heights 6,7,8 -> deposit now has 3 confirmations

	before := d.Snapshot()
	require.Equal(t, uint64(8), before.Height())
	require.NotNil(t, before.ReceiptOf(depTx), "deposit receipt should exist before the reorg")
	oldH5 := before.ByNumber(5).Hash

	// Fork from height 4 and lay down 5 empty blocks (5'..9'). New head is
	// height 9 > 8, so this branch wins; none of the new blocks contain the deposit.
	require.NoError(t, d.Reorg(4, make([][]Tx, 5)))

	after := d.Snapshot()
	assert.Equal(t, uint64(9), after.Height())
	assert.NotEqual(t, oldH5, after.ByNumber(5).Hash, "height 5 should be a new block after reorg")
	assert.NotNil(t, after.ByHash(oldH5), "orphaned block should still be findable by hash")
	// The killer assertion: the deposit's receipt is gone — the signal a deposit
	// engine must react to (reverse the credited balance).
	assert.Nil(t, after.ReceiptOf(depTx), "orphaned deposit receipt must be nil after reorg")
}

// A reorg branch can also *introduce* txs. A tx that arrives via the winning
// branch is canonical, so its receipt must be queryable — not nil (which would
// make it indistinguishable from an orphaned tx).
func TestReorgBranchTxIsQueryable(t *testing.T) {
	d := NewDriver(1)
	produceEmpty(d, 3) // canonical height 3 (empty blocks)

	dep := deposit("reorg-dep")

	// Fork at height 1, lay down 4 blocks; the deposit rides in the 2nd new
	// block (height 3). New head height = 1 + 4 = 5 > 3, so the branch wins.
	branch := [][]Tx{nil, {dep}, nil, nil}
	require.NoError(t, d.Reorg(1, branch))

	r := d.Snapshot().ReceiptOf(hashTx(dep))
	require.NotNil(t, r, "receipt for a tx introduced by the reorg branch should not be nil")
	assert.Equal(t, hashTx(dep), r.TxHash)
}

// A tx can go canonical -> orphaned -> canonical again across consecutive
// reorgs. ReceiptOf must reflect the *current* canonical state each time.
func TestReorgReincludesOrphanedTx(t *testing.T) {
	d := NewDriver(1)
	dep := deposit("dep")
	depHash := hashTx(dep)

	produceEmpty(d, 2)        // heights 1,2
	d.ProduceBlock([]Tx{dep}) // height 3 carries the deposit
	produceEmpty(d, 1)        // height 4

	require.NotNil(t, d.Snapshot().ReceiptOf(depHash), "deposit should start canonical")

	// Reorg #1: fork at 2 with 3 empty blocks (new head 5 > 4) -> orphans the deposit.
	require.NoError(t, d.Reorg(2, make([][]Tx, 3)))
	assert.Nil(t, d.Snapshot().ReceiptOf(depHash), "deposit should be orphaned after reorg #1")

	// Reorg #2: fork at 1 with 6 blocks (new head 7 > 5), re-including the deposit.
	reinclude := [][]Tx{nil, {dep}, nil, nil, nil, nil}
	require.NoError(t, d.Reorg(1, reinclude))
	assert.NotNil(t, d.Snapshot().ReceiptOf(depHash), "deposit should be canonical again after reorg #2")
}

// --- Driver: finality ---

func TestFinalize(t *testing.T) {
	d := NewDriver(1)
	produceEmpty(d, 6) // height 6

	// Happy path: finalizing a height within [finalized, head] succeeds.
	require.NoError(t, d.Finalize(4))
	assert.Equal(t, uint64(4), d.Snapshot().Finalized())

	// Re-finalizing the current finalized height is allowed (idempotent boundary).
	require.NoError(t, d.Finalize(4))
	assert.Equal(t, uint64(4), d.Snapshot().Finalized())

	// Cannot finalize a block that does not exist yet.
	require.ErrorIs(t, d.Finalize(10), ErrHeightTooHigh)

	// The finalized watermark only moves forward, never backward.
	require.ErrorIs(t, d.Finalize(3), ErrHeightTooLow)

	// Rejected calls must leave the watermark untouched.
	assert.Equal(t, uint64(4), d.Snapshot().Finalized(), "finalized must be unchanged after rejected calls")
}

// --- Driver: reorg rejection (table-driven) ---

// Every rejection path must fail with a specific error and leave the chain
// completely unchanged.
func TestReorgRejects(t *testing.T) {
	u64 := func(v uint64) *uint64 { return &v }
	cases := []struct {
		name      string
		height    int     // blocks to produce before the reorg
		finalize  *uint64 // nil = skip finalize
		forkFrom  uint64
		branchLen int
		wantErr   error
	}{
		{"below finalized", 6, u64(4), 3, 5, ErrReorgBelowFinalized},
		{"does not outgrow head", 6, nil, 4, 1, ErrReorgTooShort},
		{"ties with head", 6, nil, 4, 2, ErrReorgTooShort},
		{"fork point above head", 3, nil, 5, 4, ErrForkPointNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := NewDriver(1)
			produceEmpty(d, c.height)
			if c.finalize != nil {
				require.NoError(t, d.Finalize(*c.finalize))
			}
			before := d.Snapshot()

			err := d.Reorg(c.forkFrom, make([][]Tx, c.branchLen))
			require.ErrorIs(t, err, c.wantErr)
			after := d.Snapshot()
			assert.Equal(t, before.Head().Hash, after.Head().Hash, "head must be unchanged after a rejected reorg")
			assert.Equal(t, before.Finalized(), after.Finalized(), "finalized must be unchanged after a rejected reorg")
		})
	}
}

// --- Determinism: the moat. Same call sequence -> identical hashes. ---

func TestSameSequenceProducesIdenticalHead(t *testing.T) {
	run := func() Hash {
		d := NewDriver(1)
		produceEmpty(d, 4)
		d.ProduceBlock([]Tx{deposit("alice-deposit")})
		produceEmpty(d, 3)
		require.NoError(t, d.Reorg(4, make([][]Tx, 5)))
		return d.Snapshot().Head().Hash
	}
	assert.Equal(t, run(), run(), "identical call sequences must yield identical head hashes")
}
