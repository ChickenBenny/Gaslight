package rpc

import (
	"math"
	"testing"

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
