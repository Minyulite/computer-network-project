package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrNotGroup = errors.New("room is not a group")

type RoomRecord struct {
	ID             string
	Kind           string
	Name           string
	CreatedBy      string
	DirectUserLow  string
	DirectUserHigh string
	LastSeq        int64
	CreatedAt      time.Time
}

type OwnRoomRecord struct {
	Room      RoomRecord
	JoinedSeq int64
}

type MemberRecord struct {
	UserID    string
	Username  string
	JoinedAt  time.Time
	JoinedSeq int64
}

const roomColumns = `r.id, r.kind, r.name, r.created_by, r.direct_user_low, r.direct_user_high, r.last_seq, r.created_at`

func scanRoom(row pgx.Row) (RoomRecord, error) {
	var room RoomRecord
	var name, low, high sql.NullString
	err := row.Scan(&room.ID, &room.Kind, &name, &room.CreatedBy, &low, &high, &room.LastSeq, &room.CreatedAt)
	if err != nil {
		return RoomRecord{}, err
	}
	room.Name, room.DirectUserLow, room.DirectUserHigh = name.String, low.String, high.String
	return room, nil
}

func (db *DB) UserExists(parent context.Context, userID string) (bool, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	var exists bool
	err := db.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists)
	return exists, err
}

// OpenDirect inserts the room and both initial memberships in one transaction.
// A competing opener is read in a new statement after the winner commits.
func (db *DB) OpenDirect(parent context.Context, roomID, creatorID, lowID, highID string) (RoomRecord, bool, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return RoomRecord{}, false, err
	}
	defer tx.Rollback(context.Background())
	room, err := scanRoom(tx.QueryRow(ctx, `INSERT INTO rooms AS r
		(id, kind, created_by, direct_user_low, direct_user_high)
		VALUES ($1, 'direct', $2, $3, $4)
		ON CONFLICT ON CONSTRAINT rooms_direct_key_key DO NOTHING
		RETURNING `+roomColumns, roomID, creatorID, lowID, highID))
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Rollback(ctx); err != nil {
			return RoomRecord{}, false, err
		}
		room, err = db.DirectByPair(ctx, lowID, highID)
		return room, false, err
	}
	if err != nil {
		return RoomRecord{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO room_members (room_id, user_id, joined_seq)
		VALUES ($1, $2, 0), ($1, $3, 0)`, roomID, lowID, highID); err != nil {
		return RoomRecord{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RoomRecord{}, false, err
	}
	return room, true, nil
}

func (db *DB) DirectByPair(parent context.Context, lowID, highID string) (RoomRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	room, err := scanRoom(db.Pool.QueryRow(ctx, `SELECT `+roomColumns+`
		FROM rooms r WHERE r.kind = 'direct' AND r.direct_user_low = $1 AND r.direct_user_high = $2`, lowID, highID))
	if errors.Is(err, pgx.ErrNoRows) {
		return RoomRecord{}, ErrNotFound
	}
	return room, err
}

func (db *DB) CreateGroup(parent context.Context, roomID, creatorID, name string) (RoomRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return RoomRecord{}, err
	}
	defer tx.Rollback(context.Background())
	room, err := scanRoom(tx.QueryRow(ctx, `INSERT INTO rooms AS r (id, kind, name, created_by)
		VALUES ($1, 'group', $2, $3) RETURNING `+roomColumns, roomID, name, creatorID))
	if err != nil {
		return RoomRecord{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO room_members (room_id, user_id, joined_seq)
		VALUES ($1, $2, 0)`, roomID, creatorID); err != nil {
		return RoomRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RoomRecord{}, err
	}
	return room, nil
}

// JoinGroup and SaveMessage lock the same room row to order joining against sends.
func (db *DB) JoinGroup(parent context.Context, roomID, userID string) (int64, bool, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(context.Background())
	var kind string
	var lastSeq int64
	err = tx.QueryRow(ctx, `SELECT kind, last_seq FROM rooms WHERE id = $1 FOR UPDATE`, roomID).Scan(&kind, &lastSeq)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, ErrNotFound
	}
	if err != nil {
		return 0, false, err
	}
	if kind != "group" {
		return 0, false, ErrNotGroup
	}
	tag, err := tx.Exec(ctx, `INSERT INTO room_members (room_id, user_id, joined_seq)
		VALUES ($1, $2, $3) ON CONFLICT ON CONSTRAINT room_members_pkey DO NOTHING`, roomID, userID, lastSeq)
	if err != nil {
		return 0, false, err
	}
	var joinedSeq int64
	if err := tx.QueryRow(ctx, `SELECT joined_seq FROM room_members WHERE room_id = $1 AND user_id = $2`, roomID, userID).Scan(&joinedSeq); err != nil {
		return 0, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	return joinedSeq, tag.RowsAffected() == 1, nil
}

