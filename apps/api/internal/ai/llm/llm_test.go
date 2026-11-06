package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGenerateSendsSchemaAndReturnsText(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		if r.Header.Get("x-goog-api-key") != "key" || !strings.HasSuffix(r.URL.Path, "/models/main:generateContent") {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"{\"a\":1}"}]}}]}`))
	}))
	defer server.Close()
	c := New(Config{APIKey: "key", Model: "main", BaseURL: server.URL})
	got, err := c.Generate(context.Background(), Request{System: "s", Prompt: "p", JSONSchema: map[string]any{"type": "OBJECT"}})
	if err != nil || got != `{"a":1}` {
		t.Fatalf("%q %v", got, err)
	}
	if !strings.Contains(body, `"responseMimeType":"application/json"`) || !strings.Contains(body, `"responseSchema"`) {
		t.Fatalf("schema not sent: %s", body)
	}
}

func TestGenerateWithoutKey(t *testing.T) {
	if _, err := New(Config{Model: "m"}).Generate(context.Background(), Request{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("got %v", err)
	}
}

func TestPermanentErrorsAreNotRetriedButOthersFallBack(t *testing.T) {
	var calls atomic.Int32
	var models []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		models = append(models, r.URL.Path)
		if strings.Contains(r.URL.Path, "/models/main:") {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":503,"message":"overloaded"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"ok"}]}}]}`))
	}))
	defer server.Close()
	c := New(Config{APIKey: "key", Model: "main", Fallbacks: []string{"backup"}, BaseURL: server.URL})
	if got, err := c.Generate(context.Background(), Request{Prompt: "p"}); err != nil || got != "ok" || calls.Load() != 2 {
		t.Fatalf("%q %v calls=%d %v", got, err, calls.Load(), models)
	}

	calls.Store(0)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"bad key"}}`))
	}))
	defer bad.Close()
	_, err := New(Config{APIKey: "key", Model: "main", BaseURL: bad.URL}).Generate(context.Background(), Request{Prompt: "p"})
	if !errors.Is(err, ErrUnavailable) || calls.Load() != 1 {
		t.Fatalf("%v calls=%d", err, calls.Load())
	}
}

func TestModelForUsesFallbacksOnRetries(t *testing.T) {
	c := New(Config{Model: "main", Fallbacks: []string{"one", "two"}})
	for attempt, want := range map[int]string{1: "main", 2: "one", 3: "two"} {
		if got := c.modelFor(attempt); got != want {
			t.Errorf("attempt %d: %s", attempt, got)
		}
	}
	if got := New(Config{Model: "main"}).modelFor(3); got != "main" {
		t.Errorf("no fallbacks: %s", got)
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
		_, err := parseResponse(tc.status, []byte(tc.body))
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if isPermanent(err) != tc.permanent {
			t.Errorf("%s: permanent = %v", name, isPermanent(err))
		}
	}
}

func TestParseResponseJoinsParts(t *testing.T) {
	got, err := parseResponse(http.StatusOK, []byte(`{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"I "},{"text":"went."}]}}]}`))
	if err != nil || got != "I went." {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestTruncatedAnswerIsIncomplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"MAX_TOKENS","content":{"parts":[{"text":"half"}]}}]}`))
	}))
	defer server.Close()
	_, err := New(Config{APIKey: "key", Model: "m", BaseURL: server.URL}).Generate(context.Background(), Request{Prompt: "p"})
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("got %v", err)
	}
}
