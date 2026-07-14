package chain

import (
	"errors"
	"slices"
	"sync"
	"sync/atomic"
)

// Errors returned by Finalize and Reorg when a request would violate a chain
// invariant; the chain is left unchanged.
var (
	ErrHeightTooHigh       = errors.New("chain: finalize height is above the current head")
	ErrHeightTooLow        = errors.New("chain: finalize height is below the current finalized height")
	ErrReorgBelowFinalized = errors.New("chain: cannot reorg below finalized height")
	ErrForkPointNotFound   = errors.New("chain: fork point not found")
	ErrReorgTooShort       = errors.New("chain: reorg is too short")
)

// Driver mutates the chain via copy-on-write (RCU): every write clones the
// current snapshot and atomically publishes the new one, so readers are
// lock-free and never observe torn state.
type Driver struct {
	mu      sync.Mutex
	current atomic.Pointer[ChainSnapshot]
	seq     uint64
	chainID uint64
}

// NewDriver returns a Driver whose chain contains only the genesis block.
func NewDriver(chainID uint64) *Driver {
	genesis := &Block{
		Number:     0,
		Hash:       hashBlock(0, 0, Hash{}, nil),
		ParentHash: Hash{},
	}
	snap := &ChainSnapshot{
		blocks:    map[Hash]*Block{genesis.Hash: genesis},
		canonical: []Hash{genesis.Hash},
		head:      genesis.Hash,
	}

	d := &Driver{chainID: chainID}
	d.current.Store(snap)
	return d
}

// Snapshot returns the current immutable view of the chain.
func (d *Driver) Snapshot() *ChainSnapshot {
	return d.current.Load()
}

// ProduceBlock appends a block containing txs to the canonical chain and
// returns it, minting a success receipt for every tx.
func (d *Driver) ProduceBlock(txs []Tx) *Block {
	d.mu.Lock()
	defer d.mu.Unlock()

	cur := d.current.Load()
	parent := cur.Head()
	block := d.buildBlock(parent, txs)
	ns := cur.clone()
	ns.blocks[block.Hash] = block
	ns.canonical = append(ns.canonical, block.Hash)
	ns.head = block.Hash
	d.current.Store(ns)

	return block
}

// Finalize marks height as finalized: blocks at or below it can no longer be
// reorged away. height must lie within [current finalized, head].
func (d *Driver) Finalize(height uint64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	cur := d.current.Load()

	if height > cur.Height() {
		return ErrHeightTooHigh
	}
	if height < cur.Finalized() {
		return ErrHeightTooLow
	}

	ns := cur.clone()
	ns.finalized = height
	d.current.Store(ns)

	return nil
}

// Reorg replaces the canonical chain above forkFrom with a new branch (one
// block per tx list). The branch must fork at or above the finalized height
// and outgrow the current head; orphaned blocks stay reachable by hash.
func (d *Driver) Reorg(forkFrom uint64, branch [][]Tx) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	cur := d.current.Load()

	if forkFrom < cur.Finalized() {
		return ErrReorgBelowFinalized
	}
	if forkFrom > cur.Height() {
		return ErrForkPointNotFound
	}
	if forkFrom+uint64(len(branch)) <= cur.Height() {
		return ErrReorgTooShort
	}

	parent := cur.ByNumber(forkFrom)
	if parent == nil {
		return ErrForkPointNotFound
	}

	newBlocks := make([]*Block, 0, len(branch))
	for _, txList := range branch {
		block := d.buildBlock(parent, txList)
		newBlocks = append(newBlocks, block)
		parent = block
	}

	ns := cur.clone()
	ns.canonical = ns.canonical[:forkFrom+1]
	for _, block := range newBlocks {
		ns.blocks[block.Hash] = block
		ns.canonical = append(ns.canonical, block.Hash)
	}

	ns.head = newBlocks[len(newBlocks)-1].Hash
	d.current.Store(ns)
	return nil
}

func (d *Driver) buildBlock(parent *Block, txs []Tx) *Block {
	d.seq++
	number := parent.Number + 1
	receipts := make([]Receipt, len(txs))
	for i, tx := range txs {
		receipts[i] = Receipt{TxHash: hashTx(tx), Status: 1}
	}

	return &Block{
		Number:     number,
		Hash:       hashBlock(d.seq, number, parent.Hash, txs),
		ParentHash: parent.Hash,
		Txs:        slices.Clone(txs),
		Receipts:   receipts,
	}
}
