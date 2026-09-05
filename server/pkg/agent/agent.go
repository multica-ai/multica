// Package agent provides a unified interface for executing prompts via
// coding agents (Claude Code, CodeBuddy, Codex, Copilot, OpenCode, DevEco Code,
// OpenClaw, Hermes, Pi, Oh-My-Pi, Cursor, Kimi, Reasonix, Kiro, Antigravity, Qoder,
// Trae, Grok, Qwen Code, QwenPaw, MiniMax Code). It
// mirrors the happy-cli AgentBackend pattern, translated to idiomatic Go.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
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
	// SystemPrompt carries the Multica runtime brief for the few providers
	// that cannot pick it up from disk. The daemon leaves it empty for every
	// other provider (see daemon.providerNeedsInlineSystemPrompt), because the
	// brief is already delivered as a per-task context file in the workdir —
	// CLAUDE.md, AGENTS.md, CODEBUDDY.md or QWEN.md depending on the runtime.
	//
	// A backend must therefore NOT assume this is populated, and adding a new
	// backend that only reads SystemPrompt will silently receive nothing.
	SystemPrompt              string
	ThreadName                string
	MaxTurns                  int
	Timeout                   time.Duration
	SemanticInactivityTimeout time.Duration
	// FirstTurnNoProgressTimeout optionally overrides the Codex first-turn
	// no-progress ceiling — the window a turn may stay completely silent after
	// the app-server reports turn/started before the watchdog fails it. Zero
	// keeps the provider default and the existing behaviour where
	// SemanticInactivityTimeout can only shrink that ceiling; a positive value
	// sets it explicitly, upward included. This answers a different question than
	// SemanticInactivityTimeout ("did the process ever start producing?" vs "has
	// a running turn gone quiet?"), so the two move independently. Currently
	// honoured by the codex backend (GH #3262).
	FirstTurnNoProgressTimeout time.Duration
	// IdleWatchdogTimeout optionally narrows the daemon's generic no-message
	// watchdog for this execution. Zero keeps the daemon-wide window, and a
	// value above that window cannot extend the global safety bound. The
	// daemon-wide zero still disables the watchdog entirely, and an in-flight
	// tool continues to use the separate tool watchdog budget.
	IdleWatchdogTimeout time.Duration
	// HandshakeTimeout bounds startup RPCs for providers with a long-lived
	// protocol transport. It is currently consumed by Codex app-server;
	// zero uses the provider default rather than disabling the bound.
	HandshakeTimeout time.Duration
	// ThreadHandshakeTimeout optionally gives Codex's heavier thread/start and
	// thread/resume RPCs a wider budget than initialize and turn/start. Zero
	// preserves the legacy behavior for callers that explicitly set
	// HandshakeTimeout; when both are zero Codex uses separate built-in defaults.
	ThreadHandshakeTimeout time.Duration
	ResumeSessionID        string // if non-empty, resume a previous agent session
	// ResumeExpected records that this task intended to continue a prior
	// conversation, independent of ResumeSessionID (which a fallback retry may
	// clear). When it is true but the backend ends up on a fresh thread — the
	// live resume RPC was rejected, or a transport failure forced a fresh retry —
	// the backend surfaces a continuity notice instead of silently
	// restarting. Currently honoured by the codex backend (MUL-4424).
	ResumeExpected bool
	// ResumeContinuityNotice is the text to prepend to the first turn when
	// ResumeExpected holds but the backend lands on a fresh thread anyway. The
	// caller owns the wording because only it knows what the surface lost — an
	// issue's comments and a Slack channel's history survive and can be re-read,
	// a web chat's and a Feishu channel's cannot — and that difference decides
	// whether the agent should tell the user anything at all (MUL-5722).
	//
	// Empty means say nothing, and the caller MUST leave it empty when its own
	// prompt already carries the notice. That is what keeps a turn from paying
	// for the same paragraph twice: the daemon injects it whenever it already
	// knows the resume is gone, and the backend covers only the case the daemon
	// cannot see — a live resume RPC rejected mid-run.
	ResumeContinuityNotice string
	// ExtraArgs is honoured only by backends that opt in by reading it; the
	// rest ignore it. Deliberately not enumerated here — the previous list
	// went stale as backends were added, which is how MULTICA_QWENPAW_ARGS
	// shipped plumbed but dropped. Grep for ExtraArgs to see today's set.
	ExtraArgs        []string        // daemon-wide default CLI arguments appended before CustomArgs
	CustomArgs       []string        // per-agent CLI arguments appended after ExtraArgs
	QwenpawWorkspace string          // per-task QwenPaw workspace directory (passed as --workspace to qwenpaw acp); empty when not applicable
	McpConfig        json.RawMessage // if non-nil, MCP server config to pass via --mcp-config
	// ThinkingLevel is the runtime-native reasoning/effort value (e.g.
	// Claude's "low|medium|high|xhigh|max", Codex's "none|minimal|low|
	// medium|high|xhigh", OpenCode's model variant names). Empty means
	// "use the runtime/model default" —
	// every backend that consumes this skips its --effort / reasoning_effort
	// injection so the upstream CLI's own default applies. Currently honoured
	// by the claude, codex, opencode, codebuddy, dsh, and grok (ACP
	// `--effort` on `grok agent`) backends; other backends ignore
	// the field rather than fail (so MUL-2339 can grow runtime support
	// incrementally without breaking unrelated agents).
	ThinkingLevel string
	// ServiceTier is a runtime-native Codex execution tier (for example
	// "priority", displayed as Fast). "default" explicitly selects standard
	// routing; empty means inherit local Codex config.
	// Other providers ignore this field.
	ServiceTier string
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
	// ClaudeSettingsPath is a daemon-owned, task-local settings file passed
	// through Claude Code's --settings flag. It currently carries restrictive
	// runtime-skill overrides only; other providers ignore it.
	ClaudeSettingsPath string
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
	// WaitingUntil defers the idle watchdog during a confirmed native scheduled
	// wait. The normal idle budget resumes at this time; hard timeouts still apply.
	WaitingUntil time.Time
}

