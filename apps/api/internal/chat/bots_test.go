package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"example.com/computer-network/project/apps/api/internal/auth"
)

func testBot(t *testing.T, f fixture, owner auth.Identity) BotDetail {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bot, err := f.chat.CreateBot(ctx, owner, BotInput{Name: " Cafe Bot ", Description: "A coffee shop in Bangkok.", Enabled: true,
		Templates: []BotTemplateInput{{Question: "What time do you open?", Answer: "10.00"}}})
	if err != nil {
		t.Fatal(err)
	}
	return bot
}

func TestBotOwnershipAndPrivacy(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	bot := testBot(t, f, f.a)
	if bot.Name != "Cafe Bot" || len(bot.Templates) != 1 || bot.Templates[0].Answer != "10.00" {
		t.Fatalf("unexpected bot: %+v", bot)
	}
	// Only the owner reads the configuration or edits it.
	if _, err := f.chat.GetBot(ctx, f.b, bot.ID); !errors.Is(err, ErrBotUnavailable) {
		t.Fatalf("non-owner read: %v", err)
	}
	if _, err := f.chat.UpdateBot(ctx, f.b, bot.ID, BotInput{Name: "x", Enabled: true}); !errors.Is(err, ErrBotUnavailable) {
		t.Fatalf("non-owner update: %v", err)
	}
	// The catalogue shows the bot without its description or templates.
	page, err := f.chat.BotCatalog(ctx, f.b, PageOptions{})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != bot.ID {
		t.Fatalf("catalogue: %+v %v", page, err)
	}
	// A customer's room with the bot is invisible to the owner.
	room, created, err := f.chat.OpenBot(ctx, f.b, bot.ID)
	if err != nil || !created || room.Kind != "bot" || room.BotName != "Cafe Bot" {
		t.Fatalf("open: %+v %v %v", room, created, err)
	}
	again, created, err := f.chat.OpenBot(ctx, f.b, bot.ID)
	if err != nil || created || again.ID != room.ID {
		t.Fatalf("reopen: %+v %v %v", again, created, err)
	}
	owned, err := f.chat.OwnRooms(ctx, f.a, PageOptions{})
	if err != nil || len(owned.Items) != 0 {
		t.Fatalf("owner must not see customer rooms: %+v %v", owned, err)
	}
	if _, err := f.chat.History(ctx, f.a, room.ID, HistoryOptions{}); !errors.Is(err, ErrRoomUnavailable) {
		t.Fatalf("owner history: %v", err)
	}
	if _, err := f.chat.SendMessage(ctx, f.a, room.ID, mustUUID(t), "hi"); !errors.Is(err, ErrRoomUnavailable) {
		t.Fatalf("owner send: %v", err)
	}
	if _, err := f.chat.History(ctx, f.c, room.ID, HistoryOptions{}); !errors.Is(err, ErrRoomUnavailable) {
		t.Fatalf("stranger history: %v", err)
	}
	// The owner can still try their own bot.
	if _, created, err := f.chat.OpenBot(ctx, f.a, bot.ID); err != nil || !created {
		t.Fatalf("owner opens own bot: %v %v", created, err)
	}
}

func TestBotReplyIsStoredOnceAndDelivered(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	bot := testBot(t, f, f.a)
	room, _, err := f.chat.OpenBot(ctx, f.b, bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	sent, err := f.chat.SendMessage(ctx, f.b, room.ID, mustUUID(t), "What time do you open?")
	if err != nil || sent.RoomKind != "bot" || !sent.Created {
		t.Fatalf("send: %+v %v", sent, err)
	}
	first, err := f.chat.SendBotReply(ctx, room.ID, sent.Message.ID, "10.00")
	if err != nil || !first.Created || first.Message.Seq != 2 || !containsID(first.RecipientUserIDs, f.b.User.ID) {
		t.Fatalf("reply: %+v %v", first, err)
	}
	if first.Message.Sender.ID == f.a.User.ID || first.Message.Sender.ID == f.b.User.ID {
		t.Fatal("reply must come from the bot's system user")
	}
	second, err := f.chat.SendBotReply(ctx, room.ID, sent.Message.ID, "10.00")
	if err != nil || second.Created || second.Message.ID != first.Message.ID {
		t.Fatalf("replying twice must store once: %+v %v", second, err)
	}
	history, err := f.chat.History(ctx, f.b, room.ID, HistoryOptions{})
	if err != nil || len(history.Messages) != 2 || history.Messages[1].Text != "10.00" {
		t.Fatalf("history: %+v %v", history, err)
	}
	bc, err := f.chat.BotRoomContext(ctx, room.ID)
	if err != nil || len(bc.Templates) != 1 || len(bc.History) != 2 || bc.History[0].FromBot || !bc.History[1].FromBot {
		t.Fatalf("context: %+v %v", bc, err)
	}
}

func TestBotLimitsAndValidation(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := f.chat.CreateBot(ctx, f.a, BotInput{Name: "  "}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blank name: %v", err)
	}
	if _, err := f.chat.CreateBot(ctx, f.a, BotInput{Name: "ok", Templates: []BotTemplateInput{{Question: "q", Answer: ""}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blank answer: %v", err)
	}
	tooMany := make([]BotTemplateInput, MaxBotTemplates+1)
	for i := range tooMany {
		tooMany[i] = BotTemplateInput{Question: "q", Answer: "a"}
	}
	if _, err := f.chat.CreateBot(ctx, f.a, BotInput{Name: "ok", Templates: tooMany}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("too many templates: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := f.chat.CreateBot(ctx, f.a, BotInput{Name: "bot"}); err != nil {
			t.Fatalf("bot %d: %v", i, err)
		}
	}
	if _, err := f.chat.CreateBot(ctx, f.a, BotInput{Name: "one too many"}); !errors.Is(err, ErrBotLimit) {
		t.Fatalf("limit: %v", err)
	}
	// Updating replaces the template set and can disable the bot.
	bot := testBot(t, f, f.b)
	updated, err := f.chat.UpdateBot(ctx, f.b, bot.ID, BotInput{Name: "Renamed", Enabled: false,
		Templates: []BotTemplateInput{{Question: "a?", Answer: "b"}, {Question: "c?", Answer: "d"}}})
	if err != nil || updated.Name != "Renamed" || updated.Enabled || len(updated.Templates) != 2 {
		t.Fatalf("update: %+v %v", updated, err)
	}
	if _, _, err := f.chat.OpenBot(ctx, f.c, bot.ID); !errors.Is(err, ErrBotUnavailable) {
		t.Fatalf("disabled bot must not open: %v", err)
	}
}

func TestBotUsersCannotLogInOrBeChatTargets(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	bot := testBot(t, f, f.a)
	room, _, err := f.chat.OpenBot(ctx, f.b, bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	members, err := f.db.DirectMemberIDs(ctx, room.ID)
	if err != nil || len(members) != 2 {
		t.Fatalf("members: %v %v", members, err)
	}
	for _, id := range members {
		if id == f.b.User.ID {
			continue
		}
		if _, _, err := f.chat.OpenDirect(ctx, f.c, id); !errors.Is(err, ErrUserUnavailable) {
			t.Fatalf("direct chat with bot user: %v", err)
		}
	}
}
