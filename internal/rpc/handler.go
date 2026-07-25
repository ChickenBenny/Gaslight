package rpc

import (
	"bytes"
	"encoding/json"

	"github.com/ChickenBenny/Gaslight/internal/chain"
)

type SnapshotSource interface {
	Snapshot() *chain.ChainSnapshot
}

type methodFunc func(s *chain.ChainSnapshot, params []json.RawMessage) (any, *RPCError)

type Handler struct {
	src     SnapshotSource
	chainID uint64
	methods map[string]methodFunc
}

type RPCRequest struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      json.RawMessage   `json:"id"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

func New(src SnapshotSource, chainID uint64) *Handler {
	h := &Handler{src: src, chainID: chainID}
	h.methods = map[string]methodFunc{
		"eth_blockNumber":      h.ethBlockNumber,
		"eth_chainId":          h.ethChainID,
		"net_version":          h.netVersion,
		"eth_getBlockByNumber": h.ethGetBlockByNumber,
	}
	return h
}

func (h *Handler) ServeRPC(raw []byte) []byte {
	snapshot := h.src.Snapshot()
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return mustMarshal(RPCResponse{JSONRPC: "2.0", ID: nil, Error: errParse()})
	}
	if trimmed[0] == '[' {
		return mustMarshal(h.serveBatch(snapshot, trimmed))
	}
	return mustMarshal(h.serveSingle(snapshot, trimmed))
}

func (h *Handler) serveBatch(snapshot *chain.ChainSnapshot, raw []byte) []RPCResponse {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return []RPCResponse{{JSONRPC: "2.0", ID: nil, Error: errInvalidRequest()}}
	}
	resps := make([]RPCResponse, len(items))
	for i, item := range items {
		resps[i] = h.serveSingle(snapshot, item)
	}
	return resps
}

func (h *Handler) serveSingle(snapshot *chain.ChainSnapshot, raw []byte) RPCResponse {
	var req RPCRequest
	if err := json.Unmarshal(raw, &req); err != nil || req.Method == "" {
		return RPCResponse{JSONRPC: "2.0", ID: req.ID, Error: errInvalidRequest()}
	}
	fn, ok := h.methods[req.Method]
	if !ok {
		return RPCResponse{JSONRPC: "2.0", ID: req.ID, Error: errMethodNotFound()}
	}
	result, rpcErr := fn(snapshot, req.Params)
	if rpcErr != nil {
		return RPCResponse{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
	}
	encodedResult, err := json.Marshal(result)
	if err != nil {
		return RPCResponse{JSONRPC: "2.0", ID: req.ID, Error: errInternal()}
	}
	return RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: encodedResult}
}

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
