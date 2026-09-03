package faults_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/ChickenBenny/Gaslight/internal/faults"
	"github.com/ChickenBenny/Gaslight/internal/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type resp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// newStack wires a chain, a fault registry and the rpc handler the way cmd does.
func newStack(t *testing.T) (*rpc.Handler, *chain.Driver, *faults.Registry) {
	t.Helper()
	d := chain.NewDriver(1)
	r := faults.NewRegistry()
	return rpc.New(d, 1, r), d, r
}

func call(t *testing.T, h *rpc.Handler, body string) resp {
	t.Helper()
	var out resp
	require.NoError(t, json.Unmarshal(h.ServeRPC([]byte(body)), &out))
	assert.Equal(t, "2.0", out.JSONRPC)
	return out
}

// depositTx produces a block holding one tx and returns its hash, hex encoded.
func depositTx(t *testing.T, d *chain.Driver) string {
	t.Helper()
	blk := d.ProduceBlock([]chain.Tx{{ID: "dep"}})
	require.Len(t, blk.Txs, 1)
	return "0x" + hexOf(blk.Txs[0].Hash)
}

func hexOf(h chain.Hash) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, 64)
	for _, b := range h {
		out = append(out, digits[b>>4], digits[b&0x0f])
	}
	return string(out)
}

// The flagship lie: a receipt that exists on chain is served as JSON null while
// the transport still reports success.
func TestFalseNullOverServeRPC(t *testing.T) {
	h, d, reg := newStack(t)
	tx := depositTx(t, d)
	req := `{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionReceipt","params":["` + tx + `"]}`

	truthful := call(t, h, req)
	require.Nil(t, truthful.Error)
	require.NotEqual(t, "null", string(truthful.Result), "the receipt must exist before we lie about it")

	reg.Enable(faults.NewFault("eth_getTransactionReceipt", faults.FalseNull, 0, 0))

	lied := call(t, h, req)
	assert.Nil(t, lied.Error, "a false 200 carries no error — that is what makes it dangerous")
	assert.Equal(t, "null", string(lied.Result))
	assert.Equal(t, "1", string(lied.ID))
}

// count=1 models an intermittently bad node: the first call lies, a retry gets
// the truth. This is what a deposit engine's retry path has to survive.
func TestFaultExpiresSoRetriesSucceed(t *testing.T) {
	h, d, reg := newStack(t)
	tx := depositTx(t, d)
	req := `{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionReceipt","params":["` + tx + `"]}`

	reg.Enable(faults.NewFault("eth_getTransactionReceipt", faults.FalseNull, 1, 0))

	first := call(t, h, req)
	require.Equal(t, "null", string(first.Result), "first call should be the lie")

	second := call(t, h, req)
	assert.NotEqual(t, "null", string(second.Result), "the retry should see the truth")
}

// Faults are per method: lying about receipts must not disturb head tracking.
func TestOtherMethodsAreUnaffected(t *testing.T) {
	h, d, reg := newStack(t)
	for range 3 {
		d.ProduceBlock(nil)
	}
	reg.Enable(faults.NewFault("eth_getTransactionReceipt", faults.FalseNull, 0, 0))

	out := call(t, h, `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`)
	require.Nil(t, out.Error)
	assert.Equal(t, `"0x3"`, string(out.Result))
}

// Every entry of a batch runs through the same decorated method, so a fault
// applies inside batches without the transport doing anything special.
func TestFaultAppliesInsideBatch(t *testing.T) {
	h, d, reg := newStack(t)
	tx := depositTx(t, d)
	reg.Enable(faults.NewFault("eth_getTransactionReceipt", faults.FalseNull, 0, 0))

	body := `[{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionReceipt","params":["` + tx + `"]},
	          {"jsonrpc":"2.0","id":2,"method":"eth_blockNumber"}]`

	var out []resp
	require.NoError(t, json.Unmarshal(h.ServeRPC([]byte(body)), &out))
	require.Len(t, out, 2)

	byID := map[string]string{}
	for _, r := range out {
		require.Nil(t, r.Error)
		byID[string(r.ID)] = string(r.Result)
	}
	assert.Equal(t, "null", byID["1"], "the receipt call is lied about")
	assert.Equal(t, `"0x1"`, byID["2"], "the other call in the batch is not")
}

// A wildcard fault covers whatever the client asks for.
func TestWildcardOverServeRPC(t *testing.T) {
	h, d, reg := newStack(t)
	d.ProduceBlock(nil)
	reg.Enable(faults.NewFault(faults.AllMethods, faults.FalseNull, 0, 0))

	out := call(t, h, `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`)
	require.Nil(t, out.Error)
	assert.Equal(t, "null", string(out.Result))
}

// delay slows a call down without changing its answer.
func TestDelayOverServeRPC(t *testing.T) {
	h, d, reg := newStack(t)
	d.ProduceBlock(nil)
	reg.Enable(faults.NewFault("eth_blockNumber", faults.Delay, 0, 60*time.Millisecond))

	start := time.Now()
	out := call(t, h, `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`)
	elapsed := time.Since(start)

	require.Nil(t, out.Error)
	assert.Equal(t, `"0x1"`, string(out.Result), "delay must not change the answer")
	assert.GreaterOrEqual(t, elapsed, 60*time.Millisecond)
}

// A handler built without a registry must behave exactly as before.
func TestNilRegistryIsAPassThrough(t *testing.T) {
	d := chain.NewDriver(1)
	h := rpc.New(d, 1, nil)
	d.ProduceBlock(nil)

	out := call(t, h, `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`)
	require.Nil(t, out.Error)
	assert.Equal(t, `"0x1"`, string(out.Result))
}

// Clearing the registry restores honest answers.
func TestClearRestoresTruthOverServeRPC(t *testing.T) {
	h, d, reg := newStack(t)
	tx := depositTx(t, d)
	req := `{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionReceipt","params":["` + tx + `"]}`

	reg.Enable(faults.NewFault("eth_getTransactionReceipt", faults.FalseNull, 0, 0))
	require.Equal(t, "null", string(call(t, h, req).Result))

	reg.Clear()
	assert.NotEqual(t, "null", string(call(t, h, req).Result))
}
