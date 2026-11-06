// Package llm is a small Gemini client with retries and model fallback. The API
// key stays on the server. Callers build prompts and interpret the output.
package llm

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
)

const (
	defaultBaseURL  = "https://generativelanguage.googleapis.com/v1beta"
	totalTimeout    = 30 * time.Second
	attemptTimeout  = 12 * time.Second
	maxAttempts     = 3
	retryDelay      = 500 * time.Millisecond
	maxResponseSize = 1 << 20
)

var (
	ErrNotConfigured = errors.New("llm: not configured")
	ErrUnavailable   = errors.New("llm: AI service unavailable")
	// ErrIncomplete means the model stopped before finishing its answer.
	ErrIncomplete = errors.New("llm: incomplete answer")
)

type Config struct {
	APIKey    string
	Model     string
	Fallbacks []string // Tried on retries, for example when the primary model is overloaded.
	BaseURL   string   // Overridable so tests can use a local server.
}

// Request is one single-turn generation. JSONSchema, when set, forces JSON output.
type Request struct {
	System     string
	Prompt     string
	JSONSchema map[string]any
	MaxTokens  int
}

type Client struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	return &Client{cfg: cfg, http: &http.Client{}}
}

func (c *Client) Configured() bool { return c.cfg.APIKey != "" }

// Generate returns the model's text. Permanent failures (bad key, bad request)
// stop immediately; others are retried, switching to fallback models.
func (c *Client) Generate(ctx context.Context, req Request) (string, error) {
	if !c.Configured() {
		return "", ErrNotConfigured
	}
	callCtx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptCtx, cancelAttempt := context.WithTimeout(callCtx, attemptTimeout)
		text, err := c.generate(attemptCtx, c.modelFor(attempt), req)
		cancelAttempt()
		if err == nil {
			return text, nil
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
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%w: %v", ErrUnavailable, lastErr)
}

func (c *Client) modelFor(attempt int) string {
	if attempt <= 1 || len(c.cfg.Fallbacks) == 0 {
		return c.cfg.Model
	}
	return c.cfg.Fallbacks[(attempt-2)%len(c.cfg.Fallbacks)]
}

type part struct {
	Text string `json:"text"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type generationConfig struct {
	MaxOutputTokens  int            `json:"maxOutputTokens"`
	ThinkingConfig   map[string]any `json:"thinkingConfig,omitempty"`
	ResponseMIMEType string         `json:"responseMimeType,omitempty"`
	ResponseSchema   map[string]any `json:"responseSchema,omitempty"`
}

type generateRequest struct {
	SystemInstruction content          `json:"system_instruction"`
	Contents          []content        `json:"contents"`
	GenerationConfig  generationConfig `json:"generationConfig"`
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

type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return fmt.Sprintf("gemini HTTP %d: %s", e.Status, e.Message) }

// isPermanent reports failures another attempt cannot fix. A 404 is retried
// because a fallback model may exist.
func isPermanent(err error) bool {
	var api *apiError
	if !errors.As(err, &api) {
		return false
	}
	return api.Status == http.StatusBadRequest || api.Status == http.StatusUnauthorized || api.Status == http.StatusForbidden
}

func (c *Client) generate(ctx context.Context, model string, req Request) (string, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	cfg := generationConfig{MaxOutputTokens: maxTokens, ThinkingConfig: map[string]any{"thinkingLevel": "low"}}
	if req.JSONSchema != nil {
		cfg.ResponseMIMEType = "application/json"
		cfg.ResponseSchema = req.JSONSchema
	}
	payload, err := json.Marshal(generateRequest{
		SystemInstruction: content{Parts: []part{{Text: req.System}}},
		Contents:          []content{{Role: "user", Parts: []part{{Text: req.Prompt}}}},
		GenerationConfig:  cfg,
	})
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.cfg.BaseURL, "/")+"/models/"+url.PathEscape(model)+":generateContent", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.cfg.APIKey)
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("gemini request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return "", fmt.Errorf("read gemini response: %w", err)
	}
	return parseResponse(resp.StatusCode, raw)
}

func parseResponse(status int, raw []byte) (string, error) {
	var out generateResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", &apiError{Status: status, Message: "response was not valid JSON"}
	}
	if out.Error != nil {
		message := out.Error.Message
		if r := []rune(message); len(r) > 200 {
			message = string(r[:200])
		}
		return "", &apiError{Status: out.Error.Code, Message: message}
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
	var sb strings.Builder
	for _, p := range candidate.Content.Parts {
		sb.WriteString(p.Text)
	}
	text := strings.TrimSpace(sb.String())
	if text == "" {
		return "", errors.New("gemini returned empty text")
	}
	return text, nil
}
