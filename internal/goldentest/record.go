package goldentest

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/ChickenBenny/Gaslight/internal/faults"
	"github.com/ChickenBenny/Gaslight/internal/scenario"
)

const stepLimit = 10000

func Record(path string) (string, error) {
	sc, err := scenario.Load(path)
	if err != nil {
		return "", err
	}

	d := chain.NewDriver(sc.ChainID, sc.DriverOptions()...)
	// The registry is wrapped rather than read afterwards: a fault changes
	// nothing about the chain, so without recording the calls themselves a
	// scenario's lies would be outside the gate entirely.
	reg := &recordingRegistry{inner: faults.NewRegistry()}
	e := scenario.NewEngine(sc, d, reg)

	var b strings.Builder
	fmt.Fprintf(&b, "scenario %s\n", filepath.Base(path))
	fmt.Fprintf(&b, "chain id %d\n\n", sc.ChainID)
	fmt.Fprintf(&b, "%4s  %6s  %9s  %-64s  %-20s  %s\n",
		"step", "height", "finalized", "head", "produced", "faults")

	steps := 0
	for {
		more, err := e.Step()
		if err != nil {
			return "", fmt.Errorf("step %d: %w", steps+1, err)
		}
		if !more {
			break
		}
		steps++
		if steps > stepLimit {
			return "", fmt.Errorf("scenario never reported completion after %d steps", stepLimit)
		}

		snap := d.Snapshot()
		head := snap.Head()
		fmt.Fprintf(&b, "%4d  %6d  %9d  %x  %-20s  %s\n",
			steps,
			snap.Height(),
			snap.Finalized(),
			head.Hash,
			txHashes(head),
			reg.take(),
		)
	}

	fmt.Fprintf(&b, "\nsteps %d\n", steps)
	writeChain(&b, d.Snapshot())

	return b.String(), nil
}

func txHashes(blk *chain.Block) string {
	if blk == nil || len(blk.Txs) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(blk.Txs))
	for _, tx := range blk.Txs {
		parts = append(parts, fmt.Sprintf("%s=%x", tx.ID, tx.Hash))
	}
	return strings.Join(parts, " ")
}

func writeChain(b *strings.Builder, snap *chain.ChainSnapshot) {
	fmt.Fprintf(b, "final height %d\n", snap.Height())
	fmt.Fprintf(b, "finalized %d\n", snap.Finalized())
	fmt.Fprintf(b, "\ncanonical chain\n")
	fmt.Fprintf(b, "%6s  %10s  %-64s  %s\n", "height", "timestamp", "hash", "txs")

	// Walked by height rather than over the block map: the map holds orphans
	// too, and ranging it would put this file's own output at the mercy of map
	// ordering — the exact thing the gate exists to rule out.
	for n := uint64(0); n <= snap.Height(); n++ {
		blk := snap.ByNumber(n)
		if blk == nil {
			// A hole in the canonical chain is a serious bug, so it is
			// recorded rather than skipped.
			fmt.Fprintf(b, "%6d  %10s  %-64s  %s\n", n, "-", "MISSING", "-")
			continue
		}
		fmt.Fprintf(b, "%6d  %10d  %x  %s\n",
			blk.Number, blk.Timestamp, blk.Hash, txHashes(blk))
	}
}

// recordingRegistry notes what a scenario asked of the fault registry and
// passes it on. A fault leaves no trace on the chain, so recording the calls
// is the only way a change to how faults are built — a misparsed type, a count
// that no longer means what it did — can reach the gate.
//
// It exists here rather than as an accessor on faults.Registry so that nothing
// is added to a production type for a test's benefit. scenario.FaultRegistry
// being an interface is what makes that possible.
type recordingRegistry struct {
	inner *faults.Registry
	calls []string
}

func (r *recordingRegistry) Enable(f *faults.Fault) error {
	r.calls = append(r.calls, fmt.Sprintf("enable(%s,%s,count=%d,delay=%s)",
		f.Method, f.Type, f.Remaining(), f.Delay))
	return r.inner.Enable(f)
}

func (r *recordingRegistry) Clear() {
	r.calls = append(r.calls, "clear()")
	r.inner.Clear()
}

// take returns what happened since the last call and forgets it, so each line
// of the recording describes its own step rather than everything so far.
func (r *recordingRegistry) take() string {
	if len(r.calls) == 0 {
		return "-"
	}
	out := strings.Join(r.calls, " ")
	r.calls = nil
	return out
}
