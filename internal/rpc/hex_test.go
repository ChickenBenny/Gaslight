package rpc

import (
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ethereum "quantity" encoding: 0x-prefixed, lowercase, no leading zeros, 0 -> "0x0".
func TestEncodeUint64(t *testing.T) {
	cases := map[uint64]string{
		0:              "0x0",
		1:              "0x1",
		15:             "0xf",
		16:             "0x10",
		255:            "0xff", // lowercase, never "0xFF"
		436:            "0x1b4",
		math.MaxUint64: "0xffffffffffffffff",
	}
	for v, want := range cases {
		assert.Equalf(t, want, encodeUint64(v), "encodeUint64(%d)", v)
	}
}

func TestDecodeUint64(t *testing.T) {
	cases := map[string]uint64{
		"0x0":                0,
		"0x1":                1,
		"0xf":                15,
		"0x1b4":              436,
		"0xffffffffffffffff": math.MaxUint64,
	}
	for s, want := range cases {
		got, err := decodeUint64(s)
		require.NoErrorf(t, err, "decodeUint64(%q)", s)
		assert.Equalf(t, want, got, "decodeUint64(%q)", s)
	}
}

// Decoding is lenient: it accepts forms we never *emit* — leading zeros and
// uppercase hex — because real clients send them and rejecting is pointless.
func TestDecodeUint64IsLenient(t *testing.T) {
	cases := map[string]uint64{
		"0x00": 0,
		"0x01": 1,
		"0xFF": 255,
	}
	for s, want := range cases {
		got, err := decodeUint64(s)
		require.NoErrorf(t, err, "decodeUint64(%q)", s)
		assert.Equalf(t, want, got, "decodeUint64(%q)", s)
	}
}

// Missing 0x prefix, empty magnitude, and non-hex digits must all error.
func TestDecodeUint64Rejects(t *testing.T) {
	for _, s := range []string{"", "1b4", "0x", "0xzz", "abc"} {
		_, err := decodeUint64(s)
		assert.Errorf(t, err, "decodeUint64(%q) should error", s)
	}
}

func TestUint64RoundTrip(t *testing.T) {
	for _, v := range []uint64{0, 1, 255, 4096, 1_000_000, math.MaxUint64} {
		got, err := decodeUint64(encodeUint64(v))
		require.NoError(t, err)
		assert.Equal(t, v, got)
	}
}

// --- Hash / Address: "data" hex (fixed length, 0x-prefixed, leading zeros kept) ---

// Unlike a quantity, a hash keeps every byte: the all-zero hash is 64 zeros,
// not "0x0", and the output is always exactly 66 chars.
func TestEncodeHashIsFixedLength(t *testing.T) {
	assert.Equal(t, "0x"+strings.Repeat("0", 64), encodeHash(chain.Hash{}))

	var h chain.Hash
	h[0] = 0x12
	h[31] = 0xff
	assert.Equal(t, "0x12"+strings.Repeat("0", 60)+"ff", encodeHash(h))
	assert.Len(t, encodeHash(h), 66)
}

func TestDecodeHashRoundTrip(t *testing.T) {
	var h chain.Hash
	h[0] = 0xab
	h[15] = 0xcd
	got, err := decodeHash(encodeHash(h))
	require.NoError(t, err)
	assert.Equal(t, h, got)
}

func TestDecodeHashRejects(t *testing.T) {
	cases := []string{
		"",                             // empty
		strings.Repeat("a", 64),        // 64 hex chars but no 0x prefix
		"0x1234",                       // too short
		"0x" + strings.Repeat("g", 64), // right length but non-hex
		"0x" + strings.Repeat("0", 63), // wrong length
	}
	for _, s := range cases {
		_, err := decodeHash(s)
		assert.Errorf(t, err, "decodeHash(%q) should error", s)
	}
}

func TestEncodeAddressIsFixedLength(t *testing.T) {
	assert.Equal(t, "0x"+strings.Repeat("0", 40), encodeAddress(chain.Address{}))

	var a chain.Address
	a[0] = 0xde
	a[19] = 0xad
	assert.Equal(t, "0xde"+strings.Repeat("0", 36)+"ad", encodeAddress(a))
	assert.Len(t, encodeAddress(a), 42)
}

// Two encoders, one wire format. A block number and a transfer value have to
// look identical on the wire when they hold the same number, or the quantity
// rules have drifted apart between them — and only on whichever fields happen
// to use the one that changed.
func TestTheTwoQuantityEncodersAgree(t *testing.T) {
	for _, v := range []uint64{
		0, 1, 12, 255, 256,
		1 << 32,
		1_000_000_000_000_000_000, // 1 ETH, the largest value both can hold
		math.MaxUint64,
	} {
		assert.Equalf(t, encodeUint64(v), encodeBigInt(new(big.Int).SetUint64(v)),
			"the two encoders disagreed on %d", v)
	}
}

// Value is a *big.Int precisely because a transfer can exceed what a uint64
// holds: 1000 ETH already does.
func TestEncodeBigIntHandlesValuesBeyondUint64(t *testing.T) {
	v, ok := new(big.Int).SetString("1000000000000000000000", 10) // 1000 ETH
	require.True(t, ok)
	require.False(t, v.IsUint64(), "this test is pointless if it fits in a uint64")

	assert.Equal(t, "0x3635c9adc5dea00000", encodeBigInt(v))
}

// A quantity carries no leading zeros and zero is "0x0", so a client parsing
// it as a number reads what was meant. A nil value is the zero amount rather
// than a crash, matching how hash.go already treats one.
func TestEncodeBigIntFollowsTheQuantityRules(t *testing.T) {
	assert.Equal(t, "0x0", encodeBigInt(nil))
	assert.Equal(t, "0x0", encodeBigInt(big.NewInt(0)))
	assert.Equal(t, "0x1", encodeBigInt(big.NewInt(1)))
	assert.Equal(t, "0xff", encodeBigInt(big.NewInt(255)), "lower case, no padding")
	assert.NotContains(t, encodeBigInt(big.NewInt(16)), "0x010", "no leading zero")
}
