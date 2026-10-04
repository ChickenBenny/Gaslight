package rpc

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wireTx is a transaction as a client reads it off the wire.
type wireTx struct {
	Hash             string `json:"hash"`
	From             string `json:"from"`
	To               string `json:"to"`
	Value            string `json:"value"`
	Input            string `json:"input"`
	BlockHash        string `json:"blockHash"`
	BlockNumber      string `json:"blockNumber"`
	TransactionIndex string `json:"transactionIndex"`
}

func addr(b byte) chain.Address {
	var a chain.Address
	for i := range a {
		a[i] = b
	}
	return a
}

func wei(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("bad wei " + s)
	}
	return v
}

var (
	alice    = addr(0xa1)
	exchange = addr(0xe0)
	oneETH   = wei("1000000000000000000")
)

// depositBlock produces a block carrying one transfer to the exchange, which
// is the shape a deposit watcher has to be able to recognise.
func depositBlock(t *testing.T, d *chain.Driver) *chain.Block {
	t.Helper()
	return d.ProduceBlock([]chain.Tx{
		{ID: "alice-deposit", From: alice, To: exchange, Value: oneETH},
	})
}

func call(t *testing.T, h *Handler, method string, params string) testResp {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":` + params + `}`
	return decodeResp(t, h.ServeRPC(context.Background(), []byte(body)))
}

// With fullTx false a block lists transaction hashes, which is what every
// existing client and recording expects.
func TestBlockListsHashesWhenFullTxIsFalse(t *testing.T) {
	h, d := newHandler(1)
	blk := depositBlock(t, d)

	r := call(t, h, "eth_getBlockByNumber", `["0x1",false]`)
	require.Nil(t, r.Error)

	var b struct {
		Transactions []string `json:"transactions"`
	}
	require.NoError(t, json.Unmarshal(r.Result, &b))
	assert.Equal(t, []string{encodeHash(blk.Txs[0].Hash)}, b.Transactions)
}

// The execution-apis spec marks the second parameter required, and geth
// answers a call without it with "missing value for required argument".
// Accepting it would let a client that omits it pass here and fail against a
// real node — a fake node hiding a bug, which is the one thing this project
// exists to stop.
func TestBlockMethodsRequireTheFullTxParameter(t *testing.T) {
	h, d := newHandler(1)
	blk := depositBlock(t, d)

	for name, c := range map[string]struct{ method, params string }{
		"by number": {"eth_getBlockByNumber", `["0x1"]`},
		"by hash":   {"eth_getBlockByHash", `["` + encodeHash(blk.Hash) + `"]`},
	} {
		t.Run(name, func(t *testing.T) {
			r := call(t, h, c.method, c.params)
			require.NotNil(t, r.Error, "a missing fullTx should be rejected")
			assert.Equal(t, -32602, r.Error.Code, "invalid params")
		})
	}
}

// A non-boolean is rejected rather than read as false, so a client that asked
// for full transactions is never quietly handed hashes instead.
func TestBlockMethodsRejectANonBooleanFullTx(t *testing.T) {
	h, _ := newHandler(1)

	for _, params := range []string{`["0x1","yes"]`, `["0x1",1]`, `["0x1",{}]`} {
		r := call(t, h, "eth_getBlockByNumber", params)
		require.NotNilf(t, r.Error, "params %s should be rejected", params)
		assert.Equal(t, -32602, r.Error.Code)
	}
}

// A deposit watcher finds deposits by scanning blocks for transfers to its own
// address, so from, to and value have to be on the wire. Without this a client
// can learn that a transaction exists and nothing about what it did.
func TestFullTxBlockCarriesTransferDetails(t *testing.T) {
	h, d := newHandler(1)
	blk := depositBlock(t, d)

	r := call(t, h, "eth_getBlockByNumber", `["0x1",true]`)
	require.Nil(t, r.Error)

	var b struct {
		Hash         string   `json:"hash"`
		Transactions []wireTx `json:"transactions"`
	}
	require.NoError(t, json.Unmarshal(r.Result, &b))
	require.Len(t, b.Transactions, 1)

	tx := b.Transactions[0]
	assert.Equal(t, encodeHash(blk.Txs[0].Hash), tx.Hash)
	assert.Equal(t, encodeAddress(alice), tx.From)
	assert.Equal(t, encodeAddress(exchange), tx.To)
	assert.Equal(t, "0xde0b6b3a7640000", tx.Value, "1 ETH as a hex quantity")
	assert.Equal(t, "0x", tx.Input, "a plain transfer carries no calldata")
	assert.Equal(t, b.Hash, tx.BlockHash)
	assert.Equal(t, "0x1", tx.BlockNumber)
	assert.Equal(t, "0x0", tx.TransactionIndex)
}

