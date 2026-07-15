package chain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenesisSnapshot(t *testing.T) {
	s := NewDriver(1).Snapshot()

	assert.Equal(t, uint64(0), s.Height())
	require.NotNil(t, s.Head(), "genesis head is nil")
	assert.Equal(t, uint64(0), s.Head().Number)
	assert.Equal(t, uint64(0), s.Finalized())
}

func TestByNumberAndByHashReturnNilWhenAbsent(t *testing.T) {
	s := NewDriver(1).Snapshot()

	assert.Nil(t, s.ByNumber(99), "ByNumber of a nonexistent height should be nil")
	assert.Nil(t, s.ByHash(Hash{}), "ByHash of an unknown hash should be nil")
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
			require.NotNil(t, byNum)
			assert.Equal(t, c.want.Hash, byNum.Hash)
			assert.NotNil(t, s.ByHash(c.want.Hash))
		})
	}
}

func TestReceiptOfCanonicalTx(t *testing.T) {
	d := NewDriver(1)
	blk := d.ProduceBlock([]Tx{deposit("alice-deposit")})
	txHash := blk.Receipts[0].TxHash

	r := d.Snapshot().ReceiptOf(txHash)
	require.NotNil(t, r, "ReceiptOf a tx in a canonical block returned nil")
	assert.Equal(t, txHash, r.TxHash)
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

	assert.Equal(t, heightBefore, snap.Height(), "snapshot height changed after a later ProduceBlock")
	assert.Equal(t, headBefore, snap.Head().Hash, "snapshot head changed after a later ProduceBlock")
}
