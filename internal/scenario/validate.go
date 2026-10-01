package scenario

import (
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ChickenBenny/Gaslight/internal/faults"
	"github.com/ChickenBenny/Gaslight/internal/rpc"
)

// validate rejects a scenario that would not perform the run it describes. A
// chaos tool's worst outcome is a green run against a fault that never fired,
// so every rejection names the location and the offending value.
func (s *Scenario) validate() error {
	if s.BlockInterval != nil && *s.BlockInterval == 0 {
		return fmt.Errorf("scenario: block_interval 0 would stop the chain's clock")
	}

	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("scenario: name is required")
	}
	if len(s.Timeline) == 0 {
		return fmt.Errorf("scenario: timeline is empty")
	}

	// txIDs accumulates as produce events are seen. Because the timeline is
	// non-decreasing, a reorg can only reference txs defined before it, so one
	// pass is enough.
	txIDs := map[string]bool{}

	// height and finalized model the chain as the timeline plays out, which is
	// what the checks below have to run against rather than each event's own
	// at_height. height is usually at_height, since the engine fills the gap
	// before an event with empty blocks — but a reorg leaves the chain at the
	// tip of its new branch and nothing lowers it again, so a later event can
	// find the chain already past its own at_height. finalized only ever rises,
	// and is the floor a reorg may not reach below.
	var prevAt, height, finalized uint64

	for i, e := range s.Timeline {
		where := fmt.Sprintf("timeline[%d]", i)

		if e.AtHeight == 0 {
			return fmt.Errorf("%s: at_height 0 is genesis and cannot host an event", where)
		}
		if e.AtHeight < prevAt {
			return fmt.Errorf("%s: at_height %d is out of order (previous was %d)", where, e.AtHeight, prevAt)
		}
		prevAt = e.AtHeight

		if err := validateOneAction(where, e); err != nil {
			return err
		}

		// Every action but produce waits for the chain to reach at_height.
		// Produce is the one that appends the block at at_height itself, so it
		// needs the chain still below.
		if e.Produce != nil {
			if e.AtHeight <= height {
				return fmt.Errorf("%s: produce targets height %d but the chain already stands at %d",
					where, e.AtHeight, height)
			}
		} else if e.AtHeight > height {
			height = e.AtHeight
		}

		switch {
		case e.Produce != nil:
			if err := validateProduce(where, e.Produce, txIDs); err != nil {
				return err
			}
			height = e.AtHeight
		case e.Reorg != nil:
			newHead, err := validateReorg(where, height, finalized, e.Reorg, txIDs)
			if err != nil {
				return err
			}
			height = newHead
		case e.Finalize != nil:
			if e.Finalize.Height > height {
				return fmt.Errorf("%s: finalize height %d is above the height at that point (%d)",
					where, e.Finalize.Height, height)
			}
			// Driver.Finalize returns ErrHeightTooLow rather than moving the
			// watermark back down, so a scenario that tries is not the run it
			// describes.
			if e.Finalize.Height < finalized {
				return fmt.Errorf("%s: finalize height %d is below the finalized height already reached (%d), "+
					"and the watermark only moves up", where, e.Finalize.Height, finalized)
			}
			finalized = e.Finalize.Height
		case e.Fault != nil:
			if err := validateFault(where, e.Fault); err != nil {
				return err
			}
		}
	}

	if s.EndAtHeight > 0 && s.EndAtHeight < height {
		return fmt.Errorf("scenario: end_at_height %d is below the height the timeline reaches (%d)",
			s.EndAtHeight, height)
	}
	return nil
}

// validateOneAction requires exactly one action per event, and names the
// offenders when there are several.
func validateOneAction(where string, e Event) error {
	var given []string
	for _, a := range []struct {
		name    string
		present bool
	}{
		{"produce", e.Produce != nil},
		{"reorg", e.Reorg != nil},
		{"finalize", e.Finalize != nil},
		{"fault", e.Fault != nil},
		{"clear_faults", e.ClearFaults != nil},
	} {
		if a.present {
			given = append(given, a.name)
		}
	}

	switch len(given) {
	case 0:
		return fmt.Errorf("%s: no action given (expected one of produce, reorg, finalize, fault, "+
			"clear_faults; an action with no fields needs an explicit \"{}\")", where)
	case 1:
		return nil
	default:
		return fmt.Errorf("%s: multiple actions given (%s) — an event does exactly one thing",
			where, strings.Join(given, ", "))
	}
}

func validateProduce(where string, p *ProduceAction, txIDs map[string]bool) error {
	if len(p.Txs) == 0 {
		return fmt.Errorf("%s: produce requires at least one entry in txs", where)
	}
	for j, tx := range p.Txs {
		w := fmt.Sprintf("%s.produce.txs[%d]", where, j)

		// Whitespace is rejected rather than trimmed: an id is referenced by
		// name elsewhere in the file, and " dep" would silently fail to match.
		if err := requireBareName(w, "id", tx.ID); err != nil {
			return err
		}
		if txIDs[tx.ID] {
			return fmt.Errorf("%s: duplicate tx id %q", w, tx.ID)
		}
		txIDs[tx.ID] = true

		if err := requireAddress(w, "from", tx.From); err != nil {
			return err
		}
		if err := requireAddress(w, "to", tx.To); err != nil {
			return err
		}
		if err := validateValue(w, tx.Value); err != nil {
			return err
		}
	}
	return nil
}

