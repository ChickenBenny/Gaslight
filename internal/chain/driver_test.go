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
	if after.Height() != 9 {
		t.Fatalf("post-reorg height = %d, want 9", after.Height())
	}
	// Height 5 is now a different block on the winning branch.
	if after.ByNumber(5).Hash == oldH5 {
		t.Error("ByNumber(5) still returns the orphaned block after reorg")
	}
	// The orphaned block leaves canonical but stays reachable by hash —
	// reorg-aware clients rely on this to reconcile their view.
	if after.ByHash(oldH5) == nil {
		t.Error("orphaned block should still be findable by hash")
	}
	// The killer assertion: the deposit's receipt is gone. This is the signal a
	// deposit engine must react to (reverse the credited balance).
	if after.ReceiptOf(depTx) != nil {
		t.Error("orphaned deposit receipt must be nil after reorg")
	}
}

func TestReorgBelowFinalizedIsRejected(t *testing.T) {
	d := NewDriver(1)
	produceEmpty(d, 6) // height 6
	require.NoError(t, d.Finalize(4))
	before := d.Snapshot().Head().Hash

	// Forking from height 3 would rewrite finalized block 4 — must be rejected.
	err := d.Reorg(3, make([][]Tx, 5))
	require.ErrorIs(t, err, ErrReorgBelowFinalized)
	assert.Equal(t, before, d.Snapshot().Head().Hash, "chain must be unchanged after a rejected reorg")
}

func TestReorgThatDoesNotWinIsRejected(t *testing.T) {
	d := NewDriver(1)
	produceEmpty(d, 6) // height 6

	// Fork from 4 with a single block -> new head would be height 5 < 6.
	// A reorg only takes effect if the new branch is longer.
	err := d.Reorg(4, make([][]Tx, 1))
	require.ErrorIs(t, err, ErrReorgTooShort)
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
