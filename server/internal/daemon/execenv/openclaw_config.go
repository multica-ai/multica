package execenv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// openclawConfigFile is the per-task synthesized OpenClaw config the daemon
// points the openclaw CLI at via OPENCLAW_CONFIG_PATH. It sits in the env
// root (alongside workdir/, output/, logs/) so the GC reaper sweeps it with
// the rest of the task env.
const openclawConfigFile = "openclaw-config.json"

// openclawMcpResetFile holds exactly `{"mcp":{"servers":null}}` and is included
// after the user's live config whenever the agent has a managed mcp_config.
//
// One key, and nothing else, is the whole design. OpenClaw's include merge is
// recursive for objects and replacing for a primitive or null source, so nulling
// `mcp.servers` clears the user's server map while every sibling under `mcp`
// continues to arrive from the live config untouched — this code never reads,
// rewrites or re-emits them. That is what keeps a sibling this daemon has never
// heard of correct, and what makes it impossible to write a redacted display
// value back over a working one.
//
// The wrapper's own `mcp.servers` then merges over the include result and becomes
// the only server definition. Because the reset is written only when the wrapper
// carries that key, the null cannot survive to the resolved root, where
// OpenClaw's schema would reject it (zod `.strict().optional()` rejects null).
// TestPrepareOpenclawConfigResetStagePairsWithWrapperMcp pins that pairing.
//
// Lives in envRoot at 0o600 next to the wrapper. It carries no user data at all.
const openclawMcpResetFile = "openclaw-mcp-reset.json"

// openclawMcpResetBody is that file's entire content. A literal rather than a
// marshalled map: the point of this stage is that it names exactly one key, and a
// literal is the form a reader can check against the design in one glance.
const openclawMcpResetBody = "{\n  \"mcp\": {\n    \"servers\": null\n  }\n}\n"

// openclawCLITimeout is the default context deadline set on each
// `openclaw config ...` invocation during task setup.
//
// It used to be 5s, on the assumption that the CLI answers in <200ms and 5s is
// pure cold-start headroom. Field data (#7112) retired that assumption: on a
// 2020 Intel MacBook Pro, `openclaw config file` takes 8.4–10.9s and
// `config get agents.list --json` 4.3s, every time — so the runtime was
// unusable on that host, with no user-side way to raise the limit. For scale,
// the same commands take 0.7s / 0.3s on an M-series Mac, i.e. the real spread
// across supported hardware is wider than the old margin.
//
// 30s is ~3x the slowest measured call, and even the worst case
// (openclawMaxCLIDeadlinesPerPreparation serial steps at that budget) fits
// inside the outer 5-minute task preparation deadline, so a genuinely hung CLI
// fails with this specific, actionable reason instead of the generic prepare
// timeout. Hosts outside that envelope can override with
// MULTICA_OPENCLAW_CLI_TIMEOUT (or backends.openclaw.cli_timeout in the CLI
// config, which the daemon translates into the same env var).
//
// The gap that used to be documented here — that this was a deadline and not a
// cap — is closed as of MUL-5467. Keeping the measurements, because they are
// what the fix has to hold against: CommandContext kills only the direct child,
// and cmd.Output() blocks in Wait() until the stdout pipe closes, so a CLI that
// leaves a descendant holding stdout ran for the descendant's lifetime.
// Measured on linux/dash: a shim whose backgrounded child slept 6s took 6.01s
// against a 150ms deadline. An npm shim is that shape on Windows (cmd.exe →
// node). A cmd.WaitDelay backstop bounded the call but left the descendant
// running (measured: returns in 2.17s with the grandchild still in state S),
// trading a hang for a process leak, and on Unix nothing reaped it because
// preparationProcessController.finish() is a no-op there.
//
// execOpenclawCLI goes through agent.RunCollectQuiet, which owns the pipes and
// the process tree (Unix process group, Windows Job Object). Owning the pipes is
// what makes the deadline enforceable: os/exec's own Wait cannot return while a
// descendant holds an output pipe, so the bound has to come from somewhere else,
// and a caller-side bound that reports failure would fail a call whose answer
// arrived. Owning the tree is what stops the descendant becoming an orphan.
//
// Two limits on that claim, both deliberate:
//
//   - "Enforceable", not "nothing survives": `openclaw-config` was measured with
//     its own PGID and SID, so on Unix no group signal reaches it. The deadline
//     holds anyway, because it is pipe ownership rather than the kill that makes
//     the call return.
//   - Returning before the CLI exits requires a completeness rule that the CLI's
//     pre-answer output cannot satisfy, which in practice means `--json` (see
//     openclawOutputComplete). Without one — `config file` as the fallback — this
//     deadline is what the call is bounded by, and reaching it is a failure. That
//     is a deliberate trade: an earlier revision judged `config file`'s stdout by
//     shape and review showed it returning a path-shaped *warning* line as the
//     answer.
const openclawCLITimeout = 30 * time.Second

// OpenclawCLITimeoutEnv overrides openclawCLITimeout. Accepts a Go duration
// ("45s", "2m") or a bare number of seconds ("45"). Values outside
// [openclawCLIMinTimeout, openclawCLIMaxTimeout] are clamped, and anything
// unparseable is ignored, so a typo degrades to the default instead of
// disabling the deadline.
const OpenclawCLITimeoutEnv = "MULTICA_OPENCLAW_CLI_TIMEOUT"

const (
	// openclawCLIMinTimeout keeps an override from being so small that no real
	// CLI can answer; 1s is already below every measured healthy host.
	openclawCLIMinTimeout = time.Second
	// openclawCLIMaxTimeout keeps config discovery inside the outer task
	// preparation budget (daemon.defaultTaskPrepareTimeout, 5 minutes). The
	// worst case is openclawMaxCLIDeadlinesPerPreparation serial steps, so the
	// ceiling is set so that even then (3 x 60s = 3m) the failure surfaces as a
	// specific, actionable CLI timeout with room to spare, instead of colliding
	// with the outer deadline and collapsing into the generic — and retryable —
	// prepare-timeout reason. A step may make more than one invocation — path
	// resolution falls back from `config validate --json` to `config file` under
	// one shared deadline — which is why the multiplier counts deadlines.
	openclawCLIMaxTimeout = 60 * time.Second
)

// openclawMaxCLIDeadlinesPerPreparation is how many CLI deadlines one task
// preparation can consume in the worst case. Deadlines, not invocations: each
// deadline bounds one *step*, and it is the sum of the steps that has to fit
// inside the outer preparation budget, so this is the multiplier
// openclawCLIMaxTimeout is derived from.
//
// The three steps, in the order they can fire:
//
//  1. locate the active config — `config validate --json`, then `config file`
//     if that did not answer. Two invocations, one deadline: they ask the same
//     question and openclawActiveConfigPath shares a context between them
//     precisely so the fallback cannot add a fourth budget.
//  2. `config get agents.list --json`   — pre-2026.6 agents schema
//  3. `agents list --json`              — 2026.6+ registry fallback, only
//     reached when (2) reports the config path is missing
//
// It was four until managed MCP stopped reading the user's config: the reset
// stage is a file this package writes, so a managed-MCP agent now costs the same
// CLI time as any other. Adding a fourth deadline-bearing step means re-deriving
// the ceiling. The worst-case test counts distinct deadlines rather than calls,
// so a new invocation that shares an existing budget is free and one that brings
// its own fails loudly.
const openclawMaxCLIDeadlinesPerPreparation = 3

// ErrOpenclawCLITimeout marks a task preparation that failed because the local
// openclaw CLI did not answer within the deadline. It is a sentinel rather
// than a message-matched string for a reason: the daemon classifies on it
// structurally (taskRunFailureReason), and the old text-matching path routed
// every "deadline exceeded" into agent_error.provider_network — telling the
// user to check their network for a purely local stall, and auto-retrying a
// failure that is deterministic on the affected host.
//
// Preparation runs in a helper process, so the sentinel is re-attached on the
// daemon side of that boundary; see preparationErrorKindOpenclawCLITimeout.
var ErrOpenclawCLITimeout = errors.New("openclaw cli timeout")

// openclawCLITimeoutError carries the CLI's own diagnostic text while still
// matching both ErrOpenclawCLITimeout (our classifier) and the wrapped
// context error (callers that check cancellation the standard way).
type openclawCLITimeoutError struct {
	msg   string
	cause error
}

func (e *openclawCLITimeoutError) Error() string { return e.msg }

func (e *openclawCLITimeoutError) Unwrap() []error {
	return []error{e.cause, ErrOpenclawCLITimeout}
}

