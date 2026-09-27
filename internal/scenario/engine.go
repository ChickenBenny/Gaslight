package scenario

import (
	"context"
	"fmt"
	"time"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/ChickenBenny/Gaslight/internal/faults"
)

type ChainDriver interface {
	ProduceBlock(txs []chain.Tx) *chain.Block
	Reorg(forkFrom uint64, branch [][]chain.Tx) error
	Finalize(height uint64) error
}

type FaultRegistry interface {
	Enable(f *faults.Fault) error
	Clear()
}

type Engine struct {
	scenario *Scenario
	chain    ChainDriver
	faults   FaultRegistry
	resolver *resolver

	cursor int
	height uint64
}

func NewEngine(s *Scenario, d ChainDriver, r FaultRegistry) *Engine {
	return &Engine{
		scenario: s,
		chain:    d,
		faults:   r,
		resolver: newResolver(),
	}
}

func (e *Engine) Step() (bool, error) {
	ran, err := e.runReachedEvents()
	if err != nil {
		return false, err
	}
	if ran {
		return true, nil
	}
	return e.produceOneBlock()
}

func (e *Engine) runReachedEvents() (bool, error) {
	ran := false
	for e.cursor < len(e.scenario.Timeline) {
		ev := e.scenario.Timeline[e.cursor]

		if ev.Produce != nil {
			break
		}
		if ev.AtHeight > e.height {
			break
		}

		if err := e.apply(ev); err != nil {
			return false, fmt.Errorf("timeline[%d]: %w", e.cursor, err)
		}
		e.cursor++
		ran = true
	}
	return ran, nil
}

func (e *Engine) produceOneBlock() (bool, error) {
	if e.cursor < len(e.scenario.Timeline) {
		ev := e.scenario.Timeline[e.cursor]
		if ev.Produce != nil && e.height+1 == ev.AtHeight {
			txs, err := e.resolveTxs(ev.Produce.Txs)
			if err != nil {
				return false, fmt.Errorf("timeline[%d]: %w", e.cursor, err)
			}
			e.produce(txs)
			e.cursor++
			return true, nil
		}

		e.produce(nil)
		return true, nil
	}

	if e.height < e.scenario.EndAtHeight {
		e.produce(nil)
		return true, nil
	}
	return false, nil
}

func (e *Engine) produce(txs []chain.Tx) {
	e.chain.ProduceBlock(txs)
	e.height++
}

func (e *Engine) apply(ev Event) error {
	switch {
	case ev.Finalize != nil:
		if err := e.chain.Finalize(ev.Finalize.Height); err != nil {
			return fmt.Errorf("finalize refused: %w", err)
		}

	case ev.Fault != nil:
		f, err := buildFault(ev.Fault)
		if err != nil {
			return err
		}
		if err := e.faults.Enable(f); err != nil {
			return fmt.Errorf("fault refused: %w", err)
		}

	case ev.Reorg != nil:
		branch, err := e.buildBranch(ev.Reorg)
		if err != nil {
			return err
		}
		if err := e.chain.Reorg(ev.Reorg.ForkFrom, branch); err != nil {
			return fmt.Errorf("reorg refused: %w", err)
		}
		e.height = ev.Reorg.ForkFrom + uint64(ev.Reorg.BranchLength)

	case ev.ClearFaults != nil:
		e.faults.Clear()

	default:
		return fmt.Errorf("no action the engine knows how to perform")
	}
	return nil
}

func (e *Engine) buildBranch(r *ReorgAction) ([][]chain.Tx, error) {
	branch := make([][]chain.Tx, r.BranchLength)
	newHead := r.ForkFrom + uint64(r.BranchLength)

	for _, b := range r.Txs {
		if b.AtHeight <= r.ForkFrom || b.AtHeight > newHead {
			return nil, fmt.Errorf("reorg places transactions at height %d, outside the new branch (%d..%d)",
				b.AtHeight, r.ForkFrom+1, newHead)
		}

		txs := make([]chain.Tx, 0, len(b.IDs))
		for _, id := range b.IDs {
			tx, ok := e.resolver.byID(id)
			if !ok {
				return nil, fmt.Errorf("reorg names tx %q, which no earlier produce defined", id)
			}
			txs = append(txs, tx)
		}
		branch[b.AtHeight-r.ForkFrom-1] = txs
	}
	return branch, nil
}

func buildFault(a *FaultAction) (*faults.Fault, error) {
	t, err := faults.ParseType(a.Type)
	if err != nil {
		return nil, fmt.Errorf("fault type %q: %w", a.Type, err)
	}

	var delay time.Duration
	if a.Delay != "" {
		delay, err = time.ParseDuration(a.Delay)
		if err != nil {
			return nil, fmt.Errorf("fault delay %q: %w", a.Delay, err)
		}
	}
	return faults.NewFault(a.Method, t, a.Count, delay), nil
}

func (e *Engine) resolveTxs(specs []TxSpec) ([]chain.Tx, error) {
	txs := make([]chain.Tx, 0, len(specs))
	for _, spec := range specs {
		tx, err := e.resolver.resolveTx(spec)
		if err != nil {
			return nil, err
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

func (e *Engine) Run(ctx context.Context, blockTime time.Duration) error {
	if blockTime <= 0 {
		return fmt.Errorf("scenario: a scenario needs a positive block time, got %v", blockTime)
	}

	ticker := time.NewTicker(blockTime)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			more, err := e.Step()
			if err != nil {
				return err
			}
			if !more {
				return nil
			}
		}
	}
}
