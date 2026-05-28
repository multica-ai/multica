package agent

import "encoding/json"

// HookEventType identifies the Claude Code hook event that produced a HookEvent.
// Only the events the claude-tui backend consumes are enumerated; unknown
// events are accepted by the daemon hook server and dropped here.
type HookEventType string

const (
	HookSessionStart HookEventType = "SessionStart"
	HookPreToolUse   HookEventType = "PreToolUse"
	HookPostToolUse  HookEventType = "PostToolUse"
	HookStop         HookEventType = "Stop"
)

// HookEvent is one parsed Claude Code hook delivery. The daemon's hook server
// constructs these from the JSON body Claude POSTs and pushes them onto the
// per-task subscription channel.
//
// Only fields the claude-tui backend uses are exposed as typed columns; the
// full original JSON is preserved in Raw for logging and forward-compat.
type HookEvent struct {
	Type              HookEventType
	SessionID         string
	Cwd               string
	TranscriptPath    string          // SessionStart, Stop — absolute path to the per-session JSONL claude writes
	ToolName          string          // PreToolUse, PostToolUse
	ToolUseID         string          // PreToolUse, PostToolUse
	ToolInput         json.RawMessage // PreToolUse, PostToolUse
	ToolResponse      json.RawMessage // PostToolUse
	LastAssistantText string          // Stop
	Raw               json.RawMessage // entire POST body, for debug
}

// HookSubscriber is the interface a backend uses to receive Claude Code hook
// events from the daemon's HTTP hook server. The daemon injects an
// implementation into agent.Config; backends call Subscribe once at the start
// of Execute and Cancel when the run completes.
//
// BaseURL returns the URL prefix backends should write into Claude's
// settings.local.json hook config. The token argument lets the server route
// inbound POSTs back to the right subscriber.
type HookSubscriber interface {
	Subscribe(token string) (events <-chan HookEvent, cancel func())
	BaseURL() string
}
