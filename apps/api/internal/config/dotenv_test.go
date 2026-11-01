package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "\ufeff# Local settings\r\n" +
		"DATABASE_URL='postgres://chat_app:secret@localhost:5432/chat?sslmode=disable'\r\n" +
		"export ALLOWED_ORIGINS=\"http://localhost:3000\"\r\n" +
		"PORT=9000 # local override\r\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	none := func(string) (string, bool) { return "", false }
	cfg, err := load(path, none)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9000 || cfg.DatabaseTarget != "localhost:5432/chat" || len(cfg.AllowedOrigins) != 1 || cfg.AllowedOrigins[0] != "http://localhost:3000" {
		t.Fatal("file settings were not loaded correctly")
	}
	cfg, err = load(path, func(key string) (string, bool) { return "9100", key == "PORT" })
	if err != nil || cfg.Port != 9100 {
		t.Fatal("process environment must override the file")
	}
	_, err = load(path, func(key string) (string, bool) { return "", key == "DATABASE_URL" })
	if err == nil || err.Error() != "DATABASE_URL is required" {
		t.Fatal("explicitly empty environment variable must not fall back to file credentials")
	}
}

func TestLoadWithoutDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	values := map[string]string{
		"DATABASE_URL":    "postgres://chat_app:secret@localhost/chat",
		"ALLOWED_ORIGINS": "http://localhost:3000",
	}
	lookup := func(key string) (string, bool) { value, exists := values[key]; return value, exists }
	if _, err := load(path, lookup); err != nil {
		t.Fatalf("deployment environment should work without a file: %v", err)
	}
	delete(values, "DATABASE_URL")
	if _, err := load(path, lookup); err == nil || err.Error() != "DATABASE_URL is required" {
		t.Fatal("missing required configuration should still fail")
	}
}

func TestDotEnvErrorsDoNotLeakContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("DATABASE_URL=\"secret-password"), 0600); err != nil {
		t.Fatal(err)
	}
	lookup := func(string) (string, bool) { return "", false }
	_, err := load(path, lookup)
	if err == nil || !strings.Contains(err.Error(), "invalid .env") || strings.Contains(err.Error(), "secret-password") {
		t.Fatal("malformed dotenv must fail without leaking its contents")
	}
	if _, err := load(t.TempDir(), lookup); err == nil || !strings.Contains(err.Error(), "read .env") {
		t.Fatal("unreadable dotenv must not be treated as missing")
	}
}
