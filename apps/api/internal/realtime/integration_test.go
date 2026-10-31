package realtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/chat"
	"example.com/computer-network/project/apps/api/internal/config"
	"example.com/computer-network/project/apps/api/internal/httpapi"
	"example.com/computer-network/project/apps/api/internal/store"
	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testOrigin = "http://localhost:3000"

type testRig struct {
	db     *store.DB
	auth   *auth.Service
	hub    *Hub
	server *httptest.Server
}
type testAccount struct {
	user      auth.User
	token     string
	sessionID string
}

func newRig(t *testing.T) *testRig {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL required for WebSocket integration tests")
	}
	if raw == os.Getenv("DATABASE_URL") {
		t.Fatal("use a dedicated test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	if err := admin.Ping(ctx); err != nil {
		t.Fatal("test database unavailable")
	}
	schema := "t5_" + strings.ReplaceAll(uuid()[:13], "-", "")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	})
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(ctx, u.String(), 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := store.Migrate(ctx, db.Pool); err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(db)
	if err != nil {
		t.Fatal(err)
	}
	h := New(a, chat.New(db), []string{testOrigin})
	a.SetRevocationNotifier(h)
	server := httptest.NewServer(httpapi.NewHandler(db, a, config.Config{AllowedOrigins: []string{testOrigin}}, h))
	t.Cleanup(func() {
		server.Close()
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := h.Shutdown(shutdown); err != nil {
			t.Errorf("hub shutdown: %v", err)
		}
	})
	return &testRig{db, a, h, server}
}

func (r *testRig) signup(t *testing.T, name string) testAccount {
	t.Helper()
	result, err := r.auth.Signup(context.Background(), name, "fifteen characters", name, "")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := r.auth.ValidateSession(context.Background(), result.Token)
	if err != nil {
		t.Fatal(err)
	}
	return testAccount{result.User, result.Token, identity.SessionID}
}

func (r *testRig) dial(t *testing.T, token, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{}
	if origin != "" {
		header.Set("Origin", origin)
	}
	if token != "" {
		header.Set("Cookie", auth.SessionCookieName+"="+token)
	}
	return websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(r.server.URL, "http")+"/ws", &websocket.DialOptions{HTTPHeader: header})
}

func (r *testRig) socket(t *testing.T, account testAccount) *websocket.Conn {
	t.Helper()
	conn, resp, err := r.dial(t, account.token, testOrigin)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial status %d: %v", resp.StatusCode, err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	if got := readFrame(t, conn).Type; got != "session.ready" {
		t.Fatalf("first frame: %s", got)
	}
	return conn
}

type testFrame struct {
	V         int             `json:"v"`
	Type      string          `json:"type"`
	RequestID string          `json:"requestId"`
	Payload   json.RawMessage `json:"payload"`
}

func readFrame(t *testing.T, c *websocket.Conn) testFrame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var frame testFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.V != 1 {
		t.Fatalf("protocol version %d", frame.V)
	}
	return frame
}

func until(t *testing.T, c *websocket.Conn, typ, id string) testFrame {
	t.Helper()
	for i := 0; i < 20; i++ {
		frame := readFrame(t, c)
		if frame.Type == typ && (id == "" || frame.RequestID == id) {
			return frame
		}
	}
	t.Fatalf("missing %s %s", typ, id)
	return testFrame{}
}

func untilWithout(t *testing.T, c *websocket.Conn, typ, id, forbidden string) testFrame {
	t.Helper()
	for i := 0; i < 20; i++ {
		frame := readFrame(t, c)
		if frame.Type == forbidden {
			t.Fatalf("unexpected %s event", forbidden)
		}
		if frame.Type == typ && frame.RequestID == id {
			return frame
		}
	}
	t.Fatalf("missing %s %s", typ, id)
	return testFrame{}
}

func send(t *testing.T, c *websocket.Conn, id, typ string, payload any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, wire(typ, id, payload)); err != nil {
		t.Fatal(err)
	}
}

func field(t *testing.T, f testFrame, key string) json.RawMessage {
	t.Helper()
	var p map[string]json.RawMessage
	if err := json.Unmarshal(f.Payload, &p); err != nil {
		t.Fatal(err)
	}
	return p[key]
}

func TestHandshakeAndExpiry(t *testing.T) {
	r := newRig(t)
	a := r.signup(t, "alice")
	for _, tc := range []struct {
		token, origin string
		status        int
	}{{"", testOrigin, 401}, {a.token, "", 403}, {a.token, "http://evil.example", 403}, {"invalid", testOrigin, 401}} {
		c, resp, err := r.dial(t, tc.token, tc.origin)
		if c != nil {
			_ = c.CloseNow()
		}
		if err == nil || resp == nil || resp.StatusCode != tc.status {
			t.Fatalf("handshake %q %q: status %v error %v", tc.token, tc.origin, resp, err)
		}
	}
	if _, err := r.db.Pool.Exec(context.Background(), "UPDATE sessions SET created_at=now()-interval '2 days', expires_at=now()-interval '1 day' WHERE id=$1", a.sessionID); err != nil {
		t.Fatal(err)
	}
	c, resp, err := r.dial(t, a.token, testOrigin)
	if c != nil {
		_ = c.CloseNow()
	}
	if err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("expired session accepted: %v %v", resp, err)
	}
}