// resolveOpenclawCLITimeout picks the deadline for one CLI invocation:
// explicit (tests) > MULTICA_OPENCLAW_CLI_TIMEOUT > openclawCLITimeout.
func resolveOpenclawCLITimeout(explicit time.Duration, logger *slog.Logger) time.Duration {
	if explicit > 0 {
		return explicit
	}
	raw := strings.TrimSpace(os.Getenv(OpenclawCLITimeoutEnv))
	if raw == "" {
		return openclawCLITimeout
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		// Bare number means seconds. Parsed as a duration string rather than
		// Atoi * time.Second, which overflows into a negative (or absurdly
		// large) duration for big inputs instead of failing cleanly.
		if secondsParsed, secErr := time.ParseDuration(raw + "s"); secErr == nil {
			parsed, err = secondsParsed, nil
		}
	}
	if err != nil || parsed <= 0 {
		if logger != nil {
			logger.Warn("execenv: ignoring unusable openclaw CLI timeout override; using default",
				"env", OpenclawCLITimeoutEnv, "value", raw, "default", openclawCLITimeout)
		}
		return openclawCLITimeout
	}
	clamped := min(max(parsed, openclawCLIMinTimeout), openclawCLIMaxTimeout)
	if clamped != parsed && logger != nil {
		logger.Warn("execenv: clamping openclaw CLI timeout override",
			"env", OpenclawCLITimeoutEnv, "value", raw, "applied", clamped)
	}
	return clamped
}

// OpenclawConfigPrep is the input to prepareOpenclawConfig. Only OpenclawBin
// and CacheDir are meaningful in production — Timeout is here for tests that
// need a tight deadline to assert error paths.
type OpenclawConfigPrep struct {
	// OpenclawBin is the openclaw CLI binary to invoke for config introspection.
	// Empty means resolve "openclaw" from PATH at exec time.
	OpenclawBin string
	// Timeout sets the context deadline for each CLI invocation — not a
	// guaranteed cap on how long the call takes; see openclawCLITimeout. Zero
	// falls back to the MULTICA_OPENCLAW_CLI_TIMEOUT override, then to
	// openclawCLITimeout.
	Timeout time.Duration
	// CacheDir is the directory holding this daemon profile's shared
	// discovery cache. Empty disables caching entirely — every task then pays
	// the full CLI cost, which is correct but slow. See
	// openclaw_config_cache.go for what is cached and how it is invalidated.
	CacheDir string
	// McpConfig is the agent's saved `mcp_config` JSON (Claude-style
	// `{"mcpServers": {"<name>": {...}}}`). When non-null the wrapper pins
	// `mcp.servers` to the managed set so OpenClaw resolves MCP from the
	// daemon's authoritative list instead of the user's global `mcp.servers`.
	// Null / empty means inherit the user's global config — same three-state
	// semantics codex uses (`hasManagedCodexMcpConfig`).
	McpConfig json.RawMessage
	// Gateway pins a specific OpenClaw Gateway endpoint inside the per-task
	// wrapper. Only consulted when the agent is configured for gateway-mode
	// openclaw (see ExecOptions.OpenclawMode); zero means "inherit whatever
	// the user's global openclaw.json already configures under `gateway.*`"
	// — which is the right default when the user already has a working
	// gateway set up locally. See issue #3260.
	Gateway OpenclawGatewayPin
	// Logger records the config-discovery outcome. Optional; nil disables
	// logging. Discovery used to be entirely silent, which is why #6630 —
	// a wrapper written without `$include` — could only be diagnosed by
	// reading the generated file and reverse-engineering the daemon. Paths
	// and booleans are logged; config contents never are.
	Logger *slog.Logger
}

// OpenclawGatewayPin describes the Gateway endpoint a per-task openclaw
// wrapper should pin. Fields mirror OpenClaw's own `gateway.*` config shape
// (see ~/.openclaw/openclaw.json). All fields are optional; only non-zero
// fields are emitted into the wrapper so a partial pin (e.g. host+port
// only, token left to inherit from the user's config) does the right
// thing under OpenClaw's deep-merge $include semantics.
type OpenclawGatewayPin struct {
	Host  string `json:"host,omitempty"`
	Port  int    `json:"port,omitempty"`
	Token string `json:"token,omitempty"`
	TLS   bool   `json:"tls,omitempty"`
}

// IsZero reports whether every field is zero, i.e. there is nothing to pin.
func (p OpenclawGatewayPin) IsZero() bool {
	return p == OpenclawGatewayPin{}
}

// String masks the bearer token when the pin is rendered as a string —
// `%v` / `%+v` / direct `fmt.Stringer` use cases all go through here. The
// raw Token field still exists for the wrapper-config emitter that needs
// it; this is a belt against a future caller that logs a whole task-prep
// summary at a level a non-admin can see (issue #3260 CR).
func (p OpenclawGatewayPin) String() string {
	tok := ""
	if p.Token != "" {
		tok = "***"
	}
	return fmt.Sprintf("OpenclawGatewayPin{Host:%q Port:%d Token:%s TLS:%t}", p.Host, p.Port, tok, p.TLS)
}

// MarshalJSON masks the bearer token in any default JSON dump (debug
// endpoints, error envelopes, structured-log encoders). The wrapper config
// writer goes through buildGatewayOverride, and the private preparation-helper
// transport uses its own methodless wire view, so both retain the real token.
func (p OpenclawGatewayPin) MarshalJSON() ([]byte, error) {
	type alias struct {
		Host  string `json:"host,omitempty"`
		Port  int    `json:"port,omitempty"`
		Token string `json:"token,omitempty"`
		TLS   bool   `json:"tls,omitempty"`
	}
	masked := alias{Host: p.Host, Port: p.Port, TLS: p.TLS}
	if p.Token != "" {
		masked.Token = "***"
	}
	return json.Marshal(masked)
}

// OpenclawConfigResult is what prepareOpenclawConfig returns to its callers
// in execenv.go. ConfigPath is the wrapper file the daemon points
// OPENCLAW_CONFIG_PATH at. IncludeRoot is the directory the daemon must add
// to OPENCLAW_INCLUDE_ROOTS so OpenClaw will follow the $include link out
// of envRoot into the user's active config; it is empty when no $include
// is emitted (fresh install).
type OpenclawConfigResult struct {
	ConfigPath  string
	IncludeRoot string
}

