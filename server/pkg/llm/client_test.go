package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// stubUpstream returns an httptest server that mimics the OpenAI
// chat-completions endpoint. handler receives the decoded request body.
func stubUpstream(t *testing.T, handler func(w http.ResponseWriter, body map[string]any)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		handler(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNewDisabledClient(t *testing.T) {
	c := New(Config{})
	if c.Enabled() {
		t.Fatal("expected disabled client with empty config")
	}
	if c.DefaultModel() != FallbackModel {
		t.Fatalf("expected fallback model %q, got %q", FallbackModel, c.DefaultModel())
	}
	if _, err := c.Chat(context.Background(), openai.ChatCompletionNewParams{}); err != ErrNotConfigured {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
	if _, err := c.GenerateText(context.Background(), "", "", "hi"); err != ErrNotConfigured {
		t.Fatalf("expected ErrNotConfigured from GenerateText, got %v", err)
	}
}

func TestEnabledWithBaseURLOnly(t *testing.T) {
	c := New(Config{BaseURL: "http://localhost:1234"})
	if !c.Enabled() {
		t.Fatal("expected enabled client when only base URL is set (keyless gateway)")
	}
}

func TestConfiguredDefaultModel(t *testing.T) {
	c := New(Config{APIKey: "k", DefaultModel: "my-model"})
	if c.DefaultModel() != "my-model" {
		t.Fatalf("expected configured default model, got %q", c.DefaultModel())
	}
}

func TestChatAppliesDefaultModel(t *testing.T) {
	var gotModel string
	srv := stubUpstream(t, func(w http.ResponseWriter, body map[string]any) {
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"cmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`)
	})

	c := New(Config{APIKey: "test-key", BaseURL: srv.URL, DefaultModel: "default-x"})
	// Request omits the model -> the configured default must be applied.
	completion, err := c.Chat(context.Background(), openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if gotModel != "default-x" {
		t.Fatalf("expected default model forwarded upstream, got %q", gotModel)
	}
	if len(completion.Choices) != 1 || completion.Choices[0].Message.Content != "hello" {
		t.Fatalf("unexpected completion: %+v", completion.Choices)
	}
	if completion.RawJSON() == "" {
		t.Fatal("expected non-empty RawJSON for passthrough")
	}
}

func TestChatRespectsRequestModel(t *testing.T) {
	var gotModel string
	srv := stubUpstream(t, func(w http.ResponseWriter, body map[string]any) {
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"cmpl-1","object":"chat.completion","choices":[]}`)
	})

	c := New(Config{APIKey: "test-key", BaseURL: srv.URL, DefaultModel: "default-x"})
	_, err := c.Chat(context.Background(), openai.ChatCompletionNewParams{
		Model:    shared.ChatModel("caller-model"),
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if gotModel != "caller-model" {
		t.Fatalf("expected caller model preserved, got %q", gotModel)
	}
}

func TestGenerateText(t *testing.T) {
	var sawSystem bool
	srv := stubUpstream(t, func(w http.ResponseWriter, body map[string]any) {
		if msgs, ok := body["messages"].([]any); ok {
			for _, m := range msgs {
				if mm, ok := m.(map[string]any); ok && mm["role"] == "system" {
					sawSystem = true
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"cmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"a title"},"finish_reason":"stop"}]}`)
	})

	c := New(Config{APIKey: "k", BaseURL: srv.URL})
	out, err := c.GenerateText(context.Background(), "", "you are helpful", "make a title")
	if err != nil {
		t.Fatalf("GenerateText failed: %v", err)
	}
	if out != "a title" {
		t.Fatalf("expected %q, got %q", "a title", out)
	}
	if !sawSystem {
		t.Fatal("expected system message to be sent")
	}
}

func TestChatStream(t *testing.T) {
	srv := stubUpstream(t, func(w http.ResponseWriter, _ map[string]any) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hel"}}]}`,
			`{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		}
		for _, ch := range chunks {
			_, _ = io.WriteString(w, "data: "+ch+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})

	c := New(Config{APIKey: "k", BaseURL: srv.URL})
	stream, err := c.ChatStream(context.Background(), openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}
	defer stream.Close()

	var content strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) > 0 {
			content.WriteString(chunk.Choices[0].Delta.Content)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if content.String() != "Hello" {
		t.Fatalf("expected assembled content %q, got %q", "Hello", content.String())
	}
}
