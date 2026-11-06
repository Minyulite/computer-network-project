package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// MaxBotsPerOwner bounds how many bots one account can create.
const MaxBotsPerOwner = 5

var ErrBotLimit = errors.New("bot limit reached")

type BotRecord struct {
	ID            string
	OwnerID       string
	OwnerUsername string
	UserID        string // The system user that sends the bot's replies.
	Username      string // That system user's username.
	Name          string
	Description   string
	Enabled       bool
	CreatedAt     time.Time
}

type BotTemplateRecord struct {
	ID       string
	Question string
	Answer   string
}

const botColumns = `b.id, b.owner_id, o.username, b.user_id, bu.username, b.name, b.description, b.enabled, b.created_at`
const botJoins = ` FROM bots b JOIN users o ON o.id = b.owner_id JOIN users bu ON bu.id = b.user_id`

func scanBot(row pgx.Row) (BotRecord, error) {
	var bot BotRecord
	err := row.Scan(&bot.ID, &bot.OwnerID, &bot.OwnerUsername, &bot.UserID, &bot.Username,
		&bot.Name, &bot.Description, &bot.Enabled, &bot.CreatedAt)
	return bot, err
}

func insertTemplates(ctx context.Context, tx pgx.Tx, botID string, templates []BotTemplateRecord) error {
	for i, t := range templates {
		if _, err := tx.Exec(ctx, `INSERT INTO bot_templates (id, bot_id, position, question, answer)
			VALUES ($1, $2, $3, $4, $5)`, t.ID, botID, i, t.Question, t.Answer); err != nil {
			return err
		}
	}
	return nil
}

// CreateBot inserts the bot, its system user and its templates atomically.
// The owner row is locked so concurrent creates cannot exceed the limit.
func (db *DB) CreateBot(parent context.Context, bot BotRecord, templates []BotTemplateRecord) (BotRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return BotRecord{}, err
	}
	defer tx.Rollback(context.Background())
	var locked string
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND NOT is_bot FOR UPDATE`, bot.OwnerID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BotRecord{}, ErrNotFound
		}
		return BotRecord{}, err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM bots WHERE owner_id = $1`, bot.OwnerID).Scan(&count); err != nil {
		return BotRecord{}, err
	}
	if count >= MaxBotsPerOwner {
		return BotRecord{}, ErrBotLimit
	}
	// "!" is not a valid Argon2id encoding, so this account can never sign in.
	if _, err := tx.Exec(ctx, `INSERT INTO users (id, username, password_hash, is_bot) VALUES ($1, $2, '!', true)`,
		bot.UserID, bot.Username); err != nil {
		return BotRecord{}, mapUserWriteError(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO bots (id, owner_id, user_id, name, description, enabled)
		VALUES ($1, $2, $3, $4, $5, $6)`, bot.ID, bot.OwnerID, bot.UserID, bot.Name, bot.Description, bot.Enabled); err != nil {
		return BotRecord{}, err
	}
	if err := insertTemplates(ctx, tx, bot.ID, templates); err != nil {
		return BotRecord{}, err
	}
	created, err := scanBot(tx.QueryRow(ctx, `SELECT `+botColumns+botJoins+` WHERE b.id = $1`, bot.ID))
	if err != nil {
		return BotRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BotRecord{}, err
	}
	return created, nil
}

// UpdateBot replaces the bot's settings and its whole template set. Only the
// owner can update; anyone else sees ErrNotFound.
func (db *DB) UpdateBot(parent context.Context, ownerID string, bot BotRecord, templates []BotTemplateRecord) (BotRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return BotRecord{}, err
	}
	defer tx.Rollback(context.Background())
	tag, err := tx.Exec(ctx, `UPDATE bots SET name = $3, description = $4, enabled = $5, updated_at = now()
		WHERE id = $1 AND owner_id = $2`, bot.ID, ownerID, bot.Name, bot.Description, bot.Enabled)
	if err != nil {
		return BotRecord{}, err
	}
	if tag.RowsAffected() == 0 {
		return BotRecord{}, ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM bot_templates WHERE bot_id = $1`, bot.ID); err != nil {
		return BotRecord{}, err
	}
	if err := insertTemplates(ctx, tx, bot.ID, templates); err != nil {
		return BotRecord{}, err
	}
	updated, err := scanBot(tx.QueryRow(ctx, `SELECT `+botColumns+botJoins+` WHERE b.id = $1`, bot.ID))
	if err != nil {
		return BotRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BotRecord{}, err
	}
	return updated, nil
}

