// Command seed wipes every row in the chat database and fills it with demo
// users, groups and messages. Every user's password is Test1234.
//
//	cd apps/api && go run ./cmd/seed
//
// It reads the same .env as the API, so it only touches that database.
package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/chat"
	"example.com/computer-network/project/apps/api/internal/config"
	"example.com/computer-network/project/apps/api/internal/id"
	"example.com/computer-network/project/apps/api/internal/store"
)

const password = "Test1234"

var names = []string{"Alice", "John", "Mali", "Ton", "Bob", "Nina", "Max", "Pim", "Leo", "Dora",
	"Eli", "Fah", "Gus", "Hana", "Ivan", "Joy"}

var groups = []string{"Network study", "Reading club", "Lab 3 team", "Football", "Lunch", "Movie night", "Board games", "Late night"}

var lines = []string{
	"Anyone free tonight?", "I have the notes", "Sounds good", "See you at 7", "Can you send the file?",
	"Just finished the lab", "Thanks!", "Where are we meeting?", "I'll bring snacks", "Running a bit late",
	"Did you see the new schedule?", "Let's do it", "Same here", "Good idea", "Check the doc I shared",
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := store.Open(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConns)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := store.Migrate(ctx, db.Pool); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if _, err := db.Pool.Exec(ctx, `TRUNCATE users, sessions, rooms, room_members, messages, bots, bot_templates RESTART IDENTITY CASCADE`); err != nil {
		return fmt.Errorf("clear database: %w", err)
	}
	log.Print("database cleared")

	authService, err := auth.New(db)
	if err != nil {
		return err
	}
	chatService := chat.New(db)

	people := make([]auth.Identity, 0, len(names))
	for i, name := range names {
		// Signup is limited per IP, so each user comes from its own address.
		result, err := authService.Signup(ctx, name, password, "", fmt.Sprintf("seed-%d", i), "")
		if err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
		identity, err := authService.ValidateSession(ctx, result.Token)
		if err != nil {
			return fmt.Errorf("session for %s: %w", name, err)
		}
		people = append(people, identity)
	}
	log.Printf("created %d users", len(people))

	for _, title := range groups {
		owner := people[rand.Intn(len(people))]
		room, err := chatService.CreateGroup(ctx, owner, title)
		if err != nil {
			return fmt.Errorf("create group %s: %w", title, err)
		}
		members := []auth.Identity{owner}
		for _, person := range rand.Perm(len(people)) {
			// Some people stay out, so every group still has someone who can join it.
			if len(members) >= 2+rand.Intn(6) {
				break
			}
			if people[person].User.ID == owner.User.ID {
				continue
			}
			if _, err := chatService.JoinGroup(ctx, people[person], room.ID); err != nil {
				return err
			}
			members = append(members, people[person])
		}
		for range 3 + rand.Intn(6) {
			sender := members[rand.Intn(len(members))]
			messageID, err := id.New()
			if err != nil {
				return err
			}
			if _, err := chatService.SendMessage(ctx, sender, room.ID, messageID, lines[rand.Intn(len(lines))]); err != nil {
				return err
			}
		}
	}
	log.Printf("created %d groups; every user's password is %s", len(groups), password)
	return nil
}
