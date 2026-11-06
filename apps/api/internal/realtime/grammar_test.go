package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"example.com/computer-network/project/apps/api/internal/ai/grammar"
	"example.com/computer-network/project/apps/api/internal/auth"
)

// blockingCorrector holds every call open so tests control when "the AI" answers.
// It stands in at the hub boundary only and is never reachable from the application.
type blockingCorrector struct {
	entered chan string
	release chan struct{}
	err     error
}

func newBlockingCorrector() *blockingCorrector {
	return &blockingCorrector{entered: make(chan string, 16), release: make(chan struct{})}
}

func (b *blockingCorrector) Correct(ctx context.Context, text, _, _ string) (string, error) {
	b.entered <- text
	select {
	case <-b.release:
		return "fixed: " + text, b.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func grammarHub(corrector GrammarCorrector, clients ...*client) *Hub {
	h := &Hub{grammar: corrector, control: make(chan any, 32), finished: make(chan struct{}),
		connections: map[string]*client{}, byUser: map[string]map[string]*client{}, bySession: map[string]map[string]*client{},
		rates: map[string]*userRates{}, grammarInflight: map[string]int{}}
	for _, c := range clients {
		h.connections[c.id] = c
		if h.byUser[c.identity.User.ID] == nil {
			h.byUser[c.identity.User.ID] = map[string]*client{}
		}
		h.byUser[c.identity.User.ID][c.id] = c
		if h.bySession[c.identity.SessionID] == nil {
			h.bySession[c.identity.SessionID] = map[string]*client{}
		}
		h.bySession[c.identity.SessionID][c.id] = c
	}
	return h
}

func grammarClient(t *testing.T, id, userID string) *client {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &client{id: id, identity: auth.Identity{SessionID: "session-" + id, User: auth.User{ID: userID, Username: userID}},
		ctx: ctx, cancel: cancel, out: make(chan []byte, outboundQueue), closeDone: make(chan struct{})}
}

func awaitDone(t *testing.T, h *Hub) grammarDone {
	t.Helper()
	select {
	case value := <-h.control:
		done, ok := value.(grammarDone)
		if !ok {
			t.Fatalf("unexpected control value %T", value)
		}
		return done
	case <-time.After(4 * time.Second):
		t.Fatal("correction did not finish")
		return grammarDone{}
	}
}

func awaitEntered(t *testing.T, b *blockingCorrector) {
	t.Helper()
	select {
	case <-b.entered:
	case <-time.After(4 * time.Second):
		t.Fatal("corrector was not called")
	}
}

func decodeFrame(t *testing.T, raw []byte) (typ, id string, payload map[string]string) {
	t.Helper()
	var frame struct {
		Type      string            `json:"type"`
		RequestID string            `json:"requestId"`
		Payload   map[string]string `json:"payload"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatal(err)
	}
	return frame.Type, frame.RequestID, frame.Payload
}

func TestGrammarDoesNotBlockHubAndAnswersOnlyTheRequester(t *testing.T) {
	alice, bob := grammarClient(t, "alice-1", "alice"), grammarClient(t, "bob-1", "bob")
	ai := newBlockingCorrector()
	h := grammarHub(ai, alice, bob)

	// Returning while the AI is still working is what keeps other users' chat moving.
	returned := make(chan error, 1)
	go func() { returned <- h.startGrammar(alice, "req-1", "alice", grammarRequest{Text: "i has went"}) }()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("startGrammar blocked on the AI call")
	}
	awaitEntered(t, ai)
	if len(alice.out) != 0 {
		t.Fatal("answered before the AI finished")
	}

	close(ai.release)
	h.handleControl(awaitDone(t, h))

	typ, id, payload := decodeFrame(t, <-alice.out)
	if typ != "request.ok" || id != "req-1" || payload["corrected"] != "fixed: i has went" {
		t.Fatalf("frame %s %s %v", typ, id, payload)
	}
	if len(bob.out) != 0 {
		t.Fatal("another user received a private correction")
	}
	if len(h.grammarInflight) != 0 {
		t.Fatalf("inflight not released: %v", h.grammarInflight)
	}
}

func TestGrammarLimitIsSharedAcrossDevices(t *testing.T) {
	phone, laptop := grammarClient(t, "phone", "alice"), grammarClient(t, "laptop", "alice")
	other := grammarClient(t, "bob-1", "bob")
	ai := newBlockingCorrector()
	h := grammarHub(ai, phone, laptop, other)

	for i, c := range []*client{phone, laptop, phone} {
		if err := h.startGrammar(c, "req", "alice", grammarRequest{Text: "text"}); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		awaitEntered(t, ai)
	}
	if err := h.startGrammar(laptop, "req", "alice", grammarRequest{Text: "text"}); !errors.Is(err, errGrammarBusy) {
		t.Fatalf("fourth request: %v", err)
	}
	if err := h.startGrammar(other, "req", "bob", grammarRequest{Text: "text"}); err != nil {
		t.Fatalf("another user was limited: %v", err)
	}
	awaitEntered(t, ai)

	close(ai.release)
	for range 4 {
		h.handleControl(awaitDone(t, h))
	}
	if err := h.startGrammar(phone, "req", "alice", grammarRequest{Text: "text"}); err != nil {
		t.Fatalf("slot not released: %v", err)
	}
	h.handleControl(awaitDone(t, h))
}

func TestGrammarCancelledWhenClientDisconnects(t *testing.T) {
	alice := grammarClient(t, "alice-1", "alice")
	ai := newBlockingCorrector()
	h := grammarHub(ai, alice)
	if err := h.startGrammar(alice, "req-1", "alice", grammarRequest{Text: "text"}); err != nil {
		t.Fatal(err)
	}
	awaitEntered(t, ai)

	h.removeClient(alice, 1000, "gone") // Cancels the client's context.
	done := awaitDone(t, h)
	if !errors.Is(done.err, context.Canceled) {
		t.Fatalf("err = %v", done.err)
	}
	h.handleControl(done)
	if len(alice.out) != 0 {
		t.Fatal("frame queued for a removed client")
	}
	if len(h.grammarInflight) != 0 {
		t.Fatalf("inflight not released: %v", h.grammarInflight)
	}
}

func TestGrammarErrorsMapToProtocolCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{grammar.ErrInvalidInput, "grammar_invalid"},
		{grammar.ErrNotConfigured, "grammar_unavailable"},
		{grammar.ErrIncomplete, "grammar_incomplete"},
		{grammar.ErrUnavailable, "ai_unavailable"},
		{errGrammarBusy, "grammar_busy"},
	} {
		alice := grammarClient(t, "alice-1", "alice")
		h := grammarHub(nil, alice)
		h.grammarInflight["alice"] = 1
		h.finishGrammar(grammarDone{client: alice, userID: "alice", requestID: "req", err: tc.err})
		typ, id, payload := decodeFrame(t, <-alice.out)
		if typ != "request.error" || id != "req" || payload["code"] != tc.code {
			t.Errorf("%v: frame %s %s %v", tc.err, typ, id, payload)
		}
	}
}

func TestGrammarWorkerStopsWhenHubHasFinished(t *testing.T) {
	alice := grammarClient(t, "alice-1", "alice")
	ai := newBlockingCorrector()
	h := grammarHub(ai, alice)
	h.control = make(chan any) // Unbuffered with no reader, as after shutdown.
	if err := h.startGrammar(alice, "req", "alice", grammarRequest{Text: "text"}); err != nil {
		t.Fatal(err)
	}
	awaitEntered(t, ai)
	close(h.finished)
	close(ai.release)
	// sendControl must return through h.finished rather than leak the worker.
	if err := h.sendControl(grammarDone{}); !errors.Is(err, errClosed) {
		t.Fatalf("err = %v", err)
	}
}