// TokenUsage tracks token consumption for a single model.
type TokenUsage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	// CostUSDTicks is the provider's own statement of what this usage cost,
	// in ticks of 1e-10 USD. Zero means "not reported" — only a few agents
	// return it (xAI Grok Build does, via `_meta.usage.costUsdTicks`).
	//
	// It matters because a token-times-rate estimate cannot reproduce
	// request-level pricing rules. xAI bills a request at 2x once its prompt
	// reaches 200K tokens, and a usage record aggregates every model call in
	// a turn — so the stored token counts cannot say which tier any single
	// request hit. The provider's own figure already has that priced in.
	CostUSDTicks int64
}

// CostUSDTicksPerUSD is the scale of the provider-reported cost unit: xAI
// reports whole ticks of 1e-10 USD, which keeps sub-cent turn costs exact in
// int64 all the way to the database instead of drifting through float64.
const CostUSDTicksPerUSD = 10_000_000_000

// Result is the final outcome after an agent session completes.
type Result struct {
	Status     string // "completed", "failed", "aborted", "timeout", "cancelled"
	Output     string // final user-facing output selected by the backend
	Error      string // error message if failed
	DurationMs int64
	SessionID  string
	Usage      map[string]TokenUsage // keyed by model name
	// ResumeRejected is positive evidence that this run's requested resume
	// was permanently refused — the transcript is gone, the session belongs to
	// another provider account, OR the session still exists but its history
	// can no longer be replayed to the provider (e.g. GH #5975: a stored
	// image now exceeds the provider's max dimensions, so every resumed
	// session/prompt is rejected before the turn runs). What unites these is
	// that the resume CANNOT continue and only starting over can cure it, so
	// it is what the daemon's fresh-session fallback looks for first. Note the
	// last case keeps a non-empty SessionID (the id is real, only its history
	// is unusable) — the daemon gates on this boolean, not an empty id.
	//
	// false is NOT evidence of the opposite. For a backend listed in
	// ResumeRejectionUndetectable it means "could not tell"; for every other
	// backend it means "checked, and this was not a rejection". The daemon
	// needs the provider name to tell those apart — see
	// shouldRetryWithFreshSession in internal/daemon.
	//
	// Backends must NOT set it for failures a new session cannot cure:
	// network drops, rate limits, quota, provider 5xx, or auth errors. Those
	// keep the session pointer so the platform's own retry can resume the
	// truncated conversation (see retryableReasons in internal/service/task.go).
	//
	// The auth exclusion above stands even for the one auth error a fresh
	// session DOES cure — a resumed session whose persisted provider identity
	// can no longer resolve its credentials (GH #6777). An adapter cannot tell
	// that apart from a genuinely bad credential by looking at the error, so
	// the judgement is made once, provider-agnostically, in
	// shouldRetryWithFreshSession, where "was this run a resume?" is already
	// known. Do not encode it here.
	ResumeRejected bool
	// ResumeRejectedTransient is positive evidence that this run's requested
	// resume cannot proceed right now, but the session itself remains healthy.
	// The daemon may use a fresh session for this turn, but must not retire the
	// requested session from future lookups. Backends must not set this together
	// with ResumeRejected; the latter means the session is permanently unusable.
	ResumeRejectedTransient bool
	// codexInitializeRetrySafe is provider-internal evidence that an
	// initialize timeout happened before semantic activity and after the
	// process tree was reaped. It is intentionally not part of the public
	// result contract.
	codexInitializeRetrySafe bool
	// codexStartupRefreshRetrySafe is provider-internal evidence that the
	// first turn produced no semantic progress because Codex could not load
	// its model catalog, and that the process tree was reaped afterwards.
	// Like codexInitializeRetrySafe it is not part of the public contract.
	codexStartupRefreshRetrySafe bool
}

