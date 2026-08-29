package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/ChickenBenny/Gaslight/internal/rpc"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestWS starts a server and dials it over WebSocket, returning the client
// connection and the driver so tests can advance the chain.
func newTestWS(t *testing.T) (*websocket.Conn, *chain.Driver) {
	t.Helper()
	d := chain.NewDriver(1)
	srv := httptest.NewServer(NewServer(rpc.New(d, 1), d))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if resp != nil {
		resp.Body.Close()
	}
	t.Cleanup(func() { conn.Close() })
	return conn, d
}

// call sends one JSON-RPC message and reads the single response.
func call(t *testing.T, conn *websocket.Conn, req string) testResp {
	t.Helper()
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(req)))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))

	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)

	var r testResp
	require.NoError(t, json.Unmarshal(msg, &r))
	assert.Equal(t, "2.0", r.JSONRPC)
	return r
}

func TestWSSingleRequest(t *testing.T) {
	conn, d := newTestWS(t)
	for i := 0; i < 3; i++ {
		d.ProduceBlock(nil)
	}

	r := call(t, conn, `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`)
	require.Nil(t, r.Error)
	assert.Equal(t, `"0x3"`, string(r.Result))
	assert.Equal(t, "1", string(r.ID))
}

// The read loop keeps serving: several requests over one connection, and the
// chain advancing between them is reflected in later replies.
func TestWSSequentialRequestsOnOneConnection(t *testing.T) {
	conn, d := newTestWS(t)

	r := call(t, conn, `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`)
	assert.Equal(t, `"0x0"`, string(r.Result))

	d.ProduceBlock(nil)
	r = call(t, conn, `{"jsonrpc":"2.0","id":2,"method":"eth_blockNumber"}`)
	assert.Equal(t, `"0x1"`, string(r.Result))

	r = call(t, conn, `{"jsonrpc":"2.0","id":3,"method":"eth_chainId"}`)
	assert.Equal(t, `"0x1"`, string(r.Result))
	assert.Equal(t, "3", string(r.ID))
}

func TestWSBatchRequest(t *testing.T) {
	conn, d := newTestWS(t)
	d.ProduceBlock(nil)

	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`[
		{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"},
		{"jsonrpc":"2.0","id":2,"method":"eth_chainId"}
	]`)))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))

	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)

	var arr []testResp
	require.NoError(t, json.Unmarshal(msg, &arr))
	require.Len(t, arr, 2)
	for _, r := range arr {
		require.Nil(t, r.Error)
	}
}

func TestWSErrorsMatchHTTP(t *testing.T) {
	conn, _ := newTestWS(t)

	cases := map[string]int{
		`{"jsonrpc":"2.0","id":1,"method":"eth_nope"}`: -32601,
		`{not json`: -32700,
	}
	for req, wantCode := range cases {
		r := call(t, conn, req)
		require.NotNil(t, r.Error, "req=%s", req)
		assert.Equal(t, wantCode, r.Error.Code, "req=%s", req)
	}
}

// One endpoint serves both protocols: a plain POST is still handled as HTTP
// while WebSocket upgrades are routed to the WS path.
func TestHTTPAndWSShareOneEndpoint(t *testing.T) {
	d := chain.NewDriver(1)
	srv := httptest.NewServer(NewServer(rpc.New(d, 1), d))
	defer srv.Close()
	d.ProduceBlock(nil)

	resp, err := http.Post(srv.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var httpResp testResp
	require.NoError(t, json.Unmarshal(readBody(t, resp), &httpResp))
	require.Nil(t, httpResp.Error)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, wsHandshake, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if wsHandshake != nil {
		wsHandshake.Body.Close()
	}
	defer conn.Close()

	wsResp := call(t, conn, `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`)
	require.Nil(t, wsResp.Error)
	assert.Equal(t, string(httpResp.Result), string(wsResp.Result), "both protocols must agree")
}

// Closing the client connection must let the read loop exit instead of leaking.
func TestWSClientDisconnectEndsReadLoop(t *testing.T) {
	conn, _ := newTestWS(t)

	call(t, conn, `{"jsonrpc":"2.0","id":1,"method":"eth_chainId"}`)
	require.NoError(t, conn.Close())

	// Nothing to assert on the server directly here; -race plus the leak check
	// in later slices covers it. A second write must fail now.
	err := conn.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","id":2,"method":"eth_chainId"}`))
	assert.Error(t, err)
}

// --- eth_subscribe("newHeads") ---

type wireNotification struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int   `json:"id"`
	Method  string `json:"method"`
	Params  struct {
		Subscription string `json:"subscription"`
		Result       struct {
			Number     string `json:"number"`
			Hash       string `json:"hash"`
			ParentHash string `json:"parentHash"`
			Timestamp  string `json:"timestamp"`
		} `json:"result"`
	} `json:"params"`
}

