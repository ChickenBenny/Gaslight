package chain

import "testing"

func TestGenesisSnapshot(t *testing.T) {
	d := NewDriver(1)
	s := d.Snapshot()

	if s.Height() != 0 {
		t.Fatalf("genesis height = %d, want 0", s.Height())
	}
	if s.Head() == nil {
		t.Fatal("genesis head is nil")
	}
	if s.Head().Number != 0 {
		t.Fatalf("genesis block number = %d, want 0", s.Head().Number)
	}
	if s.Finalized() != 0 {
		t.Fatalf("genesis finalized = %d, want 0", s.Finalized())
	}
}

func TestByNumberAndByHashReturnNilWhenAbsent(t *testing.T) {
	s := NewDriver(1).Snapshot()

	if s.ByNumber(99) != nil {
		t.Error("ByNumber of a nonexistent height should be nil")
	}
	if s.ByHash(Hash{}) != nil {
		t.Error("ByHash of an unknown hash should be nil")
	}
}

func TestByNumberAndByHashFindCanonicalBlocks(t *testing.T) {
	d := NewDriver(1)
	b1 := d.ProduceBlock(nil)
	b2 := d.ProduceBlock(nil)
	s := d.Snapshot()

	cases := []struct {
		name string
		want *Block
	}{
		{"height 1", b1},
		{"height 2", b2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			byNum := s.ByNumber(c.want.Number)
			if byNum == nil || byNum.Hash != c.want.Hash {
				t.Fatalf("ByNumber(%d) did not return the expected block", c.want.Number)
			}
			if s.ByHash(c.want.Hash) == nil {
				t.Fatalf("ByHash(%x) returned nil", c.want.Hash)
			}
		})
	}
}

func TestReceiptOfCanonicalTx(t *testing.T) {
	d := NewDriver(1)
	blk := d.ProduceBlock([]Tx{deposit("alice-deposit")})
	txHash := blk.Receipts[0].TxHash

	r := d.Snapshot().ReceiptOf(txHash)
	if r == nil {
		t.Fatal("ReceiptOf a tx in a canonical block returned nil")
	}
	if r.TxHash != txHash {
		t.Errorf("receipt.TxHash = %x, want %x", r.TxHash, txHash)
	}
}

// A snapshot is an immutable point-in-time view: mutating the chain afterwards
// must not change a snapshot taken earlier. This is what makes lock-free reads safe.
func TestSnapshotIsImmutable(t *testing.T) {
	d := NewDriver(1)
	d.ProduceBlock(nil)

	snap := d.Snapshot()
	heightBefore := snap.Height()
	headBefore := snap.Head().Hash

	d.ProduceBlock(nil) // advance the chain after taking the snapshot

	if snap.Height() != heightBefore {
		t.Errorf("snapshot height changed: %d -> %d", heightBefore, snap.Height())
	}
	if snap.Head().Hash != headBefore {
		t.Error("snapshot head changed after a later ProduceBlock")
	}
}
