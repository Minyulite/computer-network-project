// Package grammar corrects a selected piece of draft text with an LLM. The API
// key stays on the server; browsers only reach this through the authenticated
// chat WebSocket.
package grammar

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/computer-network/project/apps/api/internal/ai/llm"
)

const (
	// MaxTextRunes matches the chat message limit.
	MaxTextRunes = 2000
	// MaxContextRunes caps the read-only text kept on each side of the selection.
	MaxContextRunes = 500

	maxOutputTokens = 4096
	totalTimeout    = 45 * time.Second
)

var (
	ErrInvalidInput  = errors.New("grammar: invalid input")
	ErrNotConfigured = llm.ErrNotConfigured
	ErrUnavailable   = llm.ErrUnavailable
	// ErrIncomplete means the model stopped early. Offering partial text would
	// delete the rest of the user's selection when they accept it.
	ErrIncomplete = llm.ErrIncomplete
)

const systemPrompt = `You are a grammar corrector inside a chat application.
The user's message may contain up to three tagged parts:
<before>text that comes before the selection</before>
<selected>the part to correct</selected>
<after>text that comes after the selection</after>
Fix grammar, spelling, capitalization and punctuation in the SELECTED part only.
Use <before> and <after> purely as context to understand meaning, tense, agreement
and how the selection joins its neighbours. Never correct, repeat or return them.
Rules:
- Keep the original meaning, tone and language. Never translate.
- Do not add new information, greetings or explanations.
- If the selected part is already correct, return it unchanged.
- Return ONLY the corrected version of the selected part, with no tags, quotes or markdown.`

// Generator is the part of llm.Client used here; tests substitute a fake.
type Generator interface {
	Generate(ctx context.Context, req llm.Request) (string, error)
}

type Service struct{ llm Generator }

// New returns a service. With an unconfigured client, Correct reports ErrNotConfigured.
func New(generator Generator) *Service { return &Service{llm: generator} }

// Correct returns the exact replacement for text. Its outer whitespace is kept
// because the browser substitutes the result into the user's draft.
func (s *Service) Correct(ctx context.Context, text, before, after string) (string, error) {
	leading, core, trailing := splitEdges(text)
	if core == "" || utf8.RuneCountInString(text) > MaxTextRunes {
		return "", ErrInvalidInput
	}
	prompt := buildPrompt(core, lastRunes(before+leading, MaxContextRunes), firstRunes(trailing+after, MaxContextRunes))
	raw, err := s.llm.Generate(ctx, llm.Request{System: systemPrompt, Prompt: prompt,
		MaxTokens: maxOutputTokens, Timeout: totalTimeout})
	if err != nil {
		return "", err
	}
	corrected := cleanOutput(raw, core)
	if corrected == "" {
		return "", ErrUnavailable
	}
	return leading + corrected + trailing, nil
}

// buildPrompt tags the selection and its context separately so the model
// corrects only the selection but can read its neighbours.
func buildPrompt(text, before, after string) string {
	if before == "" && after == "" {
		return "<selected>" + text + "</selected>"
	}
	return "<before>" + before + "</before>\n<selected>" + text + "</selected>\n<after>" + after + "</after>"
}

// cleanOutput strips wrappers the model sometimes adds despite its instructions.
// Surrounding quotes are kept when the user's own selection had them.
func cleanOutput(s, original string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(strings.TrimPrefix(s, "<selected>"), "</selected>")
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```text")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if isQuoted(s) && !isQuoted(original) {
		s = s[1 : len(s)-1]
	}
	return strings.TrimSpace(s)
}

func isQuoted(s string) bool { return len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' }

func splitEdges(s string) (leading, core, trailing string) {
	core = strings.TrimSpace(s)
	if core == "" {
		return "", "", ""
	}
	start := strings.Index(s, core)
	return s[:start], core, s[start+len(core):]
}

func firstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
