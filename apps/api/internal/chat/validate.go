package chat

import (
	"strings"
	"unicode/utf8"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/id"
)

// callerID returns the authenticated user's canonical ID.
func callerID(identity auth.Identity) (string, error) {
	userID, ok := id.Canonical(identity.User.ID)
	if identity.SessionID == "" || !ok {
		return "", ErrUnauthenticated
	}
	return userID, nil
}

// parseID canonicalizes an ID supplied by a client.
func parseID(value string) (string, error) {
	value, ok := id.Canonical(value)
	if !ok {
		return "", ErrInvalidInput
	}
	return value, nil
}

// cleanText trims value and checks that it is valid UTF-8 of min to max runes.
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
