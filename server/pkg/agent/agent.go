// Package agent provides a unified interface for executing prompts via
// coding agents (Claude Code, Codex, Copilot, OpenCode, OpenClaw,
// Hermes, Gemini, Pi, Cursor, Kimi, Kiro, Antigravity). It mirrors the happy-cli
// AgentBackend pattern, translated to idiomatic Go.
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
	ExecutablePath string            // path to CLI binary (claude, codex, copilot, opencode, openclaw, hermes, gemini, pi, cursor, kimi, kiro-cli, agy)
	Env            map[string]string // extra environment variables
	Logger         *slog.Logger
	// Transport is "acp-stdio" or "stream-json" for external runtime
	// extensions; empty for built-in backends. Set by the daemon from
	// runtime.json. The factory routes external entries to the matching
	// generic backend (ACP or stream-json) when this field is set.
	Transport string
	// ACPArgs are extra CLI arguments appended after the daemon's own argv
	// (e.g. ["--acp"]). Sourced from runtime.json command.args. The name is
	// kept for backwards-compat — they are appended for stream-json runtimes
	// too.
	ACPArgs []string
	// IsExternal is true when the backend was loaded from a runtime.json
	// extension rather than being a built-in provider.
	IsExternal bool
	// BlockedArgs is the set of flags external runtime extensions refuse
	// from custom_args (translated from runtime.json command.blocked_args).
	// Each value must be either "value" (flag takes a separate value) or
	// "flag" (boolean flag, no value). Built-in backends ignore this and
	// use their hard-coded blocked sets instead.
	BlockedArgs map[string]string
	// SkillsRoot is the manifest-declared local skills directory to expose
	// to the spawned CLI via MULTICA_AGENT_SKILLS_ROOT. Empty means the
	// runtime relies on its native skill discovery path.
	SkillsRoot string
	// Capabilities mirrors the manifest capability flags so the wire layer
	// can decide whether to forward optional ACP / stream-json parameters
	// (mcp config, thinking level, max turns, session resume).
	Capabilities ConfigCapabilities
}

// ConfigCapabilities is the subset of RuntimeManifestCaps the agent package
// needs at task spawn time. It avoids importing the daemon package (which
// would create a cycle) by re-declaring the few flags the wire layer reads.
type ConfigCapabilities struct {
	Thinking       bool
	McpConfig      bool
	SessionResume  bool
	MaxTurns       bool
	ModelSelection bool
}

// New creates a Backend for the given agent type.
// Supported types: "claude", "codex", "copilot", "opencode", "openclaw", "hermes", "gemini", "pi", "cursor", "kimi", "kiro", "antigravity".
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
	case "antigravity":
		return &antigravityBackend{cfg: cfg}, nil
	default:
		// External runtime extensions (loaded from runtime.json) use one
		// of the supported transports. The transport field in Config
		// determines which generic external backend handles execution.
		switch cfg.Transport {
		case "acp-stdio":
			return &acpExternalBackend{cfg: cfg}, nil
		case "stream-json":
			return &streamJSONExternalBackend{cfg: cfg}, nil
		}
		if cfg.IsExternal {
			// Backwards-compat: external entries with no explicit
			// transport default to ACP, matching schema v1 behaviour.
			return &acpExternalBackend{cfg: cfg}, nil
		}
		return nil, fmt.Errorf("unknown agent type: %q (supported: claude, codex, copilot, opencode, openclaw, hermes, gemini, pi, cursor, kimi, kiro, antigravity, or runtime extensions with transport=acp-stdio|stream-json)", agentType)
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
	"antigravity": "agy -p (print mode)",
	"claude":      "claude (stream-json)",
	"codex":       "codex app-server",
	"copilot":     "copilot (json)",
	"cursor":      "cursor-agent (stream-json)",
	"gemini":      "gemini (stream-json)",
	"hermes":      "hermes acp",
	"kimi":        "kimi acp",
	"kiro":        "kiro-cli acp",
	"openclaw":    "openclaw agent (json)",
	"opencode":    "opencode run (json)",
	"pi":          "pi (json mode)",
}

// LaunchHeader returns the user-visible launch skeleton for agentType, or an
// empty string if the type is unknown. Callers render this as a preview so
// users understand which command their custom_args get appended to.
func LaunchHeader(agentType string) string {
	return launchHeaders[agentType]
}
