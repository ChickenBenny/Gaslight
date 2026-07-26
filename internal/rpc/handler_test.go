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

// --- eth_getBlockByNumber ---

type wireBlock struct {
	Number       string   `json:"number"`
	Hash         string   `json:"hash"`
	ParentHash   string   `json:"parentHash"`
	Timestamp    string   `json:"timestamp"`
	Transactions []string `json:"transactions"`
}

// getBlock calls eth_getBlockByNumber(param, false) and returns the response.
func getBlock(t *testing.T, h *Handler, param string) testResp {
	t.Helper()
	req := `{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":[` + param + `,false]}`
	return decodeResp(t, h.ServeRPC([]byte(req)))
}

func TestServeGetBlockByNumberLatest(t *testing.T) {
	h, d := newHandler(1)
	for i := 0; i < 3; i++ {
		d.ProduceBlock(nil)
	}
	r := getBlock(t, h, `"latest"`)
	require.Nil(t, r.Error)

	var b wireBlock
	require.NoError(t, json.Unmarshal(r.Result, &b))
	assert.Equal(t, "0x3", b.Number)
}

func TestServeGetBlockByNumberByHex(t *testing.T) {
	h, d := newHandler(1)
	for i := 0; i < 5; i++ {
		d.ProduceBlock(nil)
	}
	r := getBlock(t, h, `"0x2"`)
	require.Nil(t, r.Error)

	var b wireBlock
	require.NoError(t, json.Unmarshal(r.Result, &b))
	assert.Equal(t, "0x2", b.Number)
}

func TestServeGetBlockByNumberFinalized(t *testing.T) {
	h, d := newHandler(1)
	for i := 0; i < 6; i++ {
		d.ProduceBlock(nil)
	}
	require.NoError(t, d.Finalize(4))

	r := getBlock(t, h, `"finalized"`)
	require.Nil(t, r.Error)

	var b wireBlock
	require.NoError(t, json.Unmarshal(r.Result, &b))
	assert.Equal(t, "0x4", b.Number)
}

// A height above the head returns a null result, not an error.
func TestServeGetBlockByNumberNotFound(t *testing.T) {
	h, _ := newHandler(1) // genesis only, height 0
	r := getBlock(t, h, `"0x63"`)
	require.Nil(t, r.Error)
	assert.Equal(t, "null", string(r.Result))
}

func TestServeGetBlockByNumberBadTag(t *testing.T) {
	h, _ := newHandler(1)
	r := getBlock(t, h, `"banana"`)
	require.NotNil(t, r.Error)
	assert.Equal(t, -32602, r.Error.Code)
}

func TestServeGetBlockByNumberIncludesTxHashes(t *testing.T) {
	h, d := newHandler(1)
	blk := d.ProduceBlock([]chain.Tx{{ID: "a"}}) // height 1, one tx

	r := getBlock(t, h, `"0x1"`)
	require.Nil(t, r.Error)

	var b wireBlock
	require.NoError(t, json.Unmarshal(r.Result, &b))
	require.Len(t, b.Transactions, 1)
	assert.Equal(t, encodeHash(blk.Txs[0].Hash), b.Transactions[0])
}

// --- eth_getBlockByHash ---

func getBlockByHash(t *testing.T, h *Handler, hashHex string) testResp {
	t.Helper()
	req := `{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByHash","params":["` + hashHex + `",false]}`
	return decodeResp(t, h.ServeRPC([]byte(req)))
}

func TestServeGetBlockByHashFound(t *testing.T) {
	h, d := newHandler(1)
	for i := 0; i < 3; i++ {
		d.ProduceBlock(nil)
	}
	want := d.Snapshot().ByNumber(2)

	r := getBlockByHash(t, h, encodeHash(want.Hash))
	require.Nil(t, r.Error)

	var b wireBlock
	require.NoError(t, json.Unmarshal(r.Result, &b))
	assert.Equal(t, "0x2", b.Number)
	assert.Equal(t, encodeHash(want.Hash), b.Hash)
}

