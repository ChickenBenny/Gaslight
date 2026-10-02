package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/ChickenBenny/Gaslight/internal/faults"
	"github.com/ChickenBenny/Gaslight/internal/rpc"
	"github.com/ChickenBenny/Gaslight/internal/scenario"
	"github.com/ChickenBenny/Gaslight/internal/transport"
	"github.com/ChickenBenny/Gaslight/internal/version"
)

const (
	shutdownTimeout = 5 * time.Second

	// A scenario cannot run at the flag's zero default, which means "never
	// produce a block", so one is chosen rather than failing after the server
	// has already started listening.
	defaultScenarioBlockTime = time.Second
)

// flagGiven reports whether a flag was set on the command line, as opposed to
// carrying its default.
func flagGiven(name string) bool {
	given := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			given = true
		}
	})
	return given
}

func main() {
	addr := flag.String("addr", ":8545", "listen address")
	chainID := flag.Uint64("chain-id", 1, "chain id")
	blockTime := flag.Duration("block-time", 0, "produce a block every interval (0 = never; a scenario defaults to 1s)")
	scenarioPath := flag.String("scenario", "", "run a scenario file instead of producing empty blocks")
	flag.Parse()

	var sc *scenario.Scenario
	if *scenarioPath != "" {
		var err error
		sc, err = scenario.Load(*scenarioPath)
		if err != nil {
			log.Printf("%v", err)
			os.Exit(1)
		}
	}
	// A scenario names the chain it simulates, and Parse defaults a missing
	// chain_id to 1, so the file would always win. An explicitly given flag
	// beats it, because a client configured for one id cannot talk to a node
	// answering another.
	id := *chainID
	if sc != nil && !flagGiven("chain-id") {
		id = sc.ChainID
	}
	if sc != nil && id != sc.ChainID {
		log.Printf("serving chain id %d, overriding the scenario's %d", id, sc.ChainID)
	}

	if sc != nil && *blockTime <= 0 {
		*blockTime = defaultScenarioBlockTime
		log.Printf("no block time given, running the scenario at %s", *blockTime)
	}

	d := chain.NewDriver(id, sc.DriverOptions()...)
	reg := faults.NewRegistry()
	srv := transport.NewServer(rpc.New(d, id, reg), d)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("gaslight %s listening on %s (chain id %d)", version.Version, *addr, id)
		if err := srv.Start(*addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	scenarioErr := make(chan error, 1)
	switch {
	case sc != nil:
		log.Printf("running scenario %q (block time %s)", sc.Name, *blockTime)
		go func() {
			err := scenario.NewEngine(sc, d, reg).Run(ctx, *blockTime)
			switch {
			case err == nil:
				log.Print("scenario complete; the chain now stands still")
			case errors.Is(err, context.Canceled):
				// shutting down, not a failure
			default:
				scenarioErr <- err
			}
		}()
	case *blockTime > 0:
		log.Printf("producing a block every %s", *blockTime)
		go produceBlocks(ctx, d, *blockTime)
	}

	select {
	case <-ctx.Done():
		log.Print("shutting down")
	case err := <-serveErr:
		if err != nil {
			log.Printf("server error: %v", err)
			os.Exit(1) // a listen failure must not look like a clean exit
		}
	case err := <-scenarioErr:
		log.Printf("scenario: %v", err)
		os.Exit(1) // a scenario that could not be performed is not a passing test
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
		os.Exit(1)
	}
}

func produceBlocks(ctx context.Context, d *chain.Driver, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.ProduceBlock(nil)
		case <-ctx.Done():
			return
		}
	}
}
