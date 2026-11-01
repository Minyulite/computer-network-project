package store

import (
	"context"
	"testing"
	"time"
)

func TestAccountSessionCreationRollsBackTogether(t *testing.T) {
	dsn, _ := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := Open(ctx, dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(ctx, db.Pool); err != nil {
		t.Fatal(err)
	}
	_, _, err = db.CreateAccountSession(ctx,
		UserRecord{ID: "00000000-0000-4000-8000-000000000001", Username: "rollback", PasswordHash: "fixture"},
		NewSession{ID: "10000000-0000-4000-8000-000000000001", UserID: "00000000-0000-4000-8000-000000000001",
			TokenDigest: []byte{1}, ExpiresAt: time.Now().Add(time.Hour)})
	if err == nil {
		t.Fatal("invalid session was accepted")
	}
	var count int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE username=$1", "rollback").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("account survived failed session insert")
	}
}