func subscribe(t *testing.T, conn *websocket.Conn, kind string) testResp {
	t.Helper()
	return call(t, conn, `{"jsonrpc":"2.0","id":1,"method":"eth_subscribe","params":["`+kind+`"]}`)
}

// recvNotification reads one pushed notification.
func recvNotification(t *testing.T, conn *websocket.Conn) wireNotification {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)

	var n wireNotification
	require.NoError(t, json.Unmarshal(msg, &n))
	return n
}

func TestWSSubscribeReturnsID(t *testing.T) {
	conn, _ := newTestWS(t)

	r := subscribe(t, conn, "newHeads")
	require.Nil(t, r.Error)
	assert.Equal(t, `"0x1"`, string(r.Result))

	// A second subscription on the same connection gets its own id.
	r2 := call(t, conn, `{"jsonrpc":"2.0","id":2,"method":"eth_subscribe","params":["newHeads"]}`)
	require.Nil(t, r2.Error)
	assert.Equal(t, `"0x2"`, string(r2.Result))
}

func TestWSSubscribeRejectsUnsupportedKind(t *testing.T) {
	conn, _ := newTestWS(t)

	r := subscribe(t, conn, "logs")
	require.NotNil(t, r.Error)
	assert.Equal(t, -32602, r.Error.Code)
}

// A produced block is pushed as an eth_subscription notification: no id, the
// subscription id we were given, and a header carrying parentHash.
func TestWSPushesNewHeads(t *testing.T) {
	conn, d := newTestWS(t)
	require.Nil(t, subscribe(t, conn, "newHeads").Error)

	want := d.ProduceBlock(nil)

	n := recvNotification(t, conn)
	assert.Equal(t, "2.0", n.JSONRPC)
	assert.Equal(t, "eth_subscription", n.Method)
	assert.Nil(t, n.ID, "a notification must not carry an id")
	assert.Equal(t, "0x1", n.Params.Subscription)
	assert.Equal(t, "0x1", n.Params.Result.Number)
	assert.NotEmpty(t, n.Params.Result.Hash)
	assert.NotEmpty(t, n.Params.Result.ParentHash)
	assert.Len(t, n.Params.Result.Hash, 66, "block hash is data hex: 0x + 64 chars")
	assert.Equal(t, want.Number, uint64(1))
}

func TestWSPushesEveryHeadInOrder(t *testing.T) {
	conn, d := newTestWS(t)
	require.Nil(t, subscribe(t, conn, "newHeads").Error)

	for i := 0; i < 3; i++ {
		d.ProduceBlock(nil)
	}

	for want := 1; want <= 3; want++ {
		n := recvNotification(t, conn)
		assert.Equal(t, encodeQuantity(uint64(want)), n.Params.Result.Number)
	}
}

