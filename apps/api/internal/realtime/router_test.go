package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/chat"
)

// fakeAuth confirms every session as identity, so router tests need no database.
type fakeAuth struct {
	identity auth.Identity
	err      error
}

func (f fakeAuth) ValidateSession(context.Context, string) (auth.Identity, error) {
	return auth.Identity{}, errors.New("unused")
}

func (f fakeAuth) ValidateSessionID(context.Context, string) (auth.Identity, error) {
	return f.identity, f.err
}

func routerHub(t *testing.T, authErr error) (*Hub, *client) {
	t.Helper()
	c := grammarClient(t, "alice-1", "alice")
	h := grammarHub(nil, c)
	h.auth = fakeAuth{identity: c.identity, err: authErr}
	return h, c
}

// nextFrame fails instead of hanging when the router sent nothing.
func nextFrame(t *testing.T, c *client) []byte {
	t.Helper()
	select {
	case frame := <-c.out:
		return frame
	case <-time.After(2 * time.Second):
		t.Fatal("no frame was sent")
		return nil
	}
}

func cmdFor(c *client, typ, payload string) command {
	return command{client: c, envelope: envelope{V: 1, Type: typ, RequestID: "req", Payload: []byte(payload)}}
}

func TestRouterReportsUnknownCommandsAndBadPayloads(t *testing.T) {
	h, c := routerHub(t, nil)
	for typ, want := range map[string]struct{ payload, code string }{
		"no.such.command": {`{}`, "unknown_type"},
		"users.list":      {`{"extra":1}`, "invalid_payload"},
		"bots.mine":       {`[]`, "invalid_payload"},
	} {
		h.handleCommand(cmdFor(c, typ, want.payload))
		frameType, id, payload := decodeFrame(t, nextFrame(t, c))
		if frameType != frameError || id != "req" || payload["code"] != want.code {
			t.Errorf("%s: frame %s %s %v", typ, frameType, id, payload)
		}
	}
}

func TestRouterAnswersSimpleCommands(t *testing.T) {
	h, c := routerHub(t, nil)
	h.handleCommand(cmdFor(c, "users.list", `{}`))
	if typ := frameTypeOf(t, nextFrame(t, c)); typ != frameOK {
		t.Fatalf("frame %s", typ)
	}
}

func TestRouterRemovesClientWhoseSessionWasRevoked(t *testing.T) {
	h, c := routerHub(t, auth.ErrUnauthenticated)
	h.handleCommand(cmdFor(c, "users.list", `{}`))
	if h.connections[c.id] != nil {
		t.Fatal("client with a revoked session stayed connected")
	}
}

func TestRouterAppliesPerCommandLimit(t *testing.T) {
	h, c := routerHub(t, nil)
	limited := 0
	for range int(sendLimit.burst) + 1 {
		// The payload is invalid on purpose: the limit is checked before decoding.
		h.handleCommand(cmdFor(c, "message.send", `{"bad":1}`))
		if _, _, payload := decodeFrame(t, nextFrame(t, c)); payload["code"] == "rate_limited" {
			limited++
		}
	}
	if limited != 1 {
		t.Fatalf("rate limited %d times, want 1", limited)
	}
	// Other commands still have tokens in the shared bucket.
	h.handleCommand(cmdFor(c, "users.list", `{}`))
	if typ := frameTypeOf(t, nextFrame(t, c)); typ != frameOK {
		t.Fatalf("unrelated command was limited: %s", typ)
	}
}

func TestEveryRouteHasAHandler(t *testing.T) {
	for typ, rt := range routes {
		if rt.handle == nil {
			t.Errorf("%s has no handler", typ)
		}
	}
}

func TestSharedLimitRefusalDoesNotSpendCommandTokens(t *testing.T) {
	now := time.Now()
	r := &userRates{}
	r.all = bucket{tokens: 0, last: now} // Shared bucket empty.
	if r.allow(now, "message.send", &sendLimit) {
		t.Fatal("allowed with an empty shared bucket")
	}
	if r.commands["message.send"] != nil {
		t.Fatal("command bucket was charged although the shared bucket refused")
	}
}

func TestProtocolErrorForUnknownErrorsIsGeneric(t *testing.T) {
	if got := protocolErrorFor(errors.New("pq: secret details")); got.Code != "internal_error" || got.Message != "Request failed" {
		t.Fatalf("%+v", got)
	}
	wrapped := errors.Join(errors.New("context"), chat.ErrRoomUnavailable)
	if got := protocolErrorFor(wrapped); got.Code != "room_unavailable" {
		t.Fatalf("wrapped error: %+v", got)
	}
}

func frameTypeOf(t *testing.T, raw []byte) string {
	t.Helper()
	var frame struct {
		Type      string `json:"type"`
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil || frame.RequestID != "req" {
		t.Fatalf("frame %s: %v", raw, err)
	}
	return frame.Type
}
