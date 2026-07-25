package chain

import (
	"maps"
	"slices"
)

// ChainSnapshot is an immutable point-in-time view of the chain, safe for
// concurrent lock-free readers. The Driver publishes a fresh snapshot on
// every mutation and never touches published ones.
type ChainSnapshot struct {
	blocks    map[Hash]*Block
	canonical []Hash
	head      Hash
	finalized uint64
}

// Head returns the canonical head block.
func (s *ChainSnapshot) Head() *Block {
	return s.blocks[s.head]
}

// Height returns the head block number.
func (s *ChainSnapshot) Height() uint64 {
	head := s.Head()
	if head == nil {
		return 0
	}
	return head.Number
}

// ByNumber returns the canonical block at height n, or nil if n is above the head.
func (s *ChainSnapshot) ByNumber(n uint64) *Block {
	if n >= uint64(len(s.canonical)) {
		return nil
	}
	return s.blocks[s.canonical[n]]
}

// ByHash returns the block with hash h — canonical or orphaned — or nil if unknown.
func (s *ChainSnapshot) ByHash(h Hash) *Block {
	return s.blocks[h]
}

// ReceiptOf returns the receipt of txHash if it is included in the canonical
// chain, or nil otherwise (unknown or orphaned tx).
func (s *ChainSnapshot) ReceiptOf(txHash Hash) *Receipt {
	height := s.Head().Number
	for h := 0; h <= int(height); h++ {
		blk := s.blocks[s.canonical[h]]
		for i := range blk.Receipts {
			if blk.Receipts[i].TxHash == txHash {
				r := blk.Receipts[i]
				return &r
			}
		}
	}
	return nil
}

// Finalized returns the finalized height; blocks at or below it can no longer
// be reorged away.
func (s *ChainSnapshot) Finalized() uint64 {
	return s.finalized
}

// clone returns a copy-on-write duplicate: fresh containers, shared (immutable) blocks.
func (s *ChainSnapshot) clone() *ChainSnapshot {
	return &ChainSnapshot{
		blocks:    maps.Clone(s.blocks),
		canonical: slices.Clone(s.canonical),
		head:      s.head,
		finalized: s.finalized,
	}
}
