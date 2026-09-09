package scenario

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustAddr(t *testing.T, s string) chain.Address {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	require.Len(t, b, 20)

	var a chain.Address
	copy(a[:], b)
	return a
}

// The derivation is a promise, not an implementation detail: a golden run
// recorded today has to replay byte for byte on another machine next year, so
// the expected values here were computed outside Go as
// sha256("gaslight/address/" + name) truncated to 20 bytes. Changing the
// prefix or the truncation breaks reproducibility, and should break this test.
func TestDeriveAddressIsPinnedToASpec(t *testing.T) {
	for name, want := range map[string]string{
		"alice":    "30c24ac10e8572b29d76b632dc7f029c9ed166a1",
		"exchange": "cf1e2d345f525620d50ffa090ccd5edbf581e5a6",
		"bob":      "59f004ff0621d2dc49dc588f16bcac207c6f9b36",
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, mustAddr(t, want), deriveAddress(name))
		})
	}
}

func TestDeriveAddressIsStableAndDistinct(t *testing.T) {
	assert.Equal(t, deriveAddress("alice"), deriveAddress("alice"),
		"the same name must resolve the same way every time")
	assert.NotEqual(t, deriveAddress("alice"), deriveAddress("alicf"),
		"names one character apart must not collide")

	// A scenario that accidentally used the zero address would be reading as
	// "unset" everywhere it appeared.
	assert.NotEqual(t, chain.Address{}, deriveAddress("alice"))
	assert.NotEqual(t, chain.Address{}, deriveAddress(""))
}

// Nothing about the derivation may depend on the order names are seen in, or a
// tx added at the top of a file would shift every address below it.
func TestDeriveAddressDoesNotDependOnOrder(t *testing.T) {
	first := []string{"alice", "bob", "exchange"}
	second := []string{"exchange", "alice", "bob"}

	got := map[string]chain.Address{}
	for _, n := range first {
		got[n] = deriveAddress(n)
	}
	for _, n := range second {
		assert.Equal(t, got[n], deriveAddress(n), "%q shifted with the order", n)
	}
}

func TestParseAddressAcceptsAnyCase(t *testing.T) {
	want := mustAddr(t, "aaaa1111bbbb2222cccc3333dddd4444eeee5555")

	for _, s := range []string{
		"0xaaaa1111bbbb2222cccc3333dddd4444eeee5555",
		"0xAAAA1111BBBB2222CCCC3333DDDD4444EEEE5555",
		"0xAaAa1111BbBb2222CcCc3333DdDd4444EeEe5555",
	} {
		got, err := parseAddress(s)
		require.NoErrorf(t, err, "should accept %q", s)
		assert.Equal(t, want, got, "case must not change the address")
	}
}

func TestParseAddressRejects(t *testing.T) {
	cases := map[string]string{
		"no 0x prefix":  "aaaa1111bbbb2222cccc3333dddd4444eeee5555",
		"too short":     "0xaaaa1111bbbb2222cccc3333dddd4444eeee55",
		"too long":      "0xaaaa1111bbbb2222cccc3333dddd4444eeee555566",
		"not hex":       "0xzzzz1111bbbb2222cccc3333dddd4444eeee5555",
		"bare 0x":       "0x",
		"empty":         "",
		"odd digits":    "0xaaa",
		"embedded 0x":   "0xaaaa1111bbbb2222cccc3333dddd44440xee5555",
		"trailing junk": "0xaaaa1111bbbb2222cccc3333dddd4444eeee5555 ",
	}

	for label, s := range cases {
		t.Run(label, func(t *testing.T) {
			_, err := parseAddress(s)
			require.Errorf(t, err, "%q should not resolve to an address", s)
			assert.Containsf(t, err.Error(), s, "the error should quote the input; got: %v", err)
		})
	}
}

