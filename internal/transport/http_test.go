package transport

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/ChickenBenny/Gaslight/internal/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// newTestServer spins up a real HTTP server over the transport, returning its
// URL and the driver so tests can advance the chain.
func newTestServer(t *testing.T) (string, *chain.Driver) {
	t.Helper()
	d := chain.NewDriver(1)
	srv := httptest.NewServer(NewServer(rpc.New(d, 1, nil), d))
	t.Cleanup(srv.Close)
	return srv.URL, d
}

func post(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return b
}

func TestHTTPSingleRequest(t *testing.T) {
	url, d := newTestServer(t)
	for i := 0; i < 3; i++ {
		d.ProduceBlock(nil)
	}

	resp := post(t, url, `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var r testResp
	require.NoError(t, json.Unmarshal(readBody(t, resp), &r))
	require.Nil(t, r.Error)
	assert.Equal(t, `"0x3"`, string(r.Result))
	assert.Equal(t, "1", string(r.ID))
}

func TestHTTPBatchRequest(t *testing.T) {
	url, d := newTestServer(t)
	d.ProduceBlock(nil)

	resp := post(t, url, `[
		{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"},
		{"jsonrpc":"2.0","id":2,"method":"eth_chainId"}
	]`)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var arr []testResp
	require.NoError(t, json.Unmarshal(readBody(t, resp), &arr))
	require.Len(t, arr, 2)
	for _, r := range arr {
		require.Nil(t, r.Error)
	}
}

// A JSON-RPC level error still travels over HTTP 200 — the error lives in the
// body, not the status code. (This is exactly the "false 200" shape real nodes
// have, and what Gaslight will later weaponise.)
func TestHTTPJSONRPCErrorIsStill200(t *testing.T) {
	url, _ := newTestServer(t)

	cases := map[string]int{
		`{"jsonrpc":"2.0","id":1,"method":"eth_nope"}`: -32601, // unknown method
		`{not json`: -32700, // parse error
	}
	for body, wantCode := range cases {
		resp := post(t, url, body)
		require.Equal(t, http.StatusOK, resp.StatusCode, "body=%s", body)

		var r testResp
		require.NoError(t, json.Unmarshal(readBody(t, resp), &r))
		require.NotNil(t, r.Error, "body=%s", body)
		assert.Equal(t, wantCode, r.Error.Code, "body=%s", body)
	}
}

func TestHTTPRejectsNonPost(t *testing.T) {
	url, _ := newTestServer(t)

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req, err := http.NewRequest(method, url, nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()

		assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode, "method=%s", method)
		assert.Equal(t, http.MethodPost, resp.Header.Get("Allow"), "method=%s", method)
	}
}

func TestHTTPRejectsOversizedBody(t *testing.T) {
	url, _ := newTestServer(t)

	// Valid JSON-RPC wrapper padded past the 1 MiB cap.
	padding := strings.Repeat("a", maxRequestBody+1)
	body := `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":["` + padding + `"]}`

	resp := post(t, url, body)
	assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}

// A request just under the cap must still be served.
func TestHTTPAcceptsLargeButLegalBody(t *testing.T) {
	url, _ := newTestServer(t)

	padding := strings.Repeat("a", 1024)
	body := `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":["` + padding + `"]}`

	resp := post(t, url, body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var r testResp
	require.NoError(t, json.Unmarshal(readBody(t, resp), &r))
	require.Nil(t, r.Error)
	assert.Equal(t, `"0x0"`, string(r.Result))
}
