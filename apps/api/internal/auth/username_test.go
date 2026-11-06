package auth

import (
	"strings"
	"testing"
)

// Characters that are hard to see are built from code points so this file stays readable.
var (
	zeroWidthSpace = string(rune(0x200b))
	rightToLeft    = string(rune(0x202e))
	noBreakSpace   = string(rune(0x00a0))
	accentedE      = string(rune(0x00e9))
)

func TestEnglishNamesAreValid(t *testing.T) {
	for _, name := range []string{
		"Mark", "mark", "MARK", "Mark99", "mark_lee", "m@rk!", "Nat Silp", "a.b", "x1y", "O'Brien-Smith", "1a2", "!!a",
		strings.Repeat("a", 24),
	} {
		if !validUsername(canonicalUsername(name)) {
			t.Errorf("%q was refused", name)
		}
	}
}

func TestOtherNamesAreRefused(t *testing.T) {
	for name, input := range map[string]string{
		"empty":                "",
		"only spaces":          "   ",
		"too short":            "ab",
		"too long":             strings.Repeat("a", 25),
		"digits only":          "12345",
		"symbols only":         "!@#$%",
		"thai":                 "นัทศิลป์",
		"emoji":                "Mark😀",
		"accented letter":      "Caf" + accentedE,
		"null byte":            "a\x00bc",
		"escape":               "a\x1bbc",
		"zero-width space":     "a" + zeroWidthSpace + "bc",
		"right-to-left":        "abc" + rightToLeft + "def",
		"invalid utf-8":        "a\xffbc",
		"delete character":     "ab\x7fc",
		"too short after trim": "  ab  ",
	} {
		if validUsername(canonicalUsername(input)) {
			t.Errorf("%s was accepted: %q", name, input)
		}
	}
}

func TestCanonicalUsernameKeepsCaseAndTidiesSpaces(t *testing.T) {
	for input, want := range map[string]string{
		" Mark ":                          "Mark",
		"Nat   Silp":                      "Nat Silp",
		"Nat" + noBreakSpace + "\tSilp\n": "Nat Silp",
		"MARK":                            "MARK",
	} {
		if got := canonicalUsername(input); got != want {
			t.Errorf("canonicalUsername(%q) = %q, want %q", input, got, want)
		}
	}
}
