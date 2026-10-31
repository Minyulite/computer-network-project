package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/config"
	"example.com/computer-network/project/apps/api/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testOrigin = "http://localhost:3000"
const testPassword = "  fifteen characters  "

type revocationRecorder struct {
	mu  sync.Mutex
	db  *store.DB
	ids []string
}

func (r *revocationRecorder) SessionRevoked(ctx context.Context, id string) error {
	var revoked bool
	if err := r.db.Pool.QueryRow(ctx, "SELECT revoked_at IS NOT NULL FROM sessions WHERE id=$1", id).Scan(&revoked); err != nil {
		return err
	}
	if !revoked {
		return fmt.Errorf("notification preceded commit")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, id)
	return nil
}

func (r *revocationRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.ids)
}

func authTestServer(t *testing.T) (http.Handler, *store.DB, *auth.Service) {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL required for auth integration tests")
	}
	if raw == os.Getenv("DATABASE_URL") {
		t.Fatal("TEST_DATABASE_URL must be separate from DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	t.Cleanup(admin.Close)
	if err := admin.Ping(ctx); err != nil {
		t.Fatal("test database unavailable")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "t3_" + hex.EncodeToString(random[:])
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
	})
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(ctx, u.String(), 5)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := store.Migrate(ctx, db.Pool); err != nil {
		t.Fatal(err)
	}
	service, err := auth.New(db)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{AllowedOrigins: []string{testOrigin}, CookieSecure: true}
	return NewHandler(db, service, cfg), db, service
}

func authRequest(handler http.Handler, method, path, origin, ip, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = ip + ":12345"
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func jsonCredentials(username, password string) string {
	data, _ := json.Marshal(map[string]string{"username": username, "password": password})
	return string(data)
}

func sessionCookie(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			return cookie
		}
	}
	t.Fatal("session cookie missing")
	return nil
}

