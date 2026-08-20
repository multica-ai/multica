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

// retries builds a Config.MaxRetries value, failing loudly on a budget Retries
// rejects. Tests that assert an exact upstream request count pass retries(0) to
// take SDK retries out of the picture; before MUL-6364 that required the
// negative -1, because a plain 0 was indistinguishable from unset and quietly
// restored the default budget.
func retries(n int) *RetryOverride {
	override, err := Retries(n)
	if err != nil {
		panic(err)
	}
	return override
}

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

func TestGenerateJSONUsesGPT56CompatibleParameters(t *testing.T) {
	var gotBody map[string]any
	srv := stubUpstream(t, func(w http.ResponseWriter, body map[string]any) {
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"cmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"{\"actions\":[]}"},"finish_reason":"stop"}]}`)
	})

	c := New(Config{APIKey: "k", BaseURL: srv.URL, DefaultModel: "gpt-5.6-luna"})
	out, err := c.GenerateJSON(context.Background(), "", "Return JSON.", "Generate actions.", 0.3, 2048)
	if err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}
	if out != `{"actions":[]}` {
		t.Fatalf("unexpected output %q", out)
	}
	if gotBody["max_completion_tokens"] != float64(2048) {
		t.Fatalf("expected max_completion_tokens=2048, got %#v", gotBody["max_completion_tokens"])
	}
	if _, ok := gotBody["max_tokens"]; ok {
		t.Fatalf("deprecated max_tokens must be omitted, got body %#v", gotBody)
	}
	if gotBody["reasoning_effort"] != "none" {
		t.Fatalf("expected reasoning_effort=none, got body %#v", gotBody)
	}
	if _, ok := gotBody["temperature"]; ok {
		t.Fatalf("temperature must be omitted for reasoning-model compatibility, got body %#v", gotBody)
	}
	if gotBody["model"] != "gpt-5.6-luna" {
		t.Fatalf("expected configured GPT-5.6 model, got body %#v", gotBody)
	}
}

func TestGenerateJSONFallsBackToLegacyMaxTokens(t *testing.T) {
	var bodies []map[string]any
	srv := stubUpstream(t, func(w http.ResponseWriter, body map[string]any) {
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Unsupported parameter: max_completion_tokens","type":"invalid_request_error","param":"max_completion_tokens","code":"unsupported_parameter"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"cmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"{\"actions\":[]}"},"finish_reason":"stop"}]}`)
	})

	c := New(Config{APIKey: "k", BaseURL: srv.URL, MaxRetries: retries(0)})
	if _, err := c.GenerateJSON(context.Background(), "legacy-model", "Return JSON.", "Generate actions.", 0.3, 800); err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected one compatibility retry, got %d requests", len(bodies))
	}
	if bodies[0]["max_completion_tokens"] != float64(800) {
		t.Fatalf("first request must use max_completion_tokens, got %#v", bodies[0])
	}
	if _, ok := bodies[0]["max_tokens"]; ok {
		t.Fatalf("first request must omit max_tokens, got %#v", bodies[0])
	}
	if bodies[1]["max_tokens"] != float64(800) {
		t.Fatalf("fallback request must use max_tokens, got %#v", bodies[1])
	}
	if _, ok := bodies[1]["max_completion_tokens"]; ok {
		t.Fatalf("fallback request must omit max_completion_tokens, got %#v", bodies[1])
	}
	if bodies[1]["temperature"] != 0.3 {
		t.Fatalf("non-GPT-5.6 models must preserve temperature, got %#v", bodies[1])
	}
}