// After a reorg the pushed head belongs to the new branch, and its parentHash
// is one the client never saw — that mismatch is how a client detects a reorg.
func TestWSPushesReorgedHead(t *testing.T) {
	conn, d := newTestWS(t)
	require.Nil(t, subscribe(t, conn, "newHeads").Error)

	for i := 0; i < 3; i++ {
		d.ProduceBlock(nil)
	}
	var last wireNotification
	for i := 0; i < 3; i++ {
		last = recvNotification(t, conn)
	}
	oldHead := last.Params.Result.Hash

	require.NoError(t, d.Reorg(1, make([][]chain.Tx, 4))) // new head at height 5

	n := recvNotification(t, conn)
	assert.Equal(t, "0x5", n.Params.Result.Number)
	assert.NotEqual(t, oldHead, n.Params.Result.Hash)
	assert.NotEqual(t, oldHead, n.Params.Result.ParentHash, "parentHash must not chain off the orphaned head")
}

func TestWSUnsubscribeStopsPushes(t *testing.T) {
	conn, d := newTestWS(t)
	require.Nil(t, subscribe(t, conn, "newHeads").Error)

	d.ProduceBlock(nil)
	recvNotification(t, conn)

	r := call(t, conn, `{"jsonrpc":"2.0","id":2,"method":"eth_unsubscribe","params":["0x1"]}`)
	require.Nil(t, r.Error)
	assert.Equal(t, "true", string(r.Result))

	d.ProduceBlock(nil)

	// Nothing should arrive now; a short deadline must time out.
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
	_, _, err := conn.ReadMessage()
	assert.Error(t, err, "no push expected after unsubscribe")
}

func TestWSUnsubscribeUnknownIDReturnsFalse(t *testing.T) {
	conn, _ := newTestWS(t)

	r := call(t, conn, `{"jsonrpc":"2.0","id":1,"method":"eth_unsubscribe","params":["0x99"]}`)
	require.Nil(t, r.Error)
	assert.Equal(t, "false", string(r.Result))
}

// Regular RPC still works while a subscription is pushing: both the read loop
// and the pump write to the same connection, so writes must be serialised.
func TestWSRPCWorksAlongsideSubscription(t *testing.T) {
	conn, d := newTestWS(t)
	require.Nil(t, subscribe(t, conn, "newHeads").Error)

	d.ProduceBlock(nil)
	n := recvNotification(t, conn)
	require.Equal(t, "0x1", n.Params.Result.Number)

	r := call(t, conn, `{"jsonrpc":"2.0","id":2,"method":"eth_blockNumber"}`)
	require.Nil(t, r.Error)
	assert.Equal(t, `"0x1"`, string(r.Result))
}

// Two subscriptions on one connection each get their own stream.
func TestWSTwoSubscriptionsBothPush(t *testing.T) {
	conn, d := newTestWS(t)
	require.Nil(t, subscribe(t, conn, "newHeads").Error)
	require.Nil(t, call(t, conn, `{"jsonrpc":"2.0","id":2,"method":"eth_subscribe","params":["newHeads"]}`).Error)

	d.ProduceBlock(nil)

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		n := recvNotification(t, conn)
		assert.Equal(t, "0x1", n.Params.Result.Number)
		seen[n.Params.Subscription] = true
	}
	assert.True(t, seen["0x1"] && seen["0x2"], "both subscriptions should receive the head, got %v", seen)
}

func encodeQuantity(v uint64) string {
	return "0x" + strconv.FormatUint(v, 16)
}

// --- goroutine lifecycle ---

// waitForGoroutines polls until the count drops to at most want, so the test
// tolerates the scheduler taking a moment to reap finished goroutines.
func waitForGoroutines(t *testing.T, want int, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	got := runtime.NumGoroutine()
	for time.Now().Before(deadline) {
		if got <= want {
			return got
		}
		time.Sleep(10 * time.Millisecond)
		got = runtime.NumGoroutine()
	}
	return got
}

