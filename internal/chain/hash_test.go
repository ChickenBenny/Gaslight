package chain

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- hashTx ---

// nil, 0, +n and -n Values must hash distinctly, since big.Int.Bytes() alone
// drops the sign and encodes nil and 0 identically.
func TestHashTxDistinguishesValueSignAndNil(t *testing.T) {
	tx := func(v *big.Int) Tx {
		return Tx{ID: "a", From: addr("x"), To: addr("y"), Value: v}
	}
	variants := map[string]Hash{
		"nil":  hashTx(tx(nil)),
		"zero": hashTx(tx(big.NewInt(0))),
		"pos":  hashTx(tx(big.NewInt(100))),
		"neg":  hashTx(tx(big.NewInt(-100))),
	}
	seen := map[Hash]string{}
	for name, h := range variants {
		other, dup := seen[h]
		assert.Falsef(t, dup, "%s and %s hash identically", name, other)
		seen[h] = name
	}
}

func TestHashTxIsDeterministic(t *testing.T) {
	tx := Tx{ID: "a", From: addr("alice"), To: addr("bob"), Value: big.NewInt(100)}
	assert.Equal(t, hashTx(tx), hashTx(tx), "hashTx should be deterministic")
}

// Changing any field must change the hash — otherwise that field isn't being
// mixed into the digest (a classic "forgot to write a field" bug).
func TestHashTxDependsOnEveryField(t *testing.T) {
	base := Tx{ID: "a", From: addr("alice"), To: addr("bob"), Value: big.NewInt(100)}
	baseHash := hashTx(base)

	variants := map[string]Tx{
		"different ID":    {ID: "b", From: addr("alice"), To: addr("bob"), Value: big.NewInt(100)},
		"different From":  {ID: "a", From: addr("carol"), To: addr("bob"), Value: big.NewInt(100)},
		"different To":    {ID: "a", From: addr("alice"), To: addr("carol"), Value: big.NewInt(100)},
		"different Value": {ID: "a", From: addr("alice"), To: addr("bob"), Value: big.NewInt(999)},
	}
	for name, v := range variants {
		t.Run(name, func(t *testing.T) {
			assert.NotEqual(t, baseHash, hashTx(v), "this field should be mixed into hashTx")
		})
	}
}

// --- hashBlock ---

func TestHashBlockIsDeterministic(t *testing.T) {
	txs := []Tx{deposit("d1")}
	parent := Hash{1, 2, 3}
	assert.Equal(t, hashBlock(7, 5, parent, txs), hashBlock(7, 5, parent, txs))
}

func TestHashBlockDependsOnEveryInput(t *testing.T) {
	txs := []Tx{deposit("d1")}
	parent := Hash{1, 2, 3}
	base := hashBlock(7, 5, parent, txs)

	otherTx := []Tx{{ID: "other", From: addr("x"), To: addr("y"), Value: big.NewInt(5)}}

	cases := []struct {
		name string
		got  Hash
	}{
		// The seq case guards the design decision: two blocks with identical
		// number/parent/txs but a different seq MUST hash differently, or a reorg
		// branch could collide with the orphan it replaces.
		{"different seq", hashBlock(8, 5, parent, txs)},
		{"different number", hashBlock(7, 6, parent, txs)},
		{"different parentHash", hashBlock(7, 5, Hash{9, 9, 9}, txs)},
		{"different txs", hashBlock(7, 5, parent, otherTx)},
		{"empty txs", hashBlock(7, 5, parent, nil)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.NotEqual(t, base, c.got, "this input should change the block hash")
		})
	}
}

// A block is an ordered list of txs, so order must affect the hash.
func TestHashBlockIsOrderSensitive(t *testing.T) {
	a, b := deposit("a"), deposit("b")
	parent := Hash{1}
	assert.NotEqual(t,
		hashBlock(1, 1, parent, []Tx{a, b}),
		hashBlock(1, 1, parent, []Tx{b, a}),
		"hashBlock should depend on tx order")
}
