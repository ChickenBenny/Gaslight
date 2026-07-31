package rpc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Log fields must be hex-encoded on the wire: address as 20-byte data hex,
// topics as 32-byte hashes, data as raw bytes ("0x" when empty).
func TestToRPCReceiptEncodesLogs(t *testing.T) {
	var addr chain.Address
	addr[0], addr[19] = 0xab, 0xcd
	var topicA, topicB chain.Hash
	topicA[0] = 0x11
	topicB[31] = 0x22

	rcpt := chain.Receipt{
		TxHash:  chain.Hash{0x01},
		Status:  1,
		GasUsed: 21000,
		Logs: []chain.Log{
			{Address: addr, Topics: []chain.Hash{topicA, topicB}, Data: []byte{0xde, 0xad, 0xbe, 0xef}},
			{}, // zero log: no topics, no data
		},
	}
	blk := chain.Block{Number: 7, Hash: chain.Hash{0x02}}

	got := toRPCReceipt(&rcpt, &blk)

	assert.Equal(t, "0x01"+strings.Repeat("00", 31), got.TransactionHash)
	assert.Equal(t, "0x1", got.Status)
	assert.Equal(t, "0x5208", got.GasUsed)
	assert.Equal(t, "0x02"+strings.Repeat("00", 31), got.BlockHash)
	assert.Equal(t, "0x7", got.BlockNumber)

	require.Len(t, got.Logs, 2)
	assert.Equal(t, "0xab"+strings.Repeat("00", 18)+"cd", got.Logs[0].Address)
	assert.Equal(t, []string{
		"0x11" + strings.Repeat("00", 31),
		"0x" + strings.Repeat("00", 31) + "22",
	}, got.Logs[0].Topics)
	assert.Equal(t, "0xdeadbeef", got.Logs[0].Data)

	raw, err := json.Marshal(got.Logs[1])
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"address":"0x`+strings.Repeat("00", 20)+`","topics":[],"data":"0x"}`,
		string(raw), "empty topics must be [] (not null), empty data must be 0x")
}

func TestToRPCReceiptEmptyLogs(t *testing.T) {
	rcpt := chain.Receipt{TxHash: chain.Hash{0x01}, Status: 1}
	blk := chain.Block{Number: 3, Hash: chain.Hash{0x02}}

	raw, err := json.Marshal(toRPCReceipt(&rcpt, &blk))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"logs":[]`, "empty logs must serialize as [], never null")
}