func TestRegistrationRechecksRevokedSession(t *testing.T) {
	r := newRig(t)
	a := r.signup(t, "alice")
	identity, err := r.auth.ValidateSession(context.Background(), a.token)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.auth.Logout(context.Background(), a.token); err != nil {
		t.Fatal(err)
	}
	stale := newClient(r.hub, nil, identity)
	if err := r.hub.register(stale); err == nil {
		t.Fatal("registration accepted a session revoked after initial validation")
	}
}

func TestDevicesPrivacyRevocationAndHistory(t *testing.T) {
	r := newRig(t)
	a := r.signup(t, "alice")
	b := r.signup(t, "bob")
	c := r.signup(t, "charlie")
	a1 := r.socket(t, a)
	_ = until(t, a1, "presence.changed", "")
	a2 := r.socket(t, a)
	b1 := r.socket(t, b)
	_ = until(t, a1, "presence.changed", "")
	_ = until(t, a2, "presence.changed", "")
	c1 := r.socket(t, c)
	_ = until(t, a1, "presence.changed", "")
	_ = until(t, a2, "presence.changed", "")
	_ = until(t, b1, "presence.changed", "")
	send(t, a1, "users", "users.list", map[string]any{})
	users := until(t, a1, "request.ok", "users")
	var userPage struct {
		Items []onlineUser `json:"items"`
	}
	if err := json.Unmarshal(users.Payload, &userPage); err != nil {
		t.Fatal(err)
	}
	if len(userPage.Items) != 3 {
		t.Fatalf("online users %d, want 3", len(userPage.Items))
	}
	send(t, a1, "direct", "direct.open", map[string]any{"receiverId": b.user.ID})
	direct := until(t, a1, "request.ok", "direct")
	var opened struct {
		Room chat.Room `json:"room"`
	}
	if err := json.Unmarshal(direct.Payload, &opened); err != nil {
		t.Fatal(err)
	}
	if opened.Room.Kind != "direct" {
		t.Fatal("direct room missing")
	}
	_ = until(t, a2, "room.upserted", "")
	_ = until(t, b1, "room.upserted", "")
	send(t, c1, "inaccessible", "room.history", map[string]any{"roomId": opened.Room.ID})
	if string(field(t, untilWithout(t, c1, "request.error", "inaccessible", "room.upserted"), "code")) != "\"room_unavailable\"" {
		t.Fatal("private history leaked")
	}
	messageID := uuid()
	send(t, a1, "send", "message.send", map[string]any{"roomId": opened.Room.ID, "clientMessageId": messageID, "text": "hello"})
	ack := until(t, a1, "request.ok", "send")
	if len(field(t, ack, "messageId")) == 0 {
		t.Fatal("missing message ID")
	}
	for _, device := range []*websocket.Conn{a1, a2, b1} {
		if got := until(t, device, "message.created", ""); len(field(t, got, "text")) == 0 {
			t.Fatal("missing message text")
		}
	}
	send(t, c1, "rooms", "rooms.list", map[string]any{})
	if string(field(t, untilWithout(t, c1, "request.ok", "rooms", "message.created"), "items")) != "[]" {
		t.Fatal("private room leaked")
	}
	send(t, a1, "group", "group.create", map[string]any{"name": "study"})
	group := until(t, a1, "request.ok", "group")
	var created struct {
		Room chat.Room `json:"room"`
	}
	if err := json.Unmarshal(group.Payload, &created); err != nil {
		t.Fatal(err)
	}
	_ = until(t, b1, "group.created", "")
	_ = until(t, c1, "group.created", "")
	send(t, b1, "outside", "room.history", map[string]any{"roomId": created.Room.ID})
	if string(field(t, until(t, b1, "request.error", "outside"), "code")) != "\"room_unavailable\"" {
		t.Fatal("pre-join history leaked")
	}
	send(t, b1, "join", "group.join", map[string]any{"roomId": created.Room.ID})
	_ = until(t, b1, "request.ok", "join")
	_ = until(t, c1, "group.member_joined", "")
	send(t, a1, "gsend", "message.send", map[string]any{"roomId": created.Room.ID, "clientMessageId": uuid(), "text": "group text"})
	_ = until(t, a1, "request.ok", "gsend")
	_ = until(t, b1, "message.created", "")
	send(t, c1, "group-unjoined", "room.history", map[string]any{"roomId": created.Room.ID})
	if string(field(t, untilWithout(t, c1, "request.error", "group-unjoined", "message.created"), "code")) != "\"room_unavailable\"" {
		t.Fatal("group history leaked")
	}
	_ = a2.CloseNow()
	a3 := r.socket(t, a)
	send(t, a3, "recover", "room.history", map[string]any{"roomId": opened.Room.ID, "afterSeq": 0})
	page := until(t, a3, "request.ok", "recover")
	var history chat.HistoryPage
	if err := json.Unmarshal(page.Payload, &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Messages) != 1 || history.Messages[0].Text != "hello" {
		t.Fatalf("reconnect history: %+v", history)
	}
	other, err := r.auth.Login(context.Background(), "alice", "fifteen characters", "other-ip", "")
	if err != nil {
		t.Fatal(err)
	}
	otherIdentity, err := r.auth.ValidateSession(context.Background(), other.Token)
	if err != nil {
		t.Fatal(err)
	}
	aOther := r.socket(t, testAccount{other.User, other.Token, otherIdentity.SessionID})
	if err := r.auth.Logout(context.Background(), a.token); err != nil {
		t.Fatal(err)
	}
	for _, device := range []*websocket.Conn{a1, a3} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, _, err := device.Read(ctx)
		cancel()
		if err == nil { // buffered events may precede closure
			for i := 0; i < 10 && err == nil; i++ {
				ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
				_, _, err = device.Read(ctx)
				cancel()
			}
		}
		if err == nil {
			t.Fatal("revoked session socket survived")
		}
	}
	send(t, aOther, "alive", "users.list", map[string]any{})
	_ = until(t, aOther, "request.ok", "alive")
}

