package scenario

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/ChickenBenny/Gaslight/internal/faults"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDriver records what the engine asked the chain to do. The engine's
// contract is exactly that sequence of requests, so this — rather than
// anything the engine reports about itself — is what the tests assert on.
type fakeDriver struct {
	calls  []string
	height uint64

	// txsSeen keeps the transactions themselves, so a test can compare the tx
	// a reorg re-included against the one originally produced.
	txsSeen []seenTx
}

type seenTx struct {
	where string // "produce" or "reorg"
	tx    chain.Tx
}

func (f *fakeDriver) ProduceBlock(txs []chain.Tx) *chain.Block {
	f.height++
	f.calls = append(f.calls, fmt.Sprintf("produce(%s)", txIDs(txs)))
	for _, tx := range txs {
		f.txsSeen = append(f.txsSeen, seenTx{where: "produce", tx: tx})
	}
	return &chain.Block{Number: f.height}
}

func (f *fakeDriver) Reorg(forkFrom uint64, branch [][]chain.Tx) error {
	f.height = forkFrom + uint64(len(branch))
	f.calls = append(f.calls, fmt.Sprintf("reorg(from=%d, branch=%s)", forkFrom, fmtBranch(branch)))
	for _, txs := range branch {
		for _, tx := range txs {
			f.txsSeen = append(f.txsSeen, seenTx{where: "reorg", tx: tx})
		}
	}
	return nil
}

func (f *fakeDriver) Finalize(height uint64) error {
	f.calls = append(f.calls, fmt.Sprintf("finalize(%d)", height))
	return nil
}

// take returns the calls since the previous take, which is how a test sees
// what one Step did rather than only what the whole run did.
func (f *fakeDriver) take() []string {
	c := f.calls
	f.calls = nil
	return c
}

type fakeRegistry struct{ calls []string }

func (r *fakeRegistry) Enable(f *faults.Fault) error {
	r.calls = append(r.calls, fmt.Sprintf("enable(%s, %s, delay=%v)", f.Method, f.Type, f.Delay))
	return nil
}

func (r *fakeRegistry) Clear() { r.calls = append(r.calls, "clear()") }

func (r *fakeRegistry) take() []string {
	c := r.calls
	r.calls = nil
	return c
}

func txIDs(txs []chain.Tx) string {
	ids := make([]string, 0, len(txs))
	for _, tx := range txs {
		ids = append(ids, tx.ID)
	}
	return strings.Join(ids, ",")
}

// fmtBranch shows one slot per branch position, so an assertion reads as the
// shape of the new chain: [_,dep,_] is "second block carries dep".
func fmtBranch(branch [][]chain.Tx) string {
	slots := make([]string, len(branch))
	for i, txs := range branch {
		if len(txs) == 0 {
			slots[i] = "_"
			continue
		}
		slots[i] = txIDs(txs)
	}
	return "[" + strings.Join(slots, ",") + "]"
}

func mustParse(t *testing.T, yaml string) *Scenario {
	t.Helper()
	s, err := Parse([]byte(yaml))
	require.NoError(t, err)
	return s
}

// newFakes builds an engine over recorders, and returns them for assertions.
func newFakes(t *testing.T, yaml string) (*Engine, *fakeDriver, *fakeRegistry) {
	t.Helper()
	d, r := &fakeDriver{}, &fakeRegistry{}
	return NewEngine(mustParse(t, yaml), d, r), d, r
}

const flagshipEngine = `
name: reorg-eats-a-deposit
timeline:
  - at_height: 3
    produce:
      txs: [{id: dep, from: alice, to: exchange, value: "1000"}]
  - at_height: 5
    reorg:
      fork_from: 2
      branch_length: 4
  - at_height: 5
    fault:
      method: eth_getTransactionReceipt
      type: false_200
      count: 1
  - at_height: 7
    clear_faults: {}
end_at_height: 8
`

