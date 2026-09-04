package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/ChickenBenny/Gaslight/internal/rpc"
	"github.com/gorilla/websocket"
)

// wsWriteTimeout bounds a single frame write. Without it a peer that stops
// reading blocks the pump inside WriteMessage while holding c.mu, which stalls
// the read loop too and leaks both goroutines.
const wsWriteTimeout = 10 * time.Second

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type wsConn struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	server *Server
	ctx    context.Context // cancelled when the connection goes away

	subMu  sync.Mutex
	subs   map[string]func()
	subSeq uint64
}

func (c *wsConn) write(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout)); err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

// closeWithReason sends a close frame and drops the connection. Control frames
// may be written concurrently with other writes, so it does not take c.mu.
func (c *wsConn) closeWithReason(code int, reason string) {
	msg := websocket.FormatCloseMessage(code, reason)
	_ = c.conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(wsWriteTimeout))
	_ = c.conn.Close()
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
		return c.write(c.server.rpc.ServeRPC(c.ctx, msg))
	}

	switch req.Method {
	case "eth_subscribe":
		return c.handleSubscribe(req)
	case "eth_unsubscribe":
		return c.handleUnsubscribe(req)
	default:
		return c.write(c.server.rpc.ServeRPC(c.ctx, msg))
	}
}

func (c *wsConn) handleSubscribe(req rpc.RPCRequest) error {
	var kind string
	if len(req.Params) < 1 || json.Unmarshal(req.Params[0], &kind) != nil {
		return c.writeResult(req.ID, nil, rpc.NewInvalidParams("subscription kind must be a string"))
	}
	if kind != "newHeads" {
		return c.writeResult(req.ID, nil, rpc.NewInvalidParams("unsupported subscription kind: "+kind))
	}

	heads, unsub := c.server.headSource.SubscribeHeads()
	subID := c.nextSubID()
	c.addSub(subID, unsub)

	// Reply before starting the pump: a notification that overtakes the
	// subscribe response carries an id the client has not learned yet, and
	// clients drop notifications for unknown subscription ids.
	if err := c.writeResult(req.ID, subID, nil); err != nil {
		if u, ok := c.removeSub(subID); ok {
			u()
		}
		return err
	}

	go c.pump(subID, heads)
	return nil
}

func (c *wsConn) handleUnsubscribe(req rpc.RPCRequest) error {
	var subID string
	if len(req.Params) < 1 || json.Unmarshal(req.Params[0], &subID) != nil {
		return c.writeResult(req.ID, nil, rpc.NewInvalidParams("subscription id must be a string"))
	}

	unsub, ok := c.removeSub(subID)
	if ok {
		unsub() // called outside subMu: it takes the chain's lock
	}
	return c.writeResult(req.ID, ok, nil)
}

func (c *wsConn) writeResult(id json.RawMessage, result any, rpcErr *rpc.RPCError) error {
	resp := rpc.RPCResponse{JSONRPC: "2.0", ID: id, Error: rpcErr}
	if rpcErr == nil {
		encoded, err := json.Marshal(result)
		if err != nil {
			return err
		}
		resp.Result = encoded
	}

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
	conn.SetReadLimit(maxRequestBody) // same cap the HTTP path enforces

	// Cancelled when this connection ends, so anything waiting on behalf of it
	// (a delay fault, say) stops instead of outliving the connection.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	c := &wsConn{conn: conn, server: s, ctx: ctx}
	s.addConn(c)
	defer s.removeConn(c)

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

	// The channel closed. If this subscription is still registered the chain
	// dropped it for falling behind, rather than the client unsubscribing —
	// tell the client instead of leaving it with a silently dead subscription.
	if _, dropped := c.removeSub(subID); dropped {
		c.closeWithReason(websocket.ClosePolicyViolation, "subscription dropped: consumer too slow")
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
