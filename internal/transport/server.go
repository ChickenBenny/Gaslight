package transport

import (
	"context"
	"net"
	"net/http"
	"sync"
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

	// baseCtx is the parent of every request context; cancelling it on shutdown
	// releases work that would otherwise be waited out — an injected delay
	// fault, for instance, which would make a clean signal look like a crash.
	baseCtx    context.Context
	baseCancel context.CancelFunc

	connMu sync.Mutex
	conns  map[*wsConn]struct{}
}

func NewServer(rpc *rpc.Handler, headSource HeadSource) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		rpc:        rpc,
		headSource: headSource,
		baseCtx:    ctx,
		baseCancel: cancel,
		conns:      make(map[*wsConn]struct{}),
	}
	// Built here rather than in Start: Start usually runs on its own goroutine,
	// so assigning it there would race with Shutdown. Only ReadHeaderTimeout is
	// set — ReadTimeout/WriteTimeout would kill idle WebSocket subscriptions.
	s.httpSrv = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return s.baseCtx },
	}
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if websocket.IsWebSocketUpgrade(r) {
		s.serveWS(w, r)
		return
	}
	s.serveHTTPRPC(w, r)
}

func (s *Server) Start(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.httpSrv.Serve(ln)
}

// Shutdown stops the listener and drains in-flight requests. WebSocket
// connections are hijacked, so http.Server never touches them: they are closed
// here, which ends their read loops and pump goroutines.
func (s *Server) Shutdown(ctx context.Context) error {
	// Ask in-flight work to stop before draining: http.Server.Shutdown only
	// waits for handlers to return, so an injected delay would otherwise run to
	// completion and blow the caller's deadline.
	s.baseCancel()
	s.closeAllConns()
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) addConn(c *wsConn) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	s.conns[c] = struct{}{}
}

func (s *Server) removeConn(c *wsConn) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	delete(s.conns, c)
}

func (s *Server) closeAllConns() {
	s.connMu.Lock()
	conns := make([]*wsConn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.connMu.Unlock()

	for _, c := range conns {
		c.closeWithReason(websocket.CloseGoingAway, "server shutting down")
	}
}
