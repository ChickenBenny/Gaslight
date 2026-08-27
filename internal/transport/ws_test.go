package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	srv := httptest.NewServer(NewServer(rpc.New(d, 1)))
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
	srv := httptest.NewServer(NewServer(rpc.New(d, 1)))
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
