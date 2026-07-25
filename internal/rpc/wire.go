package rpc

import "github.com/ChickenBenny/Gaslight/internal/chain"

type rpcBlock struct {
	Number       string   `json:"number"`
	Hash         string   `json:"hash"`
	ParentHash   string   `json:"parentHash"`
	Timestamp    string   `json:"timestamp"`
	Transactions []string `json:"transactions"`
}

func toRPCBlock(b *chain.Block) rpcBlock {
	return rpcBlock{
		Number:     encodeUint64(b.Number),
		Hash:       encodeHash(b.Hash),
		ParentHash: encodeHash(b.ParentHash),
		Timestamp:  encodeUint64(b.Timestamp),
		Transactions: func() []string {
			txHashes := make([]string, len(b.Txs))
			for i, tx := range b.Txs {
				txHashes[i] = encodeHash(tx.Hash)
			}
			return txHashes
		}(),
	}
}