// prepareOpenclawConfig writes a per-task OpenClaw config to envRoot and
// returns its absolute path along with the include root the daemon must
// grant. The daemon sets OPENCLAW_CONFIG_PATH to the path on the spawned
// openclaw subprocess so the CLI resolves its `agents.defaults.workspace`
// (and every `agents.list[].workspace`) to the task workdir — which is
// what makes OpenClaw's native skill scanner pick up the per-task skills
// we write under `<workDir>/skills/`.
//
// Strategy: delegate JSON5 / $include / env-substitution / state-dir
// resolution to the openclaw CLI itself rather than re-implementing the
// spec. We:
//
//  1. Run `openclaw config file` to find the user's active config path.
//     For OpenClaw releases whose `config` command rejects the `file`
//     subcommand shape, fall back to resolving OpenClaw's active-config
//     candidates, including legacy Clawdbot/Moltbot/Moldbot locations.
//  2. Run `openclaw config get agents.list --json` to enumerate every
//     registered agent ID with its resolved fields. The CLI parses JSON5,
//     follows $include, and substitutes ${VAR} for us.
//  3. Write a wrapper config to envRoot/openclaw-config.json that
//     `$include`s the active path and overrides
//     `agents.defaults.workspace` plus every `agents.list[].workspace` to
//     workDir. The original config bytes are not mutated — they are loaded
//     by openclaw's own loader through the $include link, which preserves
//     comments, secrets, and nested $include chains verbatim.
//
// **Cross-directory $include confinement.** OpenClaw confines `$include`
// resolution to the directory containing the wrapper file unless the
// target's parent is listed in `OPENCLAW_INCLUDE_ROOTS`. Our wrapper lives
// in envRoot but $includes the user's active config (typically
// `~/.openclaw/openclaw.json`) — a cross-directory hop. We surface
// `filepath.Dir(activePath)` as IncludeRoot so the daemon can prepend it
// to whatever the user already has in OPENCLAW_INCLUDE_ROOTS; without
// this, OpenClaw refuses to follow the link and the wrapper boots with no
// user config. Fresh install emits no $include, so IncludeRoot is "".
//
// **Intentional task isolation.** The override of every per-agent workspace
// is deliberate. OpenClaw's resolution order is
// `agents.list[id].workspace → agents.defaults.workspace → ~/.openclaw/
// workspace`. Pinning only the default would let a per-agent workspace the
// user configured at host scope silently re-route the scanner back to the
// shared workspace, defeating the per-task skill discovery this whole flow
// exists for. The cost is that any per-agent SOUL.md / MEMORY.md / standing
// orders the user laid in `<host-agent-workspace>/` are NOT visible to the
// in-task openclaw run — task isolation wins over host carry-over. The
// user's on-disk config is untouched; this only affects the wrapper used
// for this single task.
//
// **Fail closed.** Missing openclaw binary, CLI errors, malformed CLI
// output, or any IO error during write surfaces as an error to the caller
// rather than degrading to a minimal config. An earlier version silently
// synthesized a minimal config on parse failure; that masked broken user
// configs by starting OpenClaw without the registered agents / model
// providers / API keys it expects, which led to tasks routing to the wrong
// agent or failing to authenticate. The only "synthesize minimal" case
// kept is a fresh install where the CLI reports a path but no file exists
// — there is no user data to lose in that case.
func prepareOpenclawConfig(envRoot, workDir string, opts OpenclawConfigPrep) (OpenclawConfigResult, error) {
	bin := opts.OpenclawBin
	if bin == "" {
		bin = "openclaw"
	}
	timeout := resolveOpenclawCLITimeout(opts.Timeout, opts.Logger)

	activePath, exists, resolvedList, agentsFromRegistry, cached, err := discoverOpenclawConfig(bin, timeout, opts)
	if err != nil {
		return OpenclawConfigResult{}, err
	}
	if !exists && opts.Logger != nil {
		// Not an error — a genuine fresh install lands here legitimately.
		// But it is also where a failed discovery lands, and the two are
		// indistinguishable from the outside, so say so loudly: every task
		// prepared from this point runs without the user's model providers
		// and auth profiles.
		opts.Logger.Warn("execenv: openclaw active config not found; task wrapper will omit $include so the user's models and auth profiles will NOT be visible to this task",
			"reported_path", activePath)
	}

	// Parse the agent's managed mcp_config (if any) before writing the wrapper
	// so a malformed value fails the prepare step rather than crashing the
	// openclaw subprocess later. Same fail-closed posture as Codex's
	// ensureCodexMcpConfig — silent fallback to the user's global mcp.servers
	// would be indistinguishable from "the managed set applied" and is exactly
	// the surprise the MCP Tab is supposed to remove.
	managedMcp, hasManagedMcp, err := openclawManagedMcpServers(opts.McpConfig)
	if err != nil {
		return OpenclawConfigResult{}, fmt.Errorf("render openclaw mcp_config: %w", err)
	}

	// **Strict replace for managed mcp_config.** When the agent has a managed
	// set, deep-merging the wrapper's `mcp.servers` against the user's active
	// config via `$include` would let user-only entries leak in (and an empty
	// managed set would not actually clear inherited servers).
	//
	// What this used to do instead was read the user's whole resolved config
	// through `config get --json` (no key path), strip `mcp.servers`, and write
	// the remainder into the task directory as a snapshot the wrapper included.
	// Two things were wrong with it, and only the first is why the task failed:
	//
	//   - The read is not a valid invocation. `config get` requires a path, and
	//     every channel measured on 2026-08-26 rejects the pathless form:
	//     2026.6.34 (extended-stable) exit 1 in 2.9s with
	//     `Missing required argument "path".`, 2026.7.1-2 (latest) the same in
	//     3.0s, 2026.8.1-beta.3 (beta) the same as a JSON envelope in 4.4s. So
	//     preparation failed for every managed-MCP task on every current channel.
	//     (2026.5.27 instead wrote zero bytes and never exited within 60s;
	//     #6275 made that shape fail predictably rather than hang, which is why
	//     it is adjacent to this fix and not a substitute for it.)
	//   - Even answered, the result is display data rather than a lossless
	//     round-trip: `runConfigGet` applies `redactConfigObject`, so a sensitive
	//     value comes back as the literal `__OPENCLAW_REDACTED__`. OpenClaw can
	//     restore that sentinel from the original file when it writes; this daemon
	//     cannot. Writing the remainder back would eventually overwrite a working
	//     value with a placeholder.
	//
	// Both disappear by not reading the config at all. The wrapper includes the
	// user's live config and then a reset stage carrying exactly
	// `{"mcp":{"servers":null}}`, and its own `mcp.servers` merges over the
	// result:
	//
	//	live config  ->  mcp.servers: null  ->  managed servers
	//
	// The user's `mcp` siblings arrive from the live config and are never
	// observed by this process, so nothing can be lost in transit and no key this
	// daemon does not know about can be dropped or invented. OpenClaw's own
	// loader still resolves JSON5, nested includes and env substitution behind
	// the include, and no user config bytes, API keys or provider tokens are
	// written into the task directory.
	//
	// Verified against the real loader on 2026.6.34, 2026.7.1-2 and
	// 2026.8.1-beta.3: objects merge recursively, a null source replaces, and
	// sibling keys on the includer win over the include result. See
	// openclaw_mcp_real_integration_test.go.
	resetPath := ""
	if hasManagedMcp && exists {
		resetPath = filepath.Join(envRoot, openclawMcpResetFile)
		if werr := os.WriteFile(resetPath, []byte(openclawMcpResetBody), 0o600); werr != nil {
			return OpenclawConfigResult{}, fmt.Errorf("write openclaw mcp reset: %w", werr)
		}
	}

	cfg := buildPerTaskOpenclawConfig(activePath, exists, resetPath, resolvedList, agentsFromRegistry, workDir, managedMcp, hasManagedMcp, opts.Gateway)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return OpenclawConfigResult{}, fmt.Errorf("marshal openclaw config: %w", err)
	}
	outPath := filepath.Join(envRoot, openclawConfigFile)
	// 0o600 — defense in depth. The wrapper itself carries no secrets (the
	// $include link is just a filesystem path), but the file lives next to
	// task scratch and we keep the same posture as ~/.openclaw/openclaw.json.
	if err := os.WriteFile(outPath, data, 0o600); err != nil {
		return OpenclawConfigResult{}, fmt.Errorf("write openclaw config: %w", err)
	}
	result := OpenclawConfigResult{ConfigPath: outPath}
	includeTarget := "none"
	if exists {
		// Live user config is in its own directory; tell the daemon to grant
		// it so OpenClaw's include-confinement check passes. The reset stage
		// needs no grant of its own: it sits in envRoot beside the wrapper that
		// includes it.
		result.IncludeRoot = filepath.Dir(activePath)
		includeTarget = "user-config"
		if resetPath != "" {
			includeTarget = "user-config+mcp-reset"
		}
	}
	if opts.Logger != nil {
		opts.Logger.Info("execenv: prepared openclaw config",
			"active_config", activePath,
			"active_config_exists", exists,
			"include_target", includeTarget,
			"include_root", result.IncludeRoot,
			"agents_from_registry", agentsFromRegistry,
			"discovery_cached", cached,
			"managed_mcp", hasManagedMcp)
	}
	return result, nil
}

// discoverOpenclawConfig resolves the user's active config path and resolved
// agents.list, serving both from the shared per-profile cache when the cached
// evidence still matches the host (see openclaw_config_cache.go).
//
// The two are cached as one unit because they are read as one unit: a hit that
// covered only the path would still pay the second CLI call, which on the host
// in #7112 is 4.3s of the 12.7s total.
//
// Cache faults are never fatal. A miss, an unreadable entry, or a failed store
// only costs the CLI round-trips this call was going to make anyway, so
// discovery keeps its existing fail-closed contract: only a real CLI failure
// fails the task.
func discoverOpenclawConfig(bin string, timeout time.Duration, opts OpenclawConfigPrep) (activePath string, exists bool, resolvedList []any, agentsFromRegistry bool, cached bool, err error) {
	cachePath := openclawDiscoveryCachePath(opts.CacheDir)
	if entry, ok := loadOpenclawDiscoveryCache(cachePath, bin, time.Now()); ok {
		list, decodeErr := decodeOpenclawCachedAgentsList(entry.AgentsList)
		if decodeErr == nil {
			return entry.ActiveConfigPath, true, list, entry.AgentsFromRegistry, true, nil
		}
		if opts.Logger != nil {
			opts.Logger.Warn("execenv: openclaw discovery cache entry unusable; rediscovering",
				"cache", cachePath, "error", decodeErr)
		}
	}

	activePath, exists, err = openclawActiveConfigPath(bin, timeout)
	if err != nil {
		return "", false, nil, false, false, fmt.Errorf("locate openclaw active config: %w", err)
	}
	if !exists {
		// Deliberately not cached: "no config on disk" is the one state that
		// flips the moment the user runs OpenClaw's own setup, and caching it
		// would keep a freshly configured host running without its models and
		// auth profiles for the rest of the TTL.
		return activePath, false, nil, false, false, nil
	}

	resolvedList, agentsFromRegistry, err = openclawResolvedAgentsList(bin, timeout)
	if err != nil {
		return "", false, nil, false, false, fmt.Errorf("read openclaw agents.list: %w", err)
	}
	if storeErr := storeOpenclawDiscoveryCache(cachePath, bin, activePath, resolvedList, agentsFromRegistry, time.Now()); storeErr != nil && opts.Logger != nil {
		opts.Logger.Warn("execenv: could not cache openclaw discovery; next task will rerun the CLI",
			"cache", cachePath, "error", storeErr)
	}
	return activePath, exists, resolvedList, agentsFromRegistry, false, nil
}

