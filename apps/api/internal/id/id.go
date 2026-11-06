// Package id creates and validates the lowercase UUID strings used as
// identifiers across the API.
package id

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// New returns a random version 4 UUID.
func New() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return format(b), nil
}

// MustNew is New for callers that cannot continue without randomness.
func MustNew() string {
	value, err := New()
	if err != nil {
		panic(err)
	}
	return value
}

// FromSeed derives a stable UUID from seed, so the same seed always yields the
// same identifier. It is used for idempotency keys.
func FromSeed(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	var b [16]byte
	copy(b[:], sum[:16])
	return format(b)
}

// Valid reports whether value is a lowercase canonical UUID.
func Valid(value string) bool { return pattern.MatchString(value) }

// Canonical lowercases value and reports whether it is then a valid UUID.
func Canonical(value string) (string, bool) {
	value = strings.ToLower(value)
	return value, Valid(value)
}

func format(b [16]byte) string {
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	raw := hex.EncodeToString(b[:])
	return raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
}
