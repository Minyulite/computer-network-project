package realtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/chat"
	"github.com/coder/websocket"
)

var errClosed = errors.New("realtime hub closed")
var errCapacity = errors.New("connection limit reached")

type Hub struct {
	auth           *auth.Service
	chat           *chat.Service
	allowedOrigins map[string]bool
	instanceID     string
	control        chan any
	commands       chan command
	finished       chan struct{}
	clients        sync.WaitGroup
	stop           sync.Once
	// The following state is owned exclusively by run.
	connections map[string]*client
	byUser      map[string]map[string]*client
	bySession   map[string]map[string]*client
	rates       map[string]*userRates
}

type register struct {
	client *client
	reply  chan error
}
type remove struct{ client *client }
type revoke struct {
	sessionID string
	reply     chan struct{}
}
type shutdown struct{ reply chan struct{} }
type command struct {
	client   *client
	envelope envelope
}

func New(authService *auth.Service, chatService *chat.Service, origins []string) *Hub {
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		allowed[origin] = true
	}
	h := &Hub{auth: authService, chat: chatService, allowedOrigins: allowed, instanceID: uuid(),
		control: make(chan any, 256), commands: make(chan command, 256), finished: make(chan struct{}),
		connections: map[string]*client{}, byUser: map[string]map[string]*client{},
		bySession: map[string]map[string]*client{}, rates: map[string]*userRates{}}
	go h.run()
	return h
}

func uuid() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	raw := hex.EncodeToString(b[:])
	return raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
}

func (h *Hub) sendControl(value any) error {
	select {
	case <-h.finished:
		return errClosed
	case h.control <- value:
		return nil
	}
}

func (h *Hub) register(c *client) error {
	reply := make(chan error, 1)
	if err := h.sendControl(register{c, reply}); err != nil {
		return err
	}
	select {
	case err := <-reply:
		return err
	case <-h.finished:
		return errClosed
	}
}

func (h *Hub) SessionRevoked(ctx context.Context, sessionID string) error {
	reply := make(chan struct{})
	if err := h.sendControl(revoke{sessionID, reply}); err != nil {
		return err
	}
	select {
	case <-reply:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-h.finished:
		return nil
	}
}

func (h *Hub) Shutdown(ctx context.Context) error {
	h.stop.Do(func() {
		reply := make(chan struct{})
		if h.sendControl(shutdown{reply}) == nil {
			<-reply
		}
	})
	done := make(chan struct{})
	go func() { h.clients.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hub) run() {
	defer close(h.finished)
	for {
		select {
		case value := <-h.control:
			if h.handleControl(value) {
				return
			}
		default:
			select {
			case value := <-h.control:
				if h.handleControl(value) {
					return
				}
			case cmd := <-h.commands:
				h.handleCommand(cmd)
			}
		}
	}
}

func (h *Hub) handleControl(value any) bool {
	switch v := value.(type) {
	case register:
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		identity, err := h.auth.ValidateSessionID(ctx, v.client.identity.SessionID)
		cancel()
		if err == nil && identity.User.ID != v.client.identity.User.ID {
			err = auth.ErrUnauthenticated
		}
		if err == nil && (len(h.connections) >= 200 || len(h.byUser[identity.User.ID]) >= 10) {
			err = errCapacity
		}
		if err == nil {
			c := v.client
			c.identity = identity
			first := len(h.byUser[identity.User.ID]) == 0
			h.connections[c.id] = c
			if h.byUser[identity.User.ID] == nil {
				h.byUser[identity.User.ID] = map[string]*client{}
			}
			if h.bySession[identity.SessionID] == nil {
				h.bySession[identity.SessionID] = map[string]*client{}
			}
			h.byUser[identity.User.ID][c.id] = c
			h.bySession[identity.SessionID][c.id] = c
			h.clients.Add(1)
			h.enqueue(c, wire("session.ready", "", map[string]any{"user": identity.User, "connectionId": c.id, "serverInstanceId": h.instanceID}))
			if first {
				h.broadcastAll("presence.changed", map[string]any{"userId": identity.User.ID, "username": identity.User.Username, "online": true})
			}
		}
		v.reply <- err
	case remove:
		h.removeClient(v.client, websocket.StatusNormalClosure, "Connection closed")
	case revoke:
		for _, c := range h.bySession[v.sessionID] {
			h.removeClient(c, websocket.StatusPolicyViolation, "Session revoked")
		}
		close(v.reply)
	case shutdown:
		for _, c := range h.connections {
			h.removeClient(c, websocket.StatusGoingAway, "Server shutdown")
		}
		close(v.reply)
		return true
	}
	return false
}

func (h *Hub) removeClient(c *client, code websocket.StatusCode, reason string) {
	if h.connections[c.id] == nil {
		return
	}
	delete(h.connections, c.id)
	delete(h.byUser[c.identity.User.ID], c.id)
	delete(h.bySession[c.identity.SessionID], c.id)
	if len(h.bySession[c.identity.SessionID]) == 0 {
		delete(h.bySession, c.identity.SessionID)
	}
	last := len(h.byUser[c.identity.User.ID]) == 0
	if last {
		delete(h.byUser, c.identity.User.ID)
		delete(h.rates, c.identity.User.ID)
	}
	c.close(code, reason)
	if last {
		h.broadcastAll("presence.changed", map[string]any{"userId": c.identity.User.ID, "username": c.identity.User.Username, "online": false})
	}
}

func (h *Hub) enqueue(c *client, frame []byte) {
	if h.connections[c.id] == nil {
		return
	}
	select {
	case c.out <- frame:
	default:
		h.removeClient(c, websocket.StatusPolicyViolation, "Slow client")
	}
}

func (h *Hub) broadcastAll(typ string, payload any) {
	frame := wire(typ, "", payload)
	for _, c := range h.connections {
		h.enqueue(c, frame)
	}
}

func (h *Hub) broadcastUsers(ids []string, typ string, payload any) {
	frame := wire(typ, "", payload)
	for _, id := range ids {
		for _, c := range h.byUser[id] {
			h.enqueue(c, frame)
		}
	}
}

type onlineUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

func (h *Hub) onlinePage(limit int, cursor string) (chat.Page[onlineUser], error) {
	if limit < 0 || limit > 50 {
		return chat.Page[onlineUser]{}, chat.ErrInvalidInput
	}
	if cursor != "" && !cursorPattern.MatchString(cursor) {
		return chat.Page[onlineUser]{}, chat.ErrInvalidInput
	}
	if limit == 0 {
		limit = 20
	}
	ids := make([]string, 0, len(h.byUser))
	for id := range h.byUser {
		if id > cursor {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	page := chat.Page[onlineUser]{Items: make([]onlineUser, 0, min(limit, len(ids)))}
	for _, id := range ids[:min(limit, len(ids))] {
		for _, c := range h.byUser[id] {
			page.Items = append(page.Items, onlineUser{id, c.identity.User.Username})
			break
		}
	}
	if len(ids) > limit {
		page.NextCursor = ids[limit-1]
	}
	return page, nil
}

type bucket struct {
	tokens float64
	last   time.Time
}

func (b *bucket) allow(now time.Time, rate, burst float64) bool {
	if b.last.IsZero() {
		b.tokens = burst
	}
	b.tokens = min(burst, b.tokens+now.Sub(b.last).Seconds()*rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type userRates struct {
	all   bucket
	sends bucket
}
