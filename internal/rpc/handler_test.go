package rpc

import (
	"encoding/json"
	"testing"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testResp mirrors a JSON-RPC 2.0 response envelope for assertions.
type testResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newHandler(chainID uint64) (*Handler, *chain.Driver) {
	d := chain.NewDriver(chainID)
	return New(d, chainID), d // *chain.Driver satisfies SnapshotSource
}

func decodeResp(t *testing.T, raw []byte) testResp {
	t.Helper()
	var r testResp
	require.NoError(t, json.Unmarshal(raw, &r))
	assert.Equal(t, "2.0", r.JSONRPC)
	return r
}

func TestServeEthBlockNumber(t *testing.T) {
	h, d := newHandler(1)
	for i := 0; i < 4; i++ {
		d.ProduceBlock(nil)
	}

	out := h.ServeRPC([]byte(`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}`))
	r := decodeResp(t, out)

	require.Nil(t, r.Error)
	assert.Equal(t, `"0x4"`, string(r.Result)) // height 4, quantity-encoded
	assert.Equal(t, "1", string(r.ID))         // id echoed verbatim
}

func TestServeEthChainId(t *testing.T) {
	h, _ := newHandler(5)

	out := h.ServeRPC([]byte(`{"jsonrpc":"2.0","id":1,"method":"eth_chainId"}`))
	r := decodeResp(t, out)

	require.Nil(t, r.Error)
	assert.Equal(t, `"0x5"`, string(r.Result)) // hex
}

func TestServeNetVersion(t *testing.T) {
	h, _ := newHandler(5)

	out := h.ServeRPC([]byte(`{"jsonrpc":"2.0","id":1,"method":"net_version"}`))
	r := decodeResp(t, out)

	require.Nil(t, r.Error)
	assert.Equal(t, `"5"`, string(r.Result)) // net_version is a DECIMAL string, not hex
}

func TestServeIDEchoedForStringID(t *testing.T) {
	h, _ := newHandler(1)

	out := h.ServeRPC([]byte(`{"jsonrpc":"2.0","id":"abc","method":"eth_chainId"}`))
	r := decodeResp(t, out)

	assert.Equal(t, `"abc"`, string(r.ID))
}

func TestServeUnknownMethod(t *testing.T) {
	h, _ := newHandler(1)

	out := h.ServeRPC([]byte(`{"jsonrpc":"2.0","id":1,"method":"eth_doesNotExist"}`))
	r := decodeResp(t, out)

	require.NotNil(t, r.Error)
	assert.Equal(t, -32601, r.Error.Code) // method not found
}

func TestServeParseError(t *testing.T) {
	h, _ := newHandler(1)

	out := h.ServeRPC([]byte(`{not valid json`))
	r := decodeResp(t, out)

	require.NotNil(t, r.Error)
	assert.Equal(t, -32700, r.Error.Code) // parse error
}

func TestServeBatch(t *testing.T) {
	h, d := newHandler(1)
	d.ProduceBlock(nil)
	d.ProduceBlock(nil) // height 2

	out := h.ServeRPC([]byte(`[
		{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"},
		{"jsonrpc":"2.0","id":2,"method":"eth_chainId"}
	]`))

	var arr []testResp
	require.NoError(t, json.Unmarshal(out, &arr))
	require.Len(t, arr, 2)

	byID := map[string]string{}
	for _, r := range arr {
		require.Nil(t, r.Error)
		assert.Equal(t, "2.0", r.JSONRPC)
		byID[string(r.ID)] = string(r.Result)
	}
	assert.Equal(t, `"0x2"`, byID["1"]) // eth_blockNumber
	assert.Equal(t, `"0x1"`, byID["2"]) // eth_chainId
}