func TestAuthenticationFlow(t *testing.T) {
	handler, db, service := authTestServer(t)
	revocations := &revocationRecorder{db: db}
	service.SetRevocationNotifier(revocations)
	signup := authRequest(handler, "POST", "/auth/signup", testOrigin, "192.0.2.1", jsonCredentials(" Alice ", testPassword), nil)
	if signup.Code != 201 {
		t.Fatalf("signup status=%d body=%s", signup.Code, signup.Body.String())
	}
	aliceCookie := sessionCookie(t, signup)
	if !aliceCookie.HttpOnly || aliceCookie.SameSite != http.SameSiteLaxMode || !aliceCookie.Secure ||
		aliceCookie.Path != "/" || aliceCookie.Domain != "" || aliceCookie.MaxAge != 604800 || aliceCookie.Expires.IsZero() {
		t.Fatalf("cookie attributes: %#v", aliceCookie)
	}
	if len(aliceCookie.Value) != 43 || strings.Contains(signup.Body.String(), aliceCookie.Value) ||
		strings.Contains(signup.Body.String(), "argon2id") || strings.Contains(signup.Body.String(), testPassword) {
		t.Fatal("signup response leaked a secret")
	}
	var signupBody struct {
		User auth.User `json:"user"`
	}
	if err := json.Unmarshal(signup.Body.Bytes(), &signupBody); err != nil || signupBody.User.Username != "alice" || signupBody.User.ID == "" {
		t.Fatalf("signup user: %+v %v", signupBody, err)
	}
	me := authRequest(handler, "GET", "/auth/me", "", "192.0.2.1", "", aliceCookie)
	if me.Code != 200 || !strings.Contains(me.Body.String(), `"expiresAt"`) || strings.Contains(me.Body.String(), aliceCookie.Value) {
		t.Fatalf("me status=%d body=%s", me.Code, me.Body.String())
	}
	identity, err := service.ValidateSession(context.Background(), aliceCookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	var storedHash string
	var tokenLength int
	if err := db.Pool.QueryRow(context.Background(), "SELECT password_hash FROM users WHERE id=$1", identity.User.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(context.Background(), "SELECT octet_length(token_hash) FROM sessions WHERE id=$1", identity.SessionID).Scan(&tokenLength); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(storedHash, "$argon2id$") || storedHash == testPassword || tokenLength != 32 {
		t.Fatal("stored password or token digest is incorrect")
	}
	duplicate := authRequest(handler, "POST", "/auth/signup", testOrigin, "192.0.2.2", jsonCredentials("ALICE", testPassword), nil)
	if duplicate.Code != 409 {
		t.Fatalf("duplicate signup status=%d", duplicate.Code)
	}
	wrongUser := authRequest(handler, "POST", "/auth/login", testOrigin, "192.0.2.3", jsonCredentials("unknown", testPassword), nil)
	wrongPassword := authRequest(handler, "POST", "/auth/login", testOrigin, "192.0.2.3", jsonCredentials("alice", "wrong password 123"), nil)
	if wrongUser.Code != 401 || wrongPassword.Code != 401 || wrongUser.Body.String() != wrongPassword.Body.String() {
		t.Fatalf("login failures differ: %d %q / %d %q", wrongUser.Code, wrongUser.Body.String(), wrongPassword.Code, wrongPassword.Body.String())
	}
	otherDevice := authRequest(handler, "POST", "/auth/login", testOrigin, "192.0.2.4", jsonCredentials("alice", testPassword), nil)
	if otherDevice.Code != 200 {
		t.Fatalf("other device login: %d", otherDevice.Code)
	}
	otherCookie := sessionCookie(t, otherDevice)
	if otherCookie.Value == aliceCookie.Value {
		t.Fatal("login reused a session token")
	}
	bob := authRequest(handler, "POST", "/auth/signup", testOrigin, "192.0.2.5", jsonCredentials("bob", testPassword), aliceCookie)
	if bob.Code != 201 {
		t.Fatalf("account switch signup: %d body=%s", bob.Code, bob.Body.String())
	}
	bobCookie := sessionCookie(t, bob)
	if revocations.count() != 1 {
		t.Fatal("account switch did not notify revoked session")
	}
	if authRequest(handler, "GET", "/auth/me", "", "192.0.2.5", "", aliceCookie).Code != 401 ||
		authRequest(handler, "GET", "/auth/me", "", "192.0.2.5", "", otherCookie).Code != 200 ||
		authRequest(handler, "GET", "/auth/me", "", "192.0.2.5", "", bobCookie).Code != 200 {
		t.Fatal("account switching revoked the wrong session")
	}
	backToAlice := authRequest(handler, "POST", "/auth/login", testOrigin, "192.0.2.6", jsonCredentials("alice", testPassword), bobCookie)
	if backToAlice.Code != 200 || revocations.count() != 2 {
		t.Fatalf("login switch status=%d notifications=%d", backToAlice.Code, revocations.count())
	}
	if authRequest(handler, "GET", "/auth/me", "", "192.0.2.6", "", bobCookie).Code != 401 ||
		authRequest(handler, "GET", "/auth/me", "", "192.0.2.6", "", sessionCookie(t, backToAlice)).Code != 200 {
		t.Fatal("login account switch did not replace browser session")
	}
	bobFresh := authRequest(handler, "POST", "/auth/login", testOrigin, "192.0.2.7", jsonCredentials("bob", testPassword), nil)
	if bobFresh.Code != 200 {
		t.Fatalf("bob login status=%d", bobFresh.Code)
	}
	bobCookie = sessionCookie(t, bobFresh)
	bobID, err := service.ValidateSession(context.Background(), bobCookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(context.Background(), "UPDATE sessions SET created_at=now()-interval '2 days', expires_at=now()-interval '1 day' WHERE id=$1", bobID.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if authRequest(handler, "GET", "/auth/me", "", "192.0.2.5", "", bobCookie).Code != 401 {
		t.Fatal("expired session remained valid")
	}
	logout := authRequest(handler, "POST", "/auth/logout", testOrigin, "192.0.2.4", "", otherCookie)
	if logout.Code != 204 || sessionCookie(t, logout).MaxAge != -1 {
		t.Fatalf("logout status=%d", logout.Code)
	}
	if authRequest(handler, "GET", "/auth/me", "", "192.0.2.4", "", otherCookie).Code != 401 ||
		authRequest(handler, "POST", "/auth/logout", testOrigin, "192.0.2.4", "", otherCookie).Code != 204 {
		t.Fatal("logout did not revoke idempotently")
	}
	if revocations.count() != 3 {
		t.Fatalf("logout notification count=%d", revocations.count())
	}
}

func TestDuplicateSignupRace(t *testing.T) {
	handler, db, _ := authTestServer(t)
	var wg sync.WaitGroup
	results := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			response := authRequest(handler, "POST", "/auth/signup", testOrigin,
				fmt.Sprintf("198.51.100.%d", i+1), jsonCredentials("racer", testPassword), nil)
			results <- response.Code
		}(i)
	}
	wg.Wait()
	close(results)
	created, conflicts := 0, 0
	for code := range results {
		switch code {
		case 201:
			created++
		case 409:
			conflicts++
		default:
			t.Errorf("unexpected signup status %d", code)
		}
	}
	if created != 1 || conflicts != 7 {
		t.Fatalf("created=%d conflicts=%d", created, conflicts)
	}
	var count int
	if err := db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM users WHERE username='racer'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("race persisted %d users: %v", count, err)
	}
}

func TestOriginCORSAndValidation(t *testing.T) {
	handler, _, _ := authTestServer(t)
	for _, origin := range []string{"", "null", "http://evil.example", "http://localhost:3001"} {
		response := authRequest(handler, "POST", "/auth/signup", origin, "203.0.113.1", jsonCredentials("alice", testPassword), nil)
		if response.Code != 403 || response.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("origin %q accepted: %d", origin, response.Code)
		}
	}
	preflight := authRequest(handler, "OPTIONS", "/auth/login", testOrigin, "203.0.113.1", "", nil)
	if preflight.Code != 204 || preflight.Header().Get("Access-Control-Allow-Origin") != testOrigin ||
		preflight.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("credentialed CORS preflight failed")
	}
	if authRequest(handler, "GET", "/auth/me", "http://evil.example", "203.0.113.1", "", nil).Code != 403 {
		t.Fatal("disallowed Origin reached session route")
	}
	for _, body := range []string{
		`{"username":"alice","password":"short"}`,
		`{"username":"alice","password":"` + testPassword + `","senderId":"someone"}`,
		`{"username":"alice","password":`,
		jsonCredentials("alice", strings.Repeat("x", 9000)),
	} {
		response := authRequest(handler, "POST", "/auth/signup", testOrigin, "203.0.113.2", body, nil)
		if response.Code != 400 {
			t.Fatalf("invalid body status=%d", response.Code)
		}
	}
	request := httptest.NewRequest("POST", "/auth/signup", bytes.NewBufferString(jsonCredentials("alice", testPassword)))
	request.Header.Set("Origin", testOrigin)
	request.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if w.Code != 415 {
		t.Fatalf("non-JSON request status=%d", w.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	handler, _, _ := authTestServer(t)
	for i := 0; i < 10; i++ {
		response := authRequest(handler, "POST", "/auth/login", testOrigin, "203.0.113.10", jsonCredentials("unknown", testPassword), nil)
		if response.Code != 401 {
			t.Fatalf("attempt %d status=%d", i, response.Code)
		}
	}
	response := authRequest(handler, "POST", "/auth/login", testOrigin, "203.0.113.10", jsonCredentials("unknown", testPassword), nil)
	if response.Code != 429 {
		t.Fatalf("rate limit status=%d", response.Code)
	}
}
