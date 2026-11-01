package chat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/store"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Service struct{ db *store.DB }

func New(db *store.DB) *Service { return &Service{db: db} }

func callerID(identity auth.Identity) (string, error) {
	id := strings.ToLower(identity.User.ID)
	if identity.SessionID == "" || !uuidPattern.MatchString(id) {
		return "", ErrUnauthenticated
	}
	return id, nil
}

func canonicalUUID(value string) (string, error) {
	value = strings.ToLower(value)
	if !uuidPattern.MatchString(value) {
		return "", ErrInvalidInput
	}
	return value, nil
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	raw := hex.EncodeToString(b[:])
	return raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:], nil
}

func roomValue(record store.RoomRecord, joinedSeq int64) Room {
	start := int64(1)
	if record.Kind == "group" {
		start = joinedSeq + 1
	}
	return Room{ID: record.ID, Kind: record.Kind, Name: record.Name, CreatedBy: record.CreatedBy,
		DirectUserLow: record.DirectUserLow, DirectUserHigh: record.DirectUserHigh,
		LatestSeq: record.LastSeq, HistoryStartSeq: start, CreatedAt: record.CreatedAt}
}

func groupValue(record store.RoomRecord) Group {
	return Group{ID: record.ID, Name: record.Name, CreatedBy: record.CreatedBy, CreatedAt: record.CreatedAt}
}

func (s *Service) OpenDirect(ctx context.Context, caller auth.Identity, otherUserID string) (Room, bool, error) {
	userID, err := callerID(caller)
	if err != nil {
		return Room{}, false, err
	}
	otherUserID, err = canonicalUUID(otherUserID)
	if err != nil {
		return Room{}, false, err
	}
	if userID == otherUserID {
		return Room{}, false, ErrSelfChat
	}
	exists, err := s.db.UserExists(ctx, otherUserID)
	if err != nil {
		return Room{}, false, err
	}
	if !exists {
		return Room{}, false, ErrUserUnavailable
	}
	lowID, highID := userID, otherUserID
	if highID < lowID {
		lowID, highID = highID, lowID
	}
	roomID, err := newUUID()
	if err != nil {
		return Room{}, false, err
	}
	record, created, err := s.db.OpenDirect(ctx, roomID, userID, lowID, highID)
	if err != nil {
		return Room{}, false, err
	}
	members, err := s.db.DirectMemberIDs(ctx, record.ID)
	if err != nil {
		return Room{}, false, err
	}
	if len(members) != 2 || members[0] != lowID || members[1] != highID {
		return Room{}, false, ErrRoomUnavailable
	}
	return roomValue(record, 0), created, nil
}

func (s *Service) CreateGroup(ctx context.Context, caller auth.Identity, name string) (Room, error) {
	userID, err := callerID(caller)
	if err != nil {
		return Room{}, err
	}
	if !utf8.ValidString(name) {
		return Room{}, ErrInvalidInput
	}
	name = strings.TrimSpace(name)
	if count := utf8.RuneCountInString(name); count < 1 || count > 64 {
		return Room{}, ErrInvalidInput
	}
	roomID, err := newUUID()
	if err != nil {
		return Room{}, err
	}
	record, err := s.db.CreateGroup(ctx, roomID, userID, name)
	if err != nil {
		return Room{}, err
	}
	return roomValue(record, 0), nil
}

func (s *Service) JoinGroup(ctx context.Context, caller auth.Identity, roomID string) (JoinResult, error) {
	userID, err := callerID(caller)
	if err != nil {
		return JoinResult{}, err
	}
	roomID, err = canonicalUUID(roomID)
	if err != nil {
		return JoinResult{}, err
	}
	joinedSeq, created, err := s.db.JoinGroup(ctx, roomID, userID)
	if errors.Is(err, store.ErrNotFound) {
		return JoinResult{}, ErrRoomUnavailable
	}
	if errors.Is(err, store.ErrNotGroup) {
		return JoinResult{}, ErrNotGroup
	}
	if err != nil {
		return JoinResult{}, err
	}
	record, err := s.db.RoomByID(ctx, roomID)
	if err != nil {
		return JoinResult{}, err
	}
	return JoinResult{Room: roomValue(record, joinedSeq), Created: created}, nil
}

