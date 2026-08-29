package rpc

import (
	"encoding/hex"

	"github.com/ChickenBenny/Gaslight/internal/chain"
)

type rpcBlock struct {
	Number       string   `json:"number"`
	Hash         string   `json:"hash"`
	ParentHash   string   `json:"parentHash"`
	Timestamp    string   `json:"timestamp"`
	Transactions []string `json:"transactions"`
}

type rpcReceipt struct {
	TransactionHash string   `json:"transactionHash"`
	Status          string   `json:"status"`
	GasUsed         string   `json:"gasUsed"`
	BlockHash       string   `json:"blockHash"`
	BlockNumber     string   `json:"blockNumber"`
	Logs            []rpcLog `json:"logs"`
}

type rpcLog struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}

type rpcHeader struct {
	Number     string `json:"number"`
	Hash       string `json:"hash"`
	ParentHash string `json:"parentHash"`
	Timestamp  string `json:"timestamp"`
}

func NewHeadResult(b *chain.Block) any {
	return rpcHeader{
		Number:     encodeUint64(b.Number),
		Hash:       encodeHash(b.Hash),
		ParentHash: encodeHash(b.ParentHash),
		Timestamp:  encodeUint64(b.Timestamp),
	}
}

func toRPCBlock(b *chain.Block) rpcBlock {
	txHashes := make([]string, len(b.Txs))
	for i, tx := range b.Txs {
		txHashes[i] = encodeHash(tx.Hash)
	}
	return rpcBlock{
		Number:       encodeUint64(b.Number),
		Hash:         encodeHash(b.Hash),
		ParentHash:   encodeHash(b.ParentHash),
		Timestamp:    encodeUint64(b.Timestamp),
		Transactions: txHashes,
	}
}

func toRPCReceipt(r *chain.Receipt, blk *chain.Block) rpcReceipt {
	logs := make([]rpcLog, 0, len(r.Logs)) // non-nil -> JSON "[]", never null
	for _, l := range r.Logs {
		topics := make([]string, len(l.Topics))
		for i, t := range l.Topics {
			topics[i] = encodeHash(t)
		}
		logs = append(logs, rpcLog{
			Address: encodeAddress(l.Address),
			Topics:  topics,
			Data:    "0x" + hex.EncodeToString(l.Data),
		})
	}
	return rpcReceipt{
		TransactionHash: encodeHash(r.TxHash),
		Status:          encodeUint64(r.Status),
		GasUsed:         encodeUint64(r.GasUsed),
		BlockHash:       encodeHash(blk.Hash),
		BlockNumber:     encodeUint64(blk.Number),
		Logs:            logs,
	}
}
