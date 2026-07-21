package rpc

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func errParse() *RPCError {
	return &RPCError{Code: -32700, Message: "parse error"}
}
func errInvalidRequest() *RPCError {
	return &RPCError{Code: -32600, Message: "invalid request"}
}
func errMethodNotFound() *RPCError {
	return &RPCError{Code: -32601, Message: "method not found"}
}
func errInvalidParams(detail string) *RPCError {
	return &RPCError{Code: -32602, Message: "invalid params: " + detail}
}
func errInternal() *RPCError {
	return &RPCError{Code: -32603, Message: "internal error"}
}
