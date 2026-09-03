package llm

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	openai "github.com/openai/openai-go/v3"
)

// countingHTTPClient is an option.HTTPClient that records every request the SDK
// hands it and refuses to perform any of them. It exists to assert an absence:
// a test that only checks the returned error cannot tell "returned
// ErrNotConfigured without dialing" apart from "dialed, failed, and mapped the
// failure to ErrNotConfigured".
type countingHTTPClient struct {
	requests atomic.Int64
	urls     []string
}

func (c *countingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.requests.Add(1)
	c.urls = append(c.urls, req.URL.String())
	return nil, errors.New("upstream must not be contacted by a disabled client")
}

// TestUnconfiguredClientMakesZeroUpstreamRequests pins the contract a
// deployment relies on when it leaves MULTICA_LLM_API_KEY and
// MULTICA_LLM_BASE_URL empty: this layer sends nothing, to anyone. (Only this
// layer — agent runs reach a model by their own path, which no variable here
// governs. See the package doc.)
//
// Both consumers of this package send private chat content upstream — the
// first message of a chat session (auto-titling) and the tail of a conversation
// (follow-up questions). "Leave the LLM variables empty" is the documented
// answer for an operator whose policy forbids that (.env.example, the docs
// environment-variables pages, and GitHub issue #7162), so the behaviour has to
// be a tested guarantee rather than something that happens to be true today.
//
// Every exported call path is exercised, because each one is a place a future
// refactor could start building a request before consulting Enabled().
func TestUnconfiguredClientMakesZeroUpstreamRequests(t *testing.T) {
	// A deployment that set only the model — the shape most likely to be
	// mistaken for "configured" — must be just as inert as an empty config.
	for _, cfg := range []Config{{}, {DefaultModel: "gpt-5.6-luna"}} {
		transport := &countingHTTPClient{}
		cfg.HTTPClient = transport
		c := New(cfg)

		if c.Enabled() {
			t.Fatalf("client with no API key or base URL reports enabled: %+v", cfg)
		}

		ctx := context.Background()
		userMessage := openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("private chat content")},
		}
		calls := []struct {
			name string
			run  func() error
		}{
			{"Chat", func() error {
				_, err := c.Chat(ctx, userMessage)
				return err
			}},
			{"ChatStream", func() error {
				_, err := c.ChatStream(ctx, userMessage)
				return err
			}},
			{"GenerateText", func() error {
				_, err := c.GenerateText(ctx, "", "system", "private chat content")
				return err
			}},
			{"GenerateJSON", func() error {
				_, err := c.GenerateJSON(ctx, "", "system JSON", "private chat content", 0.3, 2048)
				return err
			}},
		}
		for _, call := range calls {
			if err := call.run(); !errors.Is(err, ErrNotConfigured) {
				t.Errorf("%s on a disabled client: got %v, want ErrNotConfigured", call.name, err)
			}
		}

		if n := transport.requests.Load(); n != 0 {
			t.Errorf("disabled client (%+v) made %d upstream request(s): %v", cfg, n, transport.urls)
		}
	}

	// Guard against a vacuous pass. Everything above asserts that a counter
	// stayed at zero, which is also what a seam that stopped being wired would
	// produce: drop option.WithHTTPClient from New and the assertions keep
	// passing while the real client dials OpenAI. So prove the counter can move
	// — a configured client must reach the same transport.
	transport := &countingHTTPClient{}
	configured := New(Config{APIKey: "test-key", BaseURL: "http://127.0.0.1:1", HTTPClient: transport, MaxRetries: retries(0)})
	if _, err := configured.GenerateText(context.Background(), "", "system", "hi"); err == nil {
		t.Fatal("expected the refusing transport to fail a configured client's call")
	}
	if transport.requests.Load() == 0 {
		t.Fatal("configured client sent nothing through the test transport: the HTTPClient seam is not wired, so the zero-request assertions above prove nothing")
	}
}
