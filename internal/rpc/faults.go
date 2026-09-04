package rpc

import (
	"context"
	"encoding/json"
)

type FaultSource interface {
	Before(ctx context.Context, method string)
	After(method string, result any, err *RPCError) (any, *RPCError)
}

func withFaults(fs FaultSource, method string, next methodFunc) methodFunc {
	if fs == nil {
		return next
	}
	return func(ctx context.Context, s snapshotGetter, params []json.RawMessage) (any, *RPCError) {
		fs.Before(ctx, method)
		result, err := next(ctx, s, params)
		return fs.After(method, result, err)
	}
}
