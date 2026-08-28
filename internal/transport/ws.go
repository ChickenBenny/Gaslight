package transport

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/ChickenBenny/Gaslight/internal/rpc"
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

	subMu  sync.Mutex
	subs   map[string]func()
	subSeq uint64
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
		if err := c.handleMessage(msg); err != nil {
			return
		}
	}
}

// handleMessage answers one incoming message. Subscription methods are bound to
// this connection so they are served here; everything else goes to the rpc
// handler. Only single requests are intercepted — a batch is passed through.
func (c *wsConn) handleMessage(msg []byte) error {
	trimmed := bytes.TrimSpace(msg)

	var req rpc.RPCRequest
	if len(trimmed) == 0 || trimmed[0] == '[' || json.Unmarshal(trimmed, &req) != nil {
		return c.write(c.server.rpc.ServeRPC(msg))
	}

	switch req.Method {
	case "eth_subscribe":
		return c.handleSubscribe(req)
	case "eth_unsubscribe":
		return c.handleUnsubscribe(req)
	default:
		return c.write(c.server.rpc.ServeRPC(msg))
	}
}

func (c *wsConn) handleSubscribe(req rpc.RPCRequest) error {
	var kind string
	if len(req.Params) < 1 || json.Unmarshal(req.Params[0], &kind) != nil {
		return c.writeResult(req.ID, nil, invalidParams("subscription kind must be a string"))
	}
	if kind != "newHeads" {
		return c.writeResult(req.ID, nil, invalidParams("unsupported subscription kind: "+kind))
	}

	heads, unsub := c.server.headSource.SubscribeHeads()
	subID := c.nextSubID()
	c.addSub(subID, unsub)
	go c.pump(subID, heads)

	return c.writeResult(req.ID, subID, nil)
}

func (c *wsConn) handleUnsubscribe(req rpc.RPCRequest) error {
	var subID string
	if len(req.Params) < 1 || json.Unmarshal(req.Params[0], &subID) != nil {
		return c.writeResult(req.ID, nil, invalidParams("subscription id must be a string"))
	}

	unsub, ok := c.removeSub(subID)
	if ok {
		unsub() // called outside subMu: it takes the chain's lock
	}
	return c.writeResult(req.ID, ok, nil)
}

func (c *wsConn) writeResult(id json.RawMessage, result any, rpcErr *rpcError) error {
	resp := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result,omitempty"`
		Error   *rpcError       `json:"error,omitempty"`
	}{JSONRPC: "2.0", ID: id, Result: result, Error: rpcErr}

	out, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return c.write(out)
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	c := &wsConn{conn: conn, server: s}
	c.readLoop()
	c.closeAllSubs() // let every pump goroutine exit before we return
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

func (c *wsConn) addSub(id string, unsub func()) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	if c.subs == nil {
		c.subs = make(map[string]func())
	}
	c.subs[id] = unsub
}

func (c *wsConn) removeSub(id string) (func(), bool) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	unsub, ok := c.subs[id]
	if ok {
		delete(c.subs, id)
	}
	return unsub, ok
}

func (c *wsConn) nextSubID() string {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	c.subSeq++
	return "0x" + strconv.FormatUint(c.subSeq, 16)
}

func (c *wsConn) closeAllSubs() {
	c.subMu.Lock()
	subs := c.subs
	c.subs = nil
	c.subMu.Unlock()

	for _, unsub := range subs {
		unsub()
	}
}
