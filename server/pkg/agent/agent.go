// Package agent provides a unified interface for executing prompts via
// coding agents (Claude Code, CodeBuddy, Codex, Copilot, OpenCode, OpenClaw,
// Hermes, Gemini, Pi, Cursor, Kimi, Kiro, DeepSeek, Antigravity,
// qoderclicn, mmx). It mirrors the happy-cli AgentBackend pattern, translated to
// idiomatic Go.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Backend is the unified interface for executing prompts via coding agents.
type Backend interface {
	// Execute runs a prompt and returns a Session for streaming results.
	// The caller should read from Session.Messages (optional) and wait on
	// Session.Result for the final outcome.
	Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error)
}

// ApprovalCallback is called by provider adapters when a tool/command/file
// change needs user approval. The callback blocks until the user responds or
// the context is cancelled.
//
// A nil ApprovalCallback is equivalent to auto-approve (current default).
//
// Parameters:
//   - ctx: cancellation/timeout context
//   - req: describes what needs approval (title, detail, options)
//
// Returns:
//   - chosenOption: the option ID selected by the user (e.g. "allow", "deny")
//   - approved: true if the request was approved
//   - err: non-nil on timeout, cancellation, or communication failure
type ApprovalCallback func(ctx context.Context, req ApprovalRequest) (chosenOption string, approved bool, err error)

// ApprovalRequest describes a single approval prompt surfaced to the user.
type ApprovalRequest struct {
	Type          string // e.g. "command_approval", "file_change_approval", "permission_request"
	Title         string
	Detail        string
	Options       []protocol.InteractionOption
	DefaultOption string
}

const approvalChoiceMessageSeparator = "\x1f"
const CachedApprovalResponseMessage = "__multica_cached_approval__"

func EncodeApprovalChoice(chosenOption, responseMessage string) string {
	responseMessage = strings.TrimSpace(responseMessage)
	if responseMessage == "" {
		return chosenOption
	}
	return chosenOption + approvalChoiceMessageSeparator + responseMessage
}

func SplitApprovalChoice(value string) (chosenOption, responseMessage string) {
	parts := strings.SplitN(value, approvalChoiceMessageSeparator, 2)
	chosenOption = parts[0]
	if len(parts) > 1 {
		responseMessage = strings.TrimSpace(parts[1])
	}
	return chosenOption, responseMessage
}

// TraceCallback is called by provider adapters to record events in the local
// daemon trace store. Channel identifies the trace channel (provider_event,
// normalized, command_stdout, etc.). Content is the human-readable text.
// RawPayload is the original unstructured data (e.g. raw JSON from a provider
// stream). May be nil (no trace recording).
type TraceCallback func(channel, content, rawPayload string)

// ExecOptions configures a single execution.
type ExecOptions struct {
	Cwd   string
	Model string
	// SystemPrompt is consumed only by providers that can pass or safely inline
	// developer/system instructions. Hermes ACP intentionally ignores it and
	// relies on cwd-scoped context files such as AGENTS.md instead.
	SystemPrompt              string
	VisibleLanguage           string
	MaxTurns                  int
	Timeout                   time.Duration
	SemanticInactivityTimeout time.Duration
	ResumeSessionID           string           // if non-empty, resume a previous agent session
	ExtraArgs                 []string         // daemon-wide default CLI arguments appended before CustomArgs; currently read by claude, codex, and qoderclicn backends only
	CustomArgs                []string         // per-agent CLI arguments appended after ExtraArgs
	McpConfig                 json.RawMessage  // if non-nil, MCP server config to pass via --mcp-config
	OnApproval                ApprovalCallback // nil = auto-approve (default behaviour)
	ApprovalPolicy            string           // "auto", "prompt", or "deny"; empty treated as "auto"
	TraceCallback             TraceCallback    // nil = no trace recording (default)
	ClaudePermissionMode      string           // optional controlled override: default, plan, acceptEdits
	ClaudeUseSDKBridge        bool             // route Claude execution through the Agent SDK bridge
	// ThinkingLevel is the runtime-native reasoning/effort value (e.g.
	// Claude's "low|medium|high|xhigh|max", Codex's "none|minimal|low|
	// medium|high|xhigh"). Empty means "use the runtime/model default" —
	// every backend that consumes this skips its --effort / reasoning_effort
	// injection so the upstream CLI's own default applies. Currently honoured
	// by the claude and codex backends only; other backends ignore the
	// field rather than fail (so MUL-2339 can grow runtime support
	// incrementally without breaking unrelated agents).
	ThinkingLevel string
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
	ExecutablePath string            // path to CLI binary (claude, cbc, codex, copilot, opencode, openclaw, hermes, gemini, pi, cursor, kimi, kiro-cli, deepseek, agy, qoderclicn, mmx)
	Env            map[string]string // extra environment variables
	Logger         *slog.Logger
}

// New creates a Backend for the given agent type.
// Supported types: "claude", "codebuddy", "codex", "copilot", "opencode", "openclaw", "hermes", "gemini", "pi", "cursor", "kimi", "kiro", "DeepSeek-TUI", "antigravity", "qoderclicn", "mmx".
func New(agentType string, cfg Config) (Backend, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	switch agentType {
	case "claude":
		return &claudeBackend{cfg: cfg}, nil
	case "codebuddy":
		return newCodebuddyBackend(cfg), nil
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
	case "DeepSeek-TUI":
		return &deepseekBackend{cfg: cfg}, nil
	case "antigravity":
		return &antigravityBackend{cfg: cfg}, nil
	case "qoderclicn":
		return &qoderclicnBackend{cfg: cfg}, nil
	case "mmx":
		return &mmxBackend{cfg: cfg}, nil
	default:
		return nil, fmt.Errorf("unknown agent type: %q (supported: claude, codebuddy, codex, copilot, opencode, openclaw, hermes, gemini, pi, cursor, kimi, kiro, DeepSeek-TUI, antigravity, qoderclicn, mmx)", agentType)
	}
}

// SupportedBackends returns the set of agent types accepted by New.
func SupportedBackends() []string {
	return []string{
		"claude", "codebuddy", "codex", "copilot", "opencode", "openclaw",
		"hermes", "gemini", "pi", "cursor", "kimi", "kiro", "DeepSeek-TUI", "antigravity", "qoderclicn", "mmx",
	}
}

// RegisteredProviders returns the set of provider names with capability entries.
func RegisteredProviders() []string {
	return registeredProviders()
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
	"antigravity":  "agy -p (print mode)",
	"claude":       "claude (stream-json)",
	"codebuddy":    "codebuddy (stream-json)",
	"codex":        "codex app-server",
	"copilot":      "copilot (json)",
	"cursor":       "cursor-agent (stream-json)",
	"DeepSeek-TUI": "deepseek-tui exec --json --auto",
	"gemini":       "gemini (stream-json)",
	"hermes":       "hermes acp",
	"kimi":         "kimi acp",
	"kiro":         "kiro-cli acp",
	"openclaw":     "openclaw agent (json)",
	"opencode":     "opencode run (json)",
	"pi":           "pi (json mode)",
	"qoderclicn":   "qoderclicn (stream-json)",
	"mmx":          "mmx text chat (json)",
}

// LaunchHeader returns the user-visible launch skeleton for agentType, or an
// empty string if the type is unknown. Callers render this as a preview so
// users understand which command their custom_args get appended to.
func LaunchHeader(agentType string) string {
	return launchHeaders[agentType]
}
