// Package agent provides a unified interface for executing prompts via
// coding agents (Claude Code, Codex, Copilot, OpenCode, OpenClaw, Hermes,
// Gemini, Pi, Cursor, Kimi, Kiro, KiloCode). It mirrors the happy-cli AgentBackend
// pattern, translated to idiomatic Go.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// Backend is the unified interface for executing prompts via coding agents.
type Backend interface {
	// Execute runs a prompt and returns a Session for streaming results.
	// The caller should read from Session.Messages (optional) and wait on
	// Session.Result for the final outcome.
	Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error)
}

// ExecOptions configures a single execution.
type ExecOptions struct {
	Cwd                       string
	Model                     string
	SystemPrompt              string
	MaxTurns                  int
	Timeout                   time.Duration
	SemanticInactivityTimeout time.Duration
	ResumeSessionID           string          // if non-empty, resume a previous agent session
	ExtraArgs                 []string        // daemon-wide default CLI arguments appended before CustomArgs; currently read by claude and codex backends only
	CustomArgs                []string        // per-agent CLI arguments appended after ExtraArgs
	McpConfig                 json.RawMessage // if non-nil, MCP server config to pass via --mcp-config
}

// Session represents a running agent execution.
type Session struct {
	// Messages streams events as the agent works. The channel is closed
	// when the agent finishes (before Result is sent).
	Messages <-chan Message
	// Result receives exactly one value — the final outcome — then closes.
	Result <-chan Result
}

// MessageType identifies the kind of Message.
type MessageType string

const (
	MessageText       MessageType = "text"
	MessageThinking   MessageType = "thinking"
	MessageToolUse    MessageType = "tool-use"
	MessageToolResult MessageType = "tool-result"
	MessageStatus     MessageType = "status"
	MessageError      MessageType = "error"
	MessageLog        MessageType = "log"
)

// Message is a unified event emitted by an agent during execution.
type Message struct {
	Type      MessageType
	Content   string         // text content (Text, Error, Log)
	Tool      string         // tool name (ToolUse, ToolResult)
	CallID    string         // tool call ID (ToolUse, ToolResult)
	Input     map[string]any // tool input (ToolUse)
	Output    string         // tool output (ToolResult)
	Status    string         // agent status string (Status)
	Level     string         // log level (Log)
	SessionID string         // backend session id (Status), for early resume-pointer pinning
}

// TokenUsage tracks token consumption for a single model.
type TokenUsage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
}

// Result is the final outcome after an agent session completes.
type Result struct {
	Status     string // "completed", "failed", "aborted", "timeout", "cancelled"
	Output     string // accumulated text output
	Error      string // error message if failed
	DurationMs int64
	SessionID  string
	Usage      map[string]TokenUsage // keyed by model name
}

// Config configures a Backend instance.
type Config struct {
	ExecutablePath string            // path to CLI binary (claude, codex, copilot, opencode, openclaw, hermes, gemini, pi, cursor, kimi, kiro-cli)
	Env            map[string]string // extra environment variables
	Logger         *slog.Logger
}

// New creates a Backend for the given agent type.
// Supported types: "claude", "codex", "copilot", "opencode", "openclaw", "hermes", "gemini", "pi", "cursor", "kimi", "kiro", "kilocode".
func New(agentType string, cfg Config) (Backend, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	switch agentType {
	case "claude":
		return &claudeBackend{cfg: cfg}, nil
	case "codex":
		return &codexBackend{cfg: cfg}, nil
	case "copilot":
		return &copilotBackend{cfg: cfg}, nil
	case "opencode":
		return &opencodeBackend{cfg: cfg}, nil
	case "openclaw":
		return &openclawBackend{cfg: cfg}, nil
	case "hermes":
		return &hermesBackend{cfg: cfg}, nil
	case "gemini":
		return &geminiBackend{cfg: cfg}, nil
	case "pi":
		return &piBackend{cfg: cfg}, nil
	case "cursor":
		return &cursorBackend{cfg: cfg}, nil
	case "kimi":
		return &kimiBackend{cfg: cfg}, nil
	case "kiro":
		return &kiroBackend{cfg: cfg}, nil
	case "kilocode":
		return &kilocodeBackend{cfg: cfg}, nil
	default:
		return nil, fmt.Errorf("unknown agent type: %q (supported: claude, codex, copilot, opencode, openclaw, hermes, gemini, pi, cursor, kimi, kiro, kilocode)", agentType)
	}
}

// DetectVersion runs the agent CLI with --version and returns the output.
func DetectVersion(ctx context.Context, executablePath string) (string, error) {
	return detectCLIVersion(ctx, executablePath)
}

// launchHeaders maps each supported agent type to the user-visible skeleton
// that the daemon spawns before any custom_args are appended. This is
// intentionally minimal — only the command + subcommand (or a short mode
// label when there is no subcommand). Internal flags, transport values, and
// environment variables are deliberately omitted so the string is a hint
// about *what* users are extending, not a dump of the full command line.
var launchHeaders = map[string]string{
	"claude":   "claude (stream-json)",
	"codex":    "codex app-server",
	"copilot":  "copilot (json)",
	"cursor":   "cursor-agent (stream-json)",
	"gemini":   "gemini (stream-json)",
	"hermes":   "hermes acp",
	"kilocode": "kilo acp",
	"openclaw": "openclaw agent (json)",
	"opencode": "opencode run (json)",
	"pi":       "pi (json mode)",
	"kimi":     "kimi acp",
	"kiro":     "kiro-cli acp",
}

// LaunchHeader returns the user-visible launch skeleton for agentType, or an
// empty string if the type is unknown. Callers render this as a preview so
// users understand which command their custom_args get appended to.
func LaunchHeader(agentType string) string {
	return launchHeaders[agentType]
}
