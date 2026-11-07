package realtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"example.com/computer-network/project/apps/api/internal/ai/botai"
	"example.com/computer-network/project/apps/api/internal/ai/grammar"
	"example.com/computer-network/project/apps/api/internal/ai/llm"
	"example.com/computer-network/project/apps/api/internal/chat"
)

// scriptedLLM answers every classification with the template match and can be held back.
type scriptedLLM struct{ release chan struct{} }

func (s scriptedLLM) Generate(ctx context.Context, req llm.Request) (string, error) {
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return `{"decision":"template","template":"t1"}`, nil
}

func botRig(t *testing.T, generator botai.Generator) *testRig {
	t.Helper()
	return newRigFull(t, grammar.New(llm.New(llm.Config{})), func(c *chat.Service) BotReplier { return botai.New(generator, c) })
}

func TestBotConversationIsPrivateAndReplied(t *testing.T) {
	r := botRig(t, scriptedLLM{})
	alice := r.socket(t, r.signup(t, "alice"))
	bob := r.socket(t, r.signup(t, "bob"))
	carol := r.socket(t, r.signup(t, "carol"))

	send(t, alice, "mk", "bot.create", map[string]any{"name": "Cafe", "avatar": "b04", "description": "A coffee shop.",
		"templates": []map[string]string{{"question": "What time do you open?", "answer": "10.00"}}})
	created := until(t, alice, "request.ok", "mk")
	var detail struct {
		ID     string `json:"id"`
		Avatar string `json:"avatar"`
	}
	if err := json.Unmarshal(created.Payload, &detail); err != nil || detail.ID == "" {
		t.Fatalf("create: %s %v", created.Payload, err)
	}
	if detail.Avatar != "b04" {
		t.Fatalf("chosen picture was not kept: %s", created.Payload)
	}
	// Everyone learns about the bot, but not its description or templates.
	broadcast := until(t, bob, "bot.upserted", "")
	if strings.Contains(string(broadcast.Payload), "coffee") || strings.Contains(string(broadcast.Payload), "10.00") {
		t.Fatalf("catalogue event leaks configuration: %s", broadcast.Payload)
	}
	send(t, bob, "peek", "bot.get", map[string]string{"botId": detail.ID})
	if got := string(field(t, until(t, bob, "request.error", "peek"), "code")); got != `"bot_unavailable"` {
		t.Fatalf("non-owner read: %s", got)
	}

	send(t, bob, "open", "bot.open", map[string]string{"botId": detail.ID})
	var opened struct {
		Room struct {
			ID      string `json:"id"`
			Kind    string `json:"kind"`
			BotName string `json:"botName"`
		} `json:"room"`
	}
	if err := json.Unmarshal(until(t, bob, "request.ok", "open").Payload, &opened); err != nil || opened.Room.Kind != "bot" || opened.Room.BotName != "Cafe" {
		t.Fatalf("open: %+v %v", opened, err)
	}
	send(t, bob, "say", "message.send", map[string]string{"roomId": opened.Room.ID,
		"clientMessageId": "11111111-1111-4111-8111-111111111111", "text": "เปิดกี่โมง"})
	until(t, bob, "request.ok", "say")
	own := until(t, bob, "message.created", "")
	if !strings.Contains(string(own.Payload), "เปิดกี่โมง") {
		t.Fatalf("own message: %s", own.Payload)
	}
	reply := until(t, bob, "message.created", "")
	if got := string(field(t, reply, "text")); got != `"10.00"` {
		t.Fatalf("bot reply: %s", reply.Payload)
	}

	// Neither the owner nor a stranger sees the conversation.
	send(t, alice, "rooms", "rooms.list", map[string]any{})
	if got := string(field(t, until(t, alice, "request.ok", "rooms"), "items")); got != `[]` {
		t.Fatalf("owner rooms: %s", got)
	}
	send(t, carol, "hist", "room.history", map[string]string{"roomId": opened.Room.ID})
	until(t, carol, "request.error", "hist")
	send(t, alice, "hist", "room.history", map[string]string{"roomId": opened.Room.ID})
	until(t, alice, "request.error", "hist")
}

func TestSlowBotDoesNotBlockTheHub(t *testing.T) {
	release := make(chan struct{})
	r := botRig(t, scriptedLLM{release: release})
	alice := r.socket(t, r.signup(t, "alice"))
	bob := r.socket(t, r.signup(t, "bob"))
	send(t, alice, "mk", "bot.create", map[string]any{"name": "Cafe", "description": "Shop.",
		"templates": []map[string]string{{"question": "Hours?", "answer": "10.00"}}})
	var detail struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(until(t, alice, "request.ok", "mk").Payload, &detail); err != nil {
		t.Fatal(err)
	}
	send(t, bob, "open", "bot.open", map[string]string{"botId": detail.ID})
	var opened struct {
		Room struct {
			ID string `json:"id"`
		} `json:"room"`
	}
	if err := json.Unmarshal(until(t, bob, "request.ok", "open").Payload, &opened); err != nil {
		t.Fatal(err)
	}
	send(t, bob, "say", "message.send", map[string]string{"roomId": opened.Room.ID,
		"clientMessageId": "22222222-2222-4222-8222-222222222222", "text": "Hours?"})
	until(t, bob, "request.ok", "say") // Acknowledged while the AI is still held back.

	send(t, alice, "list", "users.list", map[string]any{})
	until(t, alice, "request.ok", "list")
	send(t, bob, "list", "users.list", map[string]any{})
	until(t, bob, "request.ok", "list")

	close(release)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("bot reply never arrived")
		default:
		}
		frame := readFrame(t, bob)
		if frame.Type == "message.created" && string(field(t, frame, "text")) == `"10.00"` {
			return
		}
	}
}
