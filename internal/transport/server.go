package transport

import (
	"context"
	"net/http"
	"time"

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
	httpSrv    *http.Server
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

func (s *Server) Start(addr string) error {
	s.httpSrv = &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s.httpSrv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(ctx)
}
