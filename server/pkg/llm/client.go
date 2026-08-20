// Package llm is a thin, reusable wrapper around the official OpenAI Go SDK
// (github.com/openai/openai-go). It exists so the rest of the server has a
// single, well-typed entry point for "just call an LLM" needs that do NOT
// require the full agent runtime — currently chat auto-titling and chat
// follow-up questions (MUL-4238).
//
// # Scope: the assist layer, not every model call in the product
//
// This package covers the LLM calls the API process makes on its own behalf.
// Running an agent is a different data path entirely: the daemon executes an
// AI coding tool as a subprocess under that tool's own credentials, and does
// not forward this layer's MULTICA_LLM_* settings to it. (It does inject the
// task-scoped Multica connection variables the agent itself needs — see
// mergeEnv in pkg/agent, which drops the daemon's inherited MULTICA_* and then
// appends the values assembled for that task.) Nothing here governs that path,
// and operator-facing copy about this layer must not imply otherwise — an
// admin who reads "empty means nothing is sent" as covering the whole product
// has been misled about where their chat content goes.
//
// # The single-entry-point rule
//
// Every LLM call the server process makes on its own behalf goes through this
// package, and nothing outside it imports the OpenAI SDK directly. That is a
// constraint to keep, not a coincidence: it is what makes "what does this
// deployment's assist layer send to a third party?" answerable by reading one
// package instead of auditing the tree. A new feature that wants a model calls
// this package. Two tests hold the halves of that rule:
// TestOpenAISDKIsImportedOnlyByThisPackage (nothing else reaches the SDK) and
// TestDocumentedConsumersAreTheOnlyCallers (nothing else reaches this client).
//
// # Consumers, and what they send upstream
//
// Keep this list current when a consumer is added, removed, or changes what it
// sends — it is the source the operator-facing copy is written from
// (.env.example and apps/docs/content/docs/environment-variables*.mdx), and
// TestDocumentedConsumersAreTheOnlyCallers fails until a new call site is
// reflected here.
//
//   - Chat auto-titling — server/internal/handler/chat_title.go. Sends the
//     first user message of a new chat session, verbatim and uncapped.
//     Attachments are never included.
//   - Chat follow-up questions, a.k.a. quick actions —
//     server/internal/service/chat_quick_actions_generate.go.
//     Sends the tail of the conversation: up to 6 messages, the reply being
//     answered capped at 3000 runes (2000 head + 1000 tail) and each older
//     message at 800.
//
// Both consumers send private chat content, which is why an unconfigured
// deployment making zero upstream requests is a contract rather than a side
// effect: New with no API key and no base URL returns a disabled client whose
// every call fails with ErrNotConfigured before an HTTP request is ever built,
// and both consumers check Enabled() before doing any work
// (TestUnconfiguredClientMakesZeroUpstreamRequests). An operator who must not
// let THIS layer send chat content leaves MULTICA_LLM_API_KEY and
// MULTICA_LLM_BASE_URL empty; the product stays whole (client-derived chat
// titles, no follow-up question buttons).
//
// The wrapper is intentionally small:
//
//   - It owns the SDK client construction (base URL + API key + retry/timeout
//     defaults) so callers never touch option.RequestOption directly.
//   - It exposes both the raw Chat Completions surface (Chat / ChatStream)
//     and a convenience GenerateText helper, used by server-internal callers
//     for simple one-shot completions (e.g. chat title generation).
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
	"fmt"
	"net/http"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/shared"
)

// FallbackModel is the last-resort model used when neither the request nor the
// configured default supplies one. It is deliberately a small, inexpensive
// model since this layer backs lightweight utility calls.
const FallbackModel = "gpt-5.6-luna"

// defaultTimeout bounds the full request lifecycle (including SDK retries) when
// the caller's context has no deadline of its own. Streaming requests are not
// subject to this because the handler owns the connection lifetime.
const defaultRequestTimeout = 60 * time.Second

