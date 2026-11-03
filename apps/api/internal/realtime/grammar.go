package realtime

import (
	"context"
	"errors"
	"log"

	"example.com/computer-network/project/apps/api/internal/grammar"
)

const maxGrammarInflight = 3 // Per user, shared across that user's devices.

var errGrammarBusy = errors.New("too many grammar corrections in progress")

// GrammarCorrector returns the replacement for text; before and after are context only.
type GrammarCorrector interface {
	Correct(ctx context.Context, text, before, after string) (string, error)
}

type grammarRequest struct {
	Text   string `json:"text"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type grammarResponse struct {
	Corrected string `json:"corrected"`
}

// grammarDone returns a finished correction to the goroutine that owns hub state.
type grammarDone struct {
	client    *client
	userID    string
	requestID string
	corrected string
	err       error
}

// startGrammar never waits for the AI: run handles every user's commands in
// order, so a slow reply here would stall all chat. The worker must not touch
// hub state or call h.ok/h.fail; it reports back through the control channel.
// The client's context cancels the call on disconnect or session revocation.
func (h *Hub) startGrammar(c *client, requestID, userID string, p grammarRequest) error {
	if h.grammarInflight[userID] >= maxGrammarInflight {
		return errGrammarBusy
	}
	h.grammarInflight[userID]++
	go func() {
		corrected, err := h.grammar.Correct(c.ctx, p.Text, p.Before, p.After)
		// sendControl fails only after the hub stopped, when no one needs the result.
		_ = h.sendControl(grammarDone{client: c, userID: userID, requestID: requestID, corrected: corrected, err: err})
	}()
	return nil
}

func (h *Hub) finishGrammar(done grammarDone) {
	if h.grammarInflight[done.userID] <= 1 {
		delete(h.grammarInflight, done.userID)
	} else {
		h.grammarInflight[done.userID]--
	}
	switch {
	case done.err == nil:
		h.ok(done.client, done.requestID, grammarResponse{done.corrected})
	case errors.Is(done.err, context.Canceled):
		// The client is gone; enqueue would drop the frame anyway.
	default:
		// Never log the text: it is the user's private draft.
		if errors.Is(done.err, grammar.ErrUnavailable) {
			log.Printf("grammar.correct %s: %v", done.requestID, done.err)
		}
		h.fail(done.client, done.requestID, done.err)
	}
}
