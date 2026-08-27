package transport

import (
	"io"
	"net/http"

	"github.com/ChickenBenny/Gaslight/internal/rpc"
)

const maxRequestBody = 1 << 20 // 1 MiB

type Server struct {
	rpc *rpc.Handler
}

func NewServer(rpc *rpc.Handler) *Server {
	return &Server{rpc: rpc}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	out := s.rpc.ServeRPC(body)
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}
