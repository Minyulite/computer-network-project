package realtime

import (
	"context"
	"errors"
	"log"
	"time"

	"example.com/computer-network/project/apps/api/internal/chat"
)

const maxBotInflight = 3 // Per user, shared across that user's devices.
const botReplyTimeout = 60 * time.Second

// BotReplier answers a customer's message in a bot room and stores the reply.
// When busy is true it must reply without calling any AI service.
type BotReplier interface {
	Reply(ctx context.Context, roomID, customerMessageID, text string, busy bool) (chat.SendResult, error)
}

// botDone returns a stored bot reply to the goroutine that owns hub state.
type botDone struct {
	userID string
	roomID string
	result chat.SendResult
	err    error
}

// startBot never waits for the AI, for the same reason as startGrammar: run
// handles every user's commands in order. The worker must not touch hub state.
// It is not tied to the client's connection: the customer's message is already
// stored, so the reply is stored too and appears when they reconnect.
func (h *Hub) startBot(userID, roomID, messageID, text string) {
	if h.bots == nil {
		return
	}
	if h.botInflight == nil {
		h.botInflight = map[string]int{}
	}
	busy := h.botInflight[userID] >= maxBotInflight
	h.botInflight[userID]++
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), botReplyTimeout)
		defer cancel()
		result, err := h.bots.Reply(ctx, roomID, messageID, text, busy)
		_ = h.sendControl(botDone{userID: userID, roomID: roomID, result: result, err: err})
	}()
}

func (h *Hub) finishBot(done botDone) {
	if h.botInflight[done.userID] <= 1 {
		delete(h.botInflight, done.userID)
	} else {
		h.botInflight[done.userID]--
	}
	if done.err != nil {
		// Never log message text: it is a customer's private conversation.
		if !errors.Is(done.err, context.Canceled) {
			log.Printf("bot reply for room %s failed: %v", done.roomID, done.err)
		}
		return
	}
	if done.result.Created {
		h.broadcastUsers(done.result.RecipientUserIDs, "message.created", done.result.Message)
	}
}
