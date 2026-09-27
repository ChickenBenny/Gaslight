package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/ChickenBenny/Gaslight/internal/faults"
	"github.com/ChickenBenny/Gaslight/internal/rpc"
	"github.com/ChickenBenny/Gaslight/internal/scenario"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const examplesDir = "../../examples"

// stack is what main wires together, driven by Step rather than a clock so the
// assertions are about the scenario rather than about timing.
type stack struct {
	t      *testing.T
	engine *scenario.Engine
	rpc    *rpc.Handler
}

func newStack(t *testing.T, path string) (*stack, *scenario.Scenario) {
	t.Helper()

	s, err := scenario.Load(path)
	require.NoError(t, err, "the shipped example must load")

	d := chain.NewDriver(s.ChainID)
	reg := faults.NewRegistry()
	return &stack{
		t:      t,
		engine: scenario.NewEngine(s, d, reg),
		rpc:    rpc.New(d, s.ChainID, reg),
	}, s
}

// call makes a real JSON-RPC request and returns the decoded result, so these
// tests see exactly what a client on the socket would see.
func (s *stack) call(method string, params ...any) json.RawMessage {
	s.t.Helper()

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	require.NoError(s.t, err)

	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(s.t, json.Unmarshal(s.rpc.ServeRPC(context.Background(), body), &resp))
	require.Nilf(s.t, resp.Error, "%s returned an error", method)
	return resp.Result
}

type wireBlock struct {
	Number       string   `json:"number"`
	Hash         string   `json:"hash"`
	Transactions []string `json:"transactions"`
}

func (s *stack) blockByNumber(height string) wireBlock {
	s.t.Helper()
	var b wireBlock
	require.NoError(s.t, json.Unmarshal(s.call("eth_getBlockByNumber", height, false), &b))
	return b
}

// stepTo advances the scenario until the chain reports the wanted height,
// failing rather than hanging if the scenario ends first.
func (s *stack) stepTo(height string) {
	s.t.Helper()
	for i := 0; i < 1000; i++ {
		var got string
		require.NoError(s.t, json.Unmarshal(s.call("eth_blockNumber"), &got))
		if got == height {
			return
		}
		more, err := s.engine.Step()
		require.NoError(s.t, err)
		require.Truef(s.t, more, "the scenario ended before reaching height %s", height)
	}
	s.t.Fatalf("height %s was never reached", height)
}

func (s *stack) drain() {
	s.t.Helper()
	for i := 0; i < 1000; i++ {
		more, err := s.engine.Step()
		require.NoError(s.t, err)
		if !more {
			return
		}
	}
	s.t.Fatal("the scenario never finished")
}

// Every shipped example has to load and run to completion, so a broken one is
// a failing build rather than something a user discovers.
func TestExamplesRunToCompletion(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(examplesDir, "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "there should be examples to run")

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			s, _ := newStack(t, path)
			s.drain()
		})
	}
}

// The flagship scenario, seen the way a deposit watcher sees it: a transaction
// that is on chain, confirmed, and then gone — while the block that carried it
// is still reachable by hash, exactly as a real node would report it.
func TestExampleReorgEatsADeposit(t *testing.T) {
	s, sc := newStack(t, filepath.Join(examplesDir, "reorg-eats-a-deposit.yaml"))
	assert.Equal(t, uint64(1337), sc.ChainID, "the example sets its own chain id")

	s.stepTo("0x3")

	deposit := s.blockByNumber("0x3")
	require.Len(t, deposit.Transactions, 1, "the deposit rides in block 3")
	txHash := deposit.Transactions[0]

	var receipt struct {
		Status      string `json:"status"`
		BlockNumber string `json:"blockNumber"`
	}
	require.NoError(t, json.Unmarshal(s.call("eth_getTransactionReceipt", txHash), &receipt))
	assert.Equal(t, "0x1", receipt.Status)
	assert.Equal(t, "0x3", receipt.BlockNumber, "a client would credit this deposit")

	s.drain()

	// Height 3 is now a different block, and it does not carry the deposit.
	replaced := s.blockByNumber("0x3")
	assert.NotEqual(t, deposit.Hash, replaced.Hash, "the reorg replaced block 3")
	assert.Empty(t, replaced.Transactions, "the new branch omits the deposit")

	// The receipt is gone, which is the ambiguity the scenario exists to test:
	// null reads the same as "still pending".
	assert.JSONEq(t, "null", string(s.call("eth_getTransactionReceipt", txHash)),
		"the credited deposit has no receipt any more")

	// The orphaned block stays reachable by hash, as on a real node, so a
	// client holding the old hash is not told the block never existed.
	var orphan wireBlock
	require.NoError(t, json.Unmarshal(s.call("eth_getBlockByHash", deposit.Hash, false), &orphan))
	assert.Equal(t, []string{txHash}, orphan.Transactions, "the orphan still carries the deposit")
}

// The fault has to be visible: here the transaction stays on chain throughout,
// so a null answer can only be the node lying.
func TestExampleNodeLiesAboutAReceipt(t *testing.T) {
	s, _ := newStack(t, filepath.Join(examplesDir, "node-lies-about-a-receipt.yaml"))

	s.stepTo("0x2")
	txHash := s.blockByNumber("0x2").Transactions[0]
	s.drain()

	require.Equal(t, "0x2", s.blockByNumber("0x2").Number)
	assert.Len(t, s.blockByNumber("0x2").Transactions, 1, "the transaction never left the chain")

	// count: 2, so the first two calls are denied and the third tells the truth.
	for i := 1; i <= 4; i++ {
		got := string(s.call("eth_getTransactionReceipt", txHash))
		if i <= 2 {
			assert.JSONEqf(t, "null", got, "call %d should have been denied", i)
			continue
		}
		assert.Containsf(t, got, `"blockNumber":"0x2"`, "call %d should tell the truth", i)
	}
}

func TestMain(m *testing.M) {
	if _, err := os.Stat(examplesDir); err != nil {
		fmt.Fprintf(os.Stderr, "examples directory not found: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
