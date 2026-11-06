package chat

import (
	"context"
	"errors"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/id"
	"example.com/computer-network/project/apps/api/internal/store"
)

const (
	maxGroupName       = 64
	maxMessageRunes    = 2000
	maxPreviewRunes    = 120 // The list only shows one line of the newest message.
	defaultHistorySize = 50
	maxHistorySize     = 100
)

type Service struct{ db *store.DB }

func New(db *store.DB) *Service { return &Service{db: db} }

func roomValue(record store.RoomRecord, joinedSeq int64) Room {
	start := int64(1)
	if record.Kind == RoomGroup {
		start = joinedSeq + 1
	}
	room := Room{ID: record.ID, Kind: record.Kind, Name: record.Name, CreatedBy: record.CreatedBy,
		DirectUserLow: record.DirectUserLow, DirectUserHigh: record.DirectUserHigh,
		LatestSeq: record.LastSeq, HistoryStartSeq: start, CreatedAt: record.CreatedAt,
		BotID: record.BotID, BotName: record.BotName, BotAvatar: record.BotAvatar}
	if record.Kind == RoomDirect {
		room.Direct = []UserSummary{
			{ID: record.DirectUserLow, Username: record.DirectLowName, Avatar: record.DirectLowAvatar},
			{ID: record.DirectUserHigh, Username: record.DirectHighName, Avatar: record.DirectHighAvatar}}
	}
	return room
}

func summaryValue(record store.MemberRecord) UserSummary {
	return UserSummary{ID: record.UserID, Username: record.Username, Avatar: record.Avatar}
}

func groupValue(record store.RoomRecord, preview []store.MemberRecord) Group {
	group := Group{ID: record.ID, Name: record.Name, CreatedBy: record.CreatedBy, CreatedAt: record.CreatedAt,
		Preview: make([]UserSummary, 0, len(preview))}
	for _, member := range preview {
		group.Preview = append(group.Preview, summaryValue(member))
	}
	return group
}

func memberValue(record store.MemberRecord) GroupMember {
	return GroupMember{User: summaryValue(record), JoinedAt: record.JoinedAt}
}

func messageValue(record store.MessageRecord, username, avatar string) Message {
	return Message{ID: record.ID, RoomID: record.RoomID,
		Sender:          UserSummary{ID: record.SenderID, Username: username, Avatar: avatar},
		ClientMessageID: record.ClientMessageID, Seq: record.Seq, Text: record.Text, SentAt: record.CreatedAt}
}

// canSee reports whether userID may read a room it has a membership row in.
// Direct rooms additionally require the user to be one of the two parties.
func canSee(room store.RoomRecord, userID string) bool {
	return room.Kind != RoomDirect || userID == room.DirectUserLow || userID == room.DirectUserHigh
}

// authorizeRoom returns the room when userID is a member allowed to use it.
func (s *Service) authorizeRoom(ctx context.Context, roomID, userID string) (store.RoomRecord, error) {
	room, _, err := s.db.Membership(ctx, roomID, userID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !canSee(room, userID)) {
		return store.RoomRecord{}, ErrRoomUnavailable
	}
	return room, err
}

// storeMessage saves record under a fresh message ID. When the client message
// ID was used before, the earlier message is returned with created false.
func (s *Service) storeMessage(ctx context.Context, record store.MessageRecord) (store.MessageRecord, bool, error) {
	messageID, err := id.New()
	if err != nil {
		return store.MessageRecord{}, false, err
	}
	record.ID = messageID
	saved, created, err := s.db.SaveMessage(ctx, record)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return store.MessageRecord{}, false, ErrRoomUnavailable
	case errors.Is(err, store.ErrSequenceExhausted):
		return store.MessageRecord{}, false, ErrSequenceExhausted
	}
	return saved, created, err
}

// sendResult wraps a stored message and, for a new one, who should receive it.
func (s *Service) sendResult(ctx context.Context, kind string, record store.MessageRecord, username, avatar string, created bool) (SendResult, error) {
	result := SendResult{RoomKind: kind, Message: messageValue(record, username, avatar), Created: created}
	if created {
		var err error
		result.RecipientUserIDs, err = s.db.EligibleMemberIDs(ctx, record.RoomID, record.Seq)
		if err != nil {
			return SendResult{}, err
		}
	}
	return result, nil
}

