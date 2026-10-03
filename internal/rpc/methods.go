package rpc

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/ChickenBenny/Gaslight/internal/chain"
)

func (h *Handler) ethBlockNumber(_ context.Context, g snapshotGetter, _ []json.RawMessage) (any, *RPCError) {
	s := g.snapshot()
	return encodeUint64(s.Height()), nil
}

func (h *Handler) ethChainID(_ context.Context, _ snapshotGetter, _ []json.RawMessage) (any, *RPCError) {
	return encodeUint64(h.chainID), nil
}

func (h *Handler) netVersion(_ context.Context, _ snapshotGetter, _ []json.RawMessage) (any, *RPCError) {
	return strconv.FormatUint(h.chainID, 10), nil
}

func (h *Handler) ethGetBlockByNumber(_ context.Context, g snapshotGetter, params []json.RawMessage) (any, *RPCError) {
	if len(params) < 1 {
		return nil, errInvalidParams("missing block number")
	}
	var tag string
	if err := json.Unmarshal(params[0], &tag); err != nil {
		return nil, errInvalidParams("block number must be a string")
	}
	s := g.snapshot()
	height, err := resolveHeight(s, tag)
	if err != nil {
		return nil, errInvalidParams("invalid block number")
	}
	return blockResult(s.ByNumber(height), params)
}

func (h *Handler) ethGetBlockByHash(_ context.Context, g snapshotGetter, params []json.RawMessage) (any, *RPCError) {
	if len(params) < 1 {
		return nil, errInvalidParams("missing block hash")
	}
	var hashStr string
	if err := json.Unmarshal(params[0], &hashStr); err != nil {
		return nil, errInvalidParams("block hash must be a string")
	}
	hash, err := decodeHash(hashStr)
	if err != nil {
		return nil, errInvalidParams("invalid block hash")
	}
	return blockResult(g.snapshot().ByHash(hash), params)
}

func blockResult(blk *chain.Block, params []json.RawMessage) (any, *RPCError) {
	fullTx, rpcErr := parseFullTx(params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	if blk == nil {
		return nil, nil
	}
	return toRPCBlock(blk, fullTx), nil
}

func parseFullTx(params []json.RawMessage) (bool, *RPCError) {
	if len(params) < 2 {
		return false, errInvalidParams("missing fullTx")
	}
	var full bool
	if err := json.Unmarshal(params[1], &full); err != nil {
		return false, errInvalidParams("fullTx must be a boolean")
	}
	return full, nil
}

func (h *Handler) ethGetTransactionReceipt(_ context.Context, g snapshotGetter, params []json.RawMessage) (any, *RPCError) {
	if len(params) < 1 {
		return nil, errInvalidParams("missing transaction hash")
	}
	var txHashStr string
	if err := json.Unmarshal(params[0], &txHashStr); err != nil {
		return nil, errInvalidParams("transaction hash must be a string")
	}
	txHash, err := decodeHash(txHashStr)
	if err != nil {
		return nil, errInvalidParams("invalid transaction hash")
	}
	blk := g.snapshot().BlockByTx(txHash)
	if blk == nil {
		return nil, nil // JSON null: unknown or orphaned tx
	}
	for i := range blk.Receipts {
		if blk.Receipts[i].TxHash == txHash {
			return toRPCReceipt(&blk.Receipts[i], blk), nil
		}
	}
	return nil, errInternal() // block found but receipt missing: invariant broken
}

func (h *Handler) ethGetTransactionByHash(_ context.Context, g snapshotGetter, params []json.RawMessage) (any, *RPCError) {
	if len(params) < 1 {
		return nil, errInvalidParams("missing transaction hash")
	}
	var txHashStr string
	if err := json.Unmarshal(params[0], &txHashStr); err != nil {
		return nil, errInvalidParams("transaction hash must be a string")
	}
	txHash, err := decodeHash(txHashStr)
	if err != nil {
		return nil, errInvalidParams("invalid transaction hash")
	}
	blk := g.snapshot().BlockByTx(txHash)
	if blk == nil {
		return nil, nil
	}
	for i := range blk.Txs {
		if blk.Txs[i].Hash == txHash {
			return toRPCTx(blk.Txs[i], blk, uint64(i)), nil
		}
	}
	return nil, errInternal()
}

func resolveHeight(s *chain.ChainSnapshot, tag string) (uint64, error) {
	switch tag {
	case "latest", "pending":
		return s.Height(), nil
	case "earliest":
		return 0, nil
	case "finalized", "safe":
		return s.Finalized(), nil
	default:
		return decodeUint64(tag)
	}
}
