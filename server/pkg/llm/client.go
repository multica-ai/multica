// Package llm is a thin, reusable wrapper around the official OpenAI Go SDK
// (github.com/openai/openai-go). It exists so the rest of the server has a
// single, well-typed entry point for "just call an LLM" needs that do NOT
// require the full agent runtime — e.g. generating a chat title or drafting a
// quick-create issue (MUL-4238).
//
// The wrapper is intentionally small:
//
//   - It owns the SDK client construction (base URL + API key + retry/timeout
//     defaults) so callers never touch option.RequestOption directly.
//   - It exposes both the raw Chat Completions surface (Chat / ChatStream),
//     used by the OpenAI-compatible HTTP proxy handlers, and a convenience
//     GenerateText helper for simple internal one-shot completions.
//   - The default model is configurable; when a request omits the model we
//     fall back to it, and when it too is empty we fall back to a sane
//     built-in default so a misconfigured deployment still returns a clear
//     upstream error rather than a 400 from our own layer.
//
// Base URL and API key are configurable so the same layer can target OpenAI,
// an OpenAI-compatible gateway, or a self-hosted model server.
package llm

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/shared"
)

// FallbackModel is the last-resort model used when neither the request nor the
// configured default supplies one. It is deliberately a small, inexpensive
// model since this layer backs lightweight utility calls.
const FallbackModel = "gpt-4o-mini"

// defaultTimeout bounds the full request lifecycle (including SDK retries) when
// the caller's context has no deadline of its own. Streaming requests are not
// subject to this because the handler owns the connection lifetime.
const defaultRequestTimeout = 60 * time.Second

// ErrNotConfigured is returned by Chat/ChatStream/GenerateText when the client
// was constructed without any credentials or base URL. Callers (e.g. the HTTP
// handlers) should translate this into a 503 so a misconfigured self-hosted
// deployment surfaces a clear error instead of dialing OpenAI with no key.
var ErrNotConfigured = errors.New("llm: no API key or base URL configured")

// Config holds the tunables for the LLM layer. All fields are optional; an
// empty Config yields a disabled client (see Client.Enabled).
type Config struct {
	// APIKey authenticates against the upstream. Maps to MULTICA_LLM_API_KEY.
	APIKey string
	// BaseURL points at OpenAI or any OpenAI-compatible gateway. When empty the
	// SDK's default (https://api.openai.com/v1) is used. Maps to
	// MULTICA_LLM_BASE_URL.
	BaseURL string
	// DefaultModel is used when a request omits the model. Maps to
	// MULTICA_LLM_DEFAULT_MODEL. When empty, FallbackModel is used.
	DefaultModel string
	// MaxRetries overrides the SDK default (2). A negative value is treated as
	// zero (no retries).
	MaxRetries int
	// HTTPClient, when set, replaces the SDK's default transport. Primarily a
	// test seam.
	HTTPClient option.HTTPClient
}

// Client is a configured, reusable LLM caller. It is safe for concurrent use;
// the underlying SDK client holds no per-request state.
type Client struct {
	sdk          openai.Client
	defaultModel string
	enabled      bool
}

// New builds a Client from cfg. It never returns an error: an unconfigured
// Config produces a disabled client whose calls return ErrNotConfigured, which
// keeps wiring in main/router simple (no boot-time failure when the LLM layer
// is simply not set up on a given deployment).
func New(cfg Config) *Client {
	opts := make([]option.RequestOption, 0, 4)
	if key := strings.TrimSpace(cfg.APIKey); key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}
	if base := strings.TrimSpace(cfg.BaseURL); base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	if cfg.MaxRetries != 0 {
		retries := cfg.MaxRetries
		if retries < 0 {
			retries = 0
		}
		opts = append(opts, option.WithMaxRetries(retries))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}

	defaultModel := strings.TrimSpace(cfg.DefaultModel)
	if defaultModel == "" {
		defaultModel = FallbackModel
	}

	return &Client{
		sdk:          openai.NewClient(opts...),
		defaultModel: defaultModel,
		// A deployment is "configured" if it gave us either a key or a base
		// URL. A bare base URL (no key) is valid for keyless local gateways.
		enabled: strings.TrimSpace(cfg.APIKey) != "" || strings.TrimSpace(cfg.BaseURL) != "",
	}
}

// Enabled reports whether the client was given any credentials or base URL.
// Handlers use this to short-circuit with a 503 before doing any work.
func (c *Client) Enabled() bool { return c != nil && c.enabled }

// DefaultModel returns the effective default model (never empty).
func (c *Client) DefaultModel() string { return c.defaultModel }

// applyDefaultModel fills in the default model when the caller left it blank.
func (c *Client) applyDefaultModel(params *openai.ChatCompletionNewParams) {
	if strings.TrimSpace(string(params.Model)) == "" {
		params.Model = shared.ChatModel(c.defaultModel)
	}
}

// Chat performs a non-streaming chat completion. The params are passed through
// to the SDK verbatim (so tools, response_format, temperature, etc. are all
// honored); only the model default is applied. The returned *ChatCompletion
// exposes RawJSON() for byte-exact OpenAI-compatible responses.
func (c *Client) Chat(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	c.applyDefaultModel(&params)

	// Give the request a bounded lifetime when the caller supplied none, so a
	// hung upstream cannot pin a goroutine indefinitely.
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()

	return c.sdk.Chat.Completions.New(ctx, params)
}

// ChatStream performs a streaming chat completion, returning the SDK stream so
// the caller can relay chunks (each chunk exposes RawJSON() for byte-exact
// OpenAI-compatible SSE). The caller MUST call Close on the returned stream.
//
// Unlike Chat, no default timeout is imposed: the stream's lifetime is owned by
// the caller (typically an HTTP handler bound to the client connection).
func (c *Client) ChatStream(ctx context.Context, params openai.ChatCompletionNewParams) (*ssestream.Stream[openai.ChatCompletionChunk], error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	c.applyDefaultModel(&params)
	return c.sdk.Chat.Completions.NewStreaming(ctx, params), nil
}

// GenerateText is a convenience for simple internal one-shot completions (chat
// titles, quick-create drafts, ...). It sends an optional system prompt plus a
// single user prompt and returns the assistant's text content. Model empty ->
// the configured default.
func (c *Client) GenerateText(ctx context.Context, model, systemPrompt, userPrompt string) (string, error) {
	if !c.Enabled() {
		return "", ErrNotConfigured
	}

	messages := make([]openai.ChatCompletionMessageParamUnion, 0, 2)
	if strings.TrimSpace(systemPrompt) != "" {
		messages = append(messages, openai.SystemMessage(systemPrompt))
	}
	messages = append(messages, openai.UserMessage(userPrompt))

	params := openai.ChatCompletionNewParams{
		Messages: messages,
		Model:    shared.ChatModel(strings.TrimSpace(model)),
	}

	completion, err := c.Chat(ctx, params)
	if err != nil {
		return "", err
	}
	if len(completion.Choices) == 0 {
		return "", errors.New("llm: upstream returned no choices")
	}
	return completion.Choices[0].Message.Content, nil
}

// withDefaultTimeout returns ctx unchanged (with a no-op cancel) when it already
// has a deadline, otherwise a child context bounded by defaultRequestTimeout.
func withDefaultTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultRequestTimeout)
}

// compile-time assertion that option.HTTPClient is satisfied by *http.Client so
// callers can pass a plain *http.Client as the test seam.
var _ option.HTTPClient = (*http.Client)(nil)
