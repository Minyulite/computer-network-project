package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"example.com/computer-network/project/apps/api/internal/store"
)

var ErrInvalidInput = errors.New("invalid input")
var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrUnauthenticated = errors.New("unauthenticated")
var ErrRateLimited = errors.New("rate limited")

var usernamePattern = regexp.MustCompile(`^[a-z0-9_]{3,24}$`)

const sessionLifetime = 7 * 24 * time.Hour
const SessionCookieName = "chat_session"

type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"createdAt"`
}

type Identity struct {
	SessionID string
	User      User
	ExpiresAt time.Time
}

type Result struct {
	User      User
	Token     string
	ExpiresAt time.Time
}

// T5 can install a synchronous observer to remove every socket of a revoked session.
// Logout and account switching wait for this callback before reporting success.
type RevocationNotifier interface {
	SessionRevoked(context.Context, string) error
}

type Service struct {
	db         *store.DB
	hashSlots  chan struct{}
	workSlots  chan struct{}
	dummyHash  string
	rates      rateLimiter
	notifierMu sync.RWMutex
	notifier   RevocationNotifier
}

func New(db *store.DB) (*Service, error) {
	dummy, err := hashPassword("not-a-real-password")
	if err != nil {
		return nil, err
	}
	return &Service{db: db, hashSlots: make(chan struct{}, 4), workSlots: make(chan struct{}, 32), dummyHash: dummy}, nil
}

func (s *Service) SetRevocationNotifier(notifier RevocationNotifier) {
	s.notifierMu.Lock()
	defer s.notifierMu.Unlock()
	s.notifier = notifier
}

func (s *Service) notify(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	s.notifierMu.RLock()
	notifier := s.notifier
	s.notifierMu.RUnlock()
	if notifier == nil {
		return nil
	}
	return notifier.SessionRevoked(ctx, sessionID)
}

func (s *Service) withHashSlot(ctx context.Context, work func() (bool, error)) (bool, error) {
	select {
	case s.workSlots <- struct{}{}:
		defer func() { <-s.workSlots }()
	default:
		return false, ErrRateLimited
	}
	select {
	case s.hashSlots <- struct{}{}:
		defer func() { <-s.hashSlots }()
		return work()
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func canonicalUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func (s *Service) Signup(ctx context.Context, username, password, sourceIP, oldToken string) (Result, error) {
	if !s.rates.allow("signup:ip:"+sourceIP, 5, time.Now()) {
		return Result{}, ErrRateLimited
	}
	username = canonicalUsername(username)
	if !usernamePattern.MatchString(username) || !validPassword(password) {
		return Result{}, ErrInvalidInput
	}
	var encoded string
	_, err := s.withHashSlot(ctx, func() (bool, error) {
		var hashErr error
		encoded, hashErr = hashPassword(password)
		return false, hashErr
	})
	if err != nil {
		return Result{}, err
	}
	userID, err := randomUUID()
	if err != nil {
		return Result{}, err
	}
	newSession, token, err := newSession(userID, oldToken)
	if err != nil {
		return Result{}, err
	}
	record, revokedID, err := s.db.CreateAccountSession(ctx,
		store.UserRecord{ID: userID, Username: username, PasswordHash: encoded}, newSession)
	if err != nil {
		return Result{}, err
	}
	if err := s.notify(ctx, revokedID); err != nil {
		return Result{}, err
	}
	return Result{User: publicUser(record), Token: token, ExpiresAt: newSession.ExpiresAt}, nil
}

func (s *Service) Login(ctx context.Context, username, password, sourceIP, oldToken string) (Result, error) {
	now := time.Now()
	username = canonicalUsername(username)
	if !s.rates.allow("login:ip:"+sourceIP, 10, now) {
		return Result{}, ErrRateLimited
	}
	if usernamePattern.MatchString(username) && !s.rates.allow("login:name:"+username, 10, now) {
		return Result{}, ErrRateLimited
	}
	if !validPassword(password) {
		return Result{}, ErrInvalidCredentials
	}
	record, err := s.db.UserByUsername(ctx, username)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return Result{}, err
	}
	encoded := s.dummyHash
	if err == nil {
		encoded = record.PasswordHash
	}
	verified, err := s.withHashSlot(ctx, func() (bool, error) { return verifyPassword(encoded, password) })
	if err != nil {
		return Result{}, err
	}
	if !verified || record.ID == "" {
		return Result{}, ErrInvalidCredentials
	}
	newSession, token, err := newSession(record.ID, oldToken)
	if err != nil {
		return Result{}, err
	}
	revokedID, err := s.db.CreateSession(ctx, newSession)
	if err != nil {
		return Result{}, err
	}
	if err := s.notify(ctx, revokedID); err != nil {
		return Result{}, err
	}
	return Result{User: publicUser(record), Token: token, ExpiresAt: newSession.ExpiresAt}, nil
}

func (s *Service) ValidateSession(ctx context.Context, token string) (Identity, error) {
	digest := digestToken(token)
	if digest == nil {
		return Identity{}, ErrUnauthenticated
	}
	record, err := s.db.SessionByDigest(ctx, digest)
	if errors.Is(err, store.ErrNotFound) {
		return Identity{}, ErrUnauthenticated
	}
	if err != nil {
		return Identity{}, err
	}
	return Identity{SessionID: record.ID, User: publicUser(record.User), ExpiresAt: record.ExpiresAt}, nil
}

func (s *Service) ValidateSessionID(ctx context.Context, id string) (Identity, error) {
	record, err := s.db.SessionByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return Identity{}, ErrUnauthenticated
	}
	if err != nil {
		return Identity{}, err
	}
	return Identity{SessionID: record.ID, User: publicUser(record.User), ExpiresAt: record.ExpiresAt}, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	digest := digestToken(token)
	if digest == nil {
		return nil
	}
	id, err := s.db.RevokeSession(ctx, digest)
	if err != nil {
		return err
	}
	return s.notify(ctx, id)
}

func publicUser(record store.UserRecord) User {
	return User{ID: record.ID, Username: record.Username, CreatedAt: record.CreatedAt}
}

func newSession(userID, oldToken string) (store.NewSession, string, error) {
	id, err := randomUUID()
	if err != nil {
		return store.NewSession{}, "", err
	}
	var tokenBytes [32]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return store.NewSession{}, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes[:])
	digest := sha256.Sum256(tokenBytes[:])
	return store.NewSession{ID: id, UserID: userID, TokenDigest: digest[:],
		ExpiresAt: time.Now().UTC().Add(sessionLifetime), OldTokenDigest: digestToken(oldToken)}, token, nil
}

func digestToken(token string) []byte {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		return nil
	}
	digest := sha256.Sum256(decoded)
	return digest[:]
}

func randomUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	raw := hex.EncodeToString(b[:])
	return raw[0:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:32], nil
}
