package agent

import (
	"encoding/json"
)

// ── Stream-json event types (shared between stream_json_external and tests) ──

// streamJSONEvent is the top-level NDJSON frame emitted by Claude-compatible
// stream-json CLIs. Each line is a self-contained JSON object with a "type"
// field and an optional "message" field.
type streamJSONEvent struct {
	Type       string          `json:"type"`
	Message    json.RawMessage `json:"message,omitempty"`
	SessionID  string          `json:"session_id,omitempty"`
	Subtype    string          `json:"subtype,omitempty"`  // "success", "error", etc.
	ResultText string          `json:"result,omitempty"`   // final text for "result" events
	IsError    bool            `json:"is_error,omitempty"` // true when the CLI reports a failure
}

// streamJSONMessageContent is the inner message payload for assistant/user
// events in the Claude stream-json protocol.
type streamJSONMessageContent struct {
	Role    string                   `json:"role"`
	Model   string                   `json:"model,omitempty"`
	Content []streamJSONContentBlock `json:"content"`
	Usage   *streamJSONUsage         `json:"usage,omitempty"`
}

type streamJSONUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
}

type streamJSONContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}
