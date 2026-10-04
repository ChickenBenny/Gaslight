package rpc

import (
	"encoding/hex"

	"github.com/ChickenBenny/Gaslight/internal/chain"
)

type rpcBlock struct {
	Number       string `json:"number"`
	Hash         string `json:"hash"`
	ParentHash   string `json:"parentHash"`
	Timestamp    string `json:"timestamp"`
	Transactions []any  `json:"transactions"`
}

type rpcReceipt struct {
	TransactionHash string   `json:"transactionHash"`
	Status          string   `json:"status"`
	GasUsed         string   `json:"gasUsed"`
	BlockHash       string   `json:"blockHash"`
	BlockNumber     string   `json:"blockNumber"`
	Logs            []rpcLog `json:"logs"`
}

type rpcTx struct {
	Hash             string `json:"hash"`
	From             string `json:"from"`
	To               string `json:"to"`
	Value            string `json:"value"`
	Input            string `json:"input"`
	BlockHash        string `json:"blockHash"`
	BlockNumber      string `json:"blockNumber"`
	TransactionIndex string `json:"transactionIndex"`
	Nonce            string `json:"nonce"`
	Gas              string `json:"gas"`
	GasPrice         string `json:"gasPrice"`
	Type             string `json:"type"`
	V                string `json:"v"`
	R                string `json:"r"`
	S                string `json:"s"`
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

func toRPCBlock(b *chain.Block, fullTx bool) rpcBlock {
	txs := make([]any, len(b.Txs))
	for i, tx := range b.Txs {
		if fullTx {
			txs[i] = toRPCTx(tx, b, uint64(i))
		} else {
			txs[i] = encodeHash(tx.Hash)
		}
	}
	return rpcBlock{
		Number:       encodeUint64(b.Number),
		Hash:         encodeHash(b.Hash),
		ParentHash:   encodeHash(b.ParentHash),
		Timestamp:    encodeUint64(b.Timestamp),
		Transactions: txs,
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

func toRPCTx(tx chain.Tx, blk *chain.Block, index uint64) rpcTx {
	return rpcTx{
		Hash:             encodeHash(tx.Hash),
		From:             encodeAddress(tx.From),
		To:               encodeAddress(tx.To),
		Value:            encodeBigInt(tx.Value),
		Input:            "0x",
		BlockHash:        encodeHash(blk.Hash),
		BlockNumber:      encodeUint64(blk.Number),
		TransactionIndex: encodeUint64(index),

		// Gaslight models none of what follows, but a transaction without
		// these cannot be decoded at all: go-ethereum's UnmarshalJSON rejects
		// a missing nonce, gas, gasPrice or signature, and ethers throws on an
		// absent nonce or gasLimit. A fixed value a client should not rely on
		// is the lesser harm against a node no client can read.
		//
		// The gas figure is the one true value here: 21000 is what a plain
		// transfer costs. An all-zero signature is what geth reads as
		// "unsigned", which skips its signature sanity check rather than
		// failing it.
		Nonce:    "0x0",
		Gas:      "0x5208",
		GasPrice: "0x0",
		Type:     "0x0",
		V:        "0x0",
		R:        "0x0",
		S:        "0x0",
	}
}
