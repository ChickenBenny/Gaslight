package transport

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type wsConn struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	server *Server
}

func (c *wsConn) write(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

func (c *wsConn) readLoop() {
	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		out := c.server.rpc.ServeRPC(msg)
		if err := c.write(out); err != nil {
			return
		}
	}
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	c := &wsConn{conn: conn, server: s}
	c.readLoop()
}

func (c *wsConn) pump(subID string, heads <-chan *chain.Block) {
	for head := range heads {
		msg, err := json.Marshal(newHeadNotification(subID, head))
		if err != nil {
			return
		}
		if err := c.write(msg); err != nil {
			return
		}
	}
}