// Config configures a Backend instance.
type Config struct {
	ExecutablePath string            // path to CLI binary (claude, codebuddy, codex, copilot, opencode, codearts, openclaw, hermes, pi, cursor, kimi, reasonix, dsh, kiro-cli, agy, qodercli, qoderclicn, traecli, grok, qwen, qwenpaw, mcode, dim, zeroclaw)
	CLIVersion     string            // detected version paired with ExecutablePath; observation only, never used to choose behavior
	Env            map[string]string // extra environment variables
	Logger         *slog.Logger
	TaskID         string
	RuntimeID      string
	DaemonVersion  string
	CodexVersion   string
	// BuiltinRuntime reports that ExecutablePath is the provider's own
	// discovered binary rather than a custom runtime profile's command. A
	// custom profile keeps its protocol family as the provider, so the
	// provider name cannot distinguish the two: `protocol_family: hermes`
	// with `command_name: jcode` arrives as "hermes" while being an
	// unrelated implementation. Backends use this to scope
	// compatibility exceptions that were verified against a specific
	// vendor's binary; it defaults to false so an unset caller fails
	// closed onto standard behavior.
	BuiltinRuntime bool
	// provider is the runtime/provider identity used in safe launch logs. New
	// fills it from the protocol family; NewRuntime preserves the concrete
	// built-in runtime identity instead (for example omp rather than pi).
	provider string
	// LaunchPrefix is the argv prefix that belongs to ExecutablePath itself —
	// a custom runtime profile's fixed_args. It is spliced in directly after
	// the executable, ahead of every argument a backend builds, because a
	// wrapper's subcommand has to be consumed before the wrapped CLI's own
	// flags mean anything (`ccms start q36` then `-p …`, GH #7046).
	//
	// Unlike ExtraArgs this is not opt-in: New filters it once and the
	// Command boundary applies it to every process the package spawns, task
	// launches and CLI probes alike. Backends never read it directly.
	LaunchPrefix []string
}

