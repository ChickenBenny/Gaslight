package chain

import (
	"math/big"
	"testing"
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
	if s.Height() != 2 {
		t.Fatalf("height = %d, want 2", s.Height())
	}
	if b1.Number != 1 || b2.Number != 2 {
		t.Fatalf("block numbers = %d,%d, want 1,2", b1.Number, b2.Number)
	}
	// Each block must point at its parent — this chain is what lets a client
	// detect a reorg (a new head whose ParentHash it has never seen).
	if b1.ParentHash != genesis.Hash {
		t.Error("b1.ParentHash does not point at genesis")
	}
	if b2.ParentHash != b1.Hash {
		t.Error("b2.ParentHash does not point at b1")
	}
	if s.Head().Hash != b2.Hash {
		t.Error("head is not the latest block")
	}
	if got := s.ByNumber(2); got == nil || got.Hash != b2.Hash {
		t.Error("ByNumber(2) did not return b2")
	}
}

// --- Driver: reorg (the flagship behaviour) ---

// Mirrors the reorg-eats-a-deposit scenario: a deposit is confirmed, then a
// longer competing branch that omits it wins, orphaning the deposit.
func TestReorgReplacesCanonicalAndOrphansDeposit(t *testing.T) {
	d := NewDriver(1)
	produceEmpty(d, 4) // heights 1..4

	depBlk := d.ProduceBlock([]Tx{deposit("alice-deposit")}) // height 5
	depTx := depBlk.Receipts[0].TxHash                       // ProduceBlock computes a receipt per tx

	produceEmpty(d, 3) // heights 6,7,8 -> deposit now has 3 confirmations

	before := d.Snapshot()
	if before.Height() != 8 {
		t.Fatalf("pre-reorg height = %d, want 8", before.Height())
	}
	if before.ReceiptOf(depTx) == nil {
		t.Fatal("deposit receipt should exist before the reorg")
	}
	oldH5 := before.ByNumber(5).Hash

	// Fork from height 4 and lay down 5 empty blocks (5'..9'). New head is
	// height 9 > 8, so this branch wins. None of the new blocks contain the deposit.
	if err := d.Reorg(4, make([][]Tx, 5)); err != nil {
		t.Fatalf("Reorg returned error: %v", err)
	}

	after := d.Snapshot()
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
	if err := d.Finalize(4); err != nil {
		t.Fatalf("Finalize returned error: %v", err)
	}
	before := d.Snapshot().Head().Hash

	// Forking from height 3 would rewrite finalized block 4 — must be rejected.
	if err := d.Reorg(3, make([][]Tx, 5)); err == nil {
		t.Fatal("Reorg below finalized height should return an error")
	}
	if d.Snapshot().Head().Hash != before {
		t.Fatal("chain must be unchanged after a rejected reorg")
	}
}

func TestReorgThatDoesNotWinIsRejected(t *testing.T) {
	d := NewDriver(1)
	produceEmpty(d, 6) // height 6

	// Fork from 4 with a single block -> new head would be height 5 < 6.
	// A reorg only takes effect if the new branch is longer.
	if err := d.Reorg(4, make([][]Tx, 1)); err == nil {
		t.Fatal("a branch that does not outgrow the current head should be rejected")
	}
}

// --- Determinism: the moat. Same call sequence -> identical hashes. ---

func TestSameSequenceProducesIdenticalHead(t *testing.T) {
	run := func() Hash {
		d := NewDriver(1)
		produceEmpty(d, 4)
		d.ProduceBlock([]Tx{deposit("alice-deposit")})
		produceEmpty(d, 3)
		if err := d.Reorg(4, make([][]Tx, 5)); err != nil {
			t.Fatalf("Reorg returned error: %v", err)
		}
		return d.Snapshot().Head().Hash
	}
	if run() != run() {
		t.Fatal("identical call sequences must yield identical head hashes")
	}
}
