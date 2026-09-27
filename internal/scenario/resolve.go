package scenario

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ChickenBenny/Gaslight/internal/chain"
)

type resolver struct {
	txs map[string]chain.Tx
}

func newResolver() *resolver {
	return &resolver{txs: make(map[string]chain.Tx)}
}

func (r *resolver) resolveTx(spec TxSpec) (chain.Tx, error) {
	from, err := resolveAddress(spec.From)
	if err != nil {
		return chain.Tx{}, fmt.Errorf("tx %q: from: %w", spec.ID, err)
	}
	to, err := resolveAddress(spec.To)
	if err != nil {
		return chain.Tx{}, fmt.Errorf("tx %q: to: %w", spec.ID, err)
	}
	value, ok := new(big.Int).SetString(spec.Value, 10)
	if !ok {
		return chain.Tx{}, fmt.Errorf("tx %q: value is not a base-10 integer: %q", spec.ID, spec.Value)
	}
	tx := chain.Tx{ID: spec.ID, From: from, To: to, Value: value}
	r.txs[spec.ID] = tx
	return tx, nil
}

func (r *resolver) byID(id string) (chain.Tx, bool) {
	tx, ok := r.txs[id]
	return tx, ok
}

func resolveAddress(s string) (chain.Address, error) {
	if strings.HasPrefix(s, "0x") {
		return parseAddress(s)
	}
	return deriveAddress(s), nil
}

func deriveAddress(name string) chain.Address {
	sum := sha256.Sum256([]byte("gaslight/address/" + name))
	var addr chain.Address
	copy(addr[:], sum[:20])
	return addr
}

func parseAddress(s string) (chain.Address, error) {
	if !strings.HasPrefix(s, "0x") {
		return chain.Address{}, fmt.Errorf("address must start with 0x: %q", s)
	}
	if len(s) != 42 {
		return chain.Address{}, fmt.Errorf("address must be 20 bytes (40 hex digits): %q", s)
	}
	b, err := hex.DecodeString(s[2:])
	if err != nil {
		// hex names the offending character, which is worth keeping: one bad
		// character in forty is hard to spot by eye.
		return chain.Address{}, fmt.Errorf("invalid address %q: %w", s, err)
	}
	var addr chain.Address
	copy(addr[:], b)
	return addr, nil
}
