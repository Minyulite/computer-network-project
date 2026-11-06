package chat

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func lastOf(t *testing.T, f fixture, who string, roomID string) *LastMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	caller := f.a
	if who == "b" {
		caller = f.b
	}
	page, err := f.chat.OwnRooms(ctx, caller, PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, room := range page.Items {
		if room.ID == roomID {
			return room.Last
		}
	}
	t.Fatalf("room %s is not in %s's list", roomID, who)
	return nil
}

func TestRoomListCarriesTheNewestVisibleMessage(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	direct, _, err := f.chat.OpenDirect(ctx, f.a, f.b.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if last := lastOf(t, f, "a", direct.ID); last != nil {
		t.Fatalf("a room with no messages has a preview: %+v", last)
	}
	for _, step := range []struct{ from, text string }{{"a", "hello"}, {"b", "second line"}} {
		caller := f.a
		if step.from == "b" {
			caller = f.b
		}
		if _, err := f.chat.SendMessage(ctx, caller, direct.ID, mustUUID(t), step.text); err != nil {
			t.Fatal(err)
		}
	}
	for _, who := range []string{"a", "b"} {
		last := lastOf(t, f, who, direct.ID)
		if last == nil || last.Text != "second line" || last.SenderName != "bob" || last.SenderID != f.b.User.ID || last.SentAt.IsZero() {
			t.Fatalf("%s sees %+v", who, last)
		}
	}

	// A long message is cut to one preview line.
	long := strings.Repeat("ก", 300)
	if _, err := f.chat.SendMessage(ctx, f.a, direct.ID, mustUUID(t), long); err != nil {
		t.Fatal(err)
	}
	if last := lastOf(t, f, "b", direct.ID); last == nil || utf8.RuneCountInString(last.Text) != maxPreviewRunes {
		t.Fatalf("preview length: %+v", last)
	}

	// Someone who joins a group later never sees what was said before they came.
	group, err := f.chat.CreateGroup(ctx, f.a, "study")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.chat.SendMessage(ctx, f.a, group.ID, mustUUID(t), "before bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.chat.JoinGroup(ctx, f.b, group.ID); err != nil {
		t.Fatal(err)
	}
	if last := lastOf(t, f, "b", group.ID); last != nil {
		t.Fatalf("a new member sees an earlier message: %+v", last)
	}
	if last := lastOf(t, f, "a", group.ID); last == nil || last.Text != "before bob" {
		t.Fatalf("the author lost the preview: %+v", last)
	}
	if _, err := f.chat.SendMessage(ctx, f.a, group.ID, mustUUID(t), "after bob"); err != nil {
		t.Fatal(err)
	}
	if last := lastOf(t, f, "b", group.ID); last == nil || last.Text != "after bob" || last.SenderName != "alice" {
		t.Fatalf("member after joining: %+v", last)
	}

	// The public group catalogue never carries message text.
	page, err := f.chat.Groups(ctx, f.c, PageOptions{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("catalogue: %+v %v", page, err)
	}
}
