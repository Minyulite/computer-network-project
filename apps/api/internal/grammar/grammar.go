// Package grammar corrects a selected piece of draft text with the Gemini API.
// The API key stays on the server; browsers only reach this through the
// authenticated chat WebSocket.
package grammar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxTextRunes matches the chat message limit.
	MaxTextRunes = 2000
	// MaxContextRunes caps the read-only text kept on each side of the selection.
	MaxContextRunes = 500

	baseURL         = "https://generativelanguage.googleapis.com/v1beta"
	maxOutputTokens = 4096
	totalTimeout    = 45 * time.Second
	attemptTimeout  = 12 * time.Second // Flaky networks often stall the TLS handshake.
	maxAttempts     = 3
	retryDelay      = 500 * time.Millisecond
	maxResponseSize = 1 << 20
)

var (
	ErrInvalidInput  = errors.New("grammar: invalid input")
	ErrNotConfigured = errors.New("grammar: not configured")
	ErrUnavailable   = errors.New("grammar: AI service unavailable")
	// ErrIncomplete means the model stopped early. Offering partial text would
	// delete the rest of the user's selection when they accept it.
	ErrIncomplete = errors.New("grammar: incomplete correction")
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

type Config struct {
	APIKey    string
	Model     string
	Fallbacks []string // Tried on retries, for example when the primary model is overloaded.
}

type Service struct {
	cfg  Config
	http *http.Client
}

// New returns a service. Without an API key, Correct reports ErrNotConfigured.
func New(cfg Config) *Service {
	// Each request carries its own deadline, so the client needs no global timeout.
	return &Service{cfg: cfg, http: &http.Client{}}
}

func (s *Service) Configured() bool { return s.cfg.APIKey != "" }

// Correct returns the exact replacement for text. Its outer whitespace is kept
// because the browser substitutes the result into the user's draft.
func (s *Service) Correct(ctx context.Context, text, before, after string) (string, error) {
	leading, core, trailing := splitEdges(text)
	if core == "" || utf8.RuneCountInString(text) > MaxTextRunes {
		return "", ErrInvalidInput
	}
	if !s.Configured() {
		return "", ErrNotConfigured
	}
	prompt := buildPrompt(core, lastRunes(before+leading, MaxContextRunes), firstRunes(trailing+after, MaxContextRunes))

	callCtx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptCtx, cancelAttempt := context.WithTimeout(callCtx, attemptTimeout)
		corrected, err := s.generate(attemptCtx, s.modelFor(attempt), prompt, core)
		cancelAttempt()
		if err == nil {
			return leading + corrected + trailing, nil
		}
		if errors.Is(err, ErrIncomplete) {
			return "", err
		}
		lastErr = err
		if isPermanent(err) {
			break
		}
		select {
		case <-callCtx.Done():
		case <-time.After(retryDelay):
		}
		if callCtx.Err() != nil {
			break
		}
	}
	// The caller left, so there is nobody to report an outage to.
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%w: %v", ErrUnavailable, lastErr)
}

// modelFor returns the model for a 1-based attempt: primary first, then fallbacks.
func (s *Service) modelFor(attempt int) string {
	if attempt <= 1 || len(s.cfg.Fallbacks) == 0 {
		return s.cfg.Model
	}
	return s.cfg.Fallbacks[(attempt-2)%len(s.cfg.Fallbacks)]
}

type part struct {
	Text string `json:"text"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type generateRequest struct {
	SystemInstruction content   `json:"system_instruction"`
	Contents          []content `json:"contents"`
	GenerationConfig  genConfig `json:"generationConfig"`
}

type genConfig struct {
	MaxOutputTokens int            `json:"maxOutputTokens"`
	ThinkingConfig  map[string]any `json:"thinkingConfig,omitempty"`
}

type generateResponse struct {
	Candidates []struct {
		Content      content `json:"content"`
		FinishReason string  `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// apiError is an error response from Gemini.
type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return fmt.Sprintf("gemini HTTP %d: %s", e.Status, e.Message) }

// isPermanent reports failures that another attempt or model cannot fix, such
// as a rejected API key. A 404 is retried because a fallback model may exist.
func isPermanent(err error) bool {
	var api *apiError
	if !errors.As(err, &api) {
		return false
	}
	return api.Status == http.StatusBadRequest || api.Status == http.StatusUnauthorized || api.Status == http.StatusForbidden
}

func (s *Service) generate(ctx context.Context, model, prompt, original string) (string, error) {
	payload, err := json.Marshal(generateRequest{
		SystemInstruction: content{Parts: []part{{Text: systemPrompt}}},
		Contents:          []content{{Role: "user", Parts: []part{{Text: prompt}}}},
		GenerationConfig: genConfig{
			MaxOutputTokens: maxOutputTokens,
			// Grammar fixing is an easy task; keep thinking low for speed.
			ThinkingConfig: map[string]any{"thinkingLevel": "low"},
		},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/models/"+url.PathEscape(model)+":generateContent", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", s.cfg.APIKey)

	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return "", fmt.Errorf("read gemini response: %w", err)
	}
	return parseResponse(resp.StatusCode, raw, original)
}

func parseResponse(status int, raw []byte, original string) (string, error) {
	var out generateResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", &apiError{Status: status, Message: "response was not valid JSON"}
	}
	if out.Error != nil {
		return "", &apiError{Status: out.Error.Code, Message: firstRunes(out.Error.Message, 200)}
	}
	if status != http.StatusOK {
		return "", &apiError{Status: status, Message: "unexpected status"}
	}
	if len(out.Candidates) == 0 {
		return "", errors.New("gemini returned no candidates")
	}
	candidate := out.Candidates[0]
	switch candidate.FinishReason {
	case "", "STOP":
	case "MAX_TOKENS":
		return "", ErrIncomplete
	default:
		return "", fmt.Errorf("gemini stopped early: %s", candidate.FinishReason)
	}
	// Thinking models may split the answer into several parts.
	var sb strings.Builder
	for _, p := range candidate.Content.Parts {
		sb.WriteString(p.Text)
	}
	result := cleanOutput(sb.String(), original)
	if result == "" {
		return "", errors.New("gemini returned empty text")
	}
	return result, nil
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
