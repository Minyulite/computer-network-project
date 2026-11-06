package chat

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/store"
)

const (
	MaxBotTemplates      = 20
	maxBotName           = 64
	maxBotDescription    = 2000
	maxTemplateQuestion  = 300
	maxTemplateAnswer    = 1000
	maxBotReplyRunes     = 2000
	botHistoryTurnsLimit = 8
)

// Bot is what everyone may see in the catalogue. Description and templates stay private to the owner.
type Bot struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	OwnerID       string    `json:"ownerId"`
	OwnerUsername string    `json:"ownerUsername"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"createdAt"`
}

type BotTemplate struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// BotDetail is the owner's full view.
type BotDetail struct {
	Bot
	Description string        `json:"description"`
	Templates   []BotTemplate `json:"templates"`
}

type BotTemplateInput struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type BotInput struct {
	Name        string
	Description string
	Enabled     bool
	Templates   []BotTemplateInput
}

// BotTurn is one earlier message, used so follow-up questions keep their context.
type BotTurn struct {
	FromBot bool
	Text    string
}

// BotContext is everything needed to answer in a bot room. It is internal and never serialized.
type BotContext struct {
	BotID       string
	Name        string
	Description string
	Enabled     bool
	Templates   []BotTemplate
	History     []BotTurn
}

func botValue(r store.BotRecord) Bot {
	return Bot{ID: r.ID, Name: r.Name, OwnerID: r.OwnerID, OwnerUsername: r.OwnerUsername, Enabled: r.Enabled, CreatedAt: r.CreatedAt}
}

func cleanText(value string, min, max int) (string, error) {
	if !utf8.ValidString(value) {
		return "", ErrInvalidInput
	}
	value = strings.TrimSpace(value)
	if count := utf8.RuneCountInString(value); count < min || count > max {
		return "", ErrInvalidInput
	}
	return value, nil
}

func validateBot(input BotInput) (BotInput, []store.BotTemplateRecord, error) {
	name, err := cleanText(input.Name, 1, maxBotName)
	if err != nil {
		return BotInput{}, nil, err
	}
	description, err := cleanText(input.Description, 0, maxBotDescription)
	if err != nil {
		return BotInput{}, nil, err
	}
	if len(input.Templates) > MaxBotTemplates {
		return BotInput{}, nil, ErrInvalidInput
	}
	records := make([]store.BotTemplateRecord, 0, len(input.Templates))
	for _, t := range input.Templates {
		question, err := cleanText(t.Question, 1, maxTemplateQuestion)
		if err != nil {
			return BotInput{}, nil, err
		}
		answer, err := cleanText(t.Answer, 1, maxTemplateAnswer)
		if err != nil {
			return BotInput{}, nil, err
		}
		id, err := newUUID()
		if err != nil {
			return BotInput{}, nil, err
		}
		records = append(records, store.BotTemplateRecord{ID: id, Question: question, Answer: answer})
	}
	return BotInput{Name: name, Description: description, Enabled: input.Enabled}, records, nil
}

func (s *Service) botDetail(ctx context.Context, record store.BotRecord) (BotDetail, error) {
	templates, err := s.db.BotTemplates(ctx, record.ID)
	if err != nil {
		return BotDetail{}, err
	}
	detail := BotDetail{Bot: botValue(record), Description: record.Description, Templates: make([]BotTemplate, 0, len(templates))}
	for _, t := range templates {
		detail.Templates = append(detail.Templates, BotTemplate{ID: t.ID, Question: t.Question, Answer: t.Answer})
	}
	return detail, nil
}

func (s *Service) CreateBot(ctx context.Context, caller auth.Identity, input BotInput) (BotDetail, error) {
	userID, err := callerID(caller)
	if err != nil {
		return BotDetail{}, err
	}
	clean, templates, err := validateBot(input)
	if err != nil {
		return BotDetail{}, err
	}
	botID, err := newUUID()
	if err != nil {
		return BotDetail{}, err
	}
	botUserID, err := newUUID()
	if err != nil {
		return BotDetail{}, err
	}
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return BotDetail{}, err
	}
	record, err := s.db.CreateBot(ctx, store.BotRecord{ID: botID, OwnerID: userID, UserID: botUserID,
		Username: "bot_" + hex.EncodeToString(suffix[:]), Name: clean.Name, Description: clean.Description, Enabled: true}, templates)
	if errors.Is(err, store.ErrBotLimit) {
		return BotDetail{}, ErrBotLimit
	}
	if errors.Is(err, store.ErrNotFound) {
		return BotDetail{}, ErrUnauthenticated
	}
	if err != nil {
		return BotDetail{}, err
	}
	return s.botDetail(ctx, record)
}

func (s *Service) UpdateBot(ctx context.Context, caller auth.Identity, botID string, input BotInput) (BotDetail, error) {
	userID, err := callerID(caller)
	if err != nil {
		return BotDetail{}, err
	}
	botID, err = canonicalUUID(botID)
	if err != nil {
		return BotDetail{}, err
	}
	clean, templates, err := validateBot(input)
	if err != nil {
		return BotDetail{}, err
	}
	record, err := s.db.UpdateBot(ctx, userID, store.BotRecord{ID: botID, Name: clean.Name,
		Description: clean.Description, Enabled: clean.Enabled}, templates)
	if errors.Is(err, store.ErrNotFound) {
		return BotDetail{}, ErrBotUnavailable
	}
	if err != nil {
		return BotDetail{}, err
	}
	return s.botDetail(ctx, record)
}

