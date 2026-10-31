package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrNotFound = errors.New("record not found")
var ErrUsernameTaken = errors.New("username already exists")

type UserRecord struct {
	ID           string
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

type SessionRecord struct {
	ID        string
	User      UserRecord
	ExpiresAt time.Time
}

func operationContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 3*time.Second)
}

func (db *DB) UserByUsername(parent context.Context, username string) (UserRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	var user UserRecord
	err := db.Pool.QueryRow(ctx, `SELECT id, username, password_hash, created_at FROM users WHERE username = $1`, username).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserRecord{}, ErrNotFound
	}
	return user, err
}

func (db *DB) SessionByDigest(parent context.Context, digest []byte) (SessionRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	var session SessionRecord
	err := db.Pool.QueryRow(ctx, `SELECT s.id, s.expires_at, u.id, u.username, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now()`, digest).
		Scan(&session.ID, &session.ExpiresAt, &session.User.ID, &session.User.Username, &session.User.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionRecord{}, ErrNotFound
	}
	return session, err
}

func (db *DB) SessionByID(parent context.Context, id string) (SessionRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	var session SessionRecord
	err := db.Pool.QueryRow(ctx, `SELECT s.id, s.expires_at, u.id, u.username, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.id = $1 AND s.revoked_at IS NULL AND s.expires_at > now()`, id).
		Scan(&session.ID, &session.ExpiresAt, &session.User.ID, &session.User.Username, &session.User.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionRecord{}, ErrNotFound
	}
	return session, err
}

type NewSession struct {
	ID             string
	UserID         string
	TokenDigest    []byte
	ExpiresAt      time.Time
	OldTokenDigest []byte
}

func (db *DB) CreateAccountSession(parent context.Context, user UserRecord, session NewSession) (UserRecord, string, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return UserRecord{}, "", err
	}
	defer tx.Rollback(context.Background())
	err = tx.QueryRow(ctx, `INSERT INTO users (id, username, password_hash) VALUES ($1, $2, $3) RETURNING created_at`,
		user.ID, user.Username, user.PasswordHash).Scan(&user.CreatedAt)
	if err != nil {
		return UserRecord{}, "", mapUserWriteError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		session.ID, user.ID, session.TokenDigest, session.ExpiresAt); err != nil {
		return UserRecord{}, "", err
	}
	oldID, err := revokeOldSession(ctx, tx, session.OldTokenDigest)
	if err != nil {
		return UserRecord{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return UserRecord{}, "", err
	}
	return user, oldID, nil
}

func (db *DB) CreateSession(parent context.Context, session NewSession) (string, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		session.ID, session.UserID, session.TokenDigest, session.ExpiresAt); err != nil {
		return "", err
	}
	oldID, err := revokeOldSession(ctx, tx, session.OldTokenDigest)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return oldID, nil
}

func revokeOldSession(ctx context.Context, tx pgx.Tx, digest []byte) (string, error) {
	if len(digest) != 32 {
		return "", nil
	}
	var id string
	err := tx.QueryRow(ctx, `UPDATE sessions SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL RETURNING id`, digest).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (db *DB) RevokeSession(parent context.Context, digest []byte) (string, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	var id string
	err := db.Pool.QueryRow(ctx, `UPDATE sessions SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL RETURNING id`, digest).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func mapUserWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_username_key" {
		return ErrUsernameTaken
	}
	return err
}