// resolveAddress is what the engine calls: a 0x form is taken literally, and
// anything else is a symbolic name.
func TestResolveAddressDispatches(t *testing.T) {
	literal, err := resolveAddress("0xaaaa1111bbbb2222cccc3333dddd4444eeee5555")
	require.NoError(t, err)
	assert.Equal(t, mustAddr(t, "aaaa1111bbbb2222cccc3333dddd4444eeee5555"), literal)

	symbolic, err := resolveAddress("alice")
	require.NoError(t, err)
	assert.Equal(t, deriveAddress("alice"), symbolic)

	// A name that looks like it meant to be hex is a typo, not a new account:
	// silently deriving an address from it is how a deposit goes to nobody.
	_, err = resolveAddress("0xnope")
	require.Error(t, err)
}

func TestResolveTx(t *testing.T) {
	r := newResolver()

	tx, err := r.resolveTx(TxSpec{
		ID:    "alice-deposit",
		From:  "alice",
		To:    "exchange",
		Value: "1000000000000000000",
	})
	require.NoError(t, err)

	assert.Equal(t, "alice-deposit", tx.ID)
	assert.Equal(t, deriveAddress("alice"), tx.From)
	assert.Equal(t, deriveAddress("exchange"), tx.To)
	assert.Equal(t, "1000000000000000000", tx.Value.String())

	// buildBlock stamps the hash and hashTx ignores the field, so leaving it
	// zero is what makes a re-included tx keep its identity across a reorg.
	assert.Equal(t, chain.Hash{}, tx.Hash, "the driver owns the tx hash")
}

// Values are strings in the schema precisely so they survive as exact
// integers; 1 ETH + 1 wei is past what a float64 can represent.
func TestResolveTxKeepsValuePrecision(t *testing.T) {
	r := newResolver()

	tx, err := r.resolveTx(TxSpec{ID: "t", From: "a", To: "b", Value: "1000000000000000001"})
	require.NoError(t, err)

	want, ok := new(big.Int).SetString("1000000000000000001", 10)
	require.True(t, ok)
	assert.Zero(t, want.Cmp(tx.Value))
}

// A reorg places transactions defined earlier in the file, so the resolver has
// to hand back the same tx it built the first time — same fields, therefore
// the same hash once the driver stamps it, therefore the client sees the
// deposit it already knows rather than a new one.
func TestResolverRemembersTxsByID(t *testing.T) {
	r := newResolver()

	built, err := r.resolveTx(TxSpec{ID: "dep", From: "alice", To: "exchange", Value: "500"})
	require.NoError(t, err)

	got, ok := r.byID("dep")
	require.True(t, ok, "a resolved tx must be retrievable by its scenario id")
	assert.Equal(t, built, got)

	_, ok = r.byID("never-defined")
	assert.False(t, ok)
}

// Two resolvers are two runs of the same file, and must agree.
func TestResolversDoNotDivergeBetweenRuns(t *testing.T) {
	spec := TxSpec{ID: "dep", From: "alice", To: "exchange", Value: "7"}

	a, err := newResolver().resolveTx(spec)
	require.NoError(t, err)
	b, err := newResolver().resolveTx(spec)
	require.NoError(t, err)

	assert.Equal(t, a, b)
}

// These are unreachable through Parse, which validates first. They are still
// errors rather than panics or zero values, because a resolver that quietly
// substitutes something plausible is how a scenario stops describing its run.
func TestResolveTxRejectsWhatValidationWouldHaveCaught(t *testing.T) {
	cases := map[string]TxSpec{
		"bad from":  {ID: "t", From: "0xshort", To: "b", Value: "1"},
		"bad to":    {ID: "t", From: "a", To: "0xshort", Value: "1"},
		"bad value": {ID: "t", From: "a", To: "b", Value: "lots"},
		"no value":  {ID: "t", From: "a", To: "b", Value: ""},
	}

	for label, spec := range cases {
		t.Run(label, func(t *testing.T) {
			_, err := newResolver().resolveTx(spec)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "t", "the error should name the tx")
		})
	}
}
