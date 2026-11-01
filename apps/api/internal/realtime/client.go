package realtime

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"example.com/computer-network/project/apps/api/internal/auth"
	"github.com/coder/websocket"
)

type client struct {
	hub       *Hub
	conn      *websocket.Conn
	id        string
	identity  auth.Identity
	ctx       context.Context
	cancel    context.CancelFunc
	out       chan []byte
	done      chan struct{}
	closeDone chan struct{}
	closeOnce sync.Once
	closing   atomic.Bool
}

func newClient(h *Hub, conn *websocket.Conn, identity auth.Identity) *client {
	ctx, cancel := context.WithCancel(context.Background())
	return &client{hub: h, conn: conn, id: uuid(), identity: identity,
		ctx: ctx, cancel: cancel, out: make(chan []byte, outboundQueue), done: make(chan struct{}), closeDone: make(chan struct{})}
}

func (c *client) start() {
	go func() {
		defer func() { close(c.done); c.hub.clients.Done() }()
		finished := make(chan struct{}, 3)
		go func() { c.readLoop(); finished <- struct{}{} }()
		go func() { c.writeLoop(); finished <- struct{}{} }()
		go func() { c.heartbeat(); finished <- struct{}{} }()
		<-finished
		if c.closing.Load() {
			<-c.closeDone
		} else {
			c.cancel()
			_ = c.conn.CloseNow()
		}
		select {
		case c.hub.control <- remove{c}:
		case <-c.hub.finished:
		}
		<-finished
		<-finished
	}()
}

func (c *client) close(code websocket.StatusCode, reason string) {
	c.closeOnce.Do(func() {
		c.closing.Store(true)
		c.cancel()
		if c.conn == nil {
			return
		}
		go func() { _ = c.conn.Close(code, reason); close(c.closeDone) }()
	})
}

func (c *client) readLoop() {
	for {
		typ, data, err := c.conn.Read(c.ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			c.close(websocket.StatusUnsupportedData, "Text frames required")
			return
		}
		env, err := parseCommand(data)
		if err != nil {
			c.close(websocket.StatusInvalidFramePayloadData, "Invalid envelope")
			return
		}
		select {
		case c.hub.commands <- command{c, env}:
		case <-c.ctx.Done():
			return
		case <-c.hub.finished:
			return
		}
	}
}

func (c *client) writeLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case frame := <-c.out:
			ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
			err := c.conn.Write(ctx, websocket.MessageText, frame)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (c *client) heartbeat() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	expiry := time.NewTimer(time.Until(c.identity.ExpiresAt))
	defer expiry.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-expiry.C:
			c.close(websocket.StatusPolicyViolation, "Session expired")
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
			err := c.conn.Ping(ctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
