package faults

import (
	"sync"
	"testing"
	"time"

	"github.com/ChickenBenny/Gaslight/internal/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const receiptMethod = "eth_getTransactionReceipt"

func truth() (any, *rpc.RPCError) { return "the truth", nil }

// An empty registry never interferes.
func TestRegistryPassesThroughWhenEmpty(t *testing.T) {
	r := NewRegistry()

	result, err := truth()
	result, err = r.After(receiptMethod, result, err)

	assert.Equal(t, "the truth", result)
	assert.Nil(t, err)
}

// false_200 replaces a successful result with nil, which serialises to JSON null.
func TestFalseNullReplacesResult(t *testing.T) {
	r := NewRegistry()
	r.Enable(NewFault(receiptMethod, FalseNull, 0, 0))

	result, err := truth()
	result, err = r.After(receiptMethod, result, err)

	assert.Nil(t, result, "result should be replaced with nil")
	assert.Nil(t, err, "false_200 must not surface an error — the lie is a successful null")
}

// A fault only fires for the method it names.
func TestFaultOnlyAffectsItsMethod(t *testing.T) {
	r := NewRegistry()
	r.Enable(NewFault(receiptMethod, FalseNull, 0, 0))

	result, err := truth()
	result, err = r.After("eth_blockNumber", result, err)

	assert.Equal(t, "the truth", result, "another method must be untouched")
	assert.Nil(t, err)
}

func TestWildcardMatchesEveryMethod(t *testing.T) {
	r := NewRegistry()
	r.Enable(NewFault(AllMethods, FalseNull, 0, 0))

	for _, m := range []string{receiptMethod, "eth_blockNumber", "net_version"} {
		result, err := truth()
		result, err = r.After(m, result, err)
		assert.Nilf(t, result, "wildcard should cover %s", m)
		assert.Nil(t, err)
	}
}

// count=N lies N times and then stops: the "intermittently bad node" shape that
// exercises a client's retry path.
func TestCountLimitsHowOftenAFaultFires(t *testing.T) {
	r := NewRegistry()
	r.Enable(NewFault(receiptMethod, FalseNull, 2, 0))

	for i := range 2 {
		result, err := truth()
		result, err = r.After(receiptMethod, result, err)
		assert.Nilf(t, result, "call %d should be a lie", i+1)
		assert.Nil(t, err)
	}

	result, err := truth()
	result, err = r.After(receiptMethod, result, err)
	assert.Equal(t, "the truth", result, "the fault is spent and must stop firing")
	assert.Nil(t, err)
}

// count=0 means unlimited.
func TestZeroCountIsUnlimited(t *testing.T) {
	r := NewRegistry()
	r.Enable(NewFault(receiptMethod, FalseNull, 0, 0))

	for i := range 50 {
		result, err := truth()
		result, _ = r.After(receiptMethod, result, err)
		assert.Nilf(t, result, "call %d should still lie", i+1)
	}
}

// false_200 is about turning a success into a null; an already failing call is
// left alone so the client still sees the real error.
func TestFalseNullLeavesErrorsAlone(t *testing.T) {
	r := NewRegistry()
	r.Enable(NewFault(receiptMethod, FalseNull, 0, 0))

	rpcErr := rpc.NewInvalidParams("bad")
	result, err := r.After(receiptMethod, nil, rpcErr)

	assert.Nil(t, result)
	assert.Equal(t, rpcErr, err, "an existing error must be passed through untouched")
}

func TestClearRemovesEveryFault(t *testing.T) {
	r := NewRegistry()
	r.Enable(NewFault(receiptMethod, FalseNull, 0, 0))
	r.Clear()

	result, err := truth()
	result, err = r.After(receiptMethod, result, err)
	assert.Equal(t, "the truth", result)
	assert.Nil(t, err)
}

// --- delay ---

func TestDelayWaitsOnTheRequestPath(t *testing.T) {
	r := NewRegistry()
	r.Enable(NewFault(receiptMethod, Delay, 0, 50*time.Millisecond))

	start := time.Now()
	r.Before(receiptMethod)
	assert.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond)
}

func TestDelayDoesNotAffectOtherMethods(t *testing.T) {
	r := NewRegistry()
	r.Enable(NewFault(receiptMethod, Delay, 0, 500*time.Millisecond))

	start := time.Now()
	r.Before("eth_blockNumber")
	assert.Less(t, time.Since(start), 100*time.Millisecond)
}

// Delay is on the request path, so it must not alter the answer.
func TestDelayLeavesTheResultIntact(t *testing.T) {
	r := NewRegistry()
	r.Enable(NewFault(receiptMethod, Delay, 0, time.Millisecond))

	result, err := truth()
	result, err = r.After(receiptMethod, result, err)
	assert.Equal(t, "the truth", result, "delay slows a call down, it does not lie")
	assert.Nil(t, err)
}

// --- concurrency ---

// The remaining-count is decremented on the hot path from many goroutines, so a
// count of N must fire exactly N times in total — never more. Run with -race.
func TestCountIsExactUnderConcurrency(t *testing.T) {
	const budget = 100
	const callers = 8
	const perCaller = 50

	r := NewRegistry()
	r.Enable(NewFault(receiptMethod, FalseNull, budget, 0))

	var mu sync.Mutex
	lies := 0

	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perCaller {
				result, err := truth()
				result, _ = r.After(receiptMethod, result, err)
				if result == nil {
					mu.Lock()
					lies++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, budget, lies, "a count of %d must fire exactly %d times", budget, budget)
}

// Enabling faults while requests are in flight must not race.
func TestEnableConcurrentWithReads(t *testing.T) {
	r := NewRegistry()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 200 {
			r.Enable(NewFault(receiptMethod, FalseNull, 1, 0))
		}
	}()
	go func() {
		defer wg.Done()
		for range 200 {
			result, err := truth()
			_, _ = r.After(receiptMethod, result, err)
		}
	}()
	wg.Wait()
}

// Faults of different types coexist: the delay runs before the call, the
// false-null replaces its result.
func TestDelayAndFalseNullCombine(t *testing.T) {
	r := NewRegistry()
	r.Enable(NewFault(receiptMethod, Delay, 0, 20*time.Millisecond))
	r.Enable(NewFault(receiptMethod, FalseNull, 0, 0))

	start := time.Now()
	r.Before(receiptMethod)
	result, err := truth()
	result, err = r.After(receiptMethod, result, err)

	assert.GreaterOrEqual(t, time.Since(start), 20*time.Millisecond)
	assert.Nil(t, result)
	assert.Nil(t, err)
}

func TestNewFaultRejectsNothingButRecordsItsFields(t *testing.T) {
	f := NewFault(receiptMethod, Delay, 3, 250*time.Millisecond)
	require.NotNil(t, f)
	assert.Equal(t, receiptMethod, f.Method)
	assert.Equal(t, Delay, f.Type)
	assert.Equal(t, 250*time.Millisecond, f.Delay)
}