// buildPerTaskOpenclawConfig assembles the wrapper map that goes on disk.
//
// Exists=true: emit a $include link to the user's active config plus the
// workspace overrides as siblings. OpenClaw deep-merges sibling object keys
// after includes, so agents.defaults.workspace lands correctly. The
// agents.list override is emitted as a full replacement carrying every
// field of every resolved entry (id, model, prompts, tools, …) verbatim
// with only `workspace` rewritten — this is robust regardless of whether
// the runtime merges the sibling array or replaces it, because either way
// the resulting list is shape-equivalent to the user's minus workspace.
//
// Exists=false: a fresh install with no on-disk config. Emit a minimal
// config containing only the workspace override. There is no user data to
// $include here, so this is not the silent-fallback case the reviewer
// flagged.
//
// resetPath, when non-empty, names the `{"mcp":{"servers":null}}` stage in
// envRoot and is appended to the include list after the user's active config.
// Include order is the mechanism: the user's servers are nulled after arriving
// and before the wrapper's own `mcp.servers` merges over the result, so the
// managed set is what resolves while every `mcp` sibling still comes straight
// from the user's file. Empty means no managed set, in which case there is
// nothing to enforce strictness against and the wrapper includes the active
// path alone.
//
// hasManagedMcp distinguishes "agent has a managed mcp_config (possibly an
// empty set)" from "agent inherits the user's global mcp.servers". When
// true we pin `mcp.servers` to managedMcp on the wrapper. Because the reset
// stage has already nulled the user's server map, the resulting view of
// `mcp.servers` is exactly the managed set — including `{}` for "admin saved
// no servers" (mirrors `hasManagedCodexMcpConfig`).
func buildPerTaskOpenclawConfig(activePath string, exists bool, resetPath string, resolvedList []any, agentsFromRegistry bool, workDir string, managedMcp map[string]any, hasManagedMcp bool, gateway OpenclawGatewayPin) map[string]any {
	agents := map[string]any{
		"defaults": map[string]any{"workspace": workDir},
	}
	// Only write per-agent overrides back to the wrapper when they came from
	// the config-schema `agents.list` path (pre-2026.6). A registry-sourced
	// list (OpenClaw 2026.6.x+) is NOT valid `agents.list[]` config — the
	// schema validator rejects it ("agents.list.0: Invalid input") and fails
	// closed before the agent runs. 2026.6.x has no in-config path for per-
	// agent workspace pinning, so `agents.defaults.workspace` (set above) is
	// the only knob, and it is sufficient: OpenClaw applies it to the agent it
	// selects from the registry (see upstream #3028, write-side half).
	if !agentsFromRegistry {
		if rewritten := rewriteAgentsListWorkspaces(resolvedList, workDir); rewritten != nil {
			agents["list"] = rewritten
		}
	}
	cfg := map[string]any{
		"agents": agents,
	}
	if hasManagedMcp {
		// Always emit `mcp.servers` (even when empty) so the wrapper's intent
		// — "admin manages this set" — is grep-able on disk and visible to
		// OpenClaw's loader. The reset stage in the include list has already
		// nulled the user's server map, so this becomes the only definition —
		// and emitting it unconditionally here is what keeps that null from
		// reaching the resolved root, where the schema would reject it.
		servers := managedMcp
		if servers == nil {
			servers = map[string]any{}
		}
		cfg["mcp"] = map[string]any{"servers": servers}
	}
	// Gateway endpoint pin (issue #3260). Mirrors the user's openclaw.json
	// `gateway.*` shape so OpenClaw's deep-merge $include semantics produce
	// the right composed config: anything we set here wins over the user's
	// global, anything we omit inherits from the user's global. Only emit
	// fields the multica admin explicitly populated — zero strings/ints
	// would override the user's value with junk.
	if gw := buildGatewayOverride(gateway); gw != nil {
		cfg["gateway"] = gw
	}
	if exists {
		// Array form so OpenClaw deep-merges the included content with our
		// sibling keys (agents overrides, mcp.servers) rather than letting the
		// include replace the whole wrapper. With a managed set the reset stage
		// follows the user's config in the same list, so the strict-replace flow
		// costs one more entry rather than a second file and a nested include.
		includes := []any{activePath}
		if resetPath != "" {
			includes = append(includes, resetPath)
		}
		cfg["$include"] = includes
	}
	return cfg
}

// buildGatewayOverride renders the non-zero subset of a Gateway pin into the
// shape OpenClaw expects under `gateway.*` (see ~/.openclaw/openclaw.json:
// host, port, tls at the top level and an `auth: {mode, token}` sub-object).
// Returns nil when nothing is populated so the caller can skip emission.
func buildGatewayOverride(p OpenclawGatewayPin) map[string]any {
	if p.IsZero() {
		return nil
	}
	out := map[string]any{}
	if p.Host != "" {
		out["host"] = p.Host
	}
	if p.Port != 0 {
		out["port"] = p.Port
	}
	if p.TLS {
		out["tls"] = true
	}
	if p.Token != "" {
		out["auth"] = map[string]any{
			"mode":  "token",
			"token": p.Token,
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// rewriteAgentsListWorkspaces copies every entry of the resolved agents.list
// and pins its `workspace` field to workDir. Returns nil when the input is
// nil or empty so buildPerTaskOpenclawConfig can omit the key entirely
// (avoiding an empty `agents.list: []` that would replace whatever the
// include carries).
func rewriteAgentsListWorkspaces(list []any, workDir string) []any {
	if len(list) == 0 {
		return nil
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			// Shape we don't recognize — skip rather than guess. Worst case
			// the user loses native skill discovery on that one agent; we
			// still won't crash the wrapper.
			continue
		}
		copyEntry := make(map[string]any, len(entry)+1)
		for k, v := range entry {
			copyEntry[k] = v
		}
		copyEntry["workspace"] = workDir
		out = append(out, copyEntry)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// openclawActiveConfigPath discovers the path the openclaw CLI considers active.
// Returns (absolutePath, exists, error).
//
// The CLI handles the full resolution chain — explicit config path, state
// directory, OPENCLAW_HOME / default home, legacy locations, migration, and `~`
// expansion — so we prefer it when the installed CLI supports the command.
//
// `config validate --json` is asked first, and `config file` is only the
// fallback, because the two differ in whether the answer can be recognised:
//
//   - `config validate --json` puts the path in a named `path` field of a JSON
//     document, and its stdout is that document and nothing else. Measured on
//     OpenClaw 2026.7.1-2 with a warning-producing config: 715 bytes, one line,
//     parseable whole. The Doctor and plugin warnings do not disappear — they
//     arrive as a `warnings` array *inside* the payload, which is what keeps the
//     stream parseable rather than merely quieter. (Upstream also has a
//     console-log reroute for `--json` argv, `withConsoleLogsRoutedToStderrForJson`,
//     but the structured payload is what was observed doing the work here.)
//   - `config file` prints the path as its *last line*, after any Doctor and
//     plugin warnings, on stdout. Deciding "is the answer in yet" then means
//     asking whether the last line looks like a path — and a warning line that
//     names one is indistinguishable. Review demonstrated it: a stub printing an
//     existing `plugin-cache.json` path, then pausing, then printing the real
//     path had the warning accepted as the answer.
//
// Both commands perform the same `readConfigFileSnapshot()` read upstream
// (checked at `v2026.7.1`: `runConfigFile` prints `shortenHomePath(snapshot.path)`
// and `runConfigValidate` reports `snapshot.path`, `snapshot.exists` and
// `snapshot.valid` from that same snapshot), so validation is already part of the
// read `config file` does. Measured, the JSON form is not merely no worse but
// meaningfully cheaper: 1.65/1.66/1.65s against 6.36/4.16/4.06s for `config file`
// over three runs each on the same host and config, which tracks the output it
// does not have to render — 715 bytes of JSON against 2663 bytes and 28 lines of
// warning UI.
//
// `config validate --json` has carried the `path` field on every branch since
// `v2026.5.5`, which is minOpenclawVersion. Two of those branches exit non-zero —
// a missing file and an invalid config, both with the path in the payload and
// stderr empty — so the exit status must not be read as "no answer"; see
// openclawValidatedConfigPath.
//
// OpenClaw 2026.2.x briefly rejected `openclaw config file` with the generic
// "too many arguments for 'config'" error. For that command-shape failure only,
// fall back to the same active-config candidate shape so task prep can still
// continue without losing upgraded users' legacy config files.
//
// A reported path may use `~` or `$OPENCLAW_HOME` shorthand; we expand it so the
// $include reference we write is unambiguously absolute.
func openclawActiveConfigPath(bin string, timeout time.Duration) (string, bool, error) {
	// One deadline for the question, not one per attempt. Both invocations answer
	// "where is the active config", so they are a single step of preparation with
	// a preferred and a fallback way of asking — and the budget that has to hold
	// is the step's. Giving the fallback a fresh full deadline made the worst case
	// five deadlines against a ceiling derived from four: at the 60s override
	// ceiling that is 5m of CLI time alone, landing exactly on
	// daemon.defaultTaskPrepareTimeout, which collapses the specific
	// non-retryable ErrOpenclawCLITimeout back into the generic retryable prepare
	// timeout — the outcome openclawCLIMaxTimeout exists to prevent.
	//
	// The consequence is deliberate: a `config validate --json` that burns the
	// whole budget leaves the fallback none, and openclawExec then fails
	// immediately on the expired context — os/exec's Start reports it, and
	// execOpenclawCLI attributes ctx first, so it still surfaces as
	// ErrOpenclawCLITimeout. That is the right report. A CLI that cannot say where
	// its config is within the entire deadline will not answer the same question
	// on a second one; a fresh budget would only double the time to an identical
	// conclusion, and spend it inside the outer preparation deadline.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if path, ok := openclawValidatedConfigPath(ctx, bin); ok {
		return openclawStatConfigPath(path)
	}

	out, err := openclawExec(ctx, bin, "config", "file")
	if err != nil {
		if isOpenclawConfigFileUnsupported(err) {
			path, exists, ferr := openclawFallbackActiveConfigPath()
			if ferr != nil {
				return "", false, fmt.Errorf("fallback after unsupported `openclaw config file` (%v): %w", err, ferr)
			}
			return path, exists, nil
		}
		return "", false, err
	}
	return openclawParseActiveConfigPath(out)
}

// openclawValidatedConfigPath asks `openclaw config validate --json` for the
// active config path, and reports whether the answer was unambiguous.
//
// It takes the caller's ctx rather than its own timeout: this attempt and the
// `config file` fallback share one deadline, because they are two ways of asking
// the same question. See openclawActiveConfigPath.
//
// The exit status is deliberately not consulted, and that is the common case
// rather than a corner: measured on 2026.7.1-2, upstream exits 1 both for a
// missing config file (`{"valid":false,"path":"…","error":"file not found"}`) and
// for an invalid one (`{"valid":false,"path":"…","issues":[…]}`), with stderr empty
// and the path present in both. A fresh install is the missing-file case, so
// reading a non-zero exit as "no answer" would break first run. Whether the user's
// config parses is not this function's question either — openclaw itself reports
// that when it runs; all that is owed here is where the file is.
//
// Failure is silent by design: every failure mode here is a reason to ask
// `config file` instead, and reporting one would turn "this CLI answered in a
// shape we do not understand" into a task failure. The only requirement is that
// the path be absolute after expansion, which is what distinguishes a real answer
// from the `CONFIG_PATH ?? "openclaw.json"` fallback upstream prints when it
// throws before reading the snapshot.
func openclawValidatedConfigPath(ctx context.Context, bin string) (string, bool) {
	out, _ := openclawExec(ctx, bin, "config", "validate", "--json")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return "", false
	}
	var payload struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return "", false
	}
	reported := strings.TrimSpace(payload.Path)
	if reported == "" {
		return "", false
	}
	// The reported value must already name an absolute location, before any
	// expansion. Upstream prints `CONFIG_PATH ?? "openclaw.json"` when it throws
	// before reading the snapshot, and expandOpenclawPath would turn that bare
	// relative literal into a confident `<daemon cwd>/openclaw.json` — the #6630
	// failure shape, arrived at from a different direction. The `~` and
	// `$OPENCLAW_HOME` forms are absolute once resolved, so they are allowed
	// through; anything else relative is not an answer, and `config file` gets
	// asked instead.
	if !filepath.IsAbs(reported) {
		_, isTilde := openclawTildeRest(reported)
		_, isHome := openclawHomeRest(reported)
		if !isTilde && !isHome {
			return "", false
		}
	}
	expanded, err := expandOpenclawPath(reported)
	if err != nil || !filepath.IsAbs(expanded) {
		return "", false
	}
	return expanded, true
}

func openclawParseActiveConfigPath(out string) (string, bool, error) {
	// OpenClaw may print terminal UI borders (e.g., Doctor warnings) before
	// the actual path. The path is always the last non-empty line.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	path := ""
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" {
			path = trimmed
			break
		}
	}
	if path == "" {
		return "", false, fmt.Errorf("`openclaw config file` returned empty output")
	}
	var err error
	path, err = expandOpenclawPath(path)
	if err != nil {
		return "", false, err
	}
	return openclawStatConfigPath(path)
}

