package grammar

import (
	"context"
	"errors"
	"strings"
	"testing"

	"example.com/computer-network/project/apps/api/internal/ai/llm"
)

type fakeGenerator struct {
	reply string
	err   error
	calls []llm.Request
}

func (f *fakeGenerator) Generate(_ context.Context, req llm.Request) (string, error) {
	f.calls = append(f.calls, req)
	return f.reply, f.err
}

func TestCorrectValidatesBeforeUsingTheNetwork(t *testing.T) {
	ai := &fakeGenerator{reply: "unused"}
	s := New(ai)
	for name, text := range map[string]string{
		"empty":      "",
		"whitespace": " \n\t ",
		"too long":   strings.Repeat("ก", MaxTextRunes+1),
	} {
		if _, err := s.Correct(context.Background(), text, "", ""); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if len(ai.calls) != 0 {
		t.Fatalf("AI called %d times for invalid input", len(ai.calls))
	}
}

func TestCorrectWithoutKeyIsNotConfigured(t *testing.T) {
	s := New(llm.New(llm.Config{Model: "model"}))
	if _, err := s.Correct(context.Background(), "i has went", "", ""); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v", err)
	}
}

func TestCorrectKeepsOuterWhitespaceAndSendsContext(t *testing.T) {
	ai := &fakeGenerator{reply: "\"went\"\n"}
	got, err := New(ai).Correct(context.Background(), " go ", "Yesterday I", "to school.")
	if err != nil || got != " went " {
		t.Fatalf("got %q, %v", got, err)
	}
	req := ai.calls[0]
	if req.System != systemPrompt || req.MaxTokens != maxOutputTokens || req.Timeout != totalTimeout {
		t.Fatalf("request settings %+v", req)
	}
	if want := "<before>Yesterday I </before>\n<selected>go</selected>\n<after> to school.</after>"; req.Prompt != want {
		t.Fatalf("prompt %q", req.Prompt)
	}
}

func TestCorrectPassesThroughAIErrors(t *testing.T) {
	for _, want := range []error{ErrIncomplete, ErrUnavailable, context.Canceled} {
		if _, err := New(&fakeGenerator{err: want}).Correct(context.Background(), "text", "", ""); !errors.Is(err, want) {
			t.Errorf("want %v, got %v", want, err)
		}
	}
}

func TestCorrectRejectsAnswerThatCleansToNothing(t *testing.T) {
	if _, err := New(&fakeGenerator{reply: "```"}).Correct(context.Background(), "text", "", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
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