func TestPresenceFirstAndLastConnection(t *testing.T) {
	r := newRig(t)
	a := r.signup(t, "alice")
	b := r.signup(t, "bob")
	a1 := r.socket(t, a)
	self := until(t, a1, "presence.changed", "")
	if string(field(t, self, "online")) != "true" {
		t.Fatal("first socket did not publish online")
	}
	b1 := r.socket(t, b)
	first := until(t, b1, "presence.changed", "")
	if string(field(t, first, "userId")) != "\""+b.user.ID+"\"" {
		t.Fatal("bob did not publish online")
	}
	a2 := r.socket(t, a)
	send(t, b1, "still-online", "users.list", map[string]any{})
	_ = untilWithout(t, b1, "request.ok", "still-online", "presence.changed")
	_ = a2.CloseNow()
	send(t, b1, "one-left", "users.list", map[string]any{})
	_ = untilWithout(t, b1, "request.ok", "one-left", "presence.changed")
	_ = a1.CloseNow()
	// The coordinator removes A before processing B's next command.
	deadline := time.Now().Add(3 * time.Second)
	for {
		send(t, b1, "offline", "users.list", map[string]any{})
		frame := until(t, b1, "presence.changed", "")
		if string(field(t, frame, "userId")) == "\""+a.user.ID+"\"" && string(field(t, frame, "online")) == "false" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("last disconnect did not publish offline")
		}
	}
}

func TestInvalidFramesAndShutdown(t *testing.T) {
	r := newRig(t)
	a := r.signup(t, "alice")
	c := r.socket(t, a)
	_ = until(t, c, "presence.changed", "")
	send(t, c, "unknown", "does.not.exist", map[string]any{})
	if string(field(t, until(t, c, "request.error", "unknown"), "code")) != "\"unknown_type\"" {
		t.Fatal("unknown command")
	}
	send(t, c, "bad", "group.create", map[string]any{"name": "x", "createdBy": a.user.ID})
	if string(field(t, until(t, c, "request.error", "bad"), "code")) != "\"invalid_payload\"" {
		t.Fatal("extra field accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := c.Write(ctx, websocket.MessageBinary, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	_, _, err := c.Read(ctx)
	cancel()
	if err == nil {
		t.Fatal("binary frame accepted")
	}
	if status := websocket.CloseStatus(err); status != websocket.StatusUnsupportedData {
		t.Fatalf("binary close status %d: %v", status, err)
	}
	for _, bad := range []struct {
		data   []byte
		status websocket.StatusCode
	}{
		{[]byte(`{"v":1,"type":"users.list","requestId":"bad","payload":{}} trailing`), websocket.StatusInvalidFramePayloadData},
		{[]byte(`{"v":2,"type":"users.list","requestId":"bad","payload":{}}`), websocket.StatusInvalidFramePayloadData},
		{make([]byte, inboundLimit+1), websocket.StatusMessageTooBig},
	} {
		invalid := r.socket(t, a)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if writeErr := invalid.Write(ctx, websocket.MessageText, bad.data); writeErr != nil {
			t.Fatal(writeErr)
		}
		cancel()
		for i := 0; i < 3; i++ {
			ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
			_, _, err = invalid.Read(ctx)
			cancel()
			if err != nil {
				break
			}
		}
		if err == nil {
			t.Fatal("invalid frame left socket open")
		}
		if status := websocket.CloseStatus(err); status != bad.status {
			t.Fatalf("invalid frame close status %d, want %d: %v", status, bad.status, err)
		}
	}
	c2 := r.socket(t, a)
	shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := r.hub.Shutdown(shutdown); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
		_, _, err = c2.Read(ctx)
		cancel()
		if err != nil {
			break
		}
	}
	if err == nil {
		t.Fatal("shutdown left socket open")
	}
}
