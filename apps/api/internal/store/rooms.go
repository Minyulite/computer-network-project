package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrNotGroup = errors.New("room is not a group")

// Values of rooms.kind. SQL in this package spells them out literally.
const (
	RoomDirect = "direct"
	RoomGroup  = "group"
	RoomBot    = "bot"
)

type RoomRecord struct {
	ID             string
	Kind           string
	Name           string
	CreatedBy      string
	DirectUserLow  string
	DirectUserHigh string
	LastSeq        int64
	CreatedAt      time.Time
	BotID          string
	BotName        string
	BotAvatar      string
	// Both people of a direct room, so a client can show who the other one is
	// even while they are offline.
	DirectLowName, DirectLowAvatar   string
	DirectHighName, DirectHighAvatar string
}

// LastMessageRecord is the newest message of a room that its member may see.
type LastMessageRecord struct {
	SenderID   string
	SenderName string
	Text       string
	CreatedAt  time.Time
}

type OwnRoomRecord struct {
	Room      RoomRecord
	JoinedSeq int64
	Last      *LastMessageRecord // Nil when nothing visible to this member was sent yet.
}

type MemberRecord struct {
	UserID    string
	Username  string
	Avatar    string
	JoinedAt  time.Time
	JoinedSeq int64
}

const roomColumns = `r.id, r.kind, r.name, r.created_by, r.direct_user_low, r.direct_user_high, r.last_seq, r.created_at,
	r.bot_id, (SELECT b.name FROM bots b WHERE b.id = r.bot_id), (SELECT b.avatar FROM bots b WHERE b.id = r.bot_id),
	(SELECT u.username FROM users u WHERE u.id = r.direct_user_low), (SELECT u.avatar FROM users u WHERE u.id = r.direct_user_low),
	(SELECT u.username FROM users u WHERE u.id = r.direct_user_high), (SELECT u.avatar FROM users u WHERE u.id = r.direct_user_high)`

func scanRoom(row pgx.Row) (RoomRecord, error) { return scanRoomWith(row) }

// scanRoomWith reads the room columns, then any extra destinations the query appends.
func scanRoomWith(row pgx.Row, extra ...any) (RoomRecord, error) {
	var room RoomRecord
	var name, low, high, botID, botName, botAvatar, lowName, lowAvatar, highName, highAvatar sql.NullString
	dest := append([]any{&room.ID, &room.Kind, &name, &room.CreatedBy, &low, &high, &room.LastSeq, &room.CreatedAt,
		&botID, &botName, &botAvatar, &lowName, &lowAvatar, &highName, &highAvatar}, extra...)
	if err := row.Scan(dest...); err != nil {
		return RoomRecord{}, err
	}
	room.Name, room.DirectUserLow, room.DirectUserHigh = name.String, low.String, high.String
	room.BotID, room.BotName, room.BotAvatar = botID.String, botName.String, botAvatar.String
	room.DirectLowName, room.DirectLowAvatar = lowName.String, lowAvatar.String
	room.DirectHighName, room.DirectHighAvatar = highName.String, highAvatar.String
	return room, nil
}

func (db *DB) UserExists(parent context.Context, userID string) (bool, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	var exists bool
	err := db.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND NOT is_bot)`, userID).Scan(&exists)
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
	joinedSeq, created, err := joinGroupTx(ctx, tx, roomID, userID)
	if err != nil {
		return 0, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	return joinedSeq, created, nil
}

// joinGroupTx adds userID to a group inside tx. It reports the sequence the
// member joined at and whether this call added them.
func joinGroupTx(ctx context.Context, tx pgx.Tx, roomID, userID string) (int64, bool, error) {
	var kind string
	var lastSeq int64
	err := tx.QueryRow(ctx, `SELECT kind, last_seq FROM rooms WHERE id = $1 FOR UPDATE`, roomID).Scan(&kind, &lastSeq)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, ErrNotFound
	}
	if err != nil {
		return 0, false, err
	}
	if kind != RoomGroup {
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
	return joinedSeq, tag.RowsAffected() == 1, nil
}

// GroupPreviews returns up to two members per group, the first to join, for the
// overlapping pictures shown in a conversation list.
func (db *DB) GroupPreviews(parent context.Context, roomIDs []string) (map[string][]MemberRecord, error) {
	previews := map[string][]MemberRecord{}
	if len(roomIDs) == 0 {
		return previews, nil
	}
	ctx, cancel := operationContext(parent)
	defer cancel()
	rows, err := db.Pool.Query(ctx, `SELECT room_id, user_id, username, avatar FROM (
			SELECT m.room_id, u.id AS user_id, u.username, u.avatar,
			       row_number() OVER (PARTITION BY m.room_id ORDER BY m.joined_at, u.id) AS n
			FROM room_members m JOIN users u ON u.id = m.user_id
			WHERE m.room_id::text = ANY($1::text[])) t
		WHERE n <= 2 ORDER BY room_id, n`, roomIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var roomID string
		var member MemberRecord
		if err := rows.Scan(&roomID, &member.UserID, &member.Username, &member.Avatar); err != nil {
			return nil, err
		}
		previews[roomID] = append(previews[roomID], member)
	}
	return previews, rows.Err()
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
	return scanRoomWith(row, joinedSeq)
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
	// rooms.last_seq is the sequence of the newest message, so it is found by key.
	// A group member never sees a message from before they joined.
	rows, err := db.Pool.Query(ctx, `SELECT `+roomColumns+`, m.joined_seq, lm.text, lm.sender_id::text, lu.username, lm.created_at
		FROM rooms r
		JOIN room_members m ON m.room_id = r.id
		LEFT JOIN messages lm ON lm.room_id = r.id AND lm.seq = r.last_seq AND r.last_seq > m.joined_seq
		LEFT JOIN users lu ON lu.id = lm.sender_id
		WHERE m.user_id = $1 AND ($2::uuid IS NULL OR r.id > $2::uuid)
		ORDER BY r.id LIMIT $3`, userID, cursorArg(cursor), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rooms []OwnRoomRecord
	for rows.Next() {
		var joinedSeq int64
		var text, senderID, senderName sql.NullString
		var sentAt sql.NullTime
		room, err := scanRoomWith(rows, &joinedSeq, &text, &senderID, &senderName, &sentAt)
		if err != nil {
			return nil, err
		}
		record := OwnRoomRecord{Room: room, JoinedSeq: joinedSeq}
		if text.Valid {
			record.Last = &LastMessageRecord{SenderID: senderID.String, SenderName: senderName.String, Text: text.String, CreatedAt: sentAt.Time}
		}
		rooms = append(rooms, record)
	}
	return rooms, rows.Err()
}

func (db *DB) GroupMembers(parent context.Context, roomID, cursor string, limit int) ([]MemberRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	rows, err := db.Pool.Query(ctx, `SELECT u.id, u.username, u.avatar, m.joined_at, m.joined_seq
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
		if err := rows.Scan(&member.UserID, &member.Username, &member.Avatar, &member.JoinedAt, &member.JoinedSeq); err != nil {
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
		 OR (r.kind = 'group' AND m.joined_seq < $2)
		 OR r.kind = 'bot')
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
