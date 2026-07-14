package chain

import (
	"crypto/sha256"
	"encoding/binary"
	"io"
	"math/big"
)

// hashTx digests every Tx field; variable-length fields are length-prefixed
// so field boundaries stay unambiguous.
func hashTx(tx Tx) Hash {
	h := sha256.New()
	writeBytes(h, []byte(tx.ID))
	h.Write(tx.From[:])
	h.Write(tx.To[:])
	writeValue(h, tx.Value)

	var hash Hash
	copy(hash[:], h.Sum(nil))
	return hash
}

// hashBlock computes sha256(seq ‖ number ‖ parentHash ‖ txHashes). seq
// disambiguates blocks with identical number/parent/txs, so a reorg branch
// can never collide with the orphan it replaces.
func hashBlock(seq, number uint64, parent Hash, txs []Tx) Hash {
	h := sha256.New()
	writeUint64(h, seq)
	writeUint64(h, number)
	h.Write(parent[:])

	for _, tx := range txs {
		txHash := hashTx(tx)
		h.Write(txHash[:])
	}

	var hash Hash
	copy(hash[:], h.Sum(nil))
	return hash
}

// writeBytes writes b length-prefixed.
func writeBytes(h io.Writer, b []byte) {
	writeUint64(h, uint64(len(b)))
	h.Write(b)
}

// writeValue digests a *big.Int with an explicit sign/nil tag, so nil, 0, +n
// and -n all hash distinctly (big.Int.Bytes() drops the sign and encodes nil
// and 0 identically).
func writeValue(h io.Writer, v *big.Int) {
	switch {
	case v == nil:
		h.Write([]byte{0})
		return
	case v.Sign() < 0:
		h.Write([]byte{1})
	case v.Sign() == 0:
		h.Write([]byte{2})
	default:
		h.Write([]byte{3})
	}
	writeBytes(h, v.Bytes())
}

func writeUint64(h io.Writer, v uint64) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], v)
	h.Write(n[:])
}
