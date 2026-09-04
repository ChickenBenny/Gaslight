package faults

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ChickenBenny/Gaslight/internal/rpc"
)

type Type string

const (
	FalseNull Type = "false_200"
	Delay     Type = "delay"
)

// AllMethods matches every method served by the rpc handler. Note that the
// transport answers eth_subscribe and eth_unsubscribe itself, so those never
// reach a fault — not even a wildcard one.
const AllMethods = "*"

// ErrUnknownType is returned for a Type that is neither FalseNull nor Delay.
// Silently keeping such a fault would be the worst outcome for a chaos tool:
// the run stays green and "the client survived" is indistinguishable from
// "the fault never fired".
var ErrUnknownType = errors.New("faults: unknown fault type")

func (t Type) valid() bool { return t == FalseNull || t == Delay }

// ParseType converts a wire value (a scenario file, a flag) into a Type.
func ParseType(s string) (Type, error) {
	t := Type(s)
	if !t.valid() {
		return "", fmt.Errorf("%w: %q", ErrUnknownType, s)
	}
	return t, nil
}

type Fault struct {
	Method    string
	Type      Type
	Delay     time.Duration
	remaining atomic.Int64
}

// NewFault builds a fault. count <= 0 means it fires indefinitely.
func NewFault(method string, t Type, count int, delay time.Duration) *Fault {
	if count <= 0 {
		count = -1
	}
	f := &Fault{
		Method: method,
		Type:   t,
		Delay:  delay,
	}
	f.remaining.Store(int64(count))
	return f
}

func (f *Fault) spent() bool { return f.remaining.Load() == 0 }

func (f *Fault) consume() bool {
	for {
		n := f.remaining.Load()
		if n < 0 {
			return true
		}
		if n == 0 {
			return false
		}
		if f.remaining.CompareAndSwap(n, n-1) {
			return true
		}
	}
}

// Registry holds the faults currently in effect. Its two kinds of state are
// guarded differently by write frequency: the list changes rarely (a scenario
// enabling faults) and takes a mutex, while each fault's remaining count is
// decremented on every request and uses an atomic instead.
//
// A nil *Registry is a working no-op, so callers may hold one unconditionally.
type Registry struct {
	mu     sync.RWMutex
	faults []*Fault
}

func NewRegistry() *Registry {
	return &Registry{}
}

// Enable puts a fault into effect, rejecting an unrecognised type. Faults that
// have used up their count are reaped here so a long scenario does not keep
// walking over them.
func (r *Registry) Enable(f *Fault) error {
	if r == nil {
		return nil
	}
	if f == nil {
		return nil
	}
	if !f.Type.valid() {
		return fmt.Errorf("%w: %q", ErrUnknownType, f.Type)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	live := r.faults[:0]
	for _, existing := range r.faults {
		if !existing.spent() {
			live = append(live, existing)
		}
	}
	r.faults = append(live, f)
	return nil
}

func (r *Registry) Clear() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.faults = nil
}

// Before runs on the request path. Overlapping delays do not add up: the
// longest matching delay wins, and only that fault spends a unit of its count.
func (r *Registry) Before(method string) {
	if r == nil {
		return
	}

	var longest *Fault
	for _, f := range r.matching(method, Delay) {
		if longest == nil || f.Delay > longest.Delay {
			longest = f
		}
	}
	if longest != nil && longest.consume() {
		time.Sleep(longest.Delay)
	}
}

// After runs on the response path and is where false_200 turns a successful
// answer into a null. A call that already failed, or that honestly had nothing
// to return, is left alone — and crucially does not spend any budget, or a
// "lie once" fault would be used up by the legitimate nulls a client sees while
// a transaction is still pending.
func (r *Registry) After(method string, result any, err *rpc.RPCError) (any, *rpc.RPCError) {
	if r == nil || err != nil || result == nil {
		return result, err
	}
	for _, f := range r.matching(method, FalseNull) {
		if f.consume() {
			return nil, nil
		}
	}
	return result, err
}

func (r *Registry) matching(method string, t Type) []*Fault {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Fault
	for _, f := range r.faults {
		if f.Type != t {
			continue
		}
		if f.Method != AllMethods && f.Method != method {
			continue
		}
		out = append(out, f)
	}
	return out
}