func (s *Service) SendMessage(ctx context.Context, caller auth.Identity,
	roomID, clientMessageID, text string) (SendResult, error) {
	userID, err := callerID(caller)
	if err != nil {
		return SendResult{}, err
	}
	roomID, err = canonicalUUID(roomID)
	if err != nil {
		return SendResult{}, err
	}
	clientMessageID, err = canonicalUUID(clientMessageID)
	if err != nil {
		return SendResult{}, err
	}
	if !utf8.ValidString(text) {
		return SendResult{}, ErrInvalidInput
	}
	text = strings.TrimSpace(text)
	if count := utf8.RuneCountInString(text); count < 1 || count > 2000 {
		return SendResult{}, ErrInvalidInput
	}
	room, _, err := s.db.Membership(ctx, roomID, userID)
	if errors.Is(err, store.ErrNotFound) {
		return SendResult{}, ErrRoomUnavailable
	}
	if err != nil {
		return SendResult{}, err
	}
	if room.Kind == "direct" && userID != room.DirectUserLow && userID != room.DirectUserHigh {
		return SendResult{}, ErrRoomUnavailable
	}
	messageID, err := newUUID()
	if err != nil {
		return SendResult{}, err
	}
	record, created, err := s.db.SaveMessage(ctx, store.MessageRecord{
		ID: messageID, RoomID: roomID, SenderID: userID, ClientMessageID: clientMessageID, Text: text})
	if errors.Is(err, store.ErrNotFound) {
		return SendResult{}, ErrRoomUnavailable
	}
	if errors.Is(err, store.ErrSequenceExhausted) {
		return SendResult{}, ErrSequenceExhausted
	}
	if err != nil {
		return SendResult{}, err
	}
	if record.RoomID != roomID || record.Text != text {
		return SendResult{}, ErrIdempotencyConflict
	}
	result := SendResult{Message: messageValue(record, caller.User.Username), Created: created}
	if created {
		result.RecipientUserIDs, err = s.db.EligibleMemberIDs(ctx, roomID, record.Seq)
		if err != nil {
			return SendResult{}, err
		}
	}
	return result, nil
}

func messageValue(record store.MessageRecord, username string) Message {
	return Message{ID: record.ID, RoomID: record.RoomID,
		Sender:          UserSummary{ID: record.SenderID, Username: username},
		ClientMessageID: record.ClientMessageID, Seq: record.Seq, Text: record.Text, SentAt: record.CreatedAt}
}

func (s *Service) History(ctx context.Context, caller auth.Identity, roomID string, options HistoryOptions) (HistoryPage, error) {
	userID, err := callerID(caller)
	if err != nil {
		return HistoryPage{}, err
	}
	roomID, err = canonicalUUID(roomID)
	if err != nil {
		return HistoryPage{}, err
	}
	if options.BeforeSeq != nil && options.AfterSeq != nil {
		return HistoryPage{}, ErrInvalidInput
	}
	if options.Limit < 0 || options.Limit > 100 ||
		(options.BeforeSeq != nil && (*options.BeforeSeq < 1 || *options.BeforeSeq > MaxSequence)) ||
		(options.AfterSeq != nil && (*options.AfterSeq < 0 || *options.AfterSeq > MaxSequence)) ||
		(options.UpToSeq != nil && (*options.UpToSeq < 0 || *options.UpToSeq > MaxSequence)) {
		return HistoryPage{}, ErrInvalidInput
	}
	if options.Limit == 0 {
		options.Limit = 50
	}
	room, _, err := s.db.Membership(ctx, roomID, userID)
	if errors.Is(err, store.ErrNotFound) {
		return HistoryPage{}, ErrRoomUnavailable
	}
	if err != nil {
		return HistoryPage{}, err
	}
	if room.Kind == "direct" && userID != room.DirectUserLow && userID != room.DirectUserHigh {
		return HistoryPage{}, ErrRoomUnavailable
	}
	record, err := s.db.History(ctx, roomID, userID, options.Limit,
		options.BeforeSeq, options.AfterSeq, options.UpToSeq)
	if errors.Is(err, store.ErrNotFound) {
		return HistoryPage{}, ErrRoomUnavailable
	}
	if err != nil {
		return HistoryPage{}, err
	}
	page := HistoryPage{RoomID: roomID, Messages: make([]Message, 0, len(record.Messages)),
		HistoryStartSeq: roomValue(record.Room, record.JoinedSeq).HistoryStartSeq,
		UpToSeq:         record.UpToSeq, HasMore: record.HasMore}
	for _, message := range record.Messages {
		page.Messages = append(page.Messages, messageValue(message, message.SenderUsername))
	}
	if page.HasMore && len(page.Messages) > 0 {
		if options.AfterSeq != nil {
			value := page.Messages[len(page.Messages)-1].Seq
			page.NextAfterSeq = &value
		} else {
			value := page.Messages[0].Seq
			page.NextBeforeSeq = &value
		}
	}
	return page, nil
}

