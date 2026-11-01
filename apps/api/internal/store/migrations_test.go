package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func isolatedDatabase(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	if raw == os.Getenv("DATABASE_URL") {
		t.Fatal("TEST_DATABASE_URL must use a dedicated test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal("test database connection configuration failed")
	}
	t.Cleanup(admin.Close)
	if err := admin.Ping(ctx); err != nil {
		message := strings.ReplaceAll(err.Error(), raw, "[redacted URL]")
		if parsed, parseErr := url.Parse(raw); parseErr == nil && parsed.User != nil {
			if password, ok := parsed.User.Password(); ok {
				message = strings.ReplaceAll(message, password, "[redacted password]")
			}
		}
		t.Fatal("test database unavailable: ", message)
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "t2_" + hex.EncodeToString(random[:])
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal("invalid TEST_DATABASE_URL")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String(), admin
}

func TestMigrationsFreshRepeatedConcurrentAndRestart(t *testing.T) {
	dsn, _ := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := Open(ctx, dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var wg sync.WaitGroup
	errors := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errors <- Migrate(ctx, db.Pool) }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(ctx, db.Pool); err != nil {
		t.Fatal("repeat migration:", err)
	}
	var versions, tables int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name IN ('users','sessions','rooms','room_members','messages')").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if versions != 1 || tables != 5 {
		t.Fatalf("versions=%d tables=%d", versions, tables)
	}
	_, err = db.Pool.Exec(ctx, "INSERT INTO users (id, username, password_hash) VALUES ($1, $2, $3)", "00000000-0000-0000-0000-000000000001", "persisted", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	reopened, err := Open(ctx, dsn, 2)
	if err != nil {
		t.Fatal("reopen:", err)
	}
	defer reopened.Close()
	if err := Migrate(ctx, reopened.Pool); err != nil {
		t.Fatal("migration after reopen:", err)
	}
	var name string
	if err := reopened.Pool.QueryRow(ctx, "SELECT username FROM users WHERE id = $1", "00000000-0000-0000-0000-000000000001").Scan(&name); err != nil || name != "persisted" {
		t.Fatalf("persistence: %q %v", name, err)
	}
}

func TestFailedMigrationRollsBack(t *testing.T) {
	dsn, _ := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := Open(ctx, dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrations, err := embeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	migrations = append(migrations, migration{version: 2, name: "002_broken.sql", sql: "CREATE TABLE users (id integer)"})
	if err := applyMigrations(ctx, db.Pool, migrations); err == nil {
		t.Fatal("broken migration succeeded")
	}
	var versionTable, userTable bool
	if err := db.Pool.QueryRow(ctx, "SELECT to_regclass('schema_migrations') IS NOT NULL, to_regclass('users') IS NOT NULL").Scan(&versionTable, &userTable); err != nil {
		t.Fatal(err)
	}
	if versionTable || userTable {
		t.Fatalf("failed migration left tables: version=%v users=%v", versionTable, userTable)
	}
	if err := Migrate(ctx, db.Pool); err != nil {
		t.Fatal("valid migration after rollback:", err)
	}
}

func TestPoolBoundAndUnavailable(t *testing.T) {
	dsn, _ := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := Open(ctx, dsn, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	first, err := db.Pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	if second, err := db.Pool.Acquire(waitCtx); err == nil {
		second.Release()
		t.Fatal("pool exceeded max connections")
	}
	if db.Pool.Stat().TotalConns() > 1 {
		t.Fatal("pool exceeded max connections")
	}
	first.Release()
	second, err := db.Pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquire after release:", err)
	}
	second.Release()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Host = "127.0.0.1:1"
	badCtx, cancelBad := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelBad()
	if bad, err := Open(badCtx, u.String(), 1); err == nil {
		bad.Close()
		t.Fatal("startup accepted database outage")
	}
}
