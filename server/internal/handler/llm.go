package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	openai "github.com/openai/openai-go/v3"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/pkg/llm"
)

// llmRequestBodyLimit caps the request body for the chat-completions endpoints.
// 1 MiB comfortably fits large multi-message conversations (including small
// inline image data URLs) while preventing an authenticated client from
// streaming unbounded bytes into the JSON decoder.
const llmRequestBodyLimit = 1 << 20 // 1 MiB

// decodeLLMChatParams reads and validates the OpenAI chat-completions request
// body. It decodes directly into the SDK's ChatCompletionNewParams (which
// implements UnmarshalJSON with full OpenAI fidelity) so every supported field
// — messages, tools, response_format, temperature, etc. — passes through
// untouched. On any failure it writes the appropriate 4xx and returns ok=false.
func decodeLLMChatParams(w http.ResponseWriter, r *http.Request) (openai.ChatCompletionNewParams, bool) {
	var params openai.ChatCompletionNewParams

	r.Body = http.MaxBytesReader(w, r.Body, llmRequestBodyLimit)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		// MaxBytesReader surfaces an oversize body as an error here.
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return params, false
	}
	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, "request body is required")
		return params, false
	}
	if err := params.UnmarshalJSON(body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return params, false
	}
	if len(params.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages is required")
		return params, false
	}
	return params, true
}

// writeUpstreamError maps an error from the LLM layer to an HTTP response.
// Upstream OpenAI errors carry their own status code, which we preserve so the
// caller sees e.g. a 401 (bad key) or 429 (rate limited) rather than a blanket
// 500. A missing configuration is a 503; anything else is a 502 (bad gateway).
func writeUpstreamError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, llm.ErrNotConfigured) {
		writeError(w, http.StatusServiceUnavailable, "LLM API is not configured")
		return
	}

	var apiErr *openai.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode >= 400 {
		slog.Warn("llm upstream error",
			append(logger.RequestAttrs(r), "status", apiErr.StatusCode, "error", err)...)
		// Preserve the upstream OpenAI-shaped error body so OpenAI clients can
		// parse it as they would a direct call. Fall back to a plain message if
		// the SDK gave us no body.
		if raw := apiErr.RawJSON(); raw != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(apiErr.StatusCode)
			_, _ = io.WriteString(w, raw)
			return
		}
		writeError(w, apiErr.StatusCode, "upstream error")
		return
	}

	slog.Warn("llm request failed", append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusBadGateway, "failed to reach LLM upstream")
}

// LLMChatCompletions is an OpenAI-compatible, non-streaming chat-completions
// endpoint (POST /api/llm/v1/chat/completions). It accepts the standard OpenAI
// request body and returns the byte-exact upstream ChatCompletion object.
//
// Auth: user-scoped (the route sits inside the authenticated group). Model is
// taken from the request; when omitted, the configured default is used.
func (h *Handler) LLMChatCompletions(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	if h.LLM == nil || !h.LLM.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "LLM API is not configured")
		return
	}

	params, ok := decodeLLMChatParams(w, r)
	if !ok {
		return
	}

	completion, err := h.LLM.Chat(r.Context(), params)
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}

	// Relay the upstream response verbatim to preserve full OpenAI-format
	// compatibility (fields the SDK struct doesn't model are still present in
	// RawJSON). Fall back to marshaling the typed object if RawJSON is empty.
	if raw := completion.RawJSON(); raw != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, raw)
		return
	}
	writeJSON(w, http.StatusOK, completion)
}

// LLMChatCompletionsStream is an OpenAI-compatible streaming chat-completions
// endpoint (POST /api/llm/v1/chat/completions/stream). It emits server-sent
// events whose `data:` payloads are the byte-exact upstream
// chat.completion.chunk objects, terminated by the OpenAI `data: [DONE]`
// sentinel — identical to calling OpenAI with stream=true.
//
// Auth: user-scoped (the route sits inside the authenticated group). Model is
// taken from the request; when omitted, the configured default is used.
func (h *Handler) LLMChatCompletionsStream(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	if h.LLM == nil || !h.LLM.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "LLM API is not configured")
		return
	}

	params, ok := decodeLLMChatParams(w, r)
	if !ok {
		return
	}

	stream, err := h.LLM.ChatStream(r.Context(), params)
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	defer stream.Close()

	// SSE headers. Disable proxy buffering so chunks flush promptly through
	// nginx-style reverse proxies.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	rc := http.NewResponseController(w)

	// Peek the first chunk before committing a 200 so a fast upstream failure
	// (bad key, unknown model) can still be reported as a JSON error with the
	// correct status instead of a half-open 200 stream.
	if !stream.Next() {
		if err := stream.Err(); err != nil {
			writeUpstreamError(w, r, err)
			return
		}
		// No content at all: emit a well-formed, empty SSE stream.
		w.WriteHeader(http.StatusOK)
		writeSSEData(w, rc, "[DONE]")
		return
	}

	w.WriteHeader(http.StatusOK)

	// First chunk (already fetched), then the rest.
	if chunk := stream.Current(); chunk.RawJSON() != "" {
		if !writeSSEData(w, rc, chunk.RawJSON()) {
			return
		}
	}
	for stream.Next() {
		chunk := stream.Current()
		if chunk.RawJSON() == "" {
			continue
		}
		if !writeSSEData(w, rc, chunk.RawJSON()) {
			return
		}
	}
	if err := stream.Err(); err != nil {
		// The connection is already a 200 SSE stream; we cannot change the
		// status. Emit an OpenAI-style error event so the client can detect the
		// mid-stream failure, then stop.
		slog.Warn("llm stream error mid-flight", append(logger.RequestAttrs(r), "error", err)...)
		writeSSEData(w, rc, `{"error":{"message":"upstream stream error","type":"upstream_error"}}`)
		return
	}

	writeSSEData(w, rc, "[DONE]")
}

// writeSSEData writes a single SSE `data:` event and flushes it. It returns
// false if the write failed (client disconnected), signaling the caller to
// stop. Flush errors are non-fatal (some ResponseWriters don't support it).
func writeSSEData(w http.ResponseWriter, rc *http.ResponseController, payload string) bool {
	if _, err := io.WriteString(w, "data: "+payload+"\n\n"); err != nil {
		return false
	}
	_ = rc.Flush()
	return true
}