// Index is the transaction's position in its block, which a client uses to
// order deposits that share one block.
func TestFullTxIndexesTransactionsWithinTheBlock(t *testing.T) {
	h, d := newHandler(1)
	d.ProduceBlock([]chain.Tx{
		{ID: "a", From: alice, To: exchange, Value: wei("1")},
		{ID: "b", From: addr(0xb1), To: exchange, Value: wei("2")},
		{ID: "c", From: addr(0xc1), To: exchange, Value: wei("3")},
	})

	r := call(t, h, "eth_getBlockByNumber", `["0x1",true]`)
	var b struct {
		Transactions []wireTx `json:"transactions"`
	}
	require.NoError(t, json.Unmarshal(r.Result, &b))
	require.Len(t, b.Transactions, 3)

	for i, tx := range b.Transactions {
		assert.Equal(t, encodeUint64(uint64(i)), tx.TransactionIndex)
	}
	assert.Equal(t, "0x1", b.Transactions[0].Value)
	assert.Equal(t, "0x3", b.Transactions[2].Value)
}

// An empty block has to serialise as [] rather than null, or a client ranging
// over it has to special-case the absence.
func TestFullTxEmptyBlockIsAnEmptyArray(t *testing.T) {
	h, d := newHandler(1)
	d.ProduceBlock(nil)

	for _, params := range []string{`["0x1",true]`, `["0x1",false]`} {
		r := call(t, h, "eth_getBlockByNumber", params)
		assert.Containsf(t, string(r.Result), `"transactions":[]`, "params %s", params)
	}
}

func TestGetBlockByHashHonoursFullTx(t *testing.T) {
	h, d := newHandler(1)
	blk := depositBlock(t, d)
	hash := encodeHash(blk.Hash)

	r := call(t, h, "eth_getBlockByHash", `["`+hash+`",true]`)
	require.Nil(t, r.Error)

	var b struct {
		Transactions []wireTx `json:"transactions"`
	}
	require.NoError(t, json.Unmarshal(r.Result, &b))
	require.Len(t, b.Transactions, 1)
	assert.Equal(t, encodeAddress(exchange), b.Transactions[0].To)
}

// A bad parameter is a bad parameter whether or not the block exists, so the
// answer must not depend on which. Validating after the lookup would make a
// missing fullTx read as "no such block".
func TestBlockMethodsCheckParamsBeforeLookingUpTheBlock(t *testing.T) {
	h, _ := newHandler(1)

	r := call(t, h, "eth_getBlockByNumber", `["0x999"]`)
	require.NotNil(t, r.Error, "a missing fullTx should not be reported as a missing block")
	assert.Equal(t, -32602, r.Error.Code)

	withFlag := call(t, h, "eth_getBlockByNumber", `["0x999",false]`)
	require.Nil(t, withFlag.Error)
	assert.JSONEq(t, "null", string(withFlag.Result), "a block that is simply absent is null")
}

// go-ethereum's types.Transaction.UnmarshalJSON rejects a transaction missing
// any of these, and ethers throws on an absent nonce or gasLimit. Gaslight
// models none of them, but omitting them leaves a node no standard client can
// read — which the raw-JSON assertions above cannot detect, since they only
// check the fields we chose to look at.
func TestTransactionCarriesWhatAStandardClientRequires(t *testing.T) {
	h, d := newHandler(1)
	depositBlock(t, d)

	r := call(t, h, "eth_getBlockByNumber", `["0x1",true]`)
	require.Nil(t, r.Error)

	var b struct {
		Transactions []map[string]any `json:"transactions"`
	}
	require.NoError(t, json.Unmarshal(r.Result, &b))
	require.Len(t, b.Transactions, 1)

	for _, field := range []string{
		"hash", "from", "to", "value", "input",
		"blockHash", "blockNumber", "transactionIndex",
		"nonce", "gas", "gasPrice", "type", "v", "r", "s",
	} {
		assert.Containsf(t, b.Transactions[0], field, "a decoder requires %q", field)
	}
}

// An all-zero signature is how geth reads "unsigned", which skips its
// signature sanity check rather than failing it. A non-zero placeholder would
// be checked and rejected.
func TestTransactionSignatureIsAllZero(t *testing.T) {
	h, d := newHandler(1)
	blk := depositBlock(t, d)

	r := call(t, h, "eth_getTransactionByHash", `["`+encodeHash(blk.Txs[0].Hash)+`"]`)
	require.Nil(t, r.Error)

	var tx map[string]string
	require.NoError(t, json.Unmarshal(r.Result, &tx))
	for _, field := range []string{"v", "r", "s"} {
		assert.Equalf(t, "0x0", tx[field], "%q must read as unsigned", field)
	}
	assert.Equal(t, "0x5208", tx["gas"], "a plain transfer costs 21000, which is true rather than a placeholder")
}
