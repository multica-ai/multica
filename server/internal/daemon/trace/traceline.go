// Package trace provides the daemon-local agent trace data model and storage.
//
// Trace captures a time-ordered sequence of events (TraceLine) for each
// task/run pair, persisted locally as JSONL files. The server never receives
// raw trace data — only the daemon's local HTTP API exposes it.
package trace

import "time"

// Channel constants identify the origin/category of a trace line.
const (
	ChannelRawStdout        = "raw_stdout"
	ChannelRawStderr        = "raw_stderr"
	ChannelProviderEvent    = "provider_event"
	ChannelCommandStdout    = "command_stdout"
	ChannelCommandStderr    = "command_stderr"
	ChannelApprovalRequest  = "approval_request"
	ChannelApprovalResponse = "approval_response"
	ChannelNormalized       = "normalized"
	ChannelDisplayEvent     = "display_event"
)

// MaxContentLen is the maximum number of bytes a TraceLine's Content field
// may hold before being truncated. Lines exceeding this limit have
// Truncated set to true.
const MaxContentLen = 128 * 1024 // 128 KiB

// TraceLine is a single event in the agent trace timeline.
type TraceLine struct {
	// Seq is a monotonically increasing sequence number within a single
	// task_id/run_id pair. Assigned by the store on append; caller-provided
	// values are ignored.
	Seq int64 `json:"seq"`

	// TaskID identifies the Multica task that produced this trace line.
	TaskID string `json:"task_id"`

	// RunID identifies a single execution run of the task (one attempt).
	// A task may be retried; each attempt gets a distinct run_id.
	RunID string `json:"run_id"`

	// Provider identifies the agent provider (e.g. "claude", "codex").
	Provider string `json:"provider,omitempty"`

	// Channel categorises this trace line's origin. See Channel* constants.
	Channel string `json:"channel"`

	// Content is the human-readable representation. May be truncated;
	// check the Truncated field.
	Content string `json:"content,omitempty"`

	// RawPayload holds the original unstructured payload (e.g. raw JSON
	// from a provider stream). Kept separate from Content so the UI can
	// show a summary by default and expand to the full detail on demand.
	RawPayload string `json:"raw_payload,omitempty"`

	// Timestamp is when this trace line was recorded (UTC). Assigned by
	// the store on append.
	Timestamp time.Time `json:"timestamp"`

	// Truncated is true when Content was trimmed to fit MaxContentLen.
	Truncated bool `json:"truncated,omitempty"`

	// Redacted is true when the line contains redacted sensitive data
	// (e.g. API keys, tokens).
	Redacted bool `json:"redacted,omitempty"`
}

// truncateContent enforces MaxContentLen on Content and RawPayload and sets
// Truncated. It operates on a value receiver and returns a new TraceLine so
// the caller always gets a copy with the limit applied.
func (l TraceLine) truncateContent() TraceLine {
	if len(l.Content) > MaxContentLen {
		l.Content = l.Content[:MaxContentLen]
		l.Truncated = true
	}
	if len(l.RawPayload) > MaxContentLen {
		l.RawPayload = l.RawPayload[:MaxContentLen]
		l.Truncated = true
	}
	return l
}