// The whole design of Step is visible here: one call, one observable change.
// Each assertion is what a client watching newHeads would see in that tick.
func TestStepPerformsTheTimelineOneChangeAtATime(t *testing.T) {
	e, d, r := newFakes(t, flagshipEngine)

	// Heights 1 and 2 are gaps the timeline does not mention, so they are
	// filled one empty block per step — never several in one tick.
	for i := 1; i <= 2; i++ {
		more, err := e.Step()
		require.NoError(t, err)
		require.True(t, more)
		assert.Equal(t, []string{"produce()"}, d.take(), "step %d should fill exactly one block", i)
	}

	// The deposit rides in the block at its own at_height, not the one after.
	more, err := e.Step()
	require.NoError(t, err)
	require.True(t, more)
	assert.Equal(t, []string{"produce(dep)"}, d.take())

	// Heights 4 and 5 are gaps again: the reorg has to wait until the chain
	// actually stands where it was written against.
	for i := 4; i <= 5; i++ {
		more, err = e.Step()
		require.NoError(t, err)
		require.True(t, more)
		assert.Equal(t, []string{"produce()"}, d.take(), "height %d should be filled", i)
	}

	// The point of two events sharing a height: no block is produced between
	// them, so the client meets the lie in the same tick it learns the chain
	// changed under it.
	more, err = e.Step()
	require.NoError(t, err)
	require.True(t, more)
	assert.Equal(t, []string{"reorg(from=2, branch=[_,_,_,_])"}, d.take())
	assert.Equal(t, []string{"enable(eth_getTransactionReceipt, false_200, delay=0s)"}, r.take(),
		"the fault must be enabled in the same step as the reorg")

	// The reorg left the chain at 6, so height 7 is one more filled block.
	more, err = e.Step()
	require.NoError(t, err)
	require.True(t, more)
	assert.Equal(t, []string{"produce()"}, d.take())

	more, err = e.Step()
	require.NoError(t, err)
	require.True(t, more)
	assert.Equal(t, []string{"clear()"}, r.take())
	assert.Empty(t, d.take(), "clearing faults does not touch the chain")

	// Nothing is left in the timeline, so the run continues to end_at_height.
	more, err = e.Step()
	require.NoError(t, err)
	require.True(t, more)
	assert.Equal(t, []string{"produce()"}, d.take())

	more, err = e.Step()
	require.NoError(t, err)
	assert.False(t, more, "the scenario is over once end_at_height is reached")
	assert.Empty(t, d.take())
}

// A reorg with no txs is how a transaction is orphaned for good, so the branch
// must reach the driver as empty blocks rather than carrying anything over.
func TestReorgWithoutTxsBuildsAnEmptyBranch(t *testing.T) {
	e, d, _ := newFakes(t, `
name: gone
timeline:
  - at_height: 2
    produce:
      txs: [{id: dep, from: alice, to: exchange, value: "1"}]
  - at_height: 3
    reorg:
      fork_from: 1
      branch_length: 3
`)
	require.NoError(t, drain(e))
	assert.Contains(t, d.calls, "reorg(from=1, branch=[_,_,_])")
}

// Branch blocks are addressed by absolute height in the file and have to land
// in the matching slot of the [][]Tx the driver takes: fork_from 1 means slot
// 0 is height 2, so height 3 is slot 1.
func TestReorgPlacesTxsAtTheRightBranchPosition(t *testing.T) {
	e, d, _ := newFakes(t, `
name: reinclude
timeline:
  - at_height: 2
    produce:
      txs: [{id: dep, from: alice, to: exchange, value: "500"}]
  - at_height: 4
    reorg:
      fork_from: 1
      branch_length: 5
      txs:
        - {at_height: 3, ids: [dep]}
`)
	require.NoError(t, drain(e))
	assert.Contains(t, d.calls, "reorg(from=1, branch=[_,dep,_,_,_])")
}

// A re-included transaction must be the one already built, because the hash
// the driver stamps covers ID/From/To/Value: same fields, same hash, and the
// client sees the deposit it knows rather than a new one.
func TestReorgReusesTheOriginalTx(t *testing.T) {
	e, d, _ := newFakes(t, `
name: reinclude
timeline:
  - at_height: 1
    produce:
      txs: [{id: dep, from: alice, to: exchange, value: "500"}]
  - at_height: 2
    reorg:
      fork_from: 0
      branch_length: 3
      txs:
        - {at_height: 1, ids: [dep]}
`)
	require.NoError(t, drain(e))

	var produced, reincluded chain.Tx
	for _, call := range d.txsSeen {
		if call.where == "produce" {
			produced = call.tx
		} else {
			reincluded = call.tx
		}
	}
	require.NotEmpty(t, produced.ID, "the deposit should have been produced")
	require.NotEmpty(t, reincluded.ID, "the deposit should have been re-included")

	assert.Equal(t, produced.From, reincluded.From)
	assert.Equal(t, produced.To, reincluded.To)
	assert.Zero(t, produced.Value.Cmp(reincluded.Value))
	assert.Equal(t, chain.Hash{}, reincluded.Hash, "the driver still owns the hash")
}

