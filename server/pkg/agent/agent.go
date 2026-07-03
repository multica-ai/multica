// Package agent provides a unified interface for executing prompts via
// coding agents (Claude Code, CodeBuddy, Codex, Copilot, OpenCode, OpenClaw,
// Hermes, Pi, Cursor, Kimi, Kiro, Antigravity, Qoder). It mirrors the
// happy-cli AgentBackend pattern, translated to idiomatic Go.
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
	Cwd   string
	Model string
	// SystemPrompt is consumed only by providers that can pass or safely inline
	// developer/system instructions. Hermes ACP intentionally ignores it and
	// relies on cwd-scoped context files such as AGENTS.md instead.
	SystemPrompt              string
	ThreadName                string
	MaxTurns                  int
	Timeout                   time.Duration
	SemanticInactivityTimeout time.Duration
	ResumeSessionID           string          // if non-empty, resume a previous agent session
	ExtraArgs                 []string        // daemon-wide default CLI arguments appended before CustomArgs; currently read by claude and codex backends only
	CustomArgs                []string        // per-agent CLI arguments appended after ExtraArgs
	McpConfig                 json.RawMessage // if non-nil, MCP server config to pass via --mcp-config
	// ThinkingLevel is the runtime-native reasoning/effort value (e.g.
	// Claude's "low|medium|high|xhigh|max", Codex's "none|minimal|low|
	// medium|high|xhigh", OpenCode's model variant names). Empty means
	// "use the runtime/model default" —
	// every backend that consumes this skips its --effort / reasoning_effort
	// injection so the upstream CLI's own default applies. Currently honoured
	// by the claude, codex, and opencode backends; other backends ignore the
	// field rather than fail (so MUL-2339 can grow runtime support
	// incrementally without breaking unrelated agents).
	ThinkingLevel string
	// OpenclawMode chooses between local (embedded) and gateway routing for
	// the openclaw backend. "" or "local" keeps the historical behaviour —
	// the daemon spawns `openclaw agent --local …` and the agent loop runs
	// in-process on the daemon host. "gateway" instructs the daemon to drop
	// the --local flag and let openclaw route the turn through a Gateway (the
	// user's globally-configured one, or an endpoint pinned in the per-task
	// config wrapper that the daemon writes from execenv.OpenclawGatewayPin —
	// see server/internal/daemon/execenv/openclaw_config.go). Other backends
	// ignore this field, mirroring ThinkingLevel's renderer-side fall-through
	// pattern. See issue #3260.
	OpenclawMode string
}

// runContext derives the execution context for an agent subprocess from the
// configured per-run timeout. A positive timeout imposes a hard wall-clock
// deadline; a zero (or negative) timeout imposes NO deadline, leaving liveness
// entirely to the daemon's inactivity watchdog so a session that keeps emitting
// events is never killed merely for running long (MUL-3064). The caller owns
// the returned CancelFunc and must call it to release resources.
func runContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return context.WithCancel(ctx)
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
	ExecutablePath string            // path to CLI binary (claude, codebuddy, codex, copilot, opencode, openclaw, hermes, pi, cursor, kimi, kiro-cli, agy, qodercli)
	Env            map[string]string // extra environment variables
	Logger         *slog.Logger
}

// New creates a Backend for the given agent type.
// Supported types: "claude", "codebuddy", "codex", "copilot", "opencode", "openclaw", "hermes", "pi", "cursor", "kimi", "kiro", "antigravity", "qoder", "traecli".
//
// SupportedTypes is the canonical whitelist of agent types eligible to back a
// custom runtime profile. It MUST stay in lockstep with the
// runtime_profile.protocol_family CHECK constraint (migration 120, widened by
// migration 134 to add qoder): a custom runtime profile may only be based on a
// backend Multica officially supports. qoder is exposed here so Qoder CN
// (`qoderclicn`) users can point the Qoder backend at a non-default binary
// instead of misrouting through Kiro/ACP with incompatible arguments (#4883).
var SupportedTypes = []string{
	"claude",
	"codebuddy",
	"codex",
	"copilot",
	"opencode",
	"openclaw",
	"hermes",
	"pi",
	"cursor",
	"kimi",
	"kiro",
	"antigravity",
	"qoder",
}

// IsSupportedType reports whether agentType is in the SupportedTypes whitelist.
// Used to validate a custom runtime profile's protocol_family before it is
// persisted or registered.
func IsSupportedType(agentType string) bool {
	for _, t := range SupportedTypes {
		if t == agentType {
			return true
		}
	}
	return false
}

func New(agentType string, cfg Config) (Backend, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	switch agentType {
	case "claude":
		return &claudeBackend{cfg: cfg}, nil
	case "codebuddy":
		return &codebuddyBackend{cfg: cfg}, nil
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
	case "pi":
		return &piBackend{cfg: cfg}, nil
	case "cursor":
		return &cursorBackend{cfg: cfg}, nil
	case "kimi":
		return &kimiBackend{cfg: cfg}, nil
	case "kiro":
		return &kiroBackend{cfg: cfg}, nil
	case "antigravity":
		return &antigravityBackend{cfg: cfg}, nil
	case "qoder":
		return &qoderBackend{cfg: cfg}, nil
	case "traecli":
		return &traecliBackend{cfg: cfg}, nil
	default:
		return nil, fmt.Errorf("unknown agent type: %q (supported: claude, codebuddy, codex, copilot, opencode, openclaw, hermes, pi, cursor, kimi, kiro, antigravity, qoder, traecli)", agentType)
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
	"antigravity": "agy -p (non-interactive)",
	"claude":      "claude (stream-json)",
	"codebuddy":   "codebuddy (stream-json)",
	"codex":       "codex app-server",
	"copilot":     "copilot (json)",
	"cursor":      "cursor-agent (stream-json)",
	"hermes":      "hermes acp",
	"kimi":        "kimi acp",
	"kiro":        "kiro-cli acp",
	"openclaw":    "openclaw agent (json)",
	"opencode":    "opencode run (json)",
	"pi":          "pi (json mode)",
	"qoder":       "qodercli --acp",
	"traecli":     "traecli acp serve",
}

// LaunchHeader returns the user-visible launch skeleton for agentType, or an
// empty string if the type is unknown. Callers render this as a preview so
// users understand which command their custom_args get appended to.
func LaunchHeader(agentType string) string {
	return launchHeaders[agentType]
}
