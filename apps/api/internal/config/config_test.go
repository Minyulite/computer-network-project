package config

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL":    "postgres://chat_app:secret@127.0.0.1:5432/chat?sslmode=disable",
		"ALLOWED_ORIGINS": "http://localhost:3000, http://127.0.0.1:3000",
	}
	parse := func(changes map[string]string) (Config, error) {
		values := map[string]string{}
		for k, v := range base {
			values[k] = v
		}
		for k, v := range changes {
			values[k] = v
		}
		return Parse(func(key string) string { return values[key] })
	}
	c, err := parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.DatabaseMaxConns != 10 || c.ListenAddr() != "127.0.0.1:8080" || len(c.AllowedOrigins) != 2 || c.DatabaseTarget != "127.0.0.1:5432/chat" {
		t.Fatalf("unexpected defaults: %#v", c)
	}
	c, err = parse(map[string]string{"DATABASE_MAX_CONNS": "2", "HOST": "0.0.0.0", "PORT": "9000", "COOKIE_SECURE": "true"})
	if err != nil || c.DatabaseMaxConns != 2 || c.ListenAddr() != "0.0.0.0:9000" || !c.CookieSecure {
		t.Fatalf("overrides: %#v %v", c, err)
	}
	for _, tc := range []map[string]string{
		{"DATABASE_URL": ""}, {"DATABASE_URL": "postgres://chat_app:secret@localhost/chat?sslmode=disable#bad"},
		{"DATABASE_URL": "postgres://chat_app:secret@localhost/"}, {"DATABASE_URL": "postgres://chat_app:secret@localhost:bad/chat"},
		{"DATABASE_MAX_CONNS": "0"}, {"DATABASE_MAX_CONNS": "101"}, {"HOST": "bad host"},
		{"PORT": "0"}, {"PORT": "65536"}, {"ALLOWED_ORIGINS": ""},
		{"ALLOWED_ORIGINS": "http://localhost:3000/path"}, {"ALLOWED_ORIGINS": "http://localhost:3000,http://localhost:3000"},
		{"COOKIE_SECURE": "maybe"},
	} {
		_, err := parse(tc)
		if err == nil {
			t.Errorf("accepted invalid config: %v", tc)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Errorf("credential leaked in error: %v", err)
		}
	}
}
