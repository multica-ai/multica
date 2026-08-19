package execenv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// openclawConfigFile is the per-task synthesized OpenClaw config the daemon
// points the openclaw CLI at via OPENCLAW_CONFIG_PATH. It sits in the env
// root (alongside workdir/, output/, logs/) so the GC reaper sweeps it with
// the rest of the task env.
const openclawConfigFile = "openclaw-config.json"

// openclawUserSnapshotFile is the sanitized copy of the user's fully
// resolved openclaw config the wrapper $includes when the agent has a
// managed mcp_config. It is the user's config minus the `mcp` block so the
// wrapper's managed `mcp.servers` is the only MCP definition visible to
// OpenClaw — true strict-replace, not deep-merge-by-name. Lives in envRoot
// at 0o600 next to the wrapper.
const openclawUserSnapshotFile = "openclaw-user-snapshot.json"

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
// (openclawMaxCLICallsPerPreparation serial calls at that budget) fits inside
// the outer 5-minute task preparation deadline, so a genuinely hung CLI fails
// with this specific, actionable reason instead of the generic prepare
// timeout. Hosts outside that envelope can override with
// MULTICA_OPENCLAW_CLI_TIMEOUT (or backends.openclaw.cli_timeout in the CLI
// config, which the daemon translates into the same env var).
//
// It is a deadline, not a guaranteed cap — see the gap below.
//
// Known gap (deliberately not fixed here): this deadline does not actually
// bound the call when the CLI leaves a descendant holding stdout.
// CommandContext kills only the direct child, and cmd.Output() blocks in
// Wait() until the stdout pipe closes, so the call runs for the descendant's
// lifetime. Measured on linux/dash: a shim whose backgrounded child slept 6s
// took 6.01s against a 150ms deadline. An npm shim is that shape on Windows
// (cmd.exe → node).
//
// A cmd.WaitDelay backstop bounds the call but leaves the descendant running
// (measured: returns in 2.17s with the grandchild still in state S), trading a
// hang for a process leak — and on Unix nothing reaps it, because
// preparationProcessController.finish() is a no-op there. Closing this properly
// needs process-tree ownership (Unix process group, Windows Job Object) so the
// deadline can terminate the whole tree, which is its own change with its own
// risk surface. Tracked in MUL-5467; this file intentionally keeps the existing
// behaviour rather than shipping half of it.
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
	// worst case is openclawMaxCLICallsPerPreparation serial calls, so the
	// ceiling is set so that even then (4 x 60s = 4m) the failure surfaces as a
	// specific, actionable CLI timeout with room to spare, instead of colliding
	// with the outer deadline and collapsing into the generic — and retryable —
	// prepare-timeout reason.
	openclawCLIMaxTimeout = 60 * time.Second
)