func TestGenerateJSONFallsBackFromUnsupportedReasoningEffort(t *testing.T) {
	var bodies []map[string]any
	srv := stubUpstream(t, func(w http.ResponseWriter, body map[string]any) {
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Unsupported parameter: reasoning_effort","type":"invalid_request_error","param":"reasoning_effort","code":"unsupported_parameter"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"cmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"{\"actions\":[]}"},"finish_reason":"stop"}]}`)
	})

	c := New(Config{APIKey: "k", BaseURL: srv.URL, MaxRetries: retries(0)})
	if _, err := c.GenerateJSON(context.Background(), "gpt-5.6-luna", "Return JSON.", "Generate actions.", 0.3, 800); err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected one compatibility retry, got %d requests", len(bodies))
	}
	if bodies[0]["reasoning_effort"] != "none" {
		t.Fatalf("first request must disable reasoning, got %#v", bodies[0])
	}
	if _, ok := bodies[1]["reasoning_effort"]; ok {
		t.Fatalf("fallback request must omit reasoning_effort, got %#v", bodies[1])
	}
	if bodies[1]["max_completion_tokens"] != float64(800) {
		t.Fatalf("fallback must preserve the preferred token field, got %#v", bodies[1])
	}
}

func TestGenerateJSONNegotiatesBothLegacyParameters(t *testing.T) {
	var bodies []map[string]any
	srv := stubUpstream(t, func(w http.ResponseWriter, body map[string]any) {
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		switch len(bodies) {
		case 1:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Unsupported parameter: max_completion_tokens","type":"invalid_request_error","param":"max_completion_tokens","code":"unsupported_parameter"}}`)
		case 2:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Unsupported parameter: reasoning_effort","type":"invalid_request_error","param":"reasoning_effort","code":"unsupported_parameter"}}`)
		default:
			_, _ = io.WriteString(w, `{"id":"cmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"{\"actions\":[]}"},"finish_reason":"stop"}]}`)
		}
	})

	c := New(Config{APIKey: "k", BaseURL: srv.URL, MaxRetries: retries(0)})
	if _, err := c.GenerateJSON(context.Background(), "gpt-5.6-luna", "Return JSON.", "Generate actions.", 0.3, 800); err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}
	if len(bodies) != 3 {
		t.Fatalf("expected two bounded compatibility retries, got %d requests", len(bodies))
	}
	last := bodies[2]
	if last["max_tokens"] != float64(800) {
		t.Fatalf("final request must use max_tokens, got %#v", last)
	}
	if _, ok := last["max_completion_tokens"]; ok {
		t.Fatalf("final request must omit max_completion_tokens, got %#v", last)
	}
	if _, ok := last["reasoning_effort"]; ok {
		t.Fatalf("final request must omit reasoning_effort, got %#v", last)
	}
}

func TestGenerateJSONDoesNotRetryInvalidTokenLimit(t *testing.T) {
	requests := 0
	srv := stubUpstream(t, func(w http.ResponseWriter, _ map[string]any) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"Invalid max_completion_tokens value","type":"invalid_request_error","param":"max_completion_tokens","code":"invalid_value"}}`)
	})

	c := New(Config{APIKey: "k", BaseURL: srv.URL, MaxRetries: retries(0)})
	if _, err := c.GenerateJSON(context.Background(), "gpt-5.6-luna", "Return JSON.", "Generate actions.", 0.3, 800); err == nil {
		t.Fatal("expected invalid token limit error")
	}
	if requests != 1 {
		t.Fatalf("invalid values must not trigger a compatibility retry, got %d requests", requests)
	}
}

func TestGenerateJSONRejectsIncompleteOrEmptyOutput(t *testing.T) {
	for _, tc := range []struct {
		name         string
		content      string
		finishReason string
		wantError    string
	}{
		{name: "token limit", content: "", finishReason: "length", wantError: "max completion token limit"},
		{name: "empty content", content: " ", finishReason: "stop", wantError: "empty JSON content"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := stubUpstream(t, func(w http.ResponseWriter, _ map[string]any) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"cmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":`+mustJSON(t, tc.content)+`},"finish_reason":`+mustJSON(t, tc.finishReason)+`}]}`)
			})

			c := New(Config{APIKey: "k", BaseURL: srv.URL})
			_, err := c.GenerateJSON(context.Background(), "gpt-5.6-luna", "Return JSON.", "Generate actions.", 0.3, 800)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want substring %q", err, tc.wantError)
			}
		})
	}
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal test value: %v", err)
	}
	return string(raw)
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
