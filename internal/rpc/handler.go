package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"

	"github.com/ChickenBenny/Gaslight/internal/chain"
)

type SnapshotSource interface {
	Snapshot() *chain.ChainSnapshot
}

// snapshotGetter defers taking the chain snapshot until a method actually
// needs it, so a delay fault sleeps first and then answers from a fresh view
// rather than from the state at request arrival. The snapshot is still taken
// at most once per message, keeping a batch internally consistent.
type snapshotGetter interface {
	snapshot() *chain.ChainSnapshot
}

type lazySnapshot struct {
	src  SnapshotSource
	once sync.Once
	snap *chain.ChainSnapshot
}

func (l *lazySnapshot) snapshot() *chain.ChainSnapshot {
	l.once.Do(func() { l.snap = l.src.Snapshot() })
	return l.snap
}

type methodFunc func(ctx context.Context, s snapshotGetter, params []json.RawMessage) (any, *RPCError)

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

func New(src SnapshotSource, chainID uint64, fs FaultSource) *Handler {
	h := &Handler{src: src, chainID: chainID}
	rawMethodMap := map[string]methodFunc{
		"eth_blockNumber":           h.ethBlockNumber,
		"eth_chainId":               h.ethChainID,
		"net_version":               h.netVersion,
		"eth_getBlockByNumber":      h.ethGetBlockByNumber,
		"eth_getBlockByHash":        h.ethGetBlockByHash,
		"eth_getTransactionReceipt": h.ethGetTransactionReceipt,
	}
	h.methods = make(map[string]methodFunc, len(rawMethodMap))
	for name, fn := range rawMethodMap {
		h.methods[name] = withFaults(fs, name, fn)
	}
	return h
}

func (h *Handler) ServeRPC(ctx context.Context, raw []byte) []byte {
	snapshot := &lazySnapshot{src: h.src}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return mustMarshal(RPCResponse{JSONRPC: "2.0", ID: nil, Error: errParse()})
	}
	if trimmed[0] == '[' {
		return mustMarshal(h.serveBatch(ctx, snapshot, trimmed))
	}
	return mustMarshal(h.serveSingle(ctx, snapshot, trimmed))
}

func (h *Handler) serveBatch(ctx context.Context, snapshot snapshotGetter, raw []byte) []RPCResponse {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return []RPCResponse{{JSONRPC: "2.0", ID: nil, Error: errInvalidRequest()}}
	}
	if len(items) == 0 {
		// JSON-RPC 2.0: an empty batch is itself an invalid request.
		return []RPCResponse{{JSONRPC: "2.0", ID: nil, Error: errInvalidRequest()}}
	}
	resps := make([]RPCResponse, len(items))
	for i, item := range items {
		resps[i] = h.serveSingle(ctx, snapshot, item)
	}
	return resps
}

func (h *Handler) serveSingle(ctx context.Context, snapshot snapshotGetter, raw []byte) RPCResponse {
	var req RPCRequest
	if err := json.Unmarshal(raw, &req); err != nil || req.Method == "" {
		return RPCResponse{JSONRPC: "2.0", ID: req.ID, Error: errInvalidRequest()}
	}
	fn, ok := h.methods[req.Method]
	if !ok {
		return RPCResponse{JSONRPC: "2.0", ID: req.ID, Error: errMethodNotFound()}
	}
	result, rpcErr := fn(ctx, snapshot, req.Params)
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
