package realtime

import (
	"context"
	"strings"
	"testing"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/chat"
)

func TestQueueOverflowRemovesOnlySlowClient(t *testing.T) {
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	slow := &client{id: "slow", identity: auth.Identity{SessionID: "one", User: auth.User{ID: "a", Username: "alice"}}, ctx: ctx1, cancel: cancel1, out: make(chan []byte, outboundQueue)}
	healthy := &client{id: "healthy", identity: auth.Identity{SessionID: "two", User: auth.User{ID: "b", Username: "bob"}}, ctx: ctx2, cancel: cancel2, out: make(chan []byte, outboundQueue)}
	h := &Hub{connections: map[string]*client{"slow": slow, "healthy": healthy}, byUser: map[string]map[string]*client{"a": {"slow": slow}, "b": {"healthy": healthy}}, bySession: map[string]map[string]*client{"one": {"slow": slow}, "two": {"healthy": healthy}}, rates: map[string]*userRates{}}
	for i := 0; i < outboundQueue; i++ {
		slow.out <- []byte("full")
	}
	h.enqueue(slow, []byte("overflow"))
	if h.connections[slow.id] != nil || h.connections[healthy.id] == nil {
		t.Fatal("queue overflow affected wrong client")
	}
	h.enqueue(healthy, []byte("still live"))
	if len(healthy.out) != 2 {
		t.Fatalf("healthy client queue length %d", len(healthy.out))
	}
}

func TestHistoryResponseCapPreservesContinuation(t *testing.T) {
	messages := make([]chat.Message, 50)
	for i := range messages {
		messages[i] = chat.Message{Seq: int64(i + 1), Text: strings.Repeat("界", 2000)}
	}
	original := chat.HistoryPage{RoomID: uuid(), Messages: messages, UpToSeq: 50}
	for _, forward := range []bool{false, true} {
		page := fitHistory(original, forward, "history")
		if len(wire("request.ok", "history", page)) > responseLimit || !page.HasMore || len(page.Messages) == len(messages) {
			t.Fatal("history response cap not enforced")
		}
		if forward {
			if page.Messages[0].Seq != 1 || page.NextAfterSeq == nil || *page.NextAfterSeq != page.Messages[len(page.Messages)-1].Seq {
				t.Fatal("forward cursor skipped messages")
			}
		} else {
			if page.Messages[len(page.Messages)-1].Seq != 50 || page.NextBeforeSeq == nil || *page.NextBeforeSeq != page.Messages[0].Seq {
				t.Fatal("backward cursor skipped messages")
			}
		}
	}
}