func pageOptions(options PageOptions) (PageOptions, error) {
	if options.Limit < 0 || options.Limit > 50 {
		return PageOptions{}, ErrInvalidInput
	}
	if options.Limit == 0 {
		options.Limit = 20
	}
	if options.Cursor != "" {
		var err error
		options.Cursor, err = canonicalUUID(options.Cursor)
		if err != nil {
			return PageOptions{}, err
		}
	}
	return options, nil
}

func (s *Service) Groups(ctx context.Context, caller auth.Identity, options PageOptions) (Page[Group], error) {
	if _, err := callerID(caller); err != nil {
		return Page[Group]{}, err
	}
	options, err := pageOptions(options)
	if err != nil {
		return Page[Group]{}, err
	}
	records, err := s.db.Groups(ctx, options.Cursor, options.Limit+1)
	if err != nil {
		return Page[Group]{}, err
	}
	page := Page[Group]{Items: make([]Group, 0, min(len(records), options.Limit))}
	if len(records) > options.Limit {
		page.NextCursor = records[options.Limit-1].ID
		records = records[:options.Limit]
	}
	for _, record := range records {
		page.Items = append(page.Items, groupValue(record))
	}
	return page, nil
}

func (s *Service) GroupMembers(ctx context.Context, caller auth.Identity, roomID string, options PageOptions) (Page[GroupMember], error) {
	if _, err := callerID(caller); err != nil {
		return Page[GroupMember]{}, err
	}
	roomID, err := canonicalUUID(roomID)
	if err != nil {
		return Page[GroupMember]{}, err
	}
	options, err = pageOptions(options)
	if err != nil {
		return Page[GroupMember]{}, err
	}
	room, err := s.db.RoomByID(ctx, roomID)
	if errors.Is(err, store.ErrNotFound) {
		return Page[GroupMember]{}, ErrRoomUnavailable
	}
	if err != nil {
		return Page[GroupMember]{}, err
	}
	if room.Kind != "group" {
		return Page[GroupMember]{}, ErrNotGroup
	}
	records, err := s.db.GroupMembers(ctx, roomID, options.Cursor, options.Limit+1)
	if err != nil {
		return Page[GroupMember]{}, err
	}
	page := Page[GroupMember]{Items: make([]GroupMember, 0, min(len(records), options.Limit))}
	if len(records) > options.Limit {
		page.NextCursor = records[options.Limit-1].UserID
		records = records[:options.Limit]
	}
	for _, record := range records {
		page.Items = append(page.Items, GroupMember{User: UserSummary{ID: record.UserID, Username: record.Username}, JoinedAt: record.JoinedAt})
	}
	return page, nil
}

func (s *Service) OwnRooms(ctx context.Context, caller auth.Identity, options PageOptions) (Page[Room], error) {
	userID, err := callerID(caller)
	if err != nil {
		return Page[Room]{}, err
	}
	options, err = pageOptions(options)
	if err != nil {
		return Page[Room]{}, err
	}
	records, err := s.db.OwnRooms(ctx, userID, options.Cursor, options.Limit+1)
	if err != nil {
		return Page[Room]{}, err
	}
	page := Page[Room]{Items: make([]Room, 0, min(len(records), options.Limit))}
	if len(records) > options.Limit {
		page.NextCursor = records[options.Limit-1].Room.ID
		records = records[:options.Limit]
	}
	for _, record := range records {
		if record.Room.Kind == "direct" && userID != record.Room.DirectUserLow && userID != record.Room.DirectUserHigh {
			continue
		}
		page.Items = append(page.Items, roomValue(record.Room, record.JoinedSeq))
	}
	return page, nil
}