func (db *DB) RoomByID(parent context.Context, roomID string) (RoomRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	room, err := scanRoom(db.Pool.QueryRow(ctx, `SELECT `+roomColumns+` FROM rooms r WHERE r.id = $1`, roomID))
	if errors.Is(err, pgx.ErrNoRows) {
		return RoomRecord{}, ErrNotFound
	}
	return room, err
}

func (db *DB) Membership(parent context.Context, roomID, userID string) (RoomRecord, int64, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	var joinedSeq int64
	room, err := scanRoomWithMembership(db.Pool.QueryRow(ctx, `SELECT `+roomColumns+`, m.joined_seq
		FROM rooms r JOIN room_members m ON m.room_id = r.id
		WHERE r.id = $1 AND m.user_id = $2`, roomID, userID), &joinedSeq)
	if errors.Is(err, pgx.ErrNoRows) {
		return RoomRecord{}, 0, ErrNotFound
	}
	return room, joinedSeq, err
}

func scanRoomWithMembership(row pgx.Row, joinedSeq *int64) (RoomRecord, error) {
	var room RoomRecord
	var name, low, high sql.NullString
	err := row.Scan(&room.ID, &room.Kind, &name, &room.CreatedBy, &low, &high, &room.LastSeq, &room.CreatedAt, joinedSeq)
	if err != nil {
		return RoomRecord{}, err
	}
	room.Name, room.DirectUserLow, room.DirectUserHigh = name.String, low.String, high.String
	return room, nil
}

func (db *DB) DirectMemberIDs(parent context.Context, roomID string) ([]string, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	rows, err := db.Pool.Query(ctx, `SELECT user_id FROM room_members WHERE room_id = $1 ORDER BY user_id`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func cursorArg(cursor string) any {
	if cursor == "" {
		return nil
	}
	return cursor
}

func (db *DB) Groups(parent context.Context, cursor string, limit int) ([]RoomRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	rows, err := db.Pool.Query(ctx, `SELECT `+roomColumns+` FROM rooms r
		WHERE r.kind = 'group' AND ($1::uuid IS NULL OR r.id > $1::uuid)
		ORDER BY r.id LIMIT $2`, cursorArg(cursor), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rooms []RoomRecord
	for rows.Next() {
		room, err := scanRoom(rows)
		if err != nil {
			return nil, err
		}
		rooms = append(rooms, room)
	}
	return rooms, rows.Err()
}

func (db *DB) OwnRooms(parent context.Context, userID, cursor string, limit int) ([]OwnRoomRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	rows, err := db.Pool.Query(ctx, `SELECT `+roomColumns+`, m.joined_seq FROM rooms r
		JOIN room_members m ON m.room_id = r.id
		WHERE m.user_id = $1 AND ($2::uuid IS NULL OR r.id > $2::uuid)
		ORDER BY r.id LIMIT $3`, userID, cursorArg(cursor), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rooms []OwnRoomRecord
	for rows.Next() {
		var joinedSeq int64
		room, err := scanRoomWithMembership(rows, &joinedSeq)
		if err != nil {
			return nil, err
		}
		rooms = append(rooms, OwnRoomRecord{Room: room, JoinedSeq: joinedSeq})
	}
	return rooms, rows.Err()
}

func (db *DB) GroupMembers(parent context.Context, roomID, cursor string, limit int) ([]MemberRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	rows, err := db.Pool.Query(ctx, `SELECT u.id, u.username, m.joined_at, m.joined_seq
		FROM room_members m JOIN users u ON u.id = m.user_id
		WHERE m.room_id = $1 AND ($2::uuid IS NULL OR u.id > $2::uuid)
		ORDER BY u.id LIMIT $3`, roomID, cursorArg(cursor), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []MemberRecord
	for rows.Next() {
		var member MemberRecord
		if err := rows.Scan(&member.UserID, &member.Username, &member.JoinedAt, &member.JoinedSeq); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (db *DB) EligibleMemberIDs(parent context.Context, roomID string, seq int64) ([]string, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	rows, err := db.Pool.Query(ctx, `SELECT m.user_id FROM room_members m JOIN rooms r ON r.id = m.room_id
		WHERE m.room_id = $1 AND
		((r.kind = 'direct' AND m.user_id IN (r.direct_user_low, r.direct_user_high))
		 OR (r.kind = 'group' AND m.joined_seq < $2))
		ORDER BY m.user_id`, roomID, seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
