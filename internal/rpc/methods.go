package rpc

import (
	"encoding/json"
	"strconv"

	"github.com/ChickenBenny/Gaslight/internal/chain"
)

func (h *Handler) ethBlockNumber(s *chain.ChainSnapshot, _ []json.RawMessage) (any, *RPCError) {
	return encodeUint64(s.Height()), nil
}

func (h *Handler) ethChainID(s *chain.ChainSnapshot, _ []json.RawMessage) (any, *RPCError) {
	return encodeUint64(h.chainID), nil
}

func (h *Handler) netVersion(s *chain.ChainSnapshot, _ []json.RawMessage) (any, *RPCError) {
	return strconv.FormatUint(h.chainID, 10), nil
}

func (h *Handler) ethGetBlockByNumber(s *chain.ChainSnapshot, params []json.RawMessage) (any, *RPCError) {
	if len(params) < 1 {
		return nil, errInvalidParams("missing block number")
	}
	var tag string
	if err := json.Unmarshal(params[0], &tag); err != nil {
		return nil, errInvalidParams("block numbermust be a string")
	}
	height, err := resolveHeight(s, tag)
	if err != nil {
		return nil, errInvalidParams("invalid block number")
	}
	blk := s.ByNumber(height)
	if blk == nil {
		return nil, nil // JSON null
	}
	return toRPCBlock(blk), nil
}

func (h *Handler) ethGetBlockByHash(s *chain.ChainSnapshot, params []json.RawMessage) (any, *RPCError) {
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
	blk := s.ByHash(hash)
	if blk == nil {
		return nil, nil // JSON null
	}
	return toRPCBlock(blk), nil
}

func (h *Handler) ethGetTransactionReceipt(s *chain.ChainSnapshot, params []json.RawMessage) (any, *RPCError) {
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
	r := s.ReceiptOf(txHash)
	if r == nil {
		return nil, nil // JSON null
	}
	blk := s.BlockByTx(txHash)
	if blk == nil {
		return nil, nil // JSON null
	}
	return toRPCReceipt(r, blk), nil
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