// New creates a Backend for the given agent type.
// Supported types: "claude", "codebuddy", "codex", "copilot", "opencode", "codearts", "deveco", "openclaw", "hermes", "pi", "cursor", "kimi", "reasonix", "dsh", "kiro", "antigravity", "qoder", "qoderclicn", "traecli", "grok", "qwen", "qwenpaw", "mcode".
//
// SupportedTypes is the canonical whitelist of agent types eligible to back a
// custom runtime profile. It MUST stay in lockstep with the
// runtime_profile.protocol_family CHECK constraint (migration 120, widened by
// migration 134 to add qoder, migration 136 to add traecli, migration 175 to
// add deveco, migration 179 to add grok, migration 202 to add qwen,
// migration 242 to add qoderclicn, migration 253 to add qwenpaw,
// migration 254 to add reasonix, migration 313 to add dsh, migration 342 to
// add mcode, migration 370 to add dim, migration 403 to add zeroclaw, and
// migration 441 to add codearts): a custom runtime profile may
// only be based on a backend Multica officially supports.
// qoder and qoderclicn share the same ACP backend; keeping both provider keys
// lets the daemon auto-detect and register the international and China-region
// binaries independently. traecli (Trae) has a New backend, launch
// header and provider branding but was previously missing from this whitelist,
// so the family picker rejected it (#4945). grok is the xAI Grok Build CLI
// ACP backend (`grok agent --always-approve stdio`). qwen is Qwen Code's
// native `qwen -p <prompt> --output-format stream-json` backend.
var SupportedTypes = []string{
	"claude",
	"codebuddy",
	"codex",
	"copilot",
	"opencode",
	"codearts",
	"deveco",
	"openclaw",
	"hermes",
	"pi",
	"cursor",
	"kimi",
	"reasonix",
	"dsh",
	"kiro",
	"antigravity",
	"qoder",
	"qoderclicn",
	"traecli",
	"grok",
	"qwen",
	"qwenpaw",
	"mcode",
	"dim",
	"zeroclaw",
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

// resumeRejectionUndetectable lists the backends that cannot produce
// Result.ResumeRejected at all. They scrape SessionID out of stream output and
// have no rejection detection: no phrase match, no structured error code, no
// internal restart. copilot's own comment documents the hole (a session.error
// arriving before session.start leaves SessionID empty), and antigravity's
// conversation-id reader returns "" whenever the CLI exits before dispatching.
//
// Membership is deliberately opt-in. A backend absent from this map is treated
// as capable, so a new backend fails closed — it reports no rejection and gets
// no fallback — rather than silently inheriting a guess-based retry. Remove an
// entry as soon as its backend learns to report rejections.
var resumeRejectionUndetectable = map[string]bool{
	"antigravity": true,
	"copilot":     true,
	"cursor":      true,
	"deveco":      true,
	"opencode":    true,
	"codearts":    true,
}

// ResumeRejectionUndetectable reports whether agentType is a backend that
// cannot tell a refused resume from any other startup failure. Callers use it
// to read a false Result.ResumeRejected correctly: "could not tell" for these,
// "checked, not a rejection" for everything else.
func ResumeRejectionUndetectable(agentType string) bool {
	return resumeRejectionUndetectable[agentType]
}

func New(agentType string, cfg Config) (Backend, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.provider == "" {
		cfg.provider = agentType
	}
	// Filter the launch prefix here, at the one point that knows both the
	// prefix and the protocol family. Doing it per-backend would be the same
	// opt-in arrangement that let ExtraArgs rot: a family that forgot the call
	// would accept a fixed_args `--output-format text` and break its own
	// stream-json channel.
	cfg.LaunchPrefix = filterLaunchPrefix(cfg.LaunchPrefix, agentType, cfg.Logger)

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
	case "codearts":
		return newCodeArtsBackend(cfg)
	case "deveco":
		return &devecoBackend{cfg: cfg}, nil
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
	case "reasonix":
		return &reasonixBackend{cfg: cfg}, nil
	case "dsh":
		return &dshBackend{cfg: cfg}, nil
	case "dim":
		return &dimBackend{cfg: cfg}, nil
	case "kiro":
		return &kiroBackend{cfg: cfg}, nil
	case "antigravity":
		return &antigravityBackend{cfg: cfg}, nil
	case "qoder", "qoderclicn":
		return &qoderBackend{cfg: cfg, defaultExecutable: qoderDefaultBinary(agentType)}, nil
	case "traecli":
		return &traecliBackend{cfg: cfg}, nil
	case "grok":
		return &grokBackend{cfg: cfg}, nil
	case "qwen":
		return &qwenBackend{cfg: cfg}, nil
	case "qwenpaw":
		return &qwenpawBackend{cfg: cfg}, nil
	case "mcode":
		return &mcodeBackend{cfg: cfg}, nil
	case "zeroclaw":
		return &zeroclawBackend{cfg: cfg}, nil
	default:
		return nil, fmt.Errorf("unknown agent type: %q (supported: %s)", agentType, strings.Join(SupportedTypes, ", "))
	}
}

// DetectVersion runs the agent CLI with --version and returns the output.
//
// cmd carries the runtime's launch prefix, so a custom profile is probed the
// way it is launched: `ccms start q36 --version` reports the version of the
// CLI the wrapper actually execs, where a bare `ccms --version` would report
// the wrapper's own and pin the runtime to the wrong compatibility policy.
func DetectVersion(ctx context.Context, cmd Command) (string, error) {
	return detectCLIVersion(ctx, cmd)
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
	"codearts":    "codearts run (json)",
	"deveco":      "deveco run (json)",
	"hermes":      "hermes acp",
	"kimi":        "kimi acp",
	"reasonix":    "reasonix acp",
	"dsh":         "dsh --profile multica (stdio)",
	"kiro":        "kiro-cli acp",
	"openclaw":    "openclaw agent (json)",
	"opencode":    "opencode run (json)",
	"pi":          "pi (json mode)",
	"qoder":       "qodercli --acp",
	"qoderclicn":  "qoderclicn --acp",
	"traecli":     "traecli acp serve",
	"grok":        "grok agent stdio",
	"qwen":        "qwen -p (stream-json)",
	"qwenpaw":     "qwenpaw acp",
	"dim":         "dim acp",
	"mcode":       "mcode acp",
	"zeroclaw":    "zeroclaw acp",
}

// LaunchHeader returns the user-visible launch skeleton for agentType, or an
// empty string if the type is unknown. Callers render this as a preview so
// users understand which command their custom_args get appended to.
func LaunchHeader(agentType string) string {
	if h := launchHeaders[agentType]; h != "" {
		return h
	}
	// Built-in runtime identities derive their launch header from the
	// descriptor, not the protocol-family map.
	if desc, ok := BuiltinRuntimeByID(agentType); ok {
		return desc.LaunchHeader
	}
	return ""
}
