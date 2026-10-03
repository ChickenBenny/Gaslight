package rpc

import (
	"encoding/json"
	"testing"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// eth_getTransactionReceipt answering null means either "still pending" or
// "orphaned", and a node says the same thing for both. Asking for the
// transaction itself is how a correct client tells them apart, so the method
// has to exist for such a client to be testable at all.
func TestGetTransactionByHashReturnsTheTransaction(t *testing.T) {
	h, d := newHandler(1)
	blk := depositBlock(t, d)
	txHash := encodeHash(blk.Txs[0].Hash)

	r := call(t, h, "eth_getTransactionByHash", `["`+txHash+`"]`)
	require.Nil(t, r.Error)

	var tx wireTx
	require.NoError(t, json.Unmarshal(r.Result, &tx))
	assert.Equal(t, txHash, tx.Hash)
	assert.Equal(t, encodeAddress(alice), tx.From)
	assert.Equal(t, encodeAddress(exchange), tx.To)
	assert.Equal(t, "0xde0b6b3a7640000", tx.Value)
	assert.Equal(t, "0x1", tx.BlockNumber)
}

func TestGetTransactionByHashIsNullForAnUnknownHash(t *testing.T) {
	h, _ := newHandler(1)
	unknown := "0x" + "11"
	for range 31 {
		unknown += "11"
	}

	r := call(t, h, "eth_getTransactionByHash", `["`+unknown+`"]`)
	require.Nil(t, r.Error)
	assert.JSONEq(t, "null", string(r.Result))
}

// After a reorg the deposit is on no canonical block, and this is the answer
// that lets a watcher reverse a credit instead of waiting forever for a
// transaction that is never coming back.
func TestGetTransactionByHashIsNullOnceOrphaned(t *testing.T) {
	h, d := newHandler(1)
	blk := depositBlock(t, d)
	txHash := encodeHash(blk.Txs[0].Hash)

	require.Nil(t, call(t, h, "eth_getTransactionByHash", `["`+txHash+`"]`).Error)
	require.NoError(t, d.Reorg(0, make([][]chain.Tx, 3)))

	r := call(t, h, "eth_getTransactionByHash", `["`+txHash+`"]`)
	require.Nil(t, r.Error)
	assert.JSONEq(t, "null", string(r.Result),
		"an orphaned transaction is gone, which is what makes the credit reversible")

	// The block it rode in is still reachable by hash, as on a real node, so a
	// client holding the old block hash is not told it never existed.
	byHash := call(t, h, "eth_getBlockByHash", `["`+encodeHash(blk.Hash)+`",true]`)
	assert.NotEqual(t, "null", string(byHash.Result))
}

func TestGetTransactionByHashRejectsBadParams(t *testing.T) {
	h, _ := newHandler(1)

	for name, params := range map[string]string{
		"no params":    `[]`,
		"not a string": `[42]`,
		"not a hash":   `["0xnope"]`,
		"wrong length": `["0x1234"]`,
	} {
		t.Run(name, func(t *testing.T) {
			r := call(t, h, "eth_getTransactionByHash", params)
			require.NotNil(t, r.Error, "should be rejected")
			assert.Equal(t, -32602, r.Error.Code, "invalid params")
		})
	}
}

// A fault can only be aimed at a method the handler wraps, and scenario
// validation checks a name against this list. A method served but missing
// here could never be made to lie.
func TestGetTransactionByHashIsAFaultableMethod(t *testing.T) {
	assert.True(t, Serves("eth_getTransactionByHash"))
	assert.Contains(t, Methods, "eth_getTransactionByHash")
}
