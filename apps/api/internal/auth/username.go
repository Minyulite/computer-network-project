package auth

import "strings"

const (
	minUsernameChars = 3
	maxUsernameChars = 24
)

// canonicalUsername tidies what the user typed: surrounding whitespace goes and
// every run of whitespace becomes one plain space. Case is kept for display;
// sign-in and uniqueness ignore it.
func canonicalUsername(username string) string {
	return strings.Join(strings.Fields(username), " ")
}

// validUsername accepts English names: 3 to 24 printable ASCII characters
// (letters, digits, spaces and symbols) with at least one letter, so "Mark",
// "Mark99" and "m@rk_lee" are all fine. Anything outside ASCII is refused,
// which also keeps out invisible and look-alike characters. Expects
// canonicalUsername's output.
func validUsername(name string) bool {
	if len(name) < minUsernameChars || len(name) > maxUsernameChars {
		return false
	}
	hasLetter := false
	for i := 0; i < len(name); i++ {
		switch c := name[i]; {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
			hasLetter = true
		case c >= ' ' && c <= '~':
		default:
			return false
		}
	}
	return hasLetter
}