// validateReorg checks the branch against the state the chain is in when the
// event runs, and returns the height it leaves behind: the tip of the new
// branch. A branch that does not outgrow the chain would lose the fork choice
// and the reorg would quietly not happen; one that reaches below the finalized
// height is refused by the driver. Both are rejected here instead.
func validateReorg(where string, height, finalized uint64, r *ReorgAction, txIDs map[string]bool) (uint64, error) {
	if r.BranchLength < 1 {
		return 0, fmt.Errorf("%s: reorg branch_length must be at least 1", where)
	}
	if r.ForkFrom > height {
		return 0, fmt.Errorf("%s: reorg fork_from %d is above the height at that point (%d)",
			where, r.ForkFrom, height)
	}
	// Matches Driver.Reorg, which returns ErrReorgBelowFinalized: forking at
	// the watermark is allowed, below it is not. A finalized block being
	// reorged away is the one thing finality rules out, so a scenario asking
	// for it is describing a chain that cannot exist.
	if r.ForkFrom < finalized {
		return 0, fmt.Errorf("%s: reorg fork_from %d is below the finalized height (%d), "+
			"which no reorg may reach past", where, r.ForkFrom, finalized)
	}

	newHead := r.ForkFrom + uint64(r.BranchLength)
	if newHead <= height {
		return 0, fmt.Errorf("%s: reorg does not outgrow the chain (new head would be %d, current is %d)",
			where, newHead, height)
	}

	// The branch is built as one tx list per position, so two entries on one
	// height would collapse into one and a repeated id would put the same
	// transaction on the canonical chain twice.
	seenHeight := map[uint64]bool{}
	seenID := map[string]bool{}

	for j, b := range r.Txs {
		w := fmt.Sprintf("%s.reorg.txs[%d]", where, j)
		lo := r.ForkFrom + 1
		if b.AtHeight < lo || b.AtHeight > newHead {
			return 0, fmt.Errorf("%s: at_height %d is outside the new branch (%d..%d)", w, b.AtHeight, lo, newHead)
		}
		if seenHeight[b.AtHeight] {
			return 0, fmt.Errorf("%s: at_height %d is given twice in this branch", w, b.AtHeight)
		}
		seenHeight[b.AtHeight] = true

		if len(b.IDs) == 0 {
			return 0, fmt.Errorf("%s: ids is empty, and a branch block is empty already unless ids places "+
				"transactions in it", w)
		}
		for _, id := range b.IDs {
			if !txIDs[id] {
				return 0, fmt.Errorf("%s: unknown tx id %q", w, id)
			}
			if seenID[id] {
				return 0, fmt.Errorf("%s: tx id %q is placed twice in this branch", w, id)
			}
			seenID[id] = true
		}
	}
	return newHead, nil
}

func validateFault(where string, f *FaultAction) error {
	if err := requireBareName(where, "fault method", f.Method); err != nil {
		return err
	}
	// Only served methods are wrapped with the fault check, so a fault on any
	// other name registers, fires zero times, and the run goes green having
	// thrown nothing at the client.
	if f.Method != faults.AllMethods && !rpc.Serves(f.Method) {
		return fmt.Errorf("%s: fault method %q is not served (expected %q or one of %s)",
			where, f.Method, faults.AllMethods, strings.Join(rpc.Methods, ", "))
	}

	t, err := faults.ParseType(f.Type)
	if err != nil {
		return fmt.Errorf("%s: fault type %q is not recognised (expected %q or %q)",
			where, f.Type, faults.FalseNull, faults.Delay)
	}
	if f.Count < 0 {
		return fmt.Errorf("%s: fault count %d must not be negative (0 means unlimited)", where, f.Count)
	}

	if t == faults.Delay {
		if f.Delay == "" {
			return fmt.Errorf("%s: a delay fault requires delay (for example 500ms)", where)
		}
		d, err := time.ParseDuration(f.Delay)
		if err != nil || d <= 0 {
			return fmt.Errorf("%s: delay %q is not a positive duration", where, f.Delay)
		}
		return nil
	}

	// Anything other than a delay fault ignores the field, so accepting it
	// would leave the author believing they had injected latency.
	if f.Delay != "" {
		return fmt.Errorf("%s: delay %q is only meaningful for a delay fault, not %s", where, f.Delay, f.Type)
	}
	return nil
}

// requireAddress resolves the value the way the engine will, so a malformed
// 0x form is a load-time error rather than one that stops a run halfway
// through, with clients already watching the chain.
func requireAddress(where, field, value string) error {
	if err := requireBareName(where, field, value); err != nil {
		return err
	}
	if _, err := resolveAddress(value); err != nil {
		return fmt.Errorf("%s: %s: %w", where, field, err)
	}
	return nil
}

// requireBareName rejects an empty value and one carrying surrounding
// whitespace, which YAML preserves inside quotes and which would break the
// name-based lookups this file relies on.
func requireBareName(where, field, value string) error {
	if value == "" {
		return fmt.Errorf("%s: %s is required", where, field)
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%s: %s %q must not have surrounding whitespace", where, field, value)
	}
	return nil
}

func validateValue(where, value string) error {
	if value == "" {
		return fmt.Errorf("%s: value is required", where)
	}
	v, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return fmt.Errorf("%s: value %q is not a base-10 integer", where, value)
	}
	if v.Sign() < 0 {
		return fmt.Errorf("%s: value %q is negative", where, value)
	}
	return nil
}