// ByHash still retrieves a block after it has been orphaned by a reorg — this
// is how a reorg-aware client detects that a block it recorded is no longer
// canonical (getBlockByNumber at that height now returns a different block).
func TestServeGetBlockByHashReturnsOrphan(t *testing.T) {
	h, d := newHandler(1)
	for i := 0; i < 3; i++ {
		d.ProduceBlock(nil)
	}
	orphanHash := d.Snapshot().ByNumber(3).Hash

	// Fork from height 1 with 4 blocks -> new head 5, orphaning old 2 and 3.
	require.NoError(t, d.Reorg(1, make([][]chain.Tx, 4)))

	r := getBlockByHash(t, h, encodeHash(orphanHash))
	require.Nil(t, r.Error)

	var b wireBlock
	require.NoError(t, json.Unmarshal(r.Result, &b))
	assert.Equal(t, encodeHash(orphanHash), b.Hash)
	assert.Equal(t, "0x3", b.Number) // still reports its original height
}

func TestServeGetBlockByHashNotFound(t *testing.T) {
	h, _ := newHandler(1)
	unknown := encodeHash(chain.Hash{0x99}) // valid format, never produced
	r := getBlockByHash(t, h, unknown)
	require.Nil(t, r.Error)
	assert.Equal(t, "null", string(r.Result))
}

func TestServeGetBlockByHashBadHash(t *testing.T) {
	h, _ := newHandler(1)
	r := getBlockByHash(t, h, "0x1234") // too short
	require.NotNil(t, r.Error)
	assert.Equal(t, -32602, r.Error.Code)
}

// --- eth_getTransactionReceipt ---

type wireReceipt struct {
	TransactionHash string            `json:"transactionHash"`
	Status          string            `json:"status"`
	GasUsed         string            `json:"gasUsed"`
	BlockHash       string            `json:"blockHash"`
	BlockNumber     string            `json:"blockNumber"`
	Logs            []json.RawMessage `json:"logs"`
}

func getReceipt(t *testing.T, h *Handler, txHashHex string) testResp {
	t.Helper()
	req := `{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionReceipt","params":["` + txHashHex + `"]}`
	return decodeResp(t, h.ServeRPC([]byte(req)))
}

func TestServeGetTransactionReceiptFound(t *testing.T) {
	h, d := newHandler(1)
	for i := 0; i < 2; i++ {
		d.ProduceBlock(nil)
	}
	blk := d.ProduceBlock([]chain.Tx{{ID: "a"}}) // height 3, one tx
	txHash := blk.Txs[0].Hash

	r := getReceipt(t, h, encodeHash(txHash))
	require.Nil(t, r.Error)

	var rc wireReceipt
	require.NoError(t, json.Unmarshal(r.Result, &rc))
	assert.Equal(t, encodeHash(txHash), rc.TransactionHash)
	assert.Equal(t, "0x1", rc.Status)
	assert.Equal(t, encodeUint64(blk.Number), rc.BlockNumber) // "0x3"
	assert.Equal(t, encodeHash(blk.Hash), rc.BlockHash)
	assert.NotNil(t, rc.Logs, "logs must be [] (empty array), not null")
}

func TestServeGetTransactionReceiptNotFound(t *testing.T) {
	h, _ := newHandler(1)
	r := getReceipt(t, h, encodeHash(chain.Hash{0x99})) // never produced
	require.Nil(t, r.Error)
	assert.Equal(t, "null", string(r.Result))
}

func TestServeGetTransactionReceiptBadHash(t *testing.T) {
	h, _ := newHandler(1)
	r := getReceipt(t, h, "0x1234") // too short
	require.NotNil(t, r.Error)
	assert.Equal(t, -32602, r.Error.Code)
}

// A tx orphaned by a reorg has no canonical receipt, so the result is null.
func TestServeGetTransactionReceiptOrphaned(t *testing.T) {
	h, d := newHandler(1)
	blk := d.ProduceBlock([]chain.Tx{{ID: "a"}}) // height 1
	txHash := blk.Txs[0].Hash

	require.NoError(t, d.Reorg(0, make([][]chain.Tx, 2))) // new head 2 > 1, orphans height 1

	r := getReceipt(t, h, encodeHash(txHash))
	require.Nil(t, r.Error)
	assert.Equal(t, "null", string(r.Result))
}
