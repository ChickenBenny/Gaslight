package rpc

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/ChickenBenny/Gaslight/internal/chain"
)

func encodeUint64(v uint64) string {
	if v == 0 {
		return "0x0"
	}
	return "0x" + strconv.FormatUint(v, 16)
}

func decodeUint64(s string) (uint64, error) {
	if !strings.HasPrefix(s, "0x") {
		return 0, fmt.Errorf("hex quantity must start with 0x: %q", s)
	}
	v, err := strconv.ParseUint(s[2:], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid hex quantity: %q", s)
	}
	return v, nil
}

func encodeHash(h chain.Hash) string {
	return "0x" + hex.EncodeToString(h[:])
}

func decodeHash(s string) (chain.Hash, error) {
	var h chain.Hash
	if !strings.HasPrefix(s, "0x") {
		return h, fmt.Errorf("hash must start with 0x: %q", s)
	}
	b, err := hex.DecodeString(s[2:])
	if err != nil {
		return h, fmt.Errorf("invalid hash: %q", s)
	}
	if len(b) != len(h) {
		return h, fmt.Errorf("hash must be %d bytes: %q", len(h), s)
	}
	copy(h[:], b)
	return h, nil
}

func encodeAddress(a chain.Address) string {
	return "0x" + hex.EncodeToString(a[:])
}