func (db *DB) BotByID(parent context.Context, botID string) (BotRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	bot, err := scanBot(db.Pool.QueryRow(ctx, `SELECT `+botColumns+botJoins+` WHERE b.id = $1`, botID))
	if errors.Is(err, pgx.ErrNoRows) {
		return BotRecord{}, ErrNotFound
	}
	return bot, err
}

// BotByRoom finds the bot behind a bot room.
func (db *DB) BotByRoom(parent context.Context, roomID string) (BotRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	bot, err := scanBot(db.Pool.QueryRow(ctx, `SELECT `+botColumns+botJoins+`
		JOIN rooms r ON r.bot_id = b.id WHERE r.id = $1 AND r.kind = 'bot'`, roomID))
	if errors.Is(err, pgx.ErrNoRows) {
		return BotRecord{}, ErrNotFound
	}
	return bot, err
}

func (db *DB) BotTemplates(parent context.Context, botID string) ([]BotTemplateRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	rows, err := db.Pool.Query(ctx, `SELECT id, question, answer FROM bot_templates WHERE bot_id = $1 ORDER BY position`, botID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var templates []BotTemplateRecord
	for rows.Next() {
		var t BotTemplateRecord
		if err := rows.Scan(&t.ID, &t.Question, &t.Answer); err != nil {
			return nil, err
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

func (db *DB) BotsByOwner(parent context.Context, ownerID string) ([]BotRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	return collectBots(db.Pool.Query(ctx, `SELECT `+botColumns+botJoins+` WHERE b.owner_id = $1 ORDER BY b.id`, ownerID))
}

// BotsCatalog lists enabled bots for everyone.
func (db *DB) BotsCatalog(parent context.Context, cursor string, limit int) ([]BotRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	return collectBots(db.Pool.Query(ctx, `SELECT `+botColumns+botJoins+`
		WHERE b.enabled AND ($1::uuid IS NULL OR b.id > $1::uuid) ORDER BY b.id LIMIT $2`, cursorArg(cursor), limit))
}

func collectBots(rows pgx.Rows, err error) ([]BotRecord, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var bots []BotRecord
	for rows.Next() {
		bot, err := scanBot(rows)
		if err != nil {
			return nil, err
		}
		bots = append(bots, bot)
	}
	return bots, rows.Err()
}

// OpenBotRoom returns the customer's room with an enabled bot, creating it and
// both memberships on first use. The owner is not a member, so the owner cannot
// read the conversation.
func (db *DB) OpenBotRoom(parent context.Context, roomID, customerID, botID string) (RoomRecord, bool, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return RoomRecord{}, false, err
	}
	defer tx.Rollback(context.Background())
	var botUserID string
	err = tx.QueryRow(ctx, `SELECT user_id FROM bots WHERE id = $1 AND enabled`, botID).Scan(&botUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return RoomRecord{}, false, ErrNotFound
	}
	if err != nil {
		return RoomRecord{}, false, err
	}
	room, err := scanRoom(tx.QueryRow(ctx, `INSERT INTO rooms AS r (id, kind, created_by, bot_id, bot_customer_id)
		VALUES ($1, 'bot', $2, $3, $2)
		ON CONFLICT ON CONSTRAINT rooms_bot_customer_key DO NOTHING
		RETURNING `+roomColumns, roomID, customerID, botID))
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err := scanRoom(tx.QueryRow(ctx, `SELECT `+roomColumns+` FROM rooms r
			WHERE r.kind = 'bot' AND r.bot_id = $1 AND r.bot_customer_id = $2`, botID, customerID))
		if err != nil {
			return RoomRecord{}, false, err
		}
		return existing, false, nil
	}
	if err != nil {
		return RoomRecord{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO room_members (room_id, user_id, joined_seq)
		VALUES ($1, $2, 0), ($1, $3, 0)`, roomID, customerID, botUserID); err != nil {
		return RoomRecord{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RoomRecord{}, false, err
	}
	return room, true, nil
}

// RecentMessages returns the newest limit messages of a room in ascending order.
func (db *DB) RecentMessages(parent context.Context, roomID string, limit int) ([]MessageRecord, error) {
	ctx, cancel := operationContext(parent)
	defer cancel()
	rows, err := db.Pool.Query(ctx, `SELECT `+messageColumns+` FROM messages m
		WHERE m.room_id = $1 ORDER BY m.seq DESC LIMIT $2`, roomID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []MessageRecord
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}
