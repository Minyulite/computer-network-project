package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const maxRoomSequence int64 = 9007199254740991

var ErrSequenceExhausted = errors.New("room sequence exhausted")

type MessageRecord struct {
	ID              string
	RoomID          string
	SenderID        string
	SenderUsername  string
	ClientMessageID string
	Seq             int64
	Text            string
	CreatedAt       time.Time
}

type HistoryRecord struct {
	Room      RoomRecord
	JoinedSeq int64
	UpToSeq   int64
	Messages  []MessageRecord
	HasMore   bool
}

const messageColumns = `m.id, m.room_id, m.sender_id, m.client_message_id, m.seq, m.text, m.created_at`

func scanMessage(row pgx.Row) (MessageRecord, error) {
	var message MessageRecord
	err := row.Scan(&message.ID, &message.RoomID, &message.SenderID, &message.ClientMessageID,
		&message.Seq, &message.Text, &message.CreatedAt)
	return message, err
}

func scanHistoryMessage(row pgx.Row) (MessageRecord, error) {
	var message MessageRecord
	err := row.Scan(&message.ID, &message.RoomID, &message.SenderID, &message.ClientMessageID,
		&message.Seq, &message.Text, &message.CreatedAt, &message.SenderUsername)
	return message, err
}

// SaveMessage serializes room sends with group joins. A retry collision on a
// different room rolls back its attempted counter before reading the winner.
func (db *DB) SaveMessage(parent context.Context, message MessageRecord) (MessageRecord, bool, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return MessageRecord{}, false, err
	}
	defer tx.Rollback(context.Background())
	var lastSeq int64
	if err := tx.QueryRow(ctx, `SELECT last_seq FROM rooms WHERE id = $1 FOR UPDATE`, message.RoomID).Scan(&lastSeq); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MessageRecord{}, false, ErrNotFound
		}
		return MessageRecord{}, false, err
	}
	var member bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM room_members WHERE room_id = $1 AND user_id = $2)`,
		message.RoomID, message.SenderID).Scan(&member); err != nil {
		return MessageRecord{}, false, err
	}
	if !member { return MessageRecord{}, false, ErrNotFound }
	prior, err := scanMessage(tx.QueryRow(ctx, `SELECT `+messageColumns+`
		FROM messages m WHERE m.sender_id = $1 AND m.client_message_id = $2`, message.SenderID, message.ClientMessageID))
	if err == nil {
		return prior, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return MessageRecord{}, false, err
	}
	if lastSeq >= maxRoomSequence {
		return MessageRecord{}, false, ErrSequenceExhausted
	}
	err = tx.QueryRow(ctx, `UPDATE rooms SET last_seq = last_seq + 1 WHERE id = $1
		RETURNING last_seq`, message.RoomID).Scan(&message.Seq)
	if err != nil {
		return MessageRecord{}, false, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO messages AS m
		(id, room_id, sender_id, client_message_id, seq, text)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+messageColumns,
		message.ID, message.RoomID, message.SenderID, message.ClientMessageID, message.Seq, message.Text).Scan(
		&message.ID, &message.RoomID, &message.SenderID, &message.ClientMessageID,
		&message.Seq, &message.Text, &message.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "messages_sender_client_id_key" {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
				return MessageRecord{}, false, rollbackErr
			}
			prior, lookupErr := db.MessageByRetry(ctx, message.SenderID, message.ClientMessageID)
			return prior, false, lookupErr
		}
		return MessageRecord{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MessageRecord{}, false, err
	}
	return message, true, nil
}

func (db *DB) MessageByRetry(parent context.Context, senderID, clientMessageID string) (MessageRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	message, err := scanMessage(db.Pool.QueryRow(ctx, `SELECT `+messageColumns+`
		FROM messages m WHERE m.sender_id = $1 AND m.client_message_id = $2`, senderID, clientMessageID))
	if errors.Is(err, pgx.ErrNoRows) {
		return MessageRecord{}, ErrNotFound
	}
	return message, err
}

// History reads the caller's membership and a fixed high-water mark in one
// read-only snapshot. Cursors are exclusive; returned messages are ascending.
func (db *DB) History(parent context.Context, roomID, userID string, limit int,
	beforeSeq, afterSeq, requestedUpTo *int64) (HistoryRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	tx, err := db.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return HistoryRecord{}, err
	}
	defer tx.Rollback(context.Background())
	var history HistoryRecord
	history.Room, err = scanRoomWithMembership(tx.QueryRow(ctx, `SELECT `+roomColumns+`, m.joined_seq
		FROM rooms r JOIN room_members m ON m.room_id = r.id
		WHERE r.id = $1 AND m.user_id = $2`, roomID, userID), &history.JoinedSeq)
	if errors.Is(err, pgx.ErrNoRows) {
		return HistoryRecord{}, ErrNotFound
	}
	if err != nil {
		return HistoryRecord{}, err
	}
	history.UpToSeq = history.Room.LastSeq
	if requestedUpTo != nil && *requestedUpTo < history.UpToSeq {
		history.UpToSeq = *requestedUpTo
	}
	startAfter := int64(0)
	if history.Room.Kind == "group" {
		startAfter = history.JoinedSeq
	}
	if afterSeq != nil && *afterSeq > startAfter {
		startAfter = *afterSeq
	}
	var beforeArg any
	if beforeSeq != nil {
		beforeArg = *beforeSeq
	}
	order := "DESC"
	if afterSeq != nil {
		order = "ASC"
	}
	rows, err := tx.Query(ctx, `SELECT `+messageColumns+`, u.username FROM messages m
		JOIN users u ON u.id = m.sender_id
		WHERE m.room_id = $1 AND m.seq > $2 AND m.seq <= $3
		AND ($4::bigint IS NULL OR m.seq < $4::bigint)
		ORDER BY m.seq `+order+` LIMIT $5`, roomID, startAfter, history.UpToSeq, beforeArg, limit+1)
	if err != nil {
		return HistoryRecord{}, err
	}
	for rows.Next() {
		message, err := scanHistoryMessage(rows)
		if err != nil {
			rows.Close()
			return HistoryRecord{}, err
		}
		history.Messages = append(history.Messages, message)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return HistoryRecord{}, err
	}
	if len(history.Messages) > limit {
		history.HasMore = true
		history.Messages = history.Messages[:limit]
	}
	if order == "DESC" {
		for i, j := 0, len(history.Messages)-1; i < j; i, j = i+1, j-1 {
			history.Messages[i], history.Messages[j] = history.Messages[j], history.Messages[i]
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return HistoryRecord{}, err
	}
	return history, nil
}