func openclawFallbackActiveConfigPath() (string, bool, error) {
	if explicitPath := strings.TrimSpace(os.Getenv("OPENCLAW_CONFIG_PATH")); explicitPath != "" {
		path, err := expandOpenclawPath(explicitPath)
		if err != nil {
			return "", false, err
		}
		return openclawStatConfigPath(path)
	}

	candidates, canonicalPath, err := openclawFallbackConfigCandidates()
	if err != nil {
		return "", false, err
	}
	for _, candidate := range candidates {
		path, err := expandOpenclawPath(candidate)
		if err != nil {
			return "", false, err
		}
		exists, err := openclawConfigPathExists(path)
		if err != nil {
			return "", false, err
		}
		if exists {
			return path, true, nil
		}
	}
	return openclawStatConfigPath(canonicalPath)
}

var openclawFallbackConfigFileNames = []string{
	"openclaw.json",
	"clawdbot.json",
	"moltbot.json",
	"moldbot.json",
}

var openclawFallbackConfigDirNames = []string{
	".openclaw",
	".clawdbot",
	".moltbot",
	".moldbot",
}

func openclawFallbackConfigCandidates() ([]string, string, error) {
	candidates := make([]string, 0, 1+2*len(openclawFallbackConfigFileNames)+len(openclawFallbackConfigDirNames)*len(openclawFallbackConfigFileNames))
	for _, env := range []string{"CLAWDBOT_CONFIG_PATH"} {
		if path := strings.TrimSpace(os.Getenv(env)); path != "" {
			candidates = append(candidates, path)
		}
	}

	for _, env := range []string{"OPENCLAW_STATE_DIR", "CLAWDBOT_STATE_DIR"} {
		if dir := strings.TrimSpace(os.Getenv(env)); dir != "" {
			candidates = appendOpenclawConfigFileCandidates(candidates, dir)
		}
	}

	home := strings.TrimSpace(os.Getenv("OPENCLAW_HOME"))
	var err error
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, "", fmt.Errorf("resolve openclaw home: %w", err)
		}
	} else {
		home, err = expandOpenclawPath(home)
		if err != nil {
			return nil, "", fmt.Errorf("resolve OPENCLAW_HOME: %w", err)
		}
	}

	for _, dirName := range openclawFallbackConfigDirNames {
		candidates = appendOpenclawConfigFileCandidates(candidates, filepath.Join(home, dirName))
	}
	return candidates, filepath.Join(home, ".openclaw", "openclaw.json"), nil
}

func appendOpenclawConfigFileCandidates(candidates []string, dir string) []string {
	for _, name := range openclawFallbackConfigFileNames {
		candidates = append(candidates, filepath.Join(dir, name))
	}
	return candidates
}

// openclawTildeRest splits a `~`-shortened path into the part after the home
// prefix, reporting whether the path was tilde-shortened at all.
//
// The separator after `~` is whatever the CLI's host OS uses: OpenClaw
// prints `~/.openclaw/openclaw.json` on Unix and `~\.openclaw\openclaw.json`
// on Windows. Matching only the forward-slash form left the Windows tilde
// unexpanded, and since `~\...` is not absolute the path then got joined
// onto the daemon's working directory, producing a path that can never
// exist. The stat miss was indistinguishable from a fresh install, so the
// wrapper silently dropped the user's `$include` and every task booted
// without their model providers or auth profiles (issue #6630).
//
// Both separators are accepted regardless of runtime.GOOS, deliberately, and
// not via os.IsPathSeparator (which rejects `\` on Unix). The daemon and the
// CLI share a host, so only the host's own form arises in production — but
// keying on the character rather than the host OS lets the Windows shape be
// exercised from the normal Linux/macOS test job instead of only on a Windows
// runner, the same trade isOpenclawShimPath makes above.
func openclawTildeRest(path string) (string, bool) {
	if path == "~" {
		return "", true
	}
	if len(path) > 1 && path[0] == '~' && (path[1] == '/' || path[1] == '\\') {
		return path[2:], true
	}
	return "", false
}

