package rpc

import (
	"fmt"
	"strconv"
	"strings"
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