// ErrNotConfigured is returned by Chat/ChatStream/GenerateText when the client
// was constructed without any credentials or base URL. Internal callers should
// treat this as a disabled-LLM signal and fall back gracefully (e.g. chat
// title generation keeps the original title) so a misconfigured self-hosted
// deployment never dials OpenAI with no key.
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
	// MaxRetries is the transport-level retry budget applied to every request
	// this client makes. Maps to MULTICA_LLM_MAX_RETRIES. Build one with
	// Retries; nil means unset, and DefaultMaxRetries applies.
	//
	//   - nil            — unset; DefaultMaxRetries applies.
	//   - Retries(0)     — retries disabled; exactly one upstream request.
	//   - Retries(N)     — at most N retries, so at most N+1 upstream requests.
	//
	// It is a pointer to a validated type rather than a bare int for two
	// reasons (MUL-6364). A bare int made 0 indistinguishable from the zero
	// value, so asking for no retries silently produced the SDK default
	// instead; and it let a negative — which option.WithMaxRetries panics on —
	// reach this layer, where the only options were to panic or to quietly
	// substitute some other budget. Retries is the sole constructor and rejects
	// negatives, so neither state is representable here at all.
	//
	// What this budget retries is decided by the SDK: connection-level failures
	// (no response at all), HTTP 408, 409, 429 and any 5xx, plus any response
	// carrying `x-should-retry: true`. Every other 4xx — 400, 401, 403, 404 —
	// is returned to the caller unretried. Backoff starts at 0.5s and doubles
	// to an 8s cap with up to 25% jitter subtracted, unless the response
	// carries a Retry-After header, which wins.
	//
	// It is NOT the parameter-compatibility retry inside GenerateJSON (which is
	// independent and bounded separately), and it does not cover a stream that
	// breaks after ChatStream has already returned. Choose a value against the
	// caller's own deadline: backoff alone costs ~1.5s at 2 retries and ~21s at
	// 6, so a budget larger than the caller's timeout only converts a
	// recoverable failure into a deadline-exceeded one.
	MaxRetries *RetryOverride
	// HTTPClient, when set, replaces the SDK's default transport. Primarily a
	// test seam.
	HTTPClient option.HTTPClient
}

// DefaultMaxRetries is the retry budget used when Config.MaxRetries is unset.
// It mirrors the openai-go default at the version we pin, but New always passes
// it explicitly so the effective policy is ours to report and can never drift
// silently with an SDK bump.
const DefaultMaxRetries = 2

// RetryOverride is a validated retry budget for Config.MaxRetries. Its only
// field is unexported and Retries is its only constructor, so an invalid budget
// cannot be represented at this boundary — New therefore has no correction
// branch to take, and needs none. #7154 asked that invalid values fail
// validation rather than be silently coerced; making them unbuildable is the
// strongest available form of that.
type RetryOverride struct{ n int }

// Retries returns an override of at most n retries per call. It rejects a
// negative n instead of correcting it: option.WithMaxRetries panics on one, and
// there is no honest correction to make — "fewer than zero retries" is a
// mistake, not a request to disable them. Retries(0) is how you disable them.
func Retries(n int) (*RetryOverride, error) {
	if n < 0 {
		return nil, fmt.Errorf("llm: max retries must not be negative, got %d (use 0 to disable retries)", n)
	}
	return &RetryOverride{n: n}, nil
}

// Value reports the configured budget. The zero value of RetryOverride reports
// 0, which is consistent: an override that was never built through Retries
// carries no retries either.
func (r *RetryOverride) Value() int {
	if r == nil {
		return 0
	}
	return r.n
}

// Retry policy sources, as reported by RetryBudget.Source.
const (
	// RetrySourceDefault means Config.MaxRetries was unset.
	RetrySourceDefault = "default"
	// RetrySourceConfig means Config.MaxRetries was set explicitly, including
	// an explicit 0.
	RetrySourceConfig = "config"
)

// RetryBudget describes a Client's effective transport retry policy. It exists
// so a deployment can report what it will actually do rather than re-derive it
// from raw configuration — re-deriving is how the unset-versus-zero ambiguity
// went unnoticed in the first place. Every field is a scalar or a fixed enum,
// so logging the whole struct can never leak an API key or a gateway URL.
type RetryBudget struct {
	// MaxRetries is the ceiling on retries after a failed request, so N means
	// at most N+1 upstream requests per call. Only retryable failures consume
	// it; a success or the caller's deadline can end the call sooner.
	MaxRetries int
	// Source is RetrySourceDefault or RetrySourceConfig.
	Source string
	// RequestTimeout bounds the whole non-streaming call chain, retries and
	// backoff included, when the caller's context has no earlier deadline of
	// its own. Every current internal caller sets a tighter one.
	RequestTimeout time.Duration
}

// Client is a configured, reusable LLM caller. It is safe for concurrent use;
// the underlying SDK client holds no per-request state.
type Client struct {
	sdk          openai.Client
	defaultModel string
	enabled      bool
	retry        RetryBudget
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
	retry := RetryBudget{
		MaxRetries:     DefaultMaxRetries,
		Source:         RetrySourceDefault,
		RequestTimeout: defaultRequestTimeout,
	}
	if cfg.MaxRetries != nil {
		// No validation and no clamping here on purpose: RetryOverride can only
		// hold a value Retries already accepted, so there is no invalid budget
		// left for this layer to silently correct.
		retry.MaxRetries = cfg.MaxRetries.Value()
		retry.Source = RetrySourceConfig
	}
	// Always set it explicitly, even for the default, so the budget the SDK
	// enforces and the one RetryBudget reports are the same number.
	opts = append(opts, option.WithMaxRetries(retry.MaxRetries))
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
		retry:   retry,
	}
}

