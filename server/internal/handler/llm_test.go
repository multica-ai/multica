package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/llm"
)

// newLLMUpstream returns a stub OpenAI upstream and a Handler wired to it.
func newLLMUpstream(t *testing.T, upstream http.HandlerFunc) *Handler {
	t.Helper()
	srv := httptest.NewServer(upstream)
	t.Cleanup(srv.Close)
	return &Handler{
		LLM: llm.New(llm.Config{APIKey: "test-key", BaseURL: srv.URL, DefaultModel: "test-model"}),
	}
}

func postLLM(t *testing.T, h http.HandlerFunc, body, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", strings.NewReader(body))
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestLLMChatCompletions_Unauthenticated(t *testing.T) {
	h := &Handler{LLM: llm.New(llm.Config{APIKey: "k"})}
	rec := postLLM(t, h.LLMChatCompletions, `{"messages":[{"role":"user","content":"hi"}]}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestLLMChatCompletions_NotConfigured(t *testing.T) {
	h := &Handler{LLM: llm.New(llm.Config{})} // disabled
	rec := postLLM(t, h.LLMChatCompletions, `{"messages":[{"role":"user","content":"hi"}]}`, "user-1")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestLLMChatCompletions_BadBody(t *testing.T) {
	h := &Handler{LLM: llm.New(llm.Config{APIKey: "k"})}
	rec := postLLM(t, h.LLMChatCompletions, `not json`, "user-1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d", rec.Code)
	}
}

func TestLLMChatCompletions_MissingMessages(t *testing.T) {
	h := &Handler{LLM: llm.New(llm.Config{APIKey: "k"})}
	rec := postLLM(t, h.LLMChatCompletions, `{"model":"x"}`, "user-1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing messages, got %d", rec.Code)
	}
}

func TestLLMChatCompletions_Success(t *testing.T) {
	h := newLLMUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("expected upstream auth header, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"cmpl-42","object":"chat.completion","model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}]}`)
	})

	rec := postLLM(t, h.LLMChatCompletions, `{"messages":[{"role":"user","content":"ping"}]}`, "user-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"cmpl-42"`) || !strings.Contains(rec.Body.String(), `"pong"`) {
		t.Fatalf("expected upstream body relayed verbatim, got %s", rec.Body.String())
	}
}

func TestLLMChatCompletions_UpstreamErrorStatusPreserved(t *testing.T) {
	h := newLLMUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key","type":"invalid_request_error"}}`)
	})

	rec := postLLM(t, h.LLMChatCompletions, `{"messages":[{"role":"user","content":"ping"}]}`, "user-1")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected upstream 401 preserved, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "bad key") {
		t.Fatalf("expected upstream error body relayed, got %s", rec.Body.String())
	}
}

func TestLLMChatCompletionsStream_Success(t *testing.T) {
	h := newLLMUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, ch := range []string{
			`{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hel"}}]}`,
			`{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		} {
			_, _ = io.WriteString(w, "data: "+ch+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})

	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions/stream",
		strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("X-User-ID", "user-1")
	rec := httptest.NewRecorder()
	h.LLMChatCompletionsStream(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", ct)
	}
	out := rec.Body.String()
	if !strings.Contains(out, `"Hel"`) || !strings.Contains(out, `"lo"`) {
		t.Fatalf("expected relayed chunks, got %s", out)
	}
	if !strings.Contains(out, "data: [DONE]") {
		t.Fatalf("expected terminating [DONE] sentinel, got %s", out)
	}
}

func TestLLMChatCompletionsStream_NotConfigured(t *testing.T) {
	h := &Handler{LLM: llm.New(llm.Config{})}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions/stream",
		strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("X-User-ID", "user-1")
	rec := httptest.NewRecorder()
	h.LLMChatCompletionsStream(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}