// Every subscription starts a pump goroutine; closing the connection must stop
// all of them, otherwise each disconnected client leaks a goroutine forever.
//
// No block is produced after the connections close: a later head would wake the
// pumps and let them exit on a failed write, masking a missing cleanup.
func TestWSSubscriptionsDoNotLeakGoroutines(t *testing.T) {
	d := chain.NewDriver(1)
	srv := httptest.NewServer(NewServer(rpc.New(d, 1), d))
	defer srv.Close()

	time.Sleep(100 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conns := make([]*websocket.Conn, 0, 5)
	for i := 0; i < 5; i++ {
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err)
		if resp != nil {
			resp.Body.Close()
		}
		conns = append(conns, conn)

		require.Nil(t, subscribe(t, conn, "newHeads").Error)
		require.Nil(t, call(t, conn, `{"jsonrpc":"2.0","id":2,"method":"eth_subscribe","params":["newHeads"]}`).Error)
	}

	for _, conn := range conns {
		require.NoError(t, conn.Close())
	}

	got := waitForGoroutines(t, baseline+2, 3*time.Second)
	assert.LessOrEqual(t, got, baseline+2,
		"goroutines leaked after closing subscribed connections (baseline %d, got %d)", baseline, got)
}

// Unsubscribing releases the pump without closing the connection.
func TestWSUnsubscribeReleasesPump(t *testing.T) {
	conn, d := newTestWS(t)

	before := runtime.NumGoroutine()
	require.Nil(t, subscribe(t, conn, "newHeads").Error)
	d.ProduceBlock(nil)
	recvNotification(t, conn)

	r := call(t, conn, `{"jsonrpc":"2.0","id":2,"method":"eth_unsubscribe","params":["0x1"]}`)
	require.Equal(t, "true", string(r.Result))

	got := waitForGoroutines(t, before, 2*time.Second)
	assert.LessOrEqual(t, got, before+1, "pump goroutine should exit after unsubscribe")
}

// --- review follow-ups ---

// A subscriber the chain drops for falling behind must be told: the connection
// is closed with a policy-violation frame instead of going silently dead.
func TestWSSlowSubscriberIsToldItWasDropped(t *testing.T) {
	conn, d := newTestWS(t)
	require.Nil(t, subscribe(t, conn, "newHeads").Error)

	// Stop reading and overflow the chain's per-subscriber buffer.
	for i := 0; i < 200; i++ {
		d.ProduceBlock(nil)
	}

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	var closeErr error
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			closeErr = err
			break
		}
	}
	assert.True(t, websocket.IsCloseError(closeErr, websocket.ClosePolicyViolation),
		"expected a policy-violation close frame, got %v", closeErr)
}

// Shutdown must close hijacked WebSocket connections; http.Server never does.
func TestShutdownClosesWebSocketConnections(t *testing.T) {
	d := chain.NewDriver(1)
	s := NewServer(rpc.New(d, 1), d)
	srv := httptest.NewServer(s)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if resp != nil {
		resp.Body.Close()
	}
	defer conn.Close()
	require.Nil(t, subscribe(t, conn, "newHeads").Error)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, s.Shutdown(ctx))

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, _, err = conn.ReadMessage()
	assert.Error(t, err, "connection should be closed by Shutdown")
}

// Shutdown before Start must not panic or block.
func TestShutdownBeforeStart(t *testing.T) {
	d := chain.NewDriver(1)
	s := NewServer(rpc.New(d, 1), d)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	assert.NoError(t, s.Shutdown(ctx))
}

// Start and Shutdown race in main.go (Start runs on its own goroutine), so the
// server must be safe to shut down while it is still coming up. Run with -race.
func TestStartShutdownNoRace(t *testing.T) {
	d := chain.NewDriver(1)
	s := NewServer(rpc.New(d, 1), d)

	done := make(chan error, 1)
	go func() { done <- s.Start("127.0.0.1:0") }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, s.Shutdown(ctx))

	select {
	case err := <-done:
		assert.True(t, err == nil || errors.Is(err, http.ErrServerClosed), "unexpected serve error: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after Shutdown")
	}
}

// The WS path enforces the same size cap as HTTP.
func TestWSRejectsOversizedMessage(t *testing.T) {
	conn, _ := newTestWS(t)

	huge := strings.Repeat("a", maxRequestBody+1)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":["`+huge+`"]}`)))

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, _, err := conn.ReadMessage()
	assert.Error(t, err, "oversized frame should close the connection")
}
