package llm

// RED contract for MULTICA_LLM_DISABLE_THINKING (PUCK-172, upstream #8178).
//
// Written BEFORE any production change, against the pinned SDK
// (github.com/openai/openai-go/v3 v3.41.1), using the existing stubUpstream
// HTTPClient seam. Expected state on first run:
//   - *Off tests PASS (byte-identical behaviour without the flag).
//   - *On  tests FAIL  (no thinking-disable override exists yet).
//
// The *_On failures are the RED proof; the fix must be the SetExtraFields
// request-policy helper at the Chat/ChatStream boundary (NOT WithJSONSet,
// NOT an SDK bump) until these tests say otherwise.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	openai "github.com/openai/openai-go/v3"
)

// sseCaptureUpstream is stubUpstream's streaming sibling: it records the
// decoded POST body that opens the stream, then serves a minimal SSE payload
// so ChatStream.Next()/Err() terminate.
func sseCaptureUpstream(t *testing.T, captured *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		*captured = body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func chatCompletionOK() string {
	return `{"id":"cmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"{\"actions\":[]}"},"finish_reason":"stop"}]}`
}

// collectBodies runs every exported outbound path once against stub upstreams
// and returns the decoded request body each path sent.
func collectBodies(t *testing.T, c *Client) map[string]map[string]any {
	t.Helper()
	bodies := map[string]map[string]any{}
	ctx := context.Background()

	chatSrv := stubUpstream(t, func(w http.ResponseWriter, body map[string]any) {
		bodies["Chat"] = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatCompletionOK())
	})
	chatClient := New(Config{APIKey: "k", BaseURL: chatSrv.URL, MaxRetries: retries(0), DisableThinking: c.DisableThinking()})
	if _, err := chatClient.Chat(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	}); err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	textSrv := stubUpstream(t, func(w http.ResponseWriter, body map[string]any) {
		bodies["GenerateText"] = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatCompletionOK())
	})
	textClient := New(Config{APIKey: "k", BaseURL: textSrv.URL, MaxRetries: retries(0), DisableThinking: c.DisableThinking()})
	if _, err := textClient.GenerateText(ctx, "", "Return JSON.", "hi"); err != nil {
		t.Fatalf("GenerateText failed: %v", err)
	}

	jsonSrv := stubUpstream(t, func(w http.ResponseWriter, body map[string]any) {
		bodies["GenerateJSON"] = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatCompletionOK())
	})
	jsonClient := New(Config{APIKey: "k", BaseURL: jsonSrv.URL, MaxRetries: retries(0), DisableThinking: c.DisableThinking()})
	if _, err := jsonClient.GenerateJSON(ctx, "plain-model", "Return JSON.", "Generate actions.", 0, 0); err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}

	var streamBody map[string]any
	streamSrv := sseCaptureUpstream(t, &streamBody)
	streamClient := New(Config{APIKey: "k", BaseURL: streamSrv.URL, MaxRetries: retries(0), DisableThinking: c.DisableThinking()})
	stream, err := streamClient.ChatStream(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}
	stream.Close()
	bodies["ChatStream"] = streamBody

	return bodies
}

func thinkingKwargs(body map[string]any) (map[string]any, bool) {
	raw, ok := body["chat_template_kwargs"]
	if !ok {
		return nil, false
	}
	m, ok := raw.(map[string]any)
	return m, ok
}

func TestDisableThinkingOffSendsNoExtraField(t *testing.T) {
	c := New(Config{APIKey: "k", MaxRetries: retries(0)})
	if c.DisableThinking() {
		t.Fatal("unset DisableThinking must report false")
	}
	for name, body := range collectBodies(t, c) {
		if _, ok := body["chat_template_kwargs"]; ok {
			t.Errorf("%s with DisableThinking=false sent chat_template_kwargs=%v, want it absent", name, body["chat_template_kwargs"])
		}
	}
}

