package transport

import (
	"net/http"

	"github.com/ChickenBenny/Gaslight/internal/rpc"
	"github.com/gorilla/websocket"
)

type Server struct {
	rpc *rpc.Handler
}

func NewServer(rpc *rpc.Handler) *Server {
	return &Server{rpc: rpc}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if websocket.IsWebSocketUpgrade(r) {
		s.serveWS(w, r)
		return
	}
	s.serveHTTPRPC(w, r)
}