// openclawMaxCLICallsPerPreparation is how many serial `openclaw ...`
// invocations one task preparation can make in the worst case. Each one gets
// its own deadline, so this is the multiplier that decides whether
// openclawCLIMaxTimeout still fits inside the outer preparation budget.
//
// The four call sites, in the order they can fire:
//
//  1. `config file`                     — locate the active config
//  2. `config get agents.list --json`   — pre-2026.6 agents schema
//  3. `agents list --json`              — 2026.6+ registry fallback, only
//     reached when (2) reports the config path is missing
//  4. `config get --json`               — full resolved config, only for an
//     agent with a managed mcp_config
//
// Adding a fifth call means re-deriving the ceiling; the budget test fails
// loudly if this constant and the real call graph drift apart.
const openclawMaxCLICallsPerPreparation = 4

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
	// managed set would not actually clear inherited servers). To enforce the
	// Codex-style "managed wins, user globals invisible" contract, fetch the
	// user's resolved config, drop just the `mcp.servers` map (keep other
	// `mcp.*` settings like `sessionIdleTtlMs`), write a sanitized snapshot
	// in envRoot, and $include the snapshot instead of the live user file.
	// The wrapper's `mcp.servers` then becomes the only MCP server definition
	// the snapshot's resolution can yield, while the user's surrounding `mcp`
	// tuning still flows through.
	snapshotPath := ""
	if hasManagedMcp && exists {
		resolved, ferr := openclawResolvedFullConfig(bin, timeout)
		if ferr != nil {
			return OpenclawConfigResult{}, fmt.Errorf("read openclaw resolved config: %w", ferr)
		}
		if resolved == nil {
			// CLI reports the file exists but `config get --json` returned
			// nothing structured. Treat as no user-config-to-strip: the
			// wrapper will carry managed mcp.servers as the sole source.
			exists = false
			activePath = ""
		} else {
			stripUserMcpServers(resolved)
			snapBytes, merr := json.MarshalIndent(resolved, "", "  ")
			if merr != nil {
				return OpenclawConfigResult{}, fmt.Errorf("marshal openclaw user snapshot: %w", merr)
			}
			snapshotPath = filepath.Join(envRoot, openclawUserSnapshotFile)
			// 0o600 — the snapshot is now a flat copy of the user's resolved
			// config and may carry API keys / model-provider tokens that
			// $include used to keep on disk in the user's own file. Lock the
			// snapshot to the daemon owner; only the openclaw child reads it.
			if werr := os.WriteFile(snapshotPath, snapBytes, 0o600); werr != nil {
				return OpenclawConfigResult{}, fmt.Errorf("write openclaw user snapshot: %w", werr)
			}
		}
	}

	cfg := buildPerTaskOpenclawConfig(activePath, exists, snapshotPath, resolvedList, agentsFromRegistry, workDir, managedMcp, hasManagedMcp, opts.Gateway)

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
	if snapshotPath != "" {
		// Sanitized snapshot lives in envRoot alongside the wrapper, so the
		// $include never crosses directories — daemon does not need to grant
		// an extra OPENCLAW_INCLUDE_ROOTS entry.
		includeTarget = "sanitized-snapshot"
	} else if exists {
		// Live user config is in its own directory; tell the daemon to grant
		// it so OpenClaw's include-confinement check passes.
		result.IncludeRoot = filepath.Dir(activePath)
		includeTarget = "user-config"
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
// snapshotPath, when non-empty, points at a sanitized copy of the user's
// resolved config (mcp stripped) sitting in envRoot. It is the $include
// target whenever the agent has a managed mcp_config — the live user file
// would otherwise leak global `mcp.servers` past the wrapper. When
// snapshotPath is empty the wrapper falls back to $include'ing the active
// path so secrets / nested includes stay in the user's own file (no
// managed mcp means there is nothing to enforce strictness against).
//
// hasManagedMcp distinguishes "agent has a managed mcp_config (possibly an
// empty set)" from "agent inherits the user's global mcp.servers". When
// true we pin `mcp.servers` to managedMcp on the wrapper. Because the
// snapshot $include has already dropped the user's `mcp` block, the
// resulting view of `mcp.servers` is exactly the managed set — including
// `{}` for "admin saved no servers" (mirrors `hasManagedCodexMcpConfig`).
func buildPerTaskOpenclawConfig(activePath string, exists bool, snapshotPath string, resolvedList []any, agentsFromRegistry bool, workDir string, managedMcp map[string]any, hasManagedMcp bool, gateway OpenclawGatewayPin) map[string]any {
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
		// OpenClaw's loader. The snapshot $include has already dropped the
		// user's `mcp` block, so this becomes the only definition.
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
	switch {
	case snapshotPath != "":
		// Sanitized snapshot path; strict-replace flow for managed mcp_config.
		// Array form so OpenClaw deep-merges the snapshot's content with our
		// sibling keys (agents overrides, mcp.servers) rather than letting the
		// include replace the whole wrapper.
		cfg["$include"] = []any{snapshotPath}
	case exists:
		cfg["$include"] = []any{activePath}
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

// stripUserMcpServers removes only `mcp.servers` from a resolved user
// config, leaving every other key under `mcp` (e.g. `sessionIdleTtlMs`)
// intact. The wrapper's managed `mcp.servers` becomes the sole server
// definition while the user's surrounding MCP tuning still applies — see
// https://docs.openclaw.ai/gateway/configuration-reference#mcp for the
// full list of sibling settings the snapshot should preserve.
//
// If the resulting `mcp` block has no keys left, the parent `mcp` key is
// dropped too so the snapshot doesn't carry an empty placeholder. Any
// non-object value for `mcp` is left as-is; we only know how to strip
// servers from the documented object shape.
func stripUserMcpServers(resolved map[string]any) {
	mcp, ok := resolved["mcp"].(map[string]any)
	if !ok {
		return
	}
	delete(mcp, "servers")
	if len(mcp) == 0 {
		delete(resolved, "mcp")
	}
}

// openclawActiveConfigPath runs `openclaw config file` to discover the path
// the openclaw CLI considers active. Returns (absolutePath, exists, error).
//
// The CLI handles the full resolution chain — explicit config path, state
// directory, OPENCLAW_HOME / default home, legacy locations, migration, and `~`
// expansion — so we prefer it when the installed CLI supports the command.
//
// OpenClaw 2026.2.x briefly rejected `openclaw config file` with the generic
// "too many arguments for 'config'" error. For that command-shape failure only,
// fall back to the same active-config candidate shape so task prep can still
// continue without losing upgraded users' legacy config files.
//
// The reported path uses `~` shorthand for the user's home; we expand it
// so the $include reference we write is unambiguous absolute.
func openclawActiveConfigPath(bin string, timeout time.Duration) (string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
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

func expandOpenclawPath(path string) (string, error) {
	if rest, isTilde := openclawTildeRest(path); isTilde {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("expand `~` in openclaw config path: %w", herr)
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

// openclawResolvedFullConfig fetches the user's fully resolved openclaw
// config via `openclaw config get --json` (no key path — root). The CLI's
// loader handles JSON5 / $include / env-substitution and emits a flat JSON
// object, which is what we need to write a sanitized snapshot that the
// wrapper can $include without inheriting the user's `mcp.servers`.
//
// Returns (nil, nil) when the CLI prints empty / null output for the root
// — interpreted as "no resolvable user config" by the caller, which then
// falls through to the fresh-install code path. Any other failure
// surfaces as an error so the daemon fails closed instead of silently
// degrading to a leaky non-strict wrapper.
func openclawResolvedFullConfig(bin string, timeout time.Duration) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := openclawExec(ctx, bin, "config", "get", "--json")
	if err != nil {
		return nil, annotateOpenclawJSONError(err, out)
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(trimmed), &cfg); err != nil {
		return nil, fmt.Errorf("parse `openclaw config get --json` output: %w", err)
	}
	return cfg, nil
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
		if isOpenclawKeyMissingResult(out, err) {
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
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = os.Environ()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	stdout := string(raw)
	if err != nil {
		stderrMsg := strings.TrimSpace(stderr.String())
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
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(stdout)), &envelope) != nil {
		return "", false
	}
	message := strings.Join(strings.Fields(envelope.Error), " ")
	return message, message != ""
}

// annotateOpenclawJSONError restores diagnostics for JSON-mode commands whose
// CLI errors are written to stdout. Only the envelope's `error` string is
// included: sibling fields may contain resolved configuration or secrets. The
// message is whitespace-normalized for single-line logs and rune-bounded to
// keep persisted task errors finite while preserving valid UTF-8.
func annotateOpenclawJSONError(err error, stdout string) error {
	if err == nil {
		return nil
	}
	message, ok := openclawJSONErrorMessage(stdout)
	if !ok {
		return err
	}
	runes := []rune(message)
	if len(runes) > openclawJSONErrorMaxRunes {
		message = string(runes[:openclawJSONErrorMaxRunes]) + "…"
	}
	return fmt.Errorf("%w (json error: %s)", err, message)
}

// isOpenclawKeyMissingResult recognizes the JSON error envelope observed in
// OpenClaw 2026.7.2-beta.7 for `config get ... --json` failures. It first
// preserves the historical stderr/error-text matching, then parses only the
// explicit `error` field and requires it to name agents.list; broad historical
// phrases such as "not set" cannot reclassify an unrelated structured error.
// Cancellation and timeout keep their original meaning even if a child emitted
// a partial missing-path envelope before it stopped.
func isOpenclawKeyMissingResult(stdout string, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if isOpenclawKeyMissing(err) {
		return true
	}
	message, ok := openclawJSONErrorMessage(stdout)
	if !ok {
		return false
	}
	return strings.Contains(strings.ToLower(message), "agents.list") &&
		isOpenclawKeyMissingMessage(message)
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
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unknown command") ||
		strings.Contains(msg, "unknown option") ||
		strings.Contains(msg, "does not recognize") ||
		strings.Contains(msg, "unknown argument")
}