// openclawHomeRest recognizes the symbolic shape current OpenClaw releases
// print when OPENCLAW_HOME is set: the variable name rather than its value, for
// example `$OPENCLAW_HOME\.openclaw\openclaw.json`. Nothing downstream expands
// it, so the line reaches filepath.Abs as an ordinary relative path and lands
// under the daemon's working directory — the same stat miss #6630 fixed for the
// tilde form, with the same consequence: indistinguishable from a fresh
// install, so the wrapper drops the user's `$include` and the task boots
// without their model providers or auth profiles.
//
// The shape is guaranteed rather than incidental. At `v2026.5.27`,
// `src/cli/config-cli.ts`'s `runConfigFile` prints `shortenHomePath(...)`, and
// `src/utils.ts:147-157` uses the `$OPENCLAW_HOME` prefix whenever that variable
// is non-empty and `~` otherwise — so setting the variable is what selects this
// form. Still true at `v2026.7.1`: `runConfigFile` is unchanged, and
// `resolveHomeDisplayPrefix` there returns `$OPENCLAW_HOME` on a non-empty
// trimmed `OPENCLAW_HOME` and `~` otherwise, with the separator coming from the
// original path rather than being inserted — which is why the rest is sliced
// after the prefix here rather than trimmed of a leading separator.
//
// Only the bare spelling is emitted by that code path. `${OPENCLAW_HOME}` is
// accepted as defense against a release that spells it the other way, not
// because anything is known to print it. Both separators are accepted
// regardless of runtime.GOOS, for the reason openclawTildeRest gives above.
func openclawHomeRest(path string) (string, bool) {
	for _, prefix := range []string{"$OPENCLAW_HOME", "${OPENCLAW_HOME}"} {
		if path == prefix {
			return "", true
		}
		if len(path) > len(prefix) && strings.HasPrefix(path, prefix) &&
			(path[len(prefix)] == '/' || path[len(prefix)] == '\\') {
			return path[len(prefix)+1:], true
		}
	}
	return "", false
}

// openclawHomeFromEnv resolves OPENCLAW_HOME to the directory the CLI's printed
// `$OPENCLAW_HOME` prefix actually stands for.
//
// The value itself may be a tilde path. Upstream documents it
// (`docs/help/environment.md` at `v2026.5.27`: "OPENCLAW_HOME can also be set to
// a tilde path (e.g. `~/svc`), which gets expanded using the same OS home
// fallback chain before use") and implements it in `src/infra/home-dir.ts:41-47`.
// It expands the value *before* computing the home that `config file` then
// shortens, so on such a host the printed `$OPENCLAW_HOME` stands for
// `<os-home>/svc`, and landing on the same file means resolving it the same way.
// Joining the raw value instead leaves the `~` embedded, filepath.Abs turns that
// into a confident absolute path under the daemon's working directory, and the
// stat miss reproduces the silent fresh-install failure this whole branch exists
// to remove.
//
// Only the tilde is expanded here, deliberately — not the variable form — so a
// self-referential value cannot resolve against itself.
func openclawHomeFromEnv() (string, error) {
	home := strings.TrimSpace(os.Getenv("OPENCLAW_HOME"))
	if home == "" {
		return "", errors.New("environment variable is empty")
	}
	rest, isTilde := openclawTildeRest(home)
	if !isTilde {
		return home, nil
	}
	osHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand `~` in OPENCLAW_HOME: %w", err)
	}
	if rest == "" {
		return osHome, nil
	}
	return filepath.Join(osHome, rest), nil
}

func expandOpenclawPath(path string) (string, error) {
	// OPENCLAW_HOME before `~`: the two shapes are mutually exclusive, but the
	// variable form is checked first because an empty OPENCLAW_HOME has to fail
	// loudly rather than fall through to a relative-path resolution that would
	// quietly produce a wrong absolute path.
	if rest, isOpenclawHome := openclawHomeRest(path); isOpenclawHome {
		home, herr := openclawHomeFromEnv()
		if herr != nil {
			return "", fmt.Errorf("expand OPENCLAW_HOME in openclaw config path %q: %w", path, herr)
		}
		if rest == "" {
			path = home
		} else {
			path = filepath.Join(home, rest)
		}
	} else if rest, isTilde := openclawTildeRest(path); isTilde {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("expand `~` in openclaw config path %q: %w", path, herr)
		}
		if rest == "" {
			path = home
		} else {
			// The remainder still carries the CLI's separators. filepath.Join
			// normalizes them to the host's on the OS that matters here
			// (Windows accepts both), and the result is what we stat.
			path = filepath.Join(home, rest)
		}
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve openclaw config path %q: %w", path, err)
		}
		path = abs
	}
	return path, nil
}

func openclawStatConfigPath(path string) (string, bool, error) {
	if !filepath.IsAbs(path) {
		return "", false, fmt.Errorf("openclaw reported non-absolute config path %q", path)
	}
	exists, err := openclawConfigPathExists(path)
	if err != nil {
		return "", false, err
	}
	return path, exists, nil
}

func openclawConfigPathExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat openclaw config %s: %w", path, err)
	}
	if info.IsDir() {
		return false, fmt.Errorf("openclaw config path %s is a directory, not a file", path)
	}
	return true, nil
}

func isOpenclawConfigFileUnsupported(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "too many arguments for 'config'") ||
		strings.Contains(msg, "expected 0 arguments but got 1") ||
		(strings.Contains(msg, "unknown") && strings.Contains(msg, "config") && strings.Contains(msg, "file"))
}

// openclawResolvedAgentsList fetches the user's resolved per-agent list and
// reports which schema produced it. The schema matters downstream: a config-
// sourced list is itself valid `agents.list[]` config and may be written back
// into the wrapper to pin per-agent workspaces, whereas a registry-sourced
// list MUST NOT be written back — see openclawRegistryAgentsList.
//
// Two schemas are supported:
//
//   - Pre-2026.6: agents live in the config under `agents.list`. We read them
//     via `openclaw config get agents.list --json`, which returns the post-
//     include, post-env-substitution array. fromRegistry=false.
//   - 2026.6.x and later: `agents.list` is no longer a config path — agents
//     live in a sqlite registry. `config get agents.list` exits non-zero with
//     "Config path not found: agents.list". We fall back to the
//     `openclaw agents list --json` *subcommand*. fromRegistry=true.
//
// Returns (nil, false, nil) when neither source yields any agents.
func openclawResolvedAgentsList(bin string, timeout time.Duration) ([]any, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := openclawExec(ctx, bin, "config", "get", "agents.list", "--json")
	if err != nil {
		if isOpenclawKeyMissingResult(out, err, "agents.list") {
			// New schema: the config path is gone; the agents live in the
			// sqlite registry. Resolve them via the subcommand instead.
			list, rerr := openclawRegistryAgentsList(bin, timeout)
			return list, true, rerr
		}
		return nil, false, annotateOpenclawJSONError(err, out)
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" || trimmed == "null" {
		return nil, false, nil
	}
	// An envelope that arrived without a non-zero exit must reach the same
	// verdict as one that did; see openclawStdoutEnvelopeError. Missing the
	// key here is what selects the registry, so letting the envelope through
	// as data would turn a graceful fallback into a failed preparation.
	if message, isEnvelope := openclawJSONErrorMessage(trimmed); isEnvelope {
		if strings.Contains(strings.ToLower(message), "agents.list") && isOpenclawKeyMissingMessage(message) {
			list, rerr := openclawRegistryAgentsList(bin, timeout)
			return list, true, rerr
		}
		return nil, false, openclawStdoutEnvelopeError("config get agents.list --json", message)
	}
	var list []any
	if err := json.Unmarshal([]byte(trimmed), &list); err != nil {
		return nil, false, fmt.Errorf("parse `openclaw config get agents.list --json` output: %w", err)
	}
	return list, false, nil
}

