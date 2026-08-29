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
	"github.com/ChickenBenny/Gaslight/internal/rpc"
	"github.com/ChickenBenny/Gaslight/internal/transport"
	"github.com/ChickenBenny/Gaslight/internal/version"
)

const shutdownTimeout = 5 * time.Second

func main() {
	addr := flag.String("addr", ":8545", "listen address")
	chainID := flag.Uint64("chain-id", 1, "chain id")
	blockTime := flag.Duration("block-time", 0, "produce a block every interval (0 = never)")
	flag.Parse()

	d := chain.NewDriver(*chainID)
	srv := transport.NewServer(rpc.New(d, *chainID), d)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("gaslight %s listening on %s (chain id %d)", version.Version, *addr, *chainID)
		if err := srv.Start(*addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	if *blockTime > 0 {
		log.Printf("producing a block every %s", *blockTime)
		go produceBlocks(ctx, d, *blockTime)
	}

	select {
	case <-ctx.Done():
		log.Print("shutting down")
	case err := <-serveErr:
		if err != nil {
			log.Printf("server error: %v", err)
		}
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
