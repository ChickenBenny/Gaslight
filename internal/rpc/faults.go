package rpc

import (
	"encoding/json"

	"github.com/ChickenBenny/Gaslight/internal/chain"
)

type FaultSource interface {
	Before(method string)
	After(method string, result any, err *RPCError) (any, *RPCError)
}

func withFaults(fs FaultSource, method string, next methodFunc) methodFunc {
	if fs == nil {
		return next
	}
	return func(s *chain.ChainSnapshot, params []json.RawMessage) (any, *RPCError) {
		fs.Before(method)
		result, err := next(s, params)
		return fs.After(method, result, err)
	}
}