func TestFinalizeAndDelayFaultReachTheirTargets(t *testing.T) {
	e, d, r := newFakes(t, `
name: slow
timeline:
  - at_height: 2
    finalize:
      height: 1
  - at_height: 3
    fault:
      method: "*"
      type: delay
      delay: 250ms
`)
	require.NoError(t, drain(e))

	assert.Contains(t, d.calls, "finalize(1)")
	assert.Equal(t, []string{"enable(*, delay, delay=250ms)"}, r.calls,
		"the delay has to arrive parsed, not as the string from the file")
}

// end_at_height is optional; without it the run stops once the timeline is
// spent rather than producing forever.
func TestRunStopsWhenTheTimelineIsSpentWithoutEndAtHeight(t *testing.T) {
	e, d, _ := newFakes(t, `
name: short
timeline:
  - at_height: 2
    produce:
      txs: [{id: t, from: a, to: b, value: "1"}]
`)
	require.NoError(t, drain(e))
	assert.Equal(t, []string{"produce()", "produce(t)"}, d.calls)
}

// A scenario is a fixed number of steps, and stays that number however often
// it is replayed — the property the reproducibility gate rests on.
func TestSteppingIsDeterministic(t *testing.T) {
	first, d1, r1 := newFakes(t, flagshipEngine)
	second, d2, r2 := newFakes(t, flagshipEngine)

	require.NoError(t, drain(first))
	require.NoError(t, drain(second))

	assert.Equal(t, d1.calls, d2.calls)
	assert.Equal(t, r1.calls, r2.calls)
	assert.Len(t, d1.calls, 8, "the flagship scenario is a fixed number of chain calls")
}

// Run drives Step on a ticker and returns when the scenario is over. A
// cancelled context has to stop it promptly rather than at the next tick.
func TestRunStopsOnContextCancel(t *testing.T) {
	e, _, _ := newFakes(t, `
name: long
timeline:
  - at_height: 1
    finalize: {height: 1}
end_at_height: 100000
`)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx, time.Millisecond) }()

	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the context was cancelled")
	}
}

func TestRunPerformsTheWholeScenario(t *testing.T) {
	e, d, r := newFakes(t, flagshipEngine)

	require.NoError(t, e.Run(context.Background(), time.Millisecond))

	assert.Len(t, d.calls, 8)
	assert.Contains(t, r.calls, "clear()")
}

// The engine's own model of the height is the one validate.go uses, and a
// driver refusing a call means that model has a hole. That is a validation
// bug, so it surfaces named rather than being swallowed.
func TestStepReportsADriverRefusal(t *testing.T) {
	s := mustParse(t, `
name: refused
timeline:
  - at_height: 2
    reorg:
      fork_from: 1
      branch_length: 2
`)
	e := NewEngine(s, &refusingDriver{fakeDriver: &fakeDriver{}}, &fakeRegistry{})

	err := drain(e)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeline[0]", "the failing event has to be named")
	assert.Contains(t, err.Error(), "refused")
}

type refusingDriver struct{ *fakeDriver }

func (r *refusingDriver) Reorg(uint64, [][]chain.Tx) error {
	return fmt.Errorf("refused for the sake of the test")
}

// drain steps until the scenario reports it is finished, with a bound so a
// mistake in Step shows up as a failure rather than a hang.
func drain(e *Engine) error {
	for i := 0; i < 10000; i++ {
		more, err := e.Step()
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
	return fmt.Errorf("Step never reported completion")
}

// time.NewTicker panics on a non-positive interval, and the block-time flag
// defaults to zero, so this is the shape of a misconfigured run rather than a
// theoretical one.
func TestRunRefusesANonPositiveBlockTime(t *testing.T) {
	e, d, _ := newFakes(t, `
name: s
timeline:
  - at_height: 1
    finalize: {height: 1}
`)

	for _, bt := range []time.Duration{0, -time.Second} {
		err := e.Run(context.Background(), bt)
		require.Error(t, err, "block time %v should be refused", bt)
		assert.Contains(t, err.Error(), "block time")
	}
	assert.Empty(t, d.calls, "a refused run must not touch the chain")
}