// GetBot returns the full configuration to the owner only.
func (s *Service) GetBot(ctx context.Context, caller auth.Identity, botID string) (BotDetail, error) {
	userID, err := callerID(caller)
	if err != nil {
		return BotDetail{}, err
	}
	botID, err = canonicalUUID(botID)
	if err != nil {
		return BotDetail{}, err
	}
	record, err := s.db.BotByID(ctx, botID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && record.OwnerID != userID) {
		return BotDetail{}, ErrBotUnavailable
	}
	if err != nil {
		return BotDetail{}, err
	}
	return s.botDetail(ctx, record)
}

func (s *Service) MyBots(ctx context.Context, caller auth.Identity) ([]Bot, error) {
	userID, err := callerID(caller)
	if err != nil {
		return nil, err
	}
	records, err := s.db.BotsByOwner(ctx, userID)
	if err != nil {
		return nil, err
	}
	bots := make([]Bot, 0, len(records))
	for _, record := range records {
		bots = append(bots, botValue(record))
	}
	return bots, nil
}

func (s *Service) BotCatalog(ctx context.Context, caller auth.Identity, options PageOptions) (Page[Bot], error) {
	if _, err := callerID(caller); err != nil {
		return Page[Bot]{}, err
	}
	options, err := pageOptions(options)
	if err != nil {
		return Page[Bot]{}, err
	}
	records, err := s.db.BotsCatalog(ctx, options.Cursor, options.Limit+1)
	if err != nil {
		return Page[Bot]{}, err
	}
	page := Page[Bot]{Items: make([]Bot, 0, min(len(records), options.Limit))}
	if len(records) > options.Limit {
		page.NextCursor = records[options.Limit-1].ID
		records = records[:options.Limit]
	}
	for _, record := range records {
		page.Items = append(page.Items, botValue(record))
	}
	return page, nil
}

// OpenBot opens (or returns) the caller's private room with a bot. Owners may
// chat with their own bot to try it.
func (s *Service) OpenBot(ctx context.Context, caller auth.Identity, botID string) (Room, bool, error) {
	userID, err := callerID(caller)
	if err != nil {
		return Room{}, false, err
	}
	botID, err = canonicalUUID(botID)
	if err != nil {
		return Room{}, false, err
	}
	roomID, err := newUUID()
	if err != nil {
		return Room{}, false, err
	}
	record, created, err := s.db.OpenBotRoom(ctx, roomID, userID, botID)
	if errors.Is(err, store.ErrNotFound) {
		return Room{}, false, ErrBotUnavailable
	}
	if err != nil {
		return Room{}, false, err
	}
	return roomValue(record, 0), created, nil
}

// BotRoomContext loads the bot's configuration and the latest turns of a bot room.
func (s *Service) BotRoomContext(ctx context.Context, roomID string) (BotContext, error) {
	record, err := s.db.BotByRoom(ctx, roomID)
	if errors.Is(err, store.ErrNotFound) {
		return BotContext{}, ErrBotUnavailable
	}
	if err != nil {
		return BotContext{}, err
	}
	templates, err := s.db.BotTemplates(ctx, record.ID)
	if err != nil {
		return BotContext{}, err
	}
	messages, err := s.db.RecentMessages(ctx, roomID, botHistoryTurnsLimit)
	if err != nil {
		return BotContext{}, err
	}
	result := BotContext{BotID: record.ID, Name: record.Name, Description: record.Description, Enabled: record.Enabled}
	for _, t := range templates {
		result.Templates = append(result.Templates, BotTemplate{ID: t.ID, Question: t.Question, Answer: t.Answer})
	}
	for _, m := range messages {
		result.History = append(result.History, BotTurn{FromBot: m.SenderID == record.UserID, Text: m.Text})
	}
	return result, nil
}

// replyClientID derives the bot reply's idempotency key from the customer
// message, so answering the same message twice stores one reply.
func replyClientID(customerMessageID string) string {
	sum := sha256.Sum256([]byte("bot-reply:" + customerMessageID))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	raw := hex.EncodeToString(b)
	return raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
}

// SendBotReply stores the bot's answer to a customer message as the bot's system user.
func (s *Service) SendBotReply(ctx context.Context, roomID, customerMessageID, text string) (SendResult, error) {
	roomID, err := canonicalUUID(roomID)
	if err != nil {
		return SendResult{}, err
	}
	customerMessageID, err = canonicalUUID(customerMessageID)
	if err != nil {
		return SendResult{}, err
	}
	if !utf8.ValidString(text) {
		return SendResult{}, ErrInvalidInput
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > maxBotReplyRunes {
		text = string([]rune(text)[:maxBotReplyRunes])
	}
	if text == "" {
		return SendResult{}, ErrInvalidInput
	}
	bot, err := s.db.BotByRoom(ctx, roomID)
	if errors.Is(err, store.ErrNotFound) {
		return SendResult{}, ErrBotUnavailable
	}
	if err != nil {
		return SendResult{}, err
	}
	messageID, err := newUUID()
	if err != nil {
		return SendResult{}, err
	}
	record, created, err := s.db.SaveMessage(ctx, store.MessageRecord{ID: messageID, RoomID: roomID,
		SenderID: bot.UserID, ClientMessageID: replyClientID(customerMessageID), Text: text})
	if errors.Is(err, store.ErrNotFound) {
		return SendResult{}, ErrRoomUnavailable
	}
	if errors.Is(err, store.ErrSequenceExhausted) {
		return SendResult{}, ErrSequenceExhausted
	}
	if err != nil {
		return SendResult{}, err
	}
	result := SendResult{RoomKind: "bot", Message: messageValue(record, bot.Username), Created: created}
	if created {
		result.RecipientUserIDs, err = s.db.EligibleMemberIDs(ctx, roomID, record.Seq)
		if err != nil {
			return SendResult{}, err
		}
	}
	return result, nil
}
