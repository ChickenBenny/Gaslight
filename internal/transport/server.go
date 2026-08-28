package transport

import (
	"net/http"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/ChickenBenny/Gaslight/internal/rpc"
	"github.com/gorilla/websocket"
)

// HeadSource is the slice of the chain the transport needs: it may subscribe to
// canonical head changes, and nothing else.
type HeadSource interface {
	SubscribeHeads() (<-chan *chain.Block, func())
}

type Server struct {
	rpc        *rpc.Handler
	headSource HeadSource
}

func NewServer(rpc *rpc.Handler, headSource HeadSource) *Server {
	return &Server{rpc: rpc, headSource: headSource}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if websocket.IsWebSocketUpgrade(r) {
		s.serveWS(w, r)
		return
	}
	s.serveHTTPRPC(w, r)
}
