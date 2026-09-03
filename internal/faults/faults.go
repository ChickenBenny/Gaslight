package faults

import (
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

const AllMethods = "*"

type Fault struct {
	Method    string
	Type      Type
	Delay     time.Duration
	remaining atomic.Int64
}

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

type Registry struct {
	mu     sync.RWMutex
	faults []*Fault
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Enable(f *Fault) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.faults = append(r.faults, f)
}

func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.faults = nil
}

func (r *Registry) Before(method string) {
	for _, f := range r.matching(method, Delay) {
		if f.consume() {
			time.Sleep(f.Delay)
		}
	}
}

func (r *Registry) After(method string, result any, err *rpc.RPCError) (any, *rpc.RPCError) {
	if err != nil {
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
