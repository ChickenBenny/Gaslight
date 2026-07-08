package chain

import (
	"math/big"
	"testing"
)

// --- hashTx ---

func TestHashTxIsDeterministic(t *testing.T) {
	tx := Tx{ID: "a", From: addr("alice"), To: addr("bob"), Value: big.NewInt(100)}
	if hashTx(tx) != hashTx(tx) {
		t.Fatal("hashTx is not deterministic: same tx produced different hashes")
	}
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
			if hashTx(v) == baseHash {
				t.Errorf("%s did not change the tx hash — is that field mixed into hashTx?", name)
			}
		})
	}
}

// --- hashBlock ---

func TestHashBlockIsDeterministic(t *testing.T) {
	txs := []Tx{deposit("d1")}
	parent := Hash{1, 2, 3}
	if hashBlock(7, 5, parent, txs) != hashBlock(7, 5, parent, txs) {
		t.Fatal("hashBlock is not deterministic")
	}
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
			if c.got == base {
				t.Errorf("%s did not change the block hash", c.name)
			}
		})
	}
}

// A block is an ordered list of txs, so order must affect the hash.
func TestHashBlockIsOrderSensitive(t *testing.T) {
	a, b := deposit("a"), deposit("b")
	parent := Hash{1}
	if hashBlock(1, 1, parent, []Tx{a, b}) == hashBlock(1, 1, parent, []Tx{b, a}) {
		t.Error("hashBlock should depend on tx order")
	}
}
