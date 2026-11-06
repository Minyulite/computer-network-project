package realtime

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/chat"
	"example.com/computer-network/project/apps/api/internal/id"
	"github.com/coder/websocket"
)

var errClosed = errors.New("realtime hub closed")
var errCapacity = errors.New("connection limit reached")

const (
	maxConnections        = 200
	maxConnectionsPerUser = 10
	sessionCheckTimeout   = 3 * time.Second
)

// Authenticator is the part of auth.Service the hub uses.
type Authenticator interface {
	ValidateSession(ctx context.Context, token string) (auth.Identity, error)
	ValidateSessionID(ctx context.Context, sessionID string) (auth.Identity, error)
}

// ChatService is the part of chat.Service the hub's commands use.
type ChatService interface {
	OwnRooms(ctx context.Context, caller auth.Identity, options chat.PageOptions) (chat.Page[chat.Room], error)
	Groups(ctx context.Context, caller auth.Identity, options chat.PageOptions) (chat.Page[chat.Group], error)
	GroupMembers(ctx context.Context, caller auth.Identity, roomID string, options chat.PageOptions) (chat.Page[chat.GroupMember], error)
	OpenDirect(ctx context.Context, caller auth.Identity, otherUserID string) (chat.Room, bool, error)
	CreateGroup(ctx context.Context, caller auth.Identity, name string) (chat.Room, error)
	JoinGroup(ctx context.Context, caller auth.Identity, roomID string) (chat.JoinResult, error)
	SendMessage(ctx context.Context, caller auth.Identity, roomID, clientMessageID, text string) (chat.SendResult, error)
	History(ctx context.Context, caller auth.Identity, roomID string, options chat.HistoryOptions) (chat.HistoryPage, error)
	BotCatalog(ctx context.Context, caller auth.Identity, options chat.PageOptions) (chat.Page[chat.Bot], error)
	MyBots(ctx context.Context, caller auth.Identity) ([]chat.Bot, error)
	GetBot(ctx context.Context, caller auth.Identity, botID string) (chat.BotDetail, error)
	CreateBot(ctx context.Context, caller auth.Identity, input chat.BotInput) (chat.BotDetail, error)
	UpdateBot(ctx context.Context, caller auth.Identity, botID string, input chat.BotInput) (chat.BotDetail, error)
	OpenBot(ctx context.Context, caller auth.Identity, botID string) (chat.Room, bool, error)
}

type Hub struct {
	auth           Authenticator
	chat           ChatService
	grammar        GrammarCorrector
	bots           BotReplier // Optional; nil disables bot replies.
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
	// Kept apart from rates, which are dropped when a user's last connection
	// closes while that user's corrections may still be finishing.
	grammarInflight map[string]int
	botInflight     map[string]int
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

func New(authService Authenticator, chatService ChatService, grammarService GrammarCorrector, botReplier BotReplier, origins []string) *Hub {
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		allowed[origin] = true
	}
	h := &Hub{auth: authService, chat: chatService, grammar: grammarService, bots: botReplier, allowedOrigins: allowed, instanceID: id.MustNew(),
		control: make(chan any, 256), commands: make(chan command, 256), finished: make(chan struct{}),
		connections: map[string]*client{}, byUser: map[string]map[string]*client{},
		bySession: map[string]map[string]*client{}, rates: map[string]*userRates{}, grammarInflight: map[string]int{}, botInflight: map[string]int{}}
	go h.run()
	return h
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

// run owns all hub state. Control messages take priority over commands so
// disconnects and revocations are never stuck behind a queue of requests.
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
		v.reply <- h.addClient(v.client)
	case remove:
		h.removeClient(v.client, websocket.StatusNormalClosure, "Connection closed")
	case revoke:
		for _, c := range h.bySession[v.sessionID] {
			h.removeClient(c, websocket.StatusPolicyViolation, "Session revoked")
		}
		close(v.reply)
	case grammarDone:
		h.finishGrammar(v)
	case botDone:
		h.finishBot(v)
	case shutdown:
		for _, c := range h.connections {
			h.removeClient(c, websocket.StatusGoingAway, "Server shutdown")
		}
		close(v.reply)
		return true
	}
	return false
}

func (h *Hub) addClient(c *client) error {
	ctx, cancel := context.WithTimeout(context.Background(), sessionCheckTimeout)
	identity, err := h.auth.ValidateSessionID(ctx, c.identity.SessionID)
	cancel()
	if err == nil && identity.User.ID != c.identity.User.ID {
		err = auth.ErrUnauthenticated
	}
	if err == nil && (len(h.connections) >= maxConnections || len(h.byUser[identity.User.ID]) >= maxConnectionsPerUser) {
		err = errCapacity
	}
	if err != nil {
		return err
	}
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
	h.enqueue(c, wire(eventSessionReady, "", map[string]any{"user": identity.User, "connectionId": c.id, "serverInstanceId": h.instanceID}))
	if first {
		h.broadcastAll(eventPresenceChanged, map[string]any{"userId": identity.User.ID, "username": identity.User.Username, "avatar": identity.User.Avatar, "online": true})
	}
	return nil
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
		h.broadcastAll(eventPresenceChanged, map[string]any{"userId": c.identity.User.ID, "username": c.identity.User.Username, "avatar": c.identity.User.Avatar, "online": false})
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
	for _, userID := range ids {
		for _, c := range h.byUser[userID] {
			h.enqueue(c, frame)
		}
	}
}

type onlineUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Avatar   string `json:"avatar"`
}

func (h *Hub) onlinePage(limit int, cursor string) (chat.Page[onlineUser], error) {
	if limit < 0 || limit > 50 {
		return chat.Page[onlineUser]{}, chat.ErrInvalidInput
	}
	if cursor != "" && !id.Valid(cursor) {
		return chat.Page[onlineUser]{}, chat.ErrInvalidInput
	}
	if limit == 0 {
		limit = 20
	}
	ids := make([]string, 0, len(h.byUser))
	for userID := range h.byUser {
		if userID > cursor {
			ids = append(ids, userID)
		}
	}
	sort.Strings(ids)
	page := chat.Page[onlineUser]{Items: make([]onlineUser, 0, min(limit, len(ids)))}
	for _, userID := range ids[:min(limit, len(ids))] {
		for _, c := range h.byUser[userID] {
			page.Items = append(page.Items, onlineUser{userID, c.identity.User.Username, c.identity.User.Avatar})
			break
		}
	}
	if len(ids) > limit {
		page.NextCursor = ids[limit-1]
	}
	return page, nil
}
