package chain

import "math/big"

// Hash is a 32-byte block or transaction hash.
type Hash [32]byte

// Address is a 20-byte account address.
type Address [20]byte

// Tx is a minimal value transfer.
type Tx struct {
	ID    string
	From  Address
	To    Address
	Value *big.Int
}

// Log is an event emitted while executing a transaction.
type Log struct {
	Address  Address
	Topics   []Hash
	Data     []byte
	LogIndex uint64
	TxHash   Hash
}

// Receipt records the outcome of an executed transaction.
type Receipt struct {
	TxHash  Hash
	Status  uint64
	GasUsed uint64
	Logs    []Log
}

// Block is a node in the block tree. Once stored it must never be mutated:
// snapshots share *Block pointers copy-on-write.
type Block struct {
	Number     uint64
	Hash       Hash
	ParentHash Hash
	Timestamp  uint64
	Txs        []Tx
	Receipts   []Receipt
}