// RetryBudget returns the effective transport retry policy, for startup
// diagnostics. Safe to log whole: it carries no credentials or URLs.
func (c *Client) RetryBudget() RetryBudget {
	if c == nil {
		return RetryBudget{}
	}
	return c.retry
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
//
// Config.MaxRetries covers only the POST that opens the stream. Once this
// returns, a stream that breaks mid-response is the caller's to handle: the SDK
// cannot replay chunks it has already delivered.
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

// GenerateJSON is GenerateText's structured sibling, for internal callers whose
// reply has to be machine-readable (quick-action suggestions, ...). It requests
// response_format=json_object and returns the assistant's raw text unparsed.
//
// JSON-object mode only guarantees the reply is syntactically valid JSON, never
// that its shape matches what the prompt asked for, so the caller still owns
// parsing and validation. One upstream constraint the caller must honor: the
// word "JSON" has to appear somewhere in the prompt, or OpenAI-compatible
// endpoints reject the request outright.
//
// This helper is for small, latency-sensitive utility work. For the GPT-5.6
// family it explicitly disables reasoning and leaves sampling controls at the
// model default. That keeps maxCompletionTokens available to the visible JSON
// instead of spending it on reasoning, and avoids sampling parameters that
// those models may reject. Other models keep the caller's temperature so a
// configurable deployment does not change behavior. temperature and
// maxCompletionTokens apply only when positive; zero leaves the corresponding
// upstream default in place. Model empty -> the configured default.
func (c *Client) GenerateJSON(ctx context.Context, model, systemPrompt, userPrompt string, temperature float64, maxCompletionTokens int64) (string, error) {
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
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		},
	}
	effectiveModel := strings.TrimSpace(model)
	if effectiveModel == "" {
		effectiveModel = c.defaultModel
	}
	if isGPT56Family(effectiveModel) {
		// GPT-5.6 defaults to medium reasoning. This path generates a tiny JSON
		// object under a strict wall-clock budget, so reasoning would add latency
		// and consume the completion-token limit without improving the contract.
		params.ReasoningEffort = shared.ReasoningEffortNone
	} else if temperature > 0 {
		params.Temperature = openai.Float(temperature)
	}
	if maxCompletionTokens > 0 {
		// max_tokens is deprecated and rejected by current reasoning models,
		// including the GPT-5.6 family. Prefer the replacement field for every
		// upstream; a narrow compatibility retry below covers older gateways
		// that have not implemented it yet.
		params.MaxCompletionTokens = openai.Int(maxCompletionTokens)
	}

	// The preferred request and its optional compatibility retry share one
	// deadline, so a legacy gateway cannot double the caller's time budget.
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()

	// Some older OpenAI-compatible gateways have not implemented one or both
	// modern fields. Negotiate only when the upstream explicitly identifies an
	// unsupported parameter: validation fails before generation, and each field
	// can be removed or replaced at most once under the shared deadline.
	//
	// This loop is a parameter-compatibility negotiation, NOT an error retry,
	// and it is deliberately independent of Config.MaxRetries: it fires only on
	// a 400 the SDK never retries, and its bound stays 2 whatever the transport
	// budget is. The two do compose, though — each attempt below carries its own
	// transport budget, so one call can cost up to two negotiation requests plus
	// MaxRetries+1 on the final attempt.
	var completion *openai.ChatCompletion
	for compatibilityRetries := 0; ; compatibilityRetries++ {
		var err error
		completion, err = c.Chat(ctx, params)
		if err == nil {
			break
		}
		if compatibilityRetries >= 2 {
			return "", err
		}

		switch {
		case params.MaxCompletionTokens.Valid() && isUnsupportedParameter(err, "max_completion_tokens"):
			params.MaxCompletionTokens = param.Opt[int64]{}
			params.MaxTokens = openai.Int(maxCompletionTokens)
		case params.ReasoningEffort != "" && isUnsupportedParameter(err, "reasoning_effort"):
			params.ReasoningEffort = ""
		default:
			return "", err
		}
	}
	if len(completion.Choices) == 0 {
		return "", errors.New("llm: upstream returned no choices")
	}
	choice := completion.Choices[0]
	if choice.FinishReason == "length" {
		return "", errors.New("llm: upstream reached the max completion token limit before producing complete JSON")
	}
	if strings.TrimSpace(choice.Message.Content) == "" {
		return "", errors.New("llm: upstream returned empty JSON content")
	}
	return choice.Message.Content, nil
}

func isUnsupportedParameter(err error, parameter string) bool {
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) ||
		apiErr.StatusCode != http.StatusBadRequest ||
		apiErr.Param != parameter {
		return false
	}
	return apiErr.Code == "unsupported_parameter" ||
		(apiErr.Code == "" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(apiErr.Message)), "unsupported parameter"))
}

func isGPT56Family(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return model == "gpt-5.6" || strings.HasPrefix(model, "gpt-5.6-")
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