func TestDisableThinkingOnAddsEnableThinkingFalse(t *testing.T) {
	c := New(Config{APIKey: "k", MaxRetries: retries(0), DisableThinking: true})
	if !c.DisableThinking() {
		t.Fatal("DisableThinking=true must be reported effective")
	}
	for name, body := range collectBodies(t, c) {
		kwargs, ok := thinkingKwargs(body)
		if !ok {
			t.Errorf("%s with DisableThinking=true sent no chat_template_kwargs object, body=%v", name, body)
			continue
		}
		if v, ok := kwargs["enable_thinking"]; !ok || v != false {
			t.Errorf("%s with DisableThinking=true: chat_template_kwargs=%v, want {\"enable_thinking\": false}", name, kwargs)
		}
	}
}

func TestDisableThinkingPreservesExistingExtras(t *testing.T) {
	var gotBody map[string]any
	srv := stubUpstream(t, func(w http.ResponseWriter, body map[string]any) {
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatCompletionOK())
	})
	c := New(Config{APIKey: "k", BaseURL: srv.URL, MaxRetries: retries(0), DisableThinking: true})

	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	}
	params.SetExtraFields(map[string]any{
		"some_gateway_option": 123,
		"chat_template_kwargs": map[string]any{
			"foo":             "bar",
			"enable_thinking": true,
		},
	})
	if _, err := c.Chat(context.Background(), params); err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if gotBody["some_gateway_option"] != float64(123) {
		t.Fatalf("unrelated top-level extra was lost, body=%v", gotBody)
	}
	kwargs, ok := thinkingKwargs(gotBody)
	if !ok {
		t.Fatalf("chat_template_kwargs object was lost, body=%v", gotBody)
	}
	if kwargs["foo"] != "bar" {
		t.Errorf("existing chat_template_kwargs sibling was lost, kwargs=%v", kwargs)
	}
	if v, ok := kwargs["enable_thinking"]; !ok || v != false {
		t.Errorf("enable_thinking was not overridden to false, kwargs=%v", kwargs)
	}
}

func TestDisableThinkingCoexistsWithGPT56ReasoningEffort(t *testing.T) {
	var gotBody map[string]any
	srv := stubUpstream(t, func(w http.ResponseWriter, body map[string]any) {
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatCompletionOK())
	})
	c := New(Config{APIKey: "k", BaseURL: srv.URL, MaxRetries: retries(0), DisableThinking: true})
	if _, err := c.GenerateJSON(context.Background(), "gpt-5.6-luna", "Return JSON.", "Generate actions.", 0.3, 800); err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}
	if gotBody["reasoning_effort"] != "none" {
		t.Errorf("GPT-5.6 reasoning_effort=none was disturbed by DisableThinking, body=%v", gotBody)
	}
	kwargs, ok := thinkingKwargs(gotBody)
	if !ok || kwargs["enable_thinking"] != false {
		t.Errorf("DisableThinking=true must still add enable_thinking=false, body=%v", gotBody)
	}
}

func TestDisableThinkingDoesNotEnableClient(t *testing.T) {
	transport := &countingHTTPClient{}
	c := New(Config{HTTPClient: transport, DisableThinking: true})
	if c.Enabled() {
		t.Fatal("DisableThinking alone must not enable the client")
	}
	ctx := context.Background()
	if _, err := c.Chat(ctx, openai.ChatCompletionNewParams{}); err == nil {
		t.Fatal("expected an error from a disabled client")
	}
	if n := transport.requests.Load(); n != 0 {
		t.Fatalf("disabled client with DisableThinking=true made %d upstream request(s)", n)
	}
}

func TestDisableThinkingRejectedFieldIsNotRetried(t *testing.T) {
	requests := 0
	srv := stubUpstream(t, func(w http.ResponseWriter, _ map[string]any) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"Unsupported parameter: chat_template_kwargs","type":"invalid_request_error","param":"chat_template_kwargs","code":"unsupported_parameter"}}`)
	})
	c := New(Config{APIKey: "k", BaseURL: srv.URL, MaxRetries: retries(0), DisableThinking: true})
	if _, err := c.GenerateJSON(context.Background(), "plain-model", "Return JSON.", "Generate actions.", 0, 0); err == nil {
		t.Fatal("expected the 400 to surface, not a silent thinking-enabled retry")
	}
	if requests != 1 {
		t.Fatalf("unsupported chat_template_kwargs must fail after exactly 1 request, got %d", requests)
	}
}
