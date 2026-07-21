package rpc

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The five standard JSON-RPC 2.0 error codes, each carrying a human-readable message.
func TestStandardErrorCodes(t *testing.T) {
	cases := []struct {
		name string
		err  *RPCError
		code int
	}{
		{"parse", errParse(), -32700},
		{"invalid request", errInvalidRequest(), -32600},
		{"method not found", errMethodNotFound(), -32601},
		{"invalid params", errInvalidParams("bad"), -32602},
		{"internal", errInternal(), -32603},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.NotNil(t, c.err)
			assert.Equal(t, c.code, c.err.Code)
			assert.NotEmpty(t, c.err.Message, "error should carry a human-readable message")
		})
	}
}

// RPCError is embedded verbatim in a response's "error" field, so its JSON
// shape must be exactly {"code":..,"message":..}.
func TestRPCErrorMarshalsToJSONRPCShape(t *testing.T) {
	e := &RPCError{Code: -32601, Message: "method not found"}
	b, err := json.Marshal(e)
	require.NoError(t, err)
	assert.JSONEq(t, `{"code":-32601,"message":"method not found"}`, string(b))
}

// errInvalidParams surfaces the caller's detail so clients get an actionable message.
func TestInvalidParamsIncludesDetail(t *testing.T) {
	e := errInvalidParams("expected 2 params, got 1")
	assert.Equal(t, -32602, e.Code)
	assert.Contains(t, e.Message, "expected 2 params, got 1")
}
