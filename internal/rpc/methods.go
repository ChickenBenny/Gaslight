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