// openclawRegistryAgentsList resolves agents from the sqlite-backed registry
// via `openclaw agents list --json` (OpenClaw 2026.6.x+).
//
// **The result is for read-side use only — it must never be written back into
// the wrapper as `agents.list`.** The registry entries carry CLI-only fields
// (identityName, identitySource, agentDir, bindings, isDefault) that are NOT
// part of the 2026.6.x config schema's `agents.list[]` shape; OpenClaw's
// validator rejects them ("agents.list.0: Invalid input") and fails closed
// before the agent runs. Worse, `agents.list` is no longer a valid config
// path at all in 2026.6.x — there is no in-config way to pin a per-agent
// workspace. The per-task workspace is instead pinned via
// `agents.defaults.workspace` alone, which the wrapper always sets and which
// OpenClaw applies to the agent it selects from the registry (verified on
// 2026.6.8). Callers gate the write-back on fromRegistry from
// openclawResolvedAgentsList.
//
// Returns nil (not an error) when the registry is empty or the subcommand
// reports no agents.
func openclawRegistryAgentsList(bin string, timeout time.Duration) ([]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := openclawExec(ctx, bin, "agents", "list", "--json")
	if err != nil {
		// Older OpenClaw builds may lack the subcommand entirely; treat an
		// unrecognized/missing subcommand the same as "no agents to pin"
		// rather than failing closed, since the defaults.workspace override
		// alone still gives correct per-task skill discovery for the common
		// single-agent case.
		if isOpenclawKeyMissing(err) || isOpenclawUnknownSubcommand(err) {
			return nil, nil
		}
		return nil, annotateOpenclawJSONError(err, out)
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	// Same reasoning as the resolver above: without a non-zero exit the envelope
	// only shows up here.
	if message, isEnvelope := openclawJSONErrorMessage(trimmed); isEnvelope {
		if isOpenclawKeyMissingMessage(message) || isOpenclawUnknownSubcommandMessage(message) {
			return nil, nil
		}
		return nil, openclawStdoutEnvelopeError("agents list --json", message)
	}
	var list []any
	if err := json.Unmarshal([]byte(trimmed), &list); err != nil {
		return nil, fmt.Errorf("parse `openclaw agents list --json` output: %w", err)
	}
	return list, nil
}

// openclawExec is the runtime hook prepareOpenclawConfig uses to invoke the
// openclaw CLI. Production points at execOpenclawCLI; tests swap in a stub
// to avoid spawning a real binary. Production code never reassigns it.
var openclawExec = execOpenclawCLI

// openclawLastNonEmptyLine returns the last non-empty, trimmed line of out.
// Used by openclawParseActiveConfigPath for the `config file` fallback, where the
// path is the last line the CLI prints.
func openclawLastNonEmptyLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if trimmed := strings.TrimSpace(lines[i]); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// openclawOutputComplete returns the rule that decides whether the bytes
// captured so far are a finished answer for this openclaw subcommand, for
// agent.RunCollectQuiet's early return.
//
// Only `--json` commands get one, and that is the whole design rather than a gap
// to fill in later. Two properties make a JSON answer recognisable, and neither
// has an equivalent for human-readable output:
//
//   - The document has to parse *as a whole*, so a response still being written
//     cannot satisfy the rule, no matter how long the writer pauses mid-way.
//   - A `--json` stdout carries the document and nothing else. Measured on
//     2026.7.1-2 with a warning-producing config, `config validate --json` emitted
//     one 715-byte line that parses whole, with the Doctor and plugin warnings
//     carried as a `warnings` array inside it. Without `--json` those same
//     warnings are 28 lines of UI sharing stdout with the answer.
//
// An earlier revision of this branch also had a rule for `config file`: accept
// the buffer once its last non-empty line looks like a path. Review broke it with
// a stub that prints an existing `plugin-cache.json` path, pauses past any fixed
// grace, and then prints the real path — the warning was returned as the answer.
// That is not fixable by waiting longer, because the pause is the host's plugin
// and Doctor work and has no bound; it is fixable by not asking a question that
// content cannot answer. openclawActiveConfigPath now prefers
// `config validate --json`, whose path arrives in a named field.
//
// A nil result means "no rule for this shape", which makes RunCollectQuiet wait
// for the process to exit — the conservative behaviour, and what `config file`
// now gets when it is used as the fallback.
func openclawOutputComplete(args []string) agent.OutputComplete {
	for _, a := range args {
		if a == "--json" {
			return agent.JSONOutputComplete
		}
	}
	return nil
}

// execOpenclawCLI executes an openclaw subcommand and returns its stdout,
// including stdout captured before a non-zero exit. Failed stdout stays in the
// separate return value and this execution layer never appends it to the error:
// config commands can print resolved configuration and secrets there. JSON
// callers may extract only a bounded error-envelope field through
// annotateOpenclawJSONError; arbitrary failed stdout remains non-diagnostic.
// The daemon's environment is inherited so OPENCLAW_CONFIG_PATH /
// OPENCLAW_STATE_DIR / OPENCLAW_HOME / OPENCLAW_INCLUDE_ROOTS pass through.
//
// stderr is captured separately and appended to error messages — failures
// here surface up to the daemon log, and a `openclaw doctor` hint there is
// more useful than just an exit code.
//
// When the CLI is a batch shim that exits non-zero and says nothing at all,
// openclawShimDiagnostic adds the interpreter-resolution detail that a bare
// `exit status 1` hides (MUL-5422 / #6061). Real stderr always wins — the
// diagnostic is a fallback for the silent case, not a replacement.
//
// Attribution order matters. openclawCLITimeout kills the child via
// CommandContext, and a killed process surfaces as *exec.ExitError
// ("signal: killed") — indistinguishable by type from a genuine exit 1. So the
// context is checked FIRST; otherwise a timeout gets reported as "node is not
// on PATH, install Node.js", sending the user to fix something that was never
// broken.
//
// In that branch the CONTEXT error is what gets %w-wrapped, not the process
// error, so errors.Is(err, context.DeadlineExceeded) holds for callers that
// check cancellation the standard way. The process error is still printed for
// diagnosis, just not as the wrapped cause.
func execOpenclawCLI(ctx context.Context, bin string, args ...string) (string, error) {
	// agent.RunCollectQuiet, not cmd.Output(): this package owns the pipes and
	// the process tree, which is what makes the deadline above enforceable at all
	// (MUL-5467 — see openclawCLITimeout). The per-subcommand rule from
	// openclawOutputComplete additionally lets a CLI that prints its answer and
	// then refuses to exit be treated as finished, so `openclaw config file` no
	// longer has to reach the deadline to be useful.
	//
	// Every error shape below is unchanged, including returning captured stdout
	// alongside the error: annotateOpenclawJSONError reads it, and the typed
	// timeout sentinel is what lets the daemon classify a local stall
	// structurally.
	raw, stderrOut, _, err := agent.RunCollectQuiet(ctx, os.Environ(), 0, openclawOutputComplete(args), bin, args...)
	stdout := string(raw)
	if err != nil {
		stderrMsg := strings.TrimSpace(stderrOut)
		if ctxErr := ctx.Err(); ctxErr != nil {
			msg := fmt.Sprintf("openclaw %s: %v (process: %v)", strings.Join(args, " "), ctxErr, err)
			if stderrMsg != "" {
				msg = fmt.Sprintf("openclaw %s: %v (process: %v; stderr: %s)", strings.Join(args, " "), ctxErr, err, stderrMsg)
			}
			// A deadline is reported through openclawCLITimeoutError so the
			// daemon can recognise a local CLI stall structurally
			// (ErrOpenclawCLITimeout) instead of string-matching "deadline
			// exceeded" — which routed it into agent_error.provider_network,
			// i.e. "check your network" copy plus an auto-retry, for a failure
			// that is local and deterministic.
			//
			// Cancellation deliberately does NOT get the sentinel: a daemon
			// shutdown or a cancelled task is not a slow CLI, and labelling it
			// as one would tell the user to raise a timeout that was never the
			// problem. Both keep the wrapped context error, so
			// errors.Is(err, context.DeadlineExceeded) / context.Canceled work
			// exactly as before.
			if errors.Is(ctxErr, context.DeadlineExceeded) {
				return stdout, &openclawCLITimeoutError{msg: msg, cause: ctxErr}
			}
			if stderrMsg != "" {
				return stdout, fmt.Errorf("openclaw %s: %w (process: %v; stderr: %s)", strings.Join(args, " "), ctxErr, err, stderrMsg)
			}
			return stdout, fmt.Errorf("openclaw %s: %w (process: %v)", strings.Join(args, " "), ctxErr, err)
		}
		if stderrMsg != "" {
			return stdout, fmt.Errorf("openclaw %s: %w (stderr: %s)", strings.Join(args, " "), err, stderrMsg)
		}
		if diag := openclawShimDiagnostic(bin, err); diag != "" {
			return stdout, fmt.Errorf("openclaw %s: %w (%s)", strings.Join(args, " "), err, diag)
		}
		return stdout, fmt.Errorf("openclaw %s: %w", strings.Join(args, " "), err)
	}
	return stdout, nil
}

// openclawManagedMcpServers parses the agent's `mcp_config` JSON and returns
// the map of server name → server config that the wrapper should emit at
// `mcp.servers`. The second return is `true` when the agent has a managed
// mcp_config saved (non-null) — including the explicit empty set
// `{}` / `{"mcpServers":{}}` — and `false` when the field is null/absent so
// the user's global config flows through unmodified.
//
// Input shape mirrors the rest of Multica: Claude-style
// `{"mcpServers": {"<name>": {...}}}`. The server-entry fields pass through
// verbatim. OpenClaw's stdio schema uses the same camelCase keys (`command`,
// `args`, `env`) as Claude; HTTP/SSE entries should set OpenClaw's
// `transport` field directly (e.g. `"transport": "streamable-http"`) rather
// than Claude's `type` since OpenClaw does not recognise the latter.
//
// Each entry must declare either `command` (stdio) or `url` (http/sse); any
// other shape returns an error so the launch fails closed with an actionable
// message rather than running with a server OpenClaw will refuse to start.
func openclawManagedMcpServers(raw json.RawMessage) (map[string]any, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, false, nil
	}
	var parsed struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return nil, false, fmt.Errorf("parse mcp_config json: %w", err)
	}
	if len(parsed.McpServers) == 0 {
		return map[string]any{}, true, nil
	}
	names := make([]string, 0, len(parsed.McpServers))
	for name := range parsed.McpServers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make(map[string]any, len(names))
	for _, name := range names {
		var entry map[string]any
		if err := json.Unmarshal(parsed.McpServers[name], &entry); err != nil {
			return nil, false, fmt.Errorf("mcp_servers.%s: %w", name, err)
		}
		if entry == nil {
			return nil, false, fmt.Errorf("mcp_servers.%s must be a JSON object", name)
		}
		command, _ := entry["command"].(string)
		url, _ := entry["url"].(string)
		if strings.TrimSpace(command) == "" && strings.TrimSpace(url) == "" {
			return nil, false, fmt.Errorf("mcp_servers.%s must declare either `command` (stdio) or `url` (http/sse)", name)
		}
		out[name] = entry
	}
	return out, true, nil
}

