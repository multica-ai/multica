package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseRetryAfterSupportsDeltaAndHTTPDate(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	if got, ok := parseRetryAfter("3", now); !ok || got != 3*time.Second {
		t.Fatalf("delta retry-after = %v, %v", got, ok)
	}
	if got, ok := parseRetryAfter(now.Add(4*time.Second).Format(http.TimeFormat), now); !ok || got != 4*time.Second {
		t.Fatalf("date retry-after = %v, %v", got, ok)
	}
	if got, ok := parseRetryAfter(now.Add(-time.Second).Format(http.TimeFormat), now); !ok || got != 0 {
		t.Fatalf("past retry-after = %v, %v", got, ok)
	}
	if _, ok := parseRetryAfter("later", now); ok {
		t.Fatal("invalid retry-after accepted")
	}
}

func TestCompatibleClientBlocksCrossOriginRedirect(t *testing.T) {
	var targetRequests int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests++
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("authorization leaked across redirect: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`))
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/chat/completions", http.StatusFound)
	}))
	defer source.Close()

	client := NewCompatibleClient(CompatibleConfig{BaseURL: source.URL, APIKey: "secret", Protocol: ProtocolOpenAI})
	_, err := client.GenerateJSON(context.Background(), "model", "system", "user", 10)
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusFound {
		t.Fatalf("redirect error = %v, want HTTP 302", err)
	}
	if targetRequests != 0 {
		t.Fatalf("cross-origin target received %d request(s)", targetRequests)
	}
}

func TestCompatibleClientRejectsZeroEmbedding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0,0]}]}`))
	}))
	defer server.Close()

	client := NewCompatibleClient(CompatibleConfig{BaseURL: server.URL, Protocol: ProtocolOpenAI})
	_, err := client.Embeddings(context.Background(), "embedding-model", []string{"text"})
	if err == nil || !strings.Contains(err.Error(), "zero vector") {
		t.Fatalf("zero embedding error = %v", err)
	}
}

func TestCompatibleClientSendsVisionDataURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("request path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("authorization = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		messages, ok := payload["messages"].([]any)
		if !ok || len(messages) != 2 {
			t.Fatalf("messages = %#v", payload["messages"])
		}
		user, ok := messages[1].(map[string]any)
		if !ok {
			t.Fatalf("user message = %#v", messages[1])
		}
		content, ok := user["content"].([]any)
		if !ok || len(content) != 2 {
			t.Fatalf("user content = %#v", user["content"])
		}
		imagePart, ok := content[1].(map[string]any)
		if !ok || imagePart["type"] != "image_url" {
			t.Fatalf("image part = %#v", content[1])
		}
		imageURL, ok := imagePart["image_url"].(map[string]any)
		if !ok || !strings.HasPrefix(imageURL["url"].(string), "data:image/png;base64,") {
			t.Fatalf("image URL = %#v", imagePart["image_url"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`))
	}))
	defer server.Close()

	client := NewCompatibleClient(CompatibleConfig{BaseURL: server.URL, APIKey: "secret", Protocol: ProtocolOpenAI})
	got, err := client.GenerateVisionJSON(context.Background(), "vision-model", "system", "inspect", []VisionImage{{MIMEType: "image/png", Data: []byte{0x89, 0x50, 0x4e, 0x47}}}, 64)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"ok":true}` {
		t.Fatalf("vision response = %q", got)
	}
}

func TestCompatibleClientKeepsCohereForRerankOnly(t *testing.T) {
	client := NewCompatibleClient(CompatibleConfig{BaseURL: "https://provider.example/v1", Protocol: ProtocolCohere})
	if _, err := client.GenerateJSON(context.Background(), "model", "system", "user", 64); err == nil || !strings.Contains(err.Error(), "openai_compatible") {
		t.Fatalf("Cohere chat result = %v", err)
	}
	if _, err := client.Embeddings(context.Background(), "model", []string{"text"}); err == nil || !strings.Contains(err.Error(), "openai_compatible") {
		t.Fatalf("Cohere embeddings result = %v", err)
	}
}
