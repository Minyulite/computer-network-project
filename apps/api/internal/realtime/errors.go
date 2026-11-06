package realtime

import (
	"errors"

	"example.com/computer-network/project/apps/api/internal/ai/grammar"
	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/chat"
)

var errUnknownCommand = errors.New("unknown command")

// errorCodes maps errors to what clients see. The first matching row wins;
// anything unlisted is reported as internal_error without details.
var errorCodes = []struct {
	errs    []error
	code    string
	message string
}{
	{[]error{errEnvelope, chat.ErrInvalidInput}, "invalid_payload", "Invalid payload"},
	{[]error{errUnknownCommand}, "unknown_type", "Unknown command"},
	{[]error{auth.ErrUnauthenticated, chat.ErrUnauthenticated}, "unauthenticated", "Authentication required"},
	{[]error{chat.ErrRoomUnavailable}, "room_unavailable", "Room unavailable"},
	{[]error{chat.ErrNotGroup}, "not_a_group", "Not a group"},
	{[]error{chat.ErrIdempotencyConflict}, "idempotency_conflict", "Message ID conflicts with earlier send"},
	{[]error{chat.ErrUserUnavailable}, "user_unavailable", "User unavailable"},
	{[]error{chat.ErrBotUnavailable}, "bot_unavailable", "Bot unavailable"},
	{[]error{chat.ErrBotLimit}, "bot_limit", "You have reached the limit of bots"},
	{[]error{chat.ErrSelfChat}, "self_chat", "Cannot open a direct room with yourself"},
	{[]error{chat.ErrSequenceExhausted}, "sequence_exhausted", "Room sequence exhausted"},
	{[]error{auth.ErrRateLimited}, "rate_limited", "Too many requests"},
	{[]error{grammar.ErrInvalidInput}, "grammar_invalid", "Select 1–2,000 characters to correct"},
	{[]error{grammar.ErrNotConfigured}, "grammar_unavailable", "Grammar correction is not configured on this server"},
	{[]error{grammar.ErrIncomplete}, "grammar_incomplete", "The correction was incomplete. Select less text and try again"},
	{[]error{grammar.ErrUnavailable}, "ai_unavailable", "The AI service is unavailable. Try again"},
	{[]error{errGrammarBusy}, "grammar_busy", "Other corrections are still running. Please wait"},
}

func protocolErrorFor(err error) protocolError {
	for _, row := range errorCodes {
		for _, target := range row.errs {
			if errors.Is(err, target) {
				return protocolError{row.code, row.message}
			}
		}
	}
	return protocolError{"internal_error", "Request failed"}
}