func (s *Service) OpenDirect(ctx context.Context, caller auth.Identity, otherUserID string) (Room, bool, error) {
	userID, err := callerID(caller)
	if err != nil {
		return Room{}, false, err
	}
	otherUserID, err = parseID(otherUserID)
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
	lowID, highID := min(userID, otherUserID), max(userID, otherUserID)
	roomID, err := id.New()
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
	name, err = cleanText(name, 1, maxGroupName)
	if err != nil {
		return Room{}, err
	}
	roomID, err := id.New()
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
	roomID, err = parseID(roomID)
	if err != nil {
		return JoinResult{}, err
	}
	joinedSeq, created, err := s.db.JoinGroup(ctx, roomID, userID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return JoinResult{}, ErrRoomUnavailable
	case errors.Is(err, store.ErrNotGroup):
		return JoinResult{}, ErrNotGroup
	case err != nil:
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
	roomID, err = parseID(roomID)
	if err != nil {
		return SendResult{}, err
	}
	clientMessageID, err = parseID(clientMessageID)
	if err != nil {
		return SendResult{}, err
	}
	text, err = cleanText(text, 1, maxMessageRunes)
	if err != nil {
		return SendResult{}, err
	}
	room, err := s.authorizeRoom(ctx, roomID, userID)
	if err != nil {
		return SendResult{}, err
	}
	record, created, err := s.storeMessage(ctx, store.MessageRecord{
		RoomID: roomID, SenderID: userID, ClientMessageID: clientMessageID, Text: text})
	if err != nil {
		return SendResult{}, err
	}
	if record.RoomID != roomID || record.Text != text {
		return SendResult{}, ErrIdempotencyConflict
	}
	return s.sendResult(ctx, room.Kind, record, caller.User.Username, caller.User.Avatar, created)
}

func (s *Service) History(ctx context.Context, caller auth.Identity, roomID string, options HistoryOptions) (HistoryPage, error) {
	userID, err := callerID(caller)
	if err != nil {
		return HistoryPage{}, err
	}
	roomID, err = parseID(roomID)
	if err != nil {
		return HistoryPage{}, err
	}
	options, err = historyOptions(options)
	if err != nil {
		return HistoryPage{}, err
	}
	if _, err := s.authorizeRoom(ctx, roomID, userID); err != nil {
		return HistoryPage{}, err
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
		page.Messages = append(page.Messages, messageValue(message, message.SenderUsername, message.SenderAvatar))
	}
	page.SetContinuation(options.AfterSeq != nil)
	return page, nil
}

func historyOptions(options HistoryOptions) (HistoryOptions, error) {
	inRange := func(v *int64, low int64) bool { return v == nil || (*v >= low && *v <= MaxSequence) }
	if (options.BeforeSeq != nil && options.AfterSeq != nil) ||
		options.Limit < 0 || options.Limit > maxHistorySize ||
		!inRange(options.BeforeSeq, 1) || !inRange(options.AfterSeq, 0) || !inRange(options.UpToSeq, 0) {
		return HistoryOptions{}, ErrInvalidInput
	}
	if options.Limit == 0 {
		options.Limit = defaultHistorySize
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
	// Previews are fetched once for the groups on this page, not once per group.
	shown, _ := trimPage(records, options.Limit, func(r store.RoomRecord) string { return r.ID })
	ids := make([]string, 0, len(shown))
	for _, record := range shown {
		ids = append(ids, record.ID)
	}
	previews, err := s.db.GroupPreviews(ctx, ids)
	if err != nil {
		return Page[Group]{}, err
	}
	return paginate(records, options.Limit, func(r store.RoomRecord) string { return r.ID },
		func(r store.RoomRecord) Group { return groupValue(r, previews[r.ID]) }), nil
}

func (s *Service) GroupMembers(ctx context.Context, caller auth.Identity, roomID string, options PageOptions) (Page[GroupMember], error) {
	if _, err := callerID(caller); err != nil {
		return Page[GroupMember]{}, err
	}
	roomID, err := parseID(roomID)
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
	if room.Kind != RoomGroup {
		return Page[GroupMember]{}, ErrNotGroup
	}
	records, err := s.db.GroupMembers(ctx, roomID, options.Cursor, options.Limit+1)
	if err != nil {
		return Page[GroupMember]{}, err
	}
	return paginate(records, options.Limit, func(r store.MemberRecord) string { return r.UserID }, memberValue), nil
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
	// The cursor comes from the unfiltered records so hidden rooms never stall paging.
	records, next := trimPage(records, options.Limit, func(r store.OwnRoomRecord) string { return r.Room.ID })
	page := Page[Room]{Items: make([]Room, 0, len(records)), NextCursor: next}
	for _, record := range records {
		if !canSee(record.Room, userID) {
			continue
		}
		room := roomValue(record.Room, record.JoinedSeq)
		if last := record.Last; last != nil {
			room.Last = &LastMessage{SenderID: last.SenderID, SenderName: last.SenderName, Text: clipRunes(last.Text, maxPreviewRunes), SentAt: last.CreatedAt}
		}
		page.Items = append(page.Items, room)
	}
	return page, nil
}
