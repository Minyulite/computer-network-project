package grammar

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestCorrectValidatesBeforeUsingTheNetwork(t *testing.T) {
	configured := New(Config{APIKey: "key", Model: "model"})
	for name, text := range map[string]string{
		"empty":      "",
		"whitespace": " \n\t ",
		"too long":   strings.Repeat("ก", MaxTextRunes+1),
	} {
		if _, err := configured.Correct(context.Background(), text, "", ""); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestCorrectWithoutKeyIsNotConfigured(t *testing.T) {
	s := New(Config{Model: "model"})
	if s.Configured() {
		t.Fatal("service without a key reports configured")
	}
	if _, err := s.Correct(context.Background(), "i has went", "", ""); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseResponse(t *testing.T) {
	got, err := parseResponse(http.StatusOK, []byte(`{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"I "},{"text":"went."}]}}]}`), "i has went")
	if err != nil || got != "I went." {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestParseResponseRejectsTruncatedAnswers(t *testing.T) {
	_, err := parseResponse(http.StatusOK, []byte(`{"candidates":[{"finishReason":"MAX_TOKENS","content":{"parts":[{"text":"I went to"}]}}]}`), "i has went to school")
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseResponseFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		status    int
		body      string
		permanent bool
	}{
		"rejected key":  {http.StatusBadRequest, `{"error":{"code":400,"message":"API key not valid"}}`, true},
		"forbidden":     {http.StatusForbidden, `{"error":{"code":403,"message":"denied"}}`, true},
		"overloaded":    {http.StatusServiceUnavailable, `{"error":{"code":503,"message":"overloaded"}}`, false},
		"unknown model": {http.StatusNotFound, `{"error":{"code":404,"message":"no such model"}}`, false},
		"not JSON":      {http.StatusBadGateway, `<html>`, false},
		"no candidate":  {http.StatusOK, `{"candidates":[]}`, false},
		"safety stop":   {http.StatusOK, `{"candidates":[{"finishReason":"SAFETY","content":{"parts":[]}}]}`, false},
		"empty text":    {http.StatusOK, `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"  "}]}}]}`, false},
	} {
		_, err := parseResponse(tc.status, []byte(tc.body), "text")
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if isPermanent(err) != tc.permanent {
			t.Errorf("%s: permanent = %v", name, isPermanent(err))
		}
	}
}

func TestBuildPrompt(t *testing.T) {
	if got := buildPrompt("go", "", ""); got != "<selected>go</selected>" {
		t.Fatalf("no context: %q", got)
	}
	want := "<before>Yesterday I </before>\n<selected>go</selected>\n<after> to school.</after>"
	if got := buildPrompt("go", "Yesterday I ", " to school."); got != want {
		t.Fatalf("context: %q", got)
	}
}

func TestCleanOutput(t *testing.T) {
	for _, tc := range []struct{ raw, original, want string }{
		{"\"I went.\"\n", "i has went", "I went."},
		{"<selected>went</selected>", "go", "went"},
		{"```text\nI went.\n```", "i has went", "I went."},
		{`"Hello there"`, `"hello there"`, `"Hello there"`},
	} {
		if got := cleanOutput(tc.raw, tc.original); got != tc.want {
			t.Errorf("cleanOutput(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestSplitEdgesKeepsWhitespaceForReplacement(t *testing.T) {
	leading, core, trailing := splitEdges("  go \n")
	if leading != "  " || core != "go" || trailing != " \n" {
		t.Fatalf("%q %q %q", leading, core, trailing)
	}
}

func TestContextTrimmingDoesNotSplitCharacters(t *testing.T) {
	if got := lastRunes("กขค", 2); got != "ขค" {
		t.Fatalf("lastRunes = %q", got)
	}
	if got := firstRunes("กขค", 2); got != "กข" {
		t.Fatalf("firstRunes = %q", got)
	}
}

func TestModelForUsesFallbacksOnRetries(t *testing.T) {
	s := New(Config{Model: "main", Fallbacks: []string{"one", "two"}})
	for attempt, want := range map[int]string{1: "main", 2: "one", 3: "two"} {
		if got := s.modelFor(attempt); got != want {
			t.Errorf("attempt %d: %s", attempt, got)
		}
	}
	if got := New(Config{Model: "main"}).modelFor(3); got != "main" {
		t.Errorf("no fallbacks: %s", got)
	}
}
