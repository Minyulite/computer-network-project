package chat

import (
	"errors"
	"time"
)

var ErrInvalidInput = errors.New("invalid chat input")
var ErrUnauthenticated = errors.New("unauthenticated")
var ErrUserUnavailable = errors.New("user unavailable")
var ErrSelfChat = errors.New("self-chat is unavailable")
var ErrRoomUnavailable = errors.New("room unavailable")
var ErrNotGroup = errors.New("not a group")
var ErrIdempotencyConflict = errors.New("client message ID conflicts with an earlier send")
var ErrSequenceExhausted = errors.New("room sequence exhausted")

const MaxSequence int64 = 9007199254740991

type UserSummary struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type Room struct {
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	Name            string    `json:"name,omitempty"`
	CreatedBy       string    `json:"createdBy"`
	DirectUserLow   string    `json:"directUserLow,omitempty"`
	DirectUserHigh  string    `json:"directUserHigh,omitempty"`
	LatestSeq       int64     `json:"latestSeq"`
	HistoryStartSeq int64     `json:"historyStartSeq"`
	CreatedAt       time.Time `json:"createdAt"`
}

type Group struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
}

type GroupMember struct {
	User     UserSummary `json:"user"`
	JoinedAt time.Time   `json:"joinedAt"`
}

type Message struct {
	ID              string      `json:"id"`
	RoomID          string      `json:"roomId"`
	Sender          UserSummary `json:"sender"`
	ClientMessageID string      `json:"clientMessageId"`
	Seq             int64       `json:"seq"`
	Text            string      `json:"text"`
	SentAt          time.Time   `json:"sentAt"`
}

type SendResult struct {
	Message          Message
	Created          bool
	RecipientUserIDs []string
}

type JoinResult struct {
	Room    Room
	Created bool
}

type PageOptions struct {
	Limit  int
	Cursor string
}

type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type HistoryOptions struct {
	Limit     int
	BeforeSeq *int64
	AfterSeq  *int64
	UpToSeq   *int64
}

type HistoryPage struct {
	RoomID          string    `json:"roomId"`
	Messages        []Message `json:"messages"`
	HistoryStartSeq int64     `json:"historyStartSeq"`
	UpToSeq         int64     `json:"upToSeq"`
	HasMore         bool      `json:"hasMore"`
	NextBeforeSeq   *int64    `json:"nextBeforeSeq,omitempty"`
	NextAfterSeq    *int64    `json:"nextAfterSeq,omitempty"`
}