// isOpenclawKeyMissing returns true when the CLI error indicates the asked-
// for path simply isn't set, as opposed to a real failure (bad config,
// CLI bug, missing binary). The CLI's "key not found" exit text has varied
// across versions, so we match on a handful of substrings rather than the
// exit code alone.
func isOpenclawKeyMissing(err error) bool {
	if err == nil {
		return false
	}
	return isOpenclawKeyMissingMessage(err.Error())
}

const openclawJSONErrorMaxRunes = 1024

func openclawJSONErrorMessage(stdout string) (string, bool) {
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(stdout)), &envelope) != nil || len(envelope.Error) == 0 {
		return "", false
	}
	var message string
	if err := json.Unmarshal(envelope.Error, &message); err != nil {
		// OpenClaw 2026.8.1-beta.3 changed JSON failures from
		// {"error":"message"} to
		// {"ok":false,"error":{"type":"cli_error","message":"message"}}.
		// Extract only the diagnostic field in either shape; sibling fields can
		// still carry resolved configuration or secrets.
		var detail struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(envelope.Error, &detail) != nil {
			return "", false
		}
		message = detail.Message
	}
	message = strings.Join(strings.Fields(message), " ")
	return message, message != ""
}

// annotateOpenclawJSONError restores diagnostics for JSON-mode commands whose
// CLI errors are written to stdout. Only the envelope's diagnostic message is
// included, whether `error` is a string or an object: sibling fields may
// contain resolved configuration or secrets. The message is
// whitespace-normalized for single-line logs and rune-bounded to keep persisted
// task errors finite while preserving valid UTF-8.
func annotateOpenclawJSONError(err error, stdout string) error {
	if err == nil {
		return nil
	}
	message, ok := openclawJSONErrorMessage(stdout)
	if !ok {
		return err
	}
	return fmt.Errorf("%w (json error: %s)", err, openclawBoundedJSONErrorMessage(message))
}

func openclawBoundedJSONErrorMessage(message string) string {
	runes := []rune(message)
	if len(runes) > openclawJSONErrorMaxRunes {
		return string(runes[:openclawJSONErrorMaxRunes]) + "…"
	}
	return message
}

// openclawStdoutEnvelopeError turns a CLI error envelope that arrived on stdout
// *without* a non-zero exit into an error.
//
// The completeness rules let RunCollectQuiet accept a finished-looking answer
// from a CLI that has printed it but not exited yet (see openclawOutputComplete),
// and a JSON error envelope is itself valid JSON, so JSONOutputComplete accepts
// one. The exit-status path therefore no longer sees every CLI error: a build
// that prints `{"error": "..."}` and then lingers past the idle grace hands back
// err == nil with the envelope sitting in stdout. Callers must check for that
// explicitly, because decoding an envelope as data is silent for an object
// target — it would be written straight into the generated config — and only
// accidentally noisy for a list target, where the type mismatch surfaces as an
// opaque parse error instead of the CLI's own message.
func openclawStdoutEnvelopeError(command, message string) error {
	return fmt.Errorf("`openclaw %s` reported: %s", command, openclawBoundedJSONErrorMessage(message))
}

// isOpenclawKeyMissingResult recognizes the "you asked for a key that is not
// there" failures of `config get ... --json`. Which channel reports it where was
// measured on 2026-08-26, and the spread is why both transports stay supported:
//
//	2026.6.34   (extended-stable)  stderr text, stdout empty
//	2026.7.1-2  (latest)           stderr text, stdout empty
//	2026.7.2-beta.7                stdout {"error":"..."}
//	2026.8.1-beta.3 (beta)         stdout {"ok":false,"error":{"message":...}}
//
// So the stdout envelope is not "the new way" that replaced stderr — the current
// stable and latest channels are stderr-only, and only the beta line puts it on
// stdout. Dropping either transport breaks a channel that is shipping today.
//
// Wording also moved: 2026.6.34 and 2026.7.1-2 say "Config path not found: mcp",
// 2026.8.1-beta.3 says "Config path is valid but unset: mcp" for a key the schema
// knows and "Unknown config path: agents.list" for one it does not.
//
// Structured messages must name the path we asked for. That is what keeps a
// *usage* error out of this branch: the pathless `config get --json` form fails
// with `Missing required argument "path".`, which names no path and so cannot be
// read as an absent key. Cancellation and timeout keep their original meaning
// even if a child emitted a partial missing-path envelope before it stopped.
func isOpenclawKeyMissingResult(stdout string, err error, keyPath string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if openclawKeyMissingMessageForPath(err.Error(), keyPath) {
		return true
	}
	message, ok := openclawJSONErrorMessage(stdout)
	if !ok {
		return false
	}
	return openclawKeyMissingMessageForPath(message, keyPath)
}

func openclawKeyMissingMessageForPath(message, keyPath string) bool {
	message = strings.ToLower(strings.Join(strings.Fields(message), " "))
	keyPath = strings.ToLower(strings.TrimSpace(keyPath))
	if keyPath == "" {
		return false
	}
	for _, marker := range []string{
		"no value at ",
		"missing key: ",
		"missing key ",
		"path not found: ",
		"config path is valid but unset: ",
		"unknown config path: ",
	} {
		if openclawMessageNamesPathAfter(message, marker, keyPath) {
			return true
		}
	}
	if openclawMessageNamesPathBefore(message, keyPath, " is not set") ||
		openclawMessageNamesPathBefore(message, keyPath, " not set") ||
		strings.Contains(message, "config path "+keyPath+" not found") {
		return true
	}
	// Some pre-2026.6 builds returned only "Path not found" after the
	// command wrapper. Accept that exact bare diagnostic when the command names
	// the expected key, but do not combine the command path with an unrelated
	// "not set" message later in stderr.
	return strings.Contains(message, "config get "+keyPath) &&
		(strings.HasSuffix(message, ": path not found") ||
			strings.Contains(message, ": path not found ("))
}

func openclawMessageNamesPathBefore(message, keyPath, suffix string) bool {
	needle := keyPath + suffix
	for offset := 0; offset < len(message); {
		relative := strings.Index(message[offset:], needle)
		if relative < 0 {
			return false
		}
		idx := offset + relative
		leftBoundary := idx == 0 || isOpenclawPathLeftBoundary(message[idx-1])
		if leftBoundary && isOpenclawPathBoundary(message[idx+len(needle):]) {
			return true
		}
		offset = idx + len(keyPath)
	}
	return false
}

func openclawMessageNamesPathAfter(message, marker, keyPath string) bool {
	for search := message; ; {
		idx := strings.Index(search, marker)
		if idx < 0 {
			return false
		}
		rest := search[idx+len(marker):]
		if strings.HasPrefix(rest, keyPath) && isOpenclawPathBoundary(rest[len(keyPath):]) {
			return true
		}
		search = rest
	}
}

func isOpenclawPathLeftBoundary(char byte) bool {
	switch char {
	case ' ', '(', '[', '{', ',', ';', ':', '\'', '"', '`':
		return true
	default:
		return false
	}
}

func isOpenclawPathBoundary(rest string) bool {
	if rest == "" {
		return true
	}
	switch rest[0] {
	case ' ', ')', ']', '}', ',', ';', ':', '\'', '"', '`':
		return true
	case '.':
		return len(rest) == 1 || rest[1] == ' ' || rest[1] == ')' || rest[1] == ']'
	default:
		return false
	}
}

func isOpenclawKeyMissingMessage(msg string) bool {
	// Match case-insensitively: the CLI's "key not found" wording has drifted
	// across versions and capitalization is not stable. Pre-2026.6 emitted
	// "Path not found"; OpenClaw 2026.6.x emits "Config path not found:
	// agents.list" (lowercase "path", "Config" prefix). A case-sensitive
	// strings.Contains on "Path not found" silently stopped matching the
	// 2026.6.x string, turning the intended graceful-skip into a fail-closed
	// error that broke every OpenClaw 2026.6.x runtime (see upstream #3028).
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "no value at ") ||
		strings.Contains(msg, "not set") ||
		strings.Contains(msg, "missing key") ||
		strings.Contains(msg, "path not found")
}

// isOpenclawUnknownSubcommand returns true when the CLI error indicates the
// invoked subcommand/option does not exist on this OpenClaw build (e.g. an
// older release predating `openclaw agents list --json`). Used so the
// registry fallback degrades to "no agents to pin" rather than failing
// closed on builds that never had the subcommand.
func isOpenclawUnknownSubcommand(err error) bool {
	if err == nil {
		return false
	}
	return isOpenclawUnknownSubcommandMessage(err.Error())
}

func isOpenclawUnknownSubcommandMessage(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "unknown command") ||
		strings.Contains(msg, "unknown option") ||
		strings.Contains(msg, "does not recognize") ||
		strings.Contains(msg, "unknown argument")
}
