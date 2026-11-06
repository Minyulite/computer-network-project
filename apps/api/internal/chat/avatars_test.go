package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/avatar"
)

func TestGroupListShowsTheFirstTwoMembersPictures(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	group, err := f.chat.CreateGroup(ctx, f.a, "study")
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.chat.Groups(ctx, f.c, PageOptions{})
	if err != nil || len(page.Items) != 1 || len(page.Items[0].Preview) != 1 || page.Items[0].Preview[0].Username != "alice" {
		t.Fatalf("one member: %+v %v", page, err)
	}
	for _, joiner := range []auth.Identity{f.b, f.c} {
		if _, err := f.chat.JoinGroup(ctx, joiner, group.ID); err != nil {
			t.Fatal(err)
		}
	}
	page, err = f.chat.Groups(ctx, f.c, PageOptions{})
	if err != nil || len(page.Items[0].Preview) != 2 || page.Items[0].Preview[0].Username != "alice" ||
		page.Items[0].Preview[1].Username != "bob" || !avatar.ValidPerson(page.Items[0].Preview[1].Avatar) {
		t.Fatalf("two members: %+v %v", page, err)
	}
}

func TestDirectRoomsNameBothPeopleWithPictures(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	room, _, err := f.chat.OpenDirect(ctx, f.a, f.b.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, person := range room.Direct {
		names[person.Username] = avatar.ValidPerson(person.Avatar)
	}
	if len(room.Direct) != 2 || !names["alice"] || !names["bob"] {
		t.Fatalf("direct people: %+v", room.Direct)
	}
}

func TestBotPictureIsChosenKeptAndShownInItsRoom(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	chosen, err := f.chat.CreateBot(ctx, f.a, BotInput{Name: "Cafe", Avatar: "b03", Enabled: true})
	if err != nil || chosen.Avatar != "b03" {
		t.Fatalf("chosen: %+v %v", chosen, err)
	}
	for _, bad := range []string{"p01", "b09", "x"} {
		if _, err := f.chat.CreateBot(ctx, f.a, BotInput{Name: "Bad", Avatar: bad, Enabled: true}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("avatar %q: %v", bad, err)
		}
	}
	assigned, err := f.chat.CreateBot(ctx, f.a, BotInput{Name: "Plain", Enabled: true})
	if err != nil || !avatar.ValidBot(assigned.Avatar) {
		t.Fatalf("assigned: %+v %v", assigned, err)
	}
	kept, err := f.chat.UpdateBot(ctx, f.a, chosen.ID, BotInput{Name: "Cafe", Enabled: true})
	if err != nil || kept.Avatar != "b03" {
		t.Fatalf("update without a picture changed it: %+v %v", kept, err)
	}
	changed, err := f.chat.UpdateBot(ctx, f.a, chosen.ID, BotInput{Name: "Cafe", Avatar: "b05", Enabled: true})
	if err != nil || changed.Avatar != "b05" {
		t.Fatalf("changed: %+v %v", changed, err)
	}
	room, _, err := f.chat.OpenBot(ctx, f.b, chosen.ID)
	if err != nil || room.BotAvatar != "b05" {
		t.Fatalf("room picture: %+v %v", room, err)
	}
}
