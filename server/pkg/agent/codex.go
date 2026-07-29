package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// codexBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. The mcp_servers config keys
// live in the per-task `$CODEX_HOME/config.toml` (written by
// ensureCodexMcpConfig); user-supplied `-c mcp_servers.…` overrides are
// stripped separately by filterCodexCustomConfigOverrides because they
// share the `-c` flag with legitimate non-MCP overrides like `-c model=…`.
var codexBlockedArgs = map[string]blockedArgMode{
	"--listen": blockedWithValue, // stdio:// transport for daemon communication
}

const (
	codexFastServiceTier = "priority"
	codexFastModeFeature = "fast_mode"
)

// codexStderrTailBytes bounds the stderr tail captured for inclusion in
// error messages when codex exits before the JSON-RPC handshake (e.g. the
// user supplied a custom_args flag that the `app-server` subcommand
// rejects). Kept as its own constant so bumping codex independently of
// other agents stays easy if codex starts shipping longer failure traces.
const (
	codexStderrTailBytes                   = 2048
	defaultCodexSemanticInactivityTimeout  = 10 * time.Minute
	defaultCodexFirstTurnNoProgressTimeout = 30 * time.Second
	defaultCodexHandshakeTimeout           = 30 * time.Second
	codexVersionDiagnosticTimeout          = 2 * time.Second
	// codexGracefulShutdownTimeout bounds how long the lifecycle goroutine
	// waits for codex to exit on its own after stdin is closed, before forcing
	// a context-cancel kill. A clean exit lets codex run its shutdown path and
	// flush buffered telemetry — OTEL batch exporters only force-flush on
	// graceful shutdown, so killing it immediately (the prior behavior) drops
	// the task's spans/metrics/logs.
	codexGracefulShutdownTimeout = 10 * time.Second
)

// codexGracefulShutdownTimeoutNanos optionally overrides
// codexGracefulShutdownTimeout for tests, in nanoseconds. Zero or negative
// values keep the production default. Tests for the cleanup-on-scanner-
// overflow path (#4520) use it to shrink the grace window from 10 s to a
// few hundred ms so the regression runs in a normal `go test` budget
// instead of burning two full grace windows per cleanup phase. Mirrors
// the opencodeTerminateGraceNanos hook.
var codexGracefulShutdownTimeoutNanos atomic.Int64
var codexProcessWaitDelayNanos atomic.Int64
var activeCodexLaunches atomic.Int64
var maxActiveCodexLaunchesObserved atomic.Int64
var codexCleanupConfirmationOverride atomic.Int32

func sanitizeCodexDiagnostic(value string) string {
	return sanitizeAgentDiagnostic(value)
}

func codexProcessExitStatus(state *os.ProcessState) any {
	if state == nil {
		return nil
	}
	return state.String()
}

func codexGracefulShutdown() time.Duration {
	if n := codexGracefulShutdownTimeoutNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return codexGracefulShutdownTimeout
}

func codexProcessWaitDelay() time.Duration {
	if n := codexProcessWaitDelayNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return 10 * time.Second
}

type codexStderrClassification struct {
	modelRefreshTimeout int
	mcpInitTransport    int
	bareTimeout         int
}

// classifyCodexStartupStderr emits bounded counters only. It deliberately does
// not make retry decisions: stderr is sibling diagnostic evidence, not proof
// that thread/start did or did not create a provider-side thread.
func classifyCodexStartupStderr(stderr string, timedOut bool) codexStderrClassification {
	lower := strings.ToLower(sanitizeCodexDiagnostic(stderr))
	classification := codexStderrClassification{
		modelRefreshTimeout: strings.Count(lower, codexModelCatalogRefreshTimeoutSignal),
	}
	for _, line := range strings.Split(lower, "\n") {
		if strings.Contains(line, "mcp") && strings.Contains(line, "transport") &&
			(strings.Contains(line, "error") || strings.Contains(line, "failed") || strings.Contains(line, "closed")) {
			classification.mcpInitTransport++
		}
	}
	if timedOut && classification.modelRefreshTimeout == 0 && classification.mcpInitTransport == 0 {
		classification.bareTimeout = 1
	}
	return classification
}

// CodexSemanticInactivityMarker prefixes timeout errors emitted when Codex
// stops making semantic progress while the process is still alive.
const CodexSemanticInactivityMarker = "codex semantic inactivity timeout"

// CodexFirstTurnNoProgressMarker identifies the app-server failure mode where
// Codex accepts a turn and then never emits any item, completion, or error.
const CodexFirstTurnNoProgressMarker = "codex app-server no progress timeout"

// CodexHandshakeTimeoutMarker identifies a Codex app-server startup RPC that
// did not answer within the bounded handshake window.
const CodexHandshakeTimeoutMarker = "codex app-server handshake timeout"

// codexModelCatalogRefreshFailureSignal matches the Codex models-manager error
// emitted when the model catalog could not be refreshed. Codex reports several
// distinct causes under this prefix ("timeout waiting for child process to
// exit", "stream disconnected before completion", ...), so match the shared
// prefix rather than one variant: they are all the same startup-blocking
// failure from the daemon's point of view.
const codexModelCatalogRefreshFailureSignal = "failed to refresh available models"
const codexModelCatalogRefreshTimeoutSignal = "failed to refresh available models: timeout waiting for child process to exit"

var errCodexProcessExited = errors.New("codex process exited")

type codexTimeoutKind int

const (
	codexTimeoutNone codexTimeoutKind = iota
	codexTimeoutSemanticInactivity
	codexTimeoutFirstTurnNoProgress
)

type codexTimeoutDiagnostic struct {
	Kind         codexTimeoutKind
	Timeout      time.Duration
	LastActivity string
	ThreadID     string
	TurnID       string
	Model        string
	CodexVersion string
}

// codexBackend implements Backend by spawning `codex app-server --listen stdio://`
// and communicating via JSON-RPC 2.0 over stdin/stdout.
type codexBackend struct {
	cfg Config
}

func buildCodexArgs(opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"app-server", "--listen", "stdio://"}
	launchArgs := NormalizeCodexLaunchArgs(opts.ExtraArgs, opts.CustomArgs, opts.McpConfig, logger)
	if opts.ServiceTier == codexFastServiceTier {
		launchArgs = enforceCodexFastMode(launchArgs, logger)
	}
	return append(args, launchArgs...)
}

// NormalizeCodexLaunchArgs returns the user-supplied Codex args (extra then
// custom) after lower-priority normalization: shell quoting stripped,
// protocol-critical flags removed, and — when a managed
// mcp_config owns the mcp_servers namespace — stray `-c mcp_servers.*`
// overrides dropped. buildCodexArgs prepends the fixed
// `app-server --listen stdio://` transport flags and may append an
// agent-owned runtime override (currently `--enable fast_mode`) after this
// result.
//
// It is exported so the daemon can reconstruct the *effective* launch args when
// deciding the Windows sandbox mode. A `-c windows.sandbox=…` opt-in may arrive
// shell-quoted (users commonly type custom_args with shell syntax, e.g.
// `'-c' 'windows.sandbox=unelevated'`), and only after this same normalization
// does it match the `-c windows.sandbox=…` shape the sandbox detector looks
// for. Reconstructing the args any other way lets the two drift, silently
// downgrading a user's isolation opt-in (MUL-4957).
func NormalizeCodexLaunchArgs(extraArgs, customArgs []string, mcpConfig json.RawMessage, logger *slog.Logger) []string {
	extra := filterCustomArgs(extraArgs, codexBlockedArgs, logger)
	custom := filterCustomArgs(customArgs, codexBlockedArgs, logger)
	// Only claim ownership of the `mcp_servers` namespace when the agent
	// actually has a managed mcp_config in the MCP Tab. Otherwise existing
	// users who configure MCP via `custom_args: ["-c", "mcp_servers.…"]`
	// would silently lose those entries. With managed mcp_config present,
	// daemon-written `$CODEX_HOME/config.toml` is the authoritative source and
	// stray `-c mcp_servers.*` overrides are dropped to keep last-wins from
	// re-shadowing it.
	if hasManagedCodexMcpConfig(mcpConfig) {
		extra = filterCodexCustomConfigOverrides(extra, logger)
		custom = filterCodexCustomConfigOverrides(custom, logger)
	}
	out := make([]string, 0, len(extra)+len(custom))
	out = append(out, extra...)
	out = append(out, custom...)
	return out
}

// enforceCodexFastMode makes the explicit agent service-tier selection
// authoritative over daemon ExtraArgs, per-agent CustomArgs, and inherited
// config.toml. Codex's `--disable fast_mode` wins over `--enable fast_mode`
// regardless of argv order, so conflicting lower-priority disable flags must
// be removed before the managed enable is appended. Conflicting `-c` /
// `--config features.fast_mode=...` overrides are removed for the same
// precedence reason, even though current Codex versions let `--enable` win:
// the final argv should encode one unambiguous owner for this setting.
//
// Other disabled features are preserved, and this helper is used only for the
// catalog-owned `priority` tier. Future service tiers must not accidentally
// inherit Fast mode semantics.
func enforceCodexFastMode(args []string, logger *slog.Logger) []string {
	args = filterCodexConfigOverrides(
		args,
		codexManagedFastModeConfigKeyRe,
		"features.fast_mode",
		logger,
	)
	filtered := make([]string, 0, len(args)+2)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		flag := arg
		value := ""
		hasInlineValue := false
		if idx := strings.Index(arg, "="); idx > 0 {
			flag = arg[:idx]
			value = arg[idx+1:]
			hasInlineValue = true
		}
		if flag == "--disable" {
			if !hasInlineValue && i+1 < len(args) {
				value = args[i+1]
			}
			if value == codexFastModeFeature {
				if logger != nil {
					logger.Warn("codex: ignored lower-priority feature disable",
						"feature", codexFastModeFeature)
				}
				if !hasInlineValue {
					i++
				}
				continue
			}
		}
		filtered = append(filtered, arg)
	}
	return append(filtered, "--enable", codexFastModeFeature)
}

// hasManagedCodexMcpConfig reports whether the agent's mcp_config field is
// "present" in the API three-state sense: a non-null JSON value. Both
// `{}` and `{"mcpServers":{}}` count as present (the admin saved an empty
// managed set — strict mode, no global fallback); only SQL NULL or the
// literal JSON `null` count as absent (CLI default).
func hasManagedCodexMcpConfig(raw json.RawMessage) bool {
	return hasManagedMcpConfig(raw)
}

// codexManagedMcpConfigKeyRe matches the daemon-managed config namespace
// (`mcp_servers.…`) when it appears as the value of a Codex `-c` /
// `--config` flag. Used by filterCodexCustomConfigOverrides to drop user
// overrides that would otherwise shadow what the MCP Tab writes into
// `$CODEX_HOME/config.toml`.
var codexManagedMcpConfigKeyRe = regexp.MustCompile(`^\s*mcp_servers(?:\s*\.|\s*=|\s*$)`)

var codexManagedFastModeConfigKeyRe = regexp.MustCompile(
	`^\s*features\s*\.\s*fast_mode\s*(?:=|$)`)

// A daemon-managed shell_environment_policy must also win over profile and
// custom-arg overrides. Match root and profile policy keys without catching an
// unrelated table field that happens to use the same final key name.
const (
	codexShellEnvPolicyKeyPattern = `(?:shell_environment_policy|"shell_environment_policy"|'shell_environment_policy')`
	codexProfileNameKeyPattern    = `(?:[A-Za-z0-9_-]+|"[^"]+"|'[^']+')`
)

var codexManagedShellEnvConfigKeyRe = regexp.MustCompile(
	`^\s*(?:` + codexShellEnvPolicyKeyPattern + `|profiles\s*\.\s*` + codexProfileNameKeyPattern + `\s*\.\s*` + codexShellEnvPolicyKeyPattern + `)\s*(?:\.|=|$)`)

// filterCodexCustomConfigOverrides drops `-c mcp_servers.…=` and
// `--config mcp_servers.…=` entries from custom args. Codex's `-c` is
// last-wins (verified against codex-cli 0.132.0), so without this filter a
// user-written `-c mcp_servers.fetch=…` in custom_args would silently
// override whatever the MCP Tab saved into the per-task config.toml. We
// own the `mcp_servers` namespace via the managed block, so user attempts
// to write into it are dropped with a warning rather than allowed to win.
// Other `-c`/`--config` keys (e.g. `-c model="o3"`) pass through unchanged.
func filterCodexCustomConfigOverrides(args []string, logger *slog.Logger) []string {
	return filterCodexConfigOverrides(args, codexManagedMcpConfigKeyRe, "mcp_servers", logger)
}

func filterCodexShellEnvConfigOverrides(args []string, logger *slog.Logger) []string {
	return filterCodexConfigOverrides(args, codexManagedShellEnvConfigKeyRe, shellEnvironmentPolicyConfigNamespace, logger)
}

const shellEnvironmentPolicyConfigNamespace = "shell_environment_policy"

func filterCodexConfigOverrides(args []string, managedKeyRe *regexp.Regexp, namespace string, logger *slog.Logger) []string {
	if len(args) == 0 {
		return args
	}
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		flag := arg
		inlineValue := ""
		hasInlineValue := false
		if idx := strings.Index(arg, "="); idx > 0 {
			flag = arg[:idx]
			inlineValue = arg[idx+1:]
			hasInlineValue = true
		}
		if flag == "-c" || flag == "--config" {
			value := inlineValue
			if !hasInlineValue && i+1 < len(args) {
				value = args[i+1]
			}
			if managedKeyRe.MatchString(value) {
				if logger != nil {
					// Log the key only, never the value. Managed config values
					// may contain secrets and must stay out of logs/argv.
					key := value
					if eqIdx := strings.Index(value, "="); eqIdx >= 0 {
						key = value[:eqIdx]
					}
					logger.Warn("custom_args: blocked managed Codex config override",
						"namespace", namespace, "flag", flag, "key", strings.TrimSpace(key))
				}
				if !hasInlineValue && i+1 < len(args) {
					i++ // skip the value arg
				}
				continue
			}
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

// Markers delimiting the daemon-managed `[mcp_servers.*]` block in
// `$CODEX_HOME/config.toml`. Match the existing sandbox / multi-agent /
// memory marker pattern so ops can grep all managed blocks consistently.
const (
	multicaCodexMcpBeginMarker = "# BEGIN multica-managed mcp_servers (do not edit; regenerated by daemon)"
	multicaCodexMcpEndMarker   = "# END multica-managed mcp_servers"
)

var codexMcpBlockRe = regexp.MustCompile(
	`(?ms)^` + regexp.QuoteMeta(multicaCodexMcpBeginMarker) +
		`.*?^` + regexp.QuoteMeta(multicaCodexMcpEndMarker) + `\n*`)

// userCodexMcpServersTableHeaderRe matches `[mcp_servers.<name>]` (and its
// quoted-key form `[mcp_servers."<name>"]`) at the start of a line. Used
// to strip user-provided mcp_servers tables from the per-task config when
// the agent has its own mcp_config — mirrors Claude's `--strict-mcp-config`
// model where the daemon's set is authoritative.
var userCodexMcpServersTableHeaderRe = regexp.MustCompile(
	`^\s*\[\s*mcp_servers\s*\.\s*(?:"[^"]*"|[^\]\s]+)\s*\]\s*(?:#.*)?$`)

// ensureCodexMcpConfig writes (or clears) the daemon-managed
// `[mcp_servers.*]` block in `$CODEX_HOME/config.toml`. The block is the
// authoritative source of MCP servers for this run: with mcp_config set
// in the agent UI the daemon also strips any inherited
// `[mcp_servers.*]` tables from the per-task config so the user's global
// `~/.codex/config.toml` doesn't shadow or collide with the managed set.
//
// The file mode is 0o600 because `mcp_servers.<id>.env` values may carry
// secrets (API keys, bearer tokens); the per-task home is owned by the
// daemon's user, so 0o600 keeps secrets out of any world-readable copy
// while still letting the codex child read them.
//
// A malformed mcp_config is returned as an error and the caller decides
// whether to surface or warn — same fail-soft contract the prior argv
// path had.
func ensureCodexMcpConfig(configPath string, mcpConfig json.RawMessage, logger *slog.Logger) error {
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config.toml: %w", err)
	}
	existing := string(data)

	// Always strip a prior managed block so reruns and clear-config flows
	// converge on a clean state.
	stripped := codexMcpBlockRe.ReplaceAllString(existing, "")

	managed := hasManagedCodexMcpConfig(mcpConfig)
	block, _, renderErr := renderCodexMcpServersBlock(mcpConfig)
	if renderErr != nil {
		return renderErr
	}

	var updated string
	if managed {
		// Agent has a managed MCP set (possibly empty — `{}` /
		// `{"mcpServers":{}}` count as "saved an empty set" in the API's
		// three-state semantics, distinct from nil/null which means
		// "fall back to CLI default"). Strip any user-defined
		// `[mcp_servers.*]` tables inherited from `~/.codex/config.toml`
		// so the managed set is strict — mirrors Claude's
		// `--strict-mcp-config`. Two reasons we cannot mix:
		//   1. TOML rejects redefining the same table; a user table
		//      named `[mcp_servers.fetch]` would crash codex if the
		//      agent also defined `fetch`.
		//   2. An admin saving an explicit list in the MCP Tab would
		//      otherwise see user-global servers silently joined in,
		//      which contradicts the UI affordance.
		stripped = stripCodexUserMcpServerTables(stripped)
		stripped = strings.TrimRight(stripped, "\n")
		// When the managed set is empty we still write the marker
		// block (with no tables between). This pins "managed but
		// empty" on disk so the next run can find and strip the
		// markers, and so the file's intent is grep-able by ops.
		if block == "" {
			block = multicaCodexMcpBeginMarker + "\n" + multicaCodexMcpEndMarker + "\n"
		}
		if stripped == "" {
			updated = block
		} else {
			updated = stripped + "\n\n" + block
		}
	} else {
		// No managed config: just remove any prior managed block and
		// leave inherited user tables alone (CLI default fallback).
		updated = stripped
	}

	if updated == existing {
		return nil
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	// os.WriteFile applies the mode only when creating a new file; if the
	// per-task config.toml was already on disk at 0o644 (the default mode
	// used by execenv.copyFile when seeding from ~/.codex/config.toml),
	// the secret-bearing values we just wrote would inherit that wider
	// mode. Chmod unconditionally to keep the secret in the daemon
	// owner's lane regardless of the prior mode.
	if err := os.Chmod(configPath, 0o600); err != nil {
		return fmt.Errorf("chmod config.toml to 0600: %w", err)
	}
	if logger != nil {
		logger.Debug("codex: wrote managed mcp_servers block to config.toml",
			"config_path", configPath, "managed", managed)
	}
	return nil
}

// renderCodexMcpServersBlock renders the agent's mcp_config JSON
// (Claude-style `{"mcpServers": {...}}`) as a TOML block of
// `[mcp_servers.<name>]` tables wrapped in BEGIN/END markers. Returns
// (block, hasServers, err); hasServers=false means the input had no
// servers to render (empty/null mcp_config) and the caller should only
// strip the prior managed block.
//
// Stdio server keys (`args`, `env`, `command`) pass through verbatim —
// Codex's config schema happens to use the same names today. Remote HTTP
// servers use Codex-specific keys, so they are normalised here rather than
// leaking provider details into the UI/dispatch layer.
func renderCodexMcpServersBlock(raw json.RawMessage) (string, bool, error) {
	if len(raw) == 0 {
		return "", false, nil
	}
	var parsed struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", false, fmt.Errorf("parse mcp_config json: %w", err)
	}
	if len(parsed.McpServers) == 0 {
		return "", false, nil
	}

	names := make([]string, 0, len(parsed.McpServers))
	for name := range parsed.McpServers {
		names = append(names, name)
	}
	sort.Strings(names)

	var sb strings.Builder
	sb.WriteString(multicaCodexMcpBeginMarker)
	sb.WriteString("\n")
	for i, name := range names {
		if !isCodexBareTomlKey(name) {
			return "", false, fmt.Errorf("mcp server name %q must be ASCII alphanumeric / _ / - to fit Codex's bare-key requirement", name)
		}
		var serverVal map[string]any
		if err := json.Unmarshal(parsed.McpServers[name], &serverVal); err != nil {
			return "", false, fmt.Errorf("mcp_servers.%s: %w", name, err)
		}
		if serverVal == nil {
			return "", false, fmt.Errorf("mcp_servers.%s must be a JSON object", name)
		}
		serverVal = normalizeCodexMcpServerConfig(serverVal)
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("[mcp_servers.")
		sb.WriteString(name)
		sb.WriteString("]\n")
		keys := make([]string, 0, len(serverVal))
		for k := range serverVal {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			tomlValue, err := jsonValueToCodexTOMLInline(serverVal[k])
			if err != nil {
				return "", false, fmt.Errorf("mcp_servers.%s.%s: %w", name, k, err)
			}
			sb.WriteString(codexTOMLKey(k))
			sb.WriteString(" = ")
			sb.WriteString(tomlValue)
			sb.WriteString("\n")
		}
	}
	sb.WriteString(multicaCodexMcpEndMarker)
	sb.WriteString("\n")
	return sb.String(), true, nil
}

func normalizeCodexMcpServerConfig(server map[string]any) map[string]any {
	if !isCodexRemoteMcpServer(server) {
		normalized := make(map[string]any, len(server))
		for k, v := range server {
			if isMulticaMcpSelectorKey(k) {
				continue
			}
			normalized[k] = v
		}
		return normalized
	}

	normalized := make(map[string]any, len(server)+1)
	for k, v := range server {
		switch {
		case isMulticaMcpSelectorKey(k):
			continue
		case k == "type":
			continue
		case k == "headers":
			if _, ok := server["http_headers"]; !ok {
				normalized["http_headers"] = v
			}
		default:
			normalized[k] = v
		}
	}
	normalized["experimental_use_rmcp_client"] = true
	return normalized
}

func isMulticaMcpSelectorKey(k string) bool {
	switch k {
	case "tools", "prompts", "resources":
		return true
	default:
		return false
	}
}

func isCodexRemoteMcpServer(server map[string]any) bool {
	if typ, ok := server["type"].(string); ok && strings.EqualFold(typ, "http") {
		return true
	}
	_, hasURL := server["url"]
	_, hasCommand := server["command"]
	return hasURL && !hasCommand
}

// stripCodexUserMcpServerTables removes every `[mcp_servers.*]` table
// (header + body lines until the next top-level table header or EOF) from
// a TOML config string. Sub-tables like `[mcp_servers.fetch.env]` count
// as part of the parent table and are dropped along with it.
func stripCodexUserMcpServerTables(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		if userCodexMcpServersTableHeaderRe.MatchString(line) {
			skipping = true
			continue
		}
		if skipping {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "[") {
				// Next table header. If it's still an `mcp_servers.*`
				// table (including a sub-table), keep skipping; otherwise
				// stop and emit this line.
				if userCodexMcpServersTableHeaderRe.MatchString(line) ||
					strings.HasPrefix(trimmed, "[mcp_servers.") ||
					strings.HasPrefix(trimmed, "[ mcp_servers.") {
					continue
				}
				skipping = false
				out = append(out, line)
				continue
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// jsonValueToCodexTOMLInline serialises a JSON value as a TOML inline
// value. Only the subset Codex's `-c` accepts is supported: strings,
// numbers, booleans, arrays, and inline tables. JSON nulls are rejected
// because TOML has no null and silently dropping them would be confusing.
func jsonValueToCodexTOMLInline(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", fmt.Errorf("null is not a valid TOML value")
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10), nil
		}
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case string:
		return codexTOMLBasicString(x), nil
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			p, err := jsonValueToCodexTOMLInline(e)
			if err != nil {
				return "", err
			}
			parts[i] = p
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			p, err := jsonValueToCodexTOMLInline(x[k])
			if err != nil {
				return "", err
			}
			parts[i] = codexTOMLKey(k) + " = " + p
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	default:
		return "", fmt.Errorf("unsupported value type %T", v)
	}
}

func codexTOMLBasicString(s string) string {
	var sb strings.Builder
	sb.Grow(len(s) + 2)
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			sb.WriteString(`\\`)
		case '"':
			sb.WriteString(`\"`)
		case '\b':
			sb.WriteString(`\b`)
		case '\t':
			sb.WriteString(`\t`)
		case '\n':
			sb.WriteString(`\n`)
		case '\f':
			sb.WriteString(`\f`)
		case '\r':
			sb.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				sb.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

func codexTOMLKey(s string) string {
	if isCodexBareTomlKey(s) {
		return s
	}
	return codexTOMLBasicString(s)
}

func isCodexBareTomlKey(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func (b *codexBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	firstSession, err := b.executeOnce(ctx, prompt, opts, 1)
	if err != nil {
		return nil, err
	}
	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer close(msgCh)
		defer close(resCh)
		session := firstSession
		attemptOpts := opts
		for attempt := 1; attempt <= 2; attempt++ {
			if attempt > 1 {
				var err error
				session, err = b.executeOnce(ctx, prompt, attemptOpts, attempt)
				if err != nil {
					resCh <- Result{Status: "failed", Error: err.Error()}
					return
				}
			}
			// Hold back the leading session-pin status messages until this
			// attempt proves it made real progress. A retry never continues the
			// discarded attempt's thread (initialize retries fail before any
			// thread exists; catalog retries clear ResumeSessionID below), so
			// forwarding its pin would leave the resume pointer aimed at a
			// thread that never produced a turn (MUL-5110). The first non-pin
			// message means the attempt is live: flush and stream from then on.
			var heldPins []Message
			holdingPins := true
			flushHeldPins := func() {
				for _, held := range heldPins {
					msgCh <- held
				}
				heldPins = nil
				holdingPins = false
			}
			for msg := range session.Messages {
				if holdingPins && msg.Type == MessageStatus && msg.Status == "running" {
					heldPins = append(heldPins, msg)
					continue
				}
				if holdingPins {
					flushHeldPins()
				}
				msgCh <- msg
			}
			result, ok := <-session.Result
			if !ok {
				flushHeldPins()
				resCh <- Result{Status: "failed", Error: "codex attempt closed without result"}
				return
			}
			retryReason := ""
			switch {
			case result.codexInitializeRetrySafe:
				retryReason = "initialize"
			case result.codexStartupRefreshRetrySafe:
				retryReason = "model_catalog_refresh"
			}
			if retryReason == "" || attempt == 2 {
				flushHeldPins()
				resCh <- result
				return
			}
			// The model catalog refresh reaches the network, so give the
			// transient failure a little more room than the local initialize
			// handshake gets. Both stay well inside the task timeout, and
			// ctx.Done() below keeps the retry from extending it.
			backoff := 75*time.Millisecond + time.Duration(time.Now().UnixNano()%50)*time.Millisecond
			if retryReason == "model_catalog_refresh" {
				backoff = 500*time.Millisecond + time.Duration(time.Now().UnixNano()%1000)*time.Millisecond
				// The stalled attempt already reached turn/started, so the prior
				// thread may hold the submitted input or an unfinished turn.
				// Resuming it again could duplicate that input; start a fresh
				// thread instead and keep ResumeExpected so codexTurnInput
				// prepends the continuity notice about the lost context.
				if attemptOpts.ResumeSessionID != "" {
					b.cfg.Logger.Warn("codex retry dropping resume pointer after model catalog refresh failure",
						"prior_thread_id", attemptOpts.ResumeSessionID,
					)
					attemptOpts.ResumeSessionID = ""
					attemptOpts.ResumeExpected = true
				}
			}
			b.cfg.Logger.Warn("codex retry scheduled", "reason", retryReason, "attempt", attempt, "next_attempt", attempt+1, "backoff", backoff.String())
			select {
			case <-ctx.Done():
				flushHeldPins()
				resCh <- result
				return
			case <-time.After(backoff):
			}
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func (b *codexBackend) executeOnce(ctx context.Context, prompt string, opts ExecOptions, attempt int) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "codex"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("codex executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	semanticInactivityTimeout := opts.SemanticInactivityTimeout
	if semanticInactivityTimeout == 0 {
		semanticInactivityTimeout = defaultCodexSemanticInactivityTimeout
	}
	handshakeTimeout := opts.HandshakeTimeout
	if handshakeTimeout <= 0 {
		handshakeTimeout = defaultCodexHandshakeTimeout
	}
	runCtx, cancel := runContext(ctx, timeout)

	// Materialise the agent's MCP config into the per-task
	// `$CODEX_HOME/config.toml`. Argv would be the simpler path, but
	// `mcp_servers.<id>.env` is allowed to carry secrets (Codex docs:
	// https://developers.openai.com/codex/mcp#configure-with-configtoml)
	// and our UI already treats mcp_config as a redacted-for-non-admins
	// field. Process argv ends up in OS-level `ps` listings and is also
	// echoed into the daemon's `agent command` log line below, so any
	// inline env-bearing TOML would defeat the redaction. Writing through
	// config.toml at 0o600 keeps the secret values out of argv and logs.
	codexHome := strings.TrimSpace(b.cfg.Env["CODEX_HOME"])
	if codexHome != "" {
		if err := ensureCodexMcpConfig(filepath.Join(codexHome, "config.toml"), opts.McpConfig, b.cfg.Logger); err != nil {
			// Fail closed when we can't materialise the managed config.
			// Warning-and-launching would silently fall back to the
			// user's global `~/.codex/config.toml` MCP servers and
			// look indistinguishable from "the saved config was
			// applied", which is exactly the surprise the MCP Tab is
			// supposed to remove.
			cancel()
			return nil, fmt.Errorf("apply codex mcp_config: %w", err)
		}
	} else if hasManagedCodexMcpConfig(opts.McpConfig) {
		// Managed mcp_config saved but no CODEX_HOME to anchor it.
		// Same reasoning as above: silently launching would inherit
		// whatever MCP setup the host user has, which is the wrong
		// shape of failure.
		cancel()
		return nil, fmt.Errorf("codex: mcp_config is set but CODEX_HOME env var is not configured; cannot apply managed MCP")
	}

	if codexHome != "" {
		// The daemon owns shell_environment_policy in the task-local config.
		// Codex -c/--config overrides are last-wins, so remove user-provided
		// root or profile policy overrides before building the final argv.
		opts.ExtraArgs = filterCodexShellEnvConfigOverrides(opts.ExtraArgs, b.cfg.Logger)
		opts.CustomArgs = filterCodexShellEnvConfigOverrides(opts.CustomArgs, b.cfg.Logger)
	}
	codexArgs := buildCodexArgs(opts, b.cfg.Logger)
	cmd := exec.CommandContext(runCtx, execPath, codexArgs...)
	hideAgentWindow(cmd)
	// Run codex in its own process group so a cancel-on-stuck cleanup
	// reaches the whole tree — the codex Node wrapper plus the native
	// Rust app-server it spawns — not just the direct child. Without
	// this, killing the leader leaves grandchildren as orphans that
	// keep consuming memory until the OS reaps them; see #4520, where a
	// scanner overflow during thread/resume otherwise leaked Codex
	// processes indefinitely. configureProcessGroup is a no-op on
	// Windows.
	configureProcessGroup(cmd)
	// Override the default exec.CommandContext cancel behaviour. The
	// default sends SIGKILL only to cmd.Process (the leader); we instead
	// signal the whole process group so descendants die too. Returning
	// nil keeps exec from logging a spurious error; cmd.WaitDelay below
	// still backstops cmd.Wait() if the kill leaves an open pipe.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			signalProcessGroup(cmd.Process, syscall.SIGKILL)
		}
		return nil
	}
	// Bound the wait after the context is cancelled so a stuck child (or an
	// open pipe held by a grandchild) can't hang cmd.Wait() forever. Matches
	// the other long-lived backends (claude, copilot, cursor, …).
	cmd.WaitDelay = codexProcessWaitDelay()
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", codexArgs)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("codex stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("codex stdin pipe: %w", err)
	}
	// Codex stderr can contain auth/provider diagnostics. Capture a bounded
	// tail and emit it only through the sanitizer in the cleanup event.
	stderrBuf := newStderrTail(io.Discard, codexStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start codex: %w", err)
	}
	activeLaunches := activeCodexLaunches.Add(1)
	for {
		maxSeen := maxActiveCodexLaunchesObserved.Load()
		if activeLaunches <= maxSeen || maxActiveCodexLaunchesObserved.CompareAndSwap(maxSeen, activeLaunches) {
			break
		}
	}
	launchStarted := time.Now()
	codexVersion := strings.TrimSpace(b.cfg.CodexVersion)
	if codexVersion == "" {
		codexVersion = "unknown"
	}

	b.cfg.Logger.Info("codex lifecycle", "phase", "spawn", "task_id", b.cfg.TaskID, "runtime_id", b.cfg.RuntimeID, "pid", cmd.Process.Pid, "process_group", cmd.Process.Pid, "cwd", opts.Cwd, "attempt", attempt, "active_launches", activeLaunches, "codex_version", codexVersion, "daemon_version", b.cfg.DaemonVersion)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	semanticActivityCh := make(chan string, 256)

	var outputMu sync.Mutex
	var output strings.Builder
	var semanticObserved atomic.Bool
	turnNotificationGate := &codexTurnNotificationGate{}

	// turnDone is set before starting the reader goroutine so there is no
	// race between the lifecycle goroutine writing and the reader reading.
	turnDone := make(chan bool, 1) // true = aborted

	c := &codexClient{
		cfg:                  b.cfg,
		stdin:                stdin,
		pending:              make(map[int]*pendingRPC),
		processDone:          make(chan struct{}),
		handshakeTimeout:     handshakeTimeout,
		pid:                  cmd.Process.Pid,
		attempt:              attempt,
		activeLaunches:       activeLaunches,
		notificationProtocol: "unknown",
		acceptNotification:   turnNotificationGate.accept,
		onDiscardedNotification: func(string, map[string]any) {
			// Any app-server notification proves the process made semantic
			// progress, even when it is intentionally excluded from the active
			// turn. Preserve initialize-retry safety without replaying content.
			semanticObserved.Store(true)
		},
		onMessage: func(msg Message) {
			logCodexAgentMessage(b.cfg.Logger, msg)
			if msg.Type == MessageText {
				outputMu.Lock()
				output.WriteString(msg.Content)
				outputMu.Unlock()
			}
			trySend(msgCh, msg)
			trySendString(semanticActivityCh, describeCodexSemanticActivity(msg))
			if describeCodexSemanticActivity(msg) != "" {
				semanticObserved.Store(true)
			}
		},
		onSemanticActivity: func(description string) {
			semanticObserved.Store(true)
			b.cfg.Logger.Debug("codex semantic activity observed", "activity", description)
			trySendString(semanticActivityCh, description)
		},
		onTurnDone: func(aborted bool) {
			select {
			case turnDone <- aborted:
			default:
			}
		},
	}

	// Start reading stdout in background
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			c.handleLine(line)
		}
		if err := scanner.Err(); err != nil {
			c.markProcessExited(fmt.Errorf("%w: %v", errCodexProcessExited, err))
			return
		}
		c.markProcessExited(errCodexProcessExited)
	}()

	// drainAndWait closes stdin so codex shuts down, then joins cmd.Wait().
	// cmd.Wait() is the only Go-stdlib-documented synchronization point for
	// os/exec's internal stderr/stdout copy goroutines — until it returns,
	// stderrBuf may not have observed every byte codex wrote before it
	// exited, and stderrBuf.Tail() can come back empty or truncated. Any
	// code that reads stderrBuf.Tail() must call drainAndWait() first.
	// sync.Once makes it safe to call from both error paths and the deferred
	// cleanup.
	//
	// drainAndWait is also the cleanup safety net for the scanner-overflow
	// path (#4520). When codex emits a single stdout line larger than the
	// scanner's MaxScanTokenSize, the reader goroutine returns with
	// scanner.Err() set, fails all in-flight RPCs via markProcessExited, and
	// closes readerDone — but the codex child process is still alive and is
	// now blocked trying to write the rest of the oversized line into a
	// stdout pipe nobody is reading. A naive stdin.Close()+cmd.Wait() then
	// hangs forever: codex never reaches its stdin-read syscall, so it never
	// sees EOF, never exits, and cmd.Wait() never returns. The lifecycle
	// goroutine therefore never sends a failed Result, the outer daemon
	// blocks on its result channel, and the higher-level fresh-session
	// fallback never fires.
	//
	// To stay correct under both clean shutdown and the stuck-child case,
	// drainAndWait runs in two bounded phases:
	//
	//  1. Close stdin and wait for the reader goroutine to finish, capped by
	//     codexGracefulShutdownTimeout. The reader exits when codex closes
	//     stdout on its own (clean shutdown — gives OTEL batch exporters a
	//     chance to flush) OR when the scanner errors out (overflow case —
	//     readerDone is already closed and the select returns immediately).
	//     Per os/exec docs, calling cmd.Wait() while reads are still
	//     in-flight on a StdoutPipe-returned pipe is incorrect because Wait
	//     closes the pipe and turns pending reads into spurious errors, so
	//     we must wait for the reader first.
	//
	//  2. Wait for cmd.Wait() to return, capped by another
	//     codexGracefulShutdownTimeout. Normally this returns immediately
	//     because the process has already exited. In the stuck-child case
	//     the process is still alive — we cancel the runCtx, which fires
	//     cmd.Cancel (the group-SIGKILL helper installed above), and
	//     cmd.WaitDelay then guarantees cmd.Wait() returns even if pipes
	//     stay open.
	var waitOnce sync.Once
	var cleanupConfirmed bool
	var waitReturned bool
	var cleanupWaitErr error
	drainAndWait := func() {
		waitOnce.Do(func() {
			stdin.Close()

			grace := codexGracefulShutdown()
			waitCh := make(chan struct{})
			var startWait sync.Once
			startProcessWait := func() {
				startWait.Do(func() {
					go func() {
						cleanupWaitErr = cmd.Wait()
						close(waitCh)
					}()
				})
			}

			// Phase 1: let the reader finish before invoking cmd.Wait().
			select {
			case <-readerDone:
				// reader drained cleanly (codex shutdown closed stdout)
				// or aborted early (e.g. scanner overflow). Either way it
				// is now safe to call cmd.Wait().
			case <-time.After(grace):
				// codex did not close stdout within the grace window. Force
				// the shutdown via context cancellation — cmd.Cancel
				// group-kills the tree, the reader unblocks when stdout
				// EOFs, and we proceed to phase 2.
				b.cfg.Logger.Warn("codex did not close stdout after stdin EOF; forcing shutdown",
					"pid", cmd.Process.Pid,
					"grace", grace.String(),
				)
				cancel()
				// On Windows, Cancel terminates only the direct child. A
				// descendant may keep inherited stdout open indefinitely. Start
				// Wait now so os/exec's WaitDelay closes the pipe after its
				// bounded deadline and lets the reader finish.
				startProcessWait()
				<-waitCh
				select {
				case <-readerDone:
				case <-time.After(grace):
					b.cfg.Logger.Warn("codex stdout reader remained open after bounded process wait",
						"pid", cmd.Process.Pid,
						"grace", grace.String(),
					)
				}
			}

			// Phase 2: bound cmd.Wait() in case the process is still alive
			// (scanner-overflow case: reader exited early on its own while
			// codex stayed blocked writing into a full stdout pipe).
			startProcessWait()
			select {
			case <-waitCh:
				waitReturned = true
				// reaped cleanly.
			case <-time.After(grace):
				b.cfg.Logger.Warn("codex process still alive after reader exited; forcing shutdown",
					"pid", cmd.Process.Pid,
					"grace", grace.String(),
				)
				cancel()
				// WaitDelay (10s) is the final backstop: even if the
				// group-kill races with an open pipe held by a
				// descendant, cmd.Wait() returns within WaitDelay of the
				// cancel.
				<-waitCh
				waitReturned = true
			}
			// Wait returning with a ProcessState is the os/exec reap boundary.
			// On Unix, ProcessState.Exited reports false for a process terminated
			// by SIGKILL even though Wait successfully reaped it.
			cleanupConfirmed = waitReturned && cmd.ProcessState != nil && waitProcessGroupGone(cmd.Process, grace)
			if codexCleanupConfirmationOverride.Load() < 0 {
				cleanupConfirmed = false
			}
			b.cfg.Logger.Info("codex lifecycle",
				"phase", "cleanup",
				"task_id", b.cfg.TaskID,
				"runtime_id", b.cfg.RuntimeID,
				"pid", cmd.Process.Pid,
				"process_group", cmd.Process.Pid,
				"attempt", attempt,
				"latency", time.Since(launchStarted).Round(time.Millisecond).String(),
				"reaped", cleanupConfirmed,
				"exit_status", codexProcessExitStatus(cmd.ProcessState),
				"wait_error", cleanupWaitErr,
				"stderr_bytes", stderrBuf.TotalBytes(),
				"stderr_truncated", stderrBuf.TotalBytes() > codexStderrTailBytes,
			)
		})
	}

	// Drive the session lifecycle in a goroutine.
	// Shutdown sequence: lifecycle goroutine closes stdin + cancels context →
	// codex process exits → reader goroutine's scanner.Scan() returns false →
	// readerDone closes → lifecycle goroutine collects final output and sends Result.
	go func() {
		defer activeCodexLaunches.Add(-1)
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer drainAndWait()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string

		// 1. Initialize handshake
		initializeStarted := time.Now()
		b.cfg.Logger.Info("codex lifecycle", "phase", "initialize_sent", "task_id", b.cfg.TaskID, "runtime_id", b.cfg.RuntimeID, "pid", cmd.Process.Pid, "attempt", attempt, "active_launches", activeLaunches)
		_, err := c.request(runCtx, "initialize", map[string]any{
			"clientInfo": map[string]any{
				"name":    "multica-agent-sdk",
				"title":   "Multica Agent SDK",
				"version": "0.2.0",
			},
			"capabilities": map[string]any{
				"experimentalApi": true,
			},
		})
		if err != nil {
			initializeLatency := time.Since(initializeStarted)
			var handshakeErr *codexHandshakeTimeoutError
			timedOut := errors.As(err, &handshakeErr) && handshakeErr.Method == "initialize"
			if timedOut {
				// A timed-out initialize may still complete after the host gives up.
				// Kill the whole process group before waiting so a leader that exits
				// on stdin EOF cannot leave detached-stdio descendants behind.
				signalProcessGroup(cmd.Process, syscall.SIGKILL)
			}
			drainAndWait() // flush os/exec stderr goroutine before sampling Tail
			finalStatus = "failed"
			finalError = fmt.Sprintf("codex initialize failed: %v", err)
			contextEnded := errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
			if !timedOut && !contextEnded {
				// Timeout stderr is untrusted provider output and may echo opaque
				// Config.Env/auth values that pattern sanitization cannot identify.
				// The same applies when the parent task deadline/cancellation wins
				// the race against the per-RPC handshake timeout.
				// Keep it out of persisted/user-visible Results; cleanup lifecycle
				// still records bounded byte/truncation metadata.
				finalError = withAgentStderr(finalError, "codex", sanitizeCodexDiagnostic(stderrBuf.Tail()))
			}
			retrySafe := timedOut && !semanticObserved.Load() && cleanupConfirmed && codexInitializeRetrySupported()
			if timedOut && !cleanupConfirmed {
				finalError += "; retry suppressed: process cleanup/reap not confirmed"
			} else if timedOut && cleanupConfirmed && !codexInitializeRetrySupported() {
				finalError += "; retry suppressed: process-tree cleanup cannot be confirmed on this platform"
			}
			b.cfg.Logger.Warn("codex lifecycle", "phase", "initialize_failure", "task_id", b.cfg.TaskID, "runtime_id", b.cfg.RuntimeID, "pid", cmd.Process.Pid, "attempt", attempt, "latency", initializeLatency.Round(time.Millisecond).String(), "semantic_activity", semanticObserved.Load(), "cleanup_confirmed", cleanupConfirmed, "retry_safe", retrySafe)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds(), codexInitializeRetrySafe: retrySafe}
			return
		}
		b.cfg.Logger.Info("codex lifecycle", "phase", "initialize_response", "task_id", b.cfg.TaskID, "runtime_id", b.cfg.RuntimeID, "pid", cmd.Process.Pid, "attempt", attempt, "latency", time.Since(initializeStarted).Round(time.Millisecond).String())
		c.notify("initialized")

		// 2. Start a new thread, or resume the prior one for this issue. When
		// resume fails (thread GCed on the server, schema drift, etc.) we fall
		// back to a fresh thread so the task still makes progress.
		threadID, resumed, err := c.startOrResumeThread(runCtx, opts, b.cfg.Logger)
		if err != nil {
			var handshakeErr *codexHandshakeTimeoutError
			timedOut := errors.As(err, &handshakeErr) && handshakeErr.Method == "thread/start"
			if timedOut {
				// A timed-out thread/start has an uncertain provider outcome. Kill
				// the whole process group before waiting so a leader that exits on
				// EOF cannot leave detached-stdio descendants behind.
				signalProcessGroup(cmd.Process, syscall.SIGKILL)
			}
			drainAndWait() // flush os/exec stderr goroutine before sampling Tail
			finalStatus = "failed"
			stderrTail := sanitizeCodexDiagnostic(stderrBuf.Tail())
			finalError = err.Error()
			if c.threadStartSent {
				classification := classifyCodexStartupStderr(stderrTail, timedOut)
				b.cfg.Logger.Warn("codex lifecycle",
					"phase", "thread_start_failure",
					"task_id", b.cfg.TaskID,
					"runtime_id", b.cfg.RuntimeID,
					"pid", cmd.Process.Pid,
					"attempt", attempt,
					"active_launches", activeLaunches,
					"method", "thread/start",
					"latency", time.Since(c.threadStartStarted).Round(time.Millisecond).String(),
					"latency_ms", time.Since(c.threadStartStarted).Milliseconds(),
					"cleanup_confirmed", cleanupConfirmed,
					"reaped", cleanupConfirmed,
					"retry_safe", false,
					"retry_attempted", false,
					"stderr_model_refresh_timeout_count", classification.modelRefreshTimeout,
					"stderr_mcp_init_transport_count", classification.mcpInitTransport,
					"stderr_bare_timeout_count", classification.bareTimeout,
				)
			}
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		c.threadID = threadID
		if resumed {
			b.cfg.Logger.Info("codex thread resumed", "thread_id", threadID)
		} else {
			b.cfg.Logger.Info("codex thread started", "thread_id", threadID)
		}

		// 3. Send turn and wait for completion. When a resume was expected but we
		// ended up on a fresh thread (the live thread/resume RPC was rejected — a
		// corrupt/incompatible rollout, server-side thread GC, schema drift — or a
		// transport failure forced a fresh retry), prepend a continuity notice so
		// the agent tells the user the prior conversation could not be restored.
		// The daemon's pre-flight gates only catch cases detectable before launch;
		// this covers the ones only the live resume reveals (MUL-4424).
		turnParams := map[string]any{
			"threadId": threadID,
			"input":    codexTurnInput(prompt, opts.ResumeExpected, resumed),
		}
		// Per-turn reasoning override. Mirrors the per-thread injection in
		// startOrResumeThread; keeping both in sync is enforced by the
		// shared `codexReasoningInjection` fixture in codex_test.go (see
		// MUL-2339 — Trump's constraint that the three injection points
		// must not drift independently).
		applyCodexReasoningEffort(turnParams, opts.ThinkingLevel)
		applyCodexServiceTier(turnParams, opts.ServiceTier)
		waitingForTurn := true
		var timeoutDiagnostic codexTimeoutDiagnostic
		var processExitErr error
		finishTurn := func(aborted bool) {
			waitingForTurn = false
			switch {
			case aborted:
				finalStatus = "aborted"
				if errMsg := c.getTurnError(); errMsg != "" {
					finalError = errMsg
				} else {
					finalError = "turn was aborted"
				}
			default:
				if errMsg := c.getTurnError(); errMsg != "" {
					finalStatus = "failed"
					finalError = errMsg
				}
			}
		}
		turnNotificationGate.arm()
		_, err = c.request(runCtx, "turn/start", turnParams)
		if err != nil {
			select {
			case aborted := <-turnDone:
				finishTurn(aborted)
			default:
				drainAndWait() // flush os/exec stderr goroutine before sampling Tail
				finalStatus = "failed"
				finalError = withAgentStderr(fmt.Sprintf("codex turn/start failed: %v", err), "codex", sanitizeCodexDiagnostic(stderrBuf.Tail()))
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
		}

		lastSemanticActivity := time.Now()
		lastSemanticActivityDescription := "turn/start"
		semanticTimer := time.NewTimer(semanticInactivityTimeout)
		defer semanticTimer.Stop()

		firstTurnNoProgressTimeout := codexFirstTurnNoProgressTimeout(semanticInactivityTimeout)
		var firstTurnNoProgressTimer *time.Timer
		var firstTurnNoProgressTimerC <-chan time.Time
		firstTurnStarted := false
		firstTurnProgressObserved := false
		stopFirstTurnNoProgressTimer := func() {
			if firstTurnNoProgressTimer == nil {
				return
			}
			stopTimer(firstTurnNoProgressTimer)
			firstTurnNoProgressTimerC = nil
		}
		defer stopFirstTurnNoProgressTimer()

		finishRunContextDone := func() {
			waitingForTurn = false
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("codex timed out after %s", timeout)
			} else {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			}
		}
		for waitingForTurn {
			select {
			case aborted := <-turnDone:
				finishTurn(aborted)
			case activity := <-semanticActivityCh:
				lastSemanticActivity = time.Now()
				lastSemanticActivityDescription = activity
				resetTimer(semanticTimer, semanticInactivityTimeout)
				if activity == "status:running" && !firstTurnStarted {
					firstTurnStarted = true
					firstTurnNoProgressTimer = time.NewTimer(firstTurnNoProgressTimeout)
					firstTurnNoProgressTimerC = firstTurnNoProgressTimer.C
				} else if firstTurnStarted && !firstTurnProgressObserved && isCodexFirstTurnProgressActivity(activity) {
					firstTurnProgressObserved = true
					stopFirstTurnNoProgressTimer()
				}
			case <-firstTurnNoProgressTimerC:
				waitingForTurn = false
				finalStatus = "timeout"
				timeoutDiagnostic = codexTimeoutDiagnostic{
					Kind:         codexTimeoutFirstTurnNoProgress,
					Timeout:      firstTurnNoProgressTimeout,
					LastActivity: lastSemanticActivityDescription,
					ThreadID:     threadID,
					TurnID:       c.turnID,
					Model:        opts.Model,
				}
				b.cfg.Logger.Warn(CodexFirstTurnNoProgressMarker,
					"pid", cmd.Process.Pid,
					"thread_id", threadID,
					"turn_id", c.turnID,
					"timeout", firstTurnNoProgressTimeout.String(),
					"last_activity", lastSemanticActivityDescription,
				)
			case <-semanticTimer.C:
				waitingForTurn = false
				finalStatus = "timeout"
				timeoutDiagnostic = codexTimeoutDiagnostic{
					Kind:         codexTimeoutSemanticInactivity,
					Timeout:      semanticInactivityTimeout,
					LastActivity: lastSemanticActivityDescription,
					ThreadID:     threadID,
					TurnID:       c.turnID,
					Model:        opts.Model,
				}
				b.cfg.Logger.Warn(CodexSemanticInactivityMarker,
					"pid", cmd.Process.Pid,
					"thread_id", threadID,
					"turn_id", c.turnID,
					"timeout", semanticInactivityTimeout.String(),
					"last_activity", lastSemanticActivityDescription,
					"idle_for", time.Since(lastSemanticActivity).Round(time.Millisecond).String(),
				)
			case <-runCtx.Done():
				finishRunContextDone()
			case <-c.processDone:
				select {
				case aborted := <-turnDone:
					finishTurn(aborted)
				default:
					if runCtx.Err() != nil {
						finishRunContextDone()
					} else {
						waitingForTurn = false
						finalStatus = "failed"
						processExitErr = c.getProcessErr()
						if processExitErr == nil {
							processExitErr = errCodexProcessExited
						}
						finalError = processExitErr.Error()
					}
				}
			}
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("codex finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		// Run cleanup. drainAndWait handles the graceful-then-cancel pattern
		// in two bounded phases (see its declaration): wait for the reader,
		// then wait for cmd.Wait(), force-cancelling either if the grace
		// window expires. A clean shutdown lets codex flush OTEL telemetry;
		// a stuck process is killed via the process-group SIGKILL.
		drainAndWait()

		if processExitErr != nil {
			finalError = withAgentStderr(processExitErr.Error(), "codex", sanitizeCodexDiagnostic(stderrBuf.Tail()))
		}
		stderrTail := sanitizeCodexDiagnostic(stderrBuf.Tail())
		if timeoutDiagnostic.Kind != codexTimeoutNone {
			timeoutDiagnostic.CodexVersion = detectCodexVersionForDiagnostics(context.Background(), execPath, cmd.Env, b.cfg.Logger)
			finalError = buildCodexTimeoutDiagnosticError(timeoutDiagnostic, stderrTail)
		}

		// A first turn that produced no semantic progress because Codex could
		// not load its model catalog is a startup-only failure: no tool ran and
		// no content reached the user, so replaying the prompt cannot duplicate
		// side effects. Reuse the same process-tree evidence initialize retries
		// require (cleanupConfirmed plus platform support) rather than a bare
		// ProcessState check: on Windows the daemon cannot prove the whole tree
		// is gone, and a surviving app-server would race the retry.
		startupRefreshRetrySafe := timeoutDiagnostic.Kind == codexTimeoutFirstTurnNoProgress &&
			!firstTurnProgressObserved &&
			strings.Contains(stderrTail, codexModelCatalogRefreshFailureSignal) &&
			cleanupConfirmed && codexInitializeRetrySupported()
		if startupRefreshRetrySafe {
			b.cfg.Logger.Warn("codex startup model catalog refresh failure is retry safe",
				"pid", cmd.Process.Pid,
				"thread_id", threadID,
				"attempt", attempt,
			)
		}

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		// Build usage map from accumulated codex usage.
		// First check JSON-RPC notifications (often empty for Codex).
		var usageMap map[string]TokenUsage
		c.usageMu.Lock()
		u := c.usage
		c.usageMu.Unlock()

		// Fallback: if no usage from JSON-RPC, scan Codex session JSONL logs.
		// Codex writes token_count events to $CODEX_HOME/sessions/YYYY/MM/DD/*.jsonl;
		// scan this backend's per-task CODEX_HOME, since sessions are isolated
		// there rather than in the shared ~/.codex/sessions (MUL-4424).
		if u.InputTokens == 0 && u.OutputTokens == 0 {
			taskCodexHome := strings.TrimSpace(b.cfg.Env["CODEX_HOME"])
			if scanned := scanCodexSessionUsage(startTime, taskCodexHome, threadID, resumed); scanned != nil {
				u = scanned.usage
				if scanned.model != "" && opts.Model == "" {
					opts.Model = scanned.model
				}
			}
		}

		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := opts.Model
			if model == "" {
				model = "unknown"
			}
			usageMap = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:                       finalStatus,
			Output:                       finalOutput,
			Error:                        finalError,
			SessionID:                    threadID,
			DurationMs:                   duration.Milliseconds(),
			Usage:                        usageMap,
			codexStartupRefreshRetrySafe: startupRefreshRetrySafe,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// codexResumeUnavailableNotice is prepended to the first turn's input when a
// resume was expected but Codex ended up on a fresh thread. It mirrors the
// daemon brief's Session Continuity Notice so the disclosure is identical
// whether the loss is detected pre-launch (daemon gate) or only by the live
// thread/resume RPC (MUL-4424).
const codexResumeUnavailableNotice = "[System notice] You were expected to continue an earlier conversation, but restoring that session failed and this is a fresh thread with no memory of the previous turns. Rebuild context from the issue/thread, and when you reply, tell the user up front (one short sentence) that the previous conversation context could not be restored and this is a new session.\n\n"

// codexTurnInput builds the input content for the first turn/start. When a
// resume was expected (resumeExpected) but the backend landed on a fresh thread
// (!resumed), it prepends codexResumeUnavailableNotice so the user learns the
// prior context was lost instead of the run silently continuing as new. The
// notice is folded into the same text block as the prompt to stay within the
// single-text-block turn input Codex already accepts.
func codexTurnInput(prompt string, resumeExpected, resumed bool) []map[string]any {
	text := prompt
	if resumeExpected && !resumed {
		text = codexResumeUnavailableNotice + prompt
	}
	return []map[string]any{{"type": "text", "text": text}}
}

// startOrResumeThread picks between Codex's thread/resume and thread/start
// based on opts.ResumeSessionID. When a prior thread ID is provided it first
// tries thread/resume; recoverable protocol errors (unknown thread, schema
// mismatch) fall back to thread/start so the task still executes, while
// transport/process failures fail fast because the app-server can no longer
// answer a fresh start request. The returned threadID is what subsequent
// turn/start calls must reference, and resumed indicates whether the prior
// thread was picked up (only useful for logging).
func (c *codexClient) startOrResumeThread(ctx context.Context, opts ExecOptions, logger *slog.Logger) (string, bool, error) {
	if priorThreadID := opts.ResumeSessionID; priorThreadID != "" {
		// thread/resume reuses the thread's persisted model and reasoning
		// effort; only override fields the daemon actually cares about.
		resumeParams := map[string]any{
			"threadId":              priorThreadID,
			"cwd":                   opts.Cwd,
			"model":                 nilIfEmpty(opts.Model),
			"developerInstructions": nilIfEmpty(opts.SystemPrompt),
		}
		// Explicit override of the persisted reasoning effort: without
		// this, a Codex resume silently reuses whatever level the prior
		// session was created with, even when the user has flipped the
		// agent's thinking_level since. See MUL-2339 — Elon flagged that
		// resume must honour the live config, not the stored one.
		applyCodexReasoningEffort(resumeParams, opts.ThinkingLevel)
		applyCodexServiceTier(resumeParams, opts.ServiceTier)
		resumeResult, err := c.request(ctx, "thread/resume", resumeParams)
		if err == nil {
			if threadID := extractThreadID(resumeResult); threadID != "" {
				return threadID, true, nil
			}
			logger.Warn("codex thread/resume returned no thread ID; falling back to thread/start", "prior_thread_id", priorThreadID)
		} else {
			if isCodexTransportError(err) {
				logger.Warn("codex thread/resume failed due to transport error; not falling back to thread/start", "prior_thread_id", priorThreadID, "error", err)
				return "", false, fmt.Errorf("codex thread/resume failed: %w", err)
			}
			logger.Warn("codex thread/resume failed; falling back to thread/start", "prior_thread_id", priorThreadID, "error", err)
		}
	}

	startParams := map[string]any{
		"model":                  nilIfEmpty(opts.Model),
		"modelProvider":          nil,
		"profile":                nil,
		"cwd":                    opts.Cwd,
		"approvalPolicy":         nil,
		"sandbox":                nil,
		"config":                 nil,
		"baseInstructions":       nil,
		"developerInstructions":  nilIfEmpty(opts.SystemPrompt),
		"compactPrompt":          nil,
		"includeApplyPatchTool":  nil,
		"experimentalRawEvents":  false,
		"persistExtendedHistory": true,
	}
	applyCodexReasoningEffort(startParams, opts.ThinkingLevel)
	applyCodexServiceTier(startParams, opts.ServiceTier)
	c.threadStartSent = true
	c.threadStartStarted = time.Now()
	logger.Info("codex lifecycle",
		"phase", "thread_start_sent",
		"task_id", c.cfg.TaskID,
		"runtime_id", c.cfg.RuntimeID,
		"pid", c.pid,
		"attempt", c.attempt,
		"active_launches", c.activeLaunches,
		"method", "thread/start",
	)
	startResult, err := c.request(ctx, "thread/start", startParams)
	if err != nil {
		return "", false, fmt.Errorf("codex thread/start failed: %w", err)
	}
	threadID := extractThreadID(startResult)
	if threadID == "" {
		return "", false, fmt.Errorf("codex thread/start returned no thread ID")
	}
	logger.Info("codex lifecycle",
		"phase", "thread_start_response",
		"task_id", c.cfg.TaskID,
		"runtime_id", c.cfg.RuntimeID,
		"pid", c.pid,
		"attempt", c.attempt,
		"active_launches", c.activeLaunches,
		"method", "thread/start",
		"latency", time.Since(c.threadStartStarted).Round(time.Millisecond).String(),
		"latency_ms", time.Since(c.threadStartStarted).Milliseconds(),
	)
	c.trySetThreadName(ctx, threadID, opts.ThreadName, logger)
	return threadID, false, nil
}

func (c *codexClient) trySetThreadName(ctx context.Context, threadID, name string, logger *slog.Logger) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if err := c.setThreadName(ctx, threadID, name); err != nil {
		logger.Warn("codex thread/name/set failed; continuing without provider-native thread title",
			"thread_id", threadID, "error", err)
	}
}

func (c *codexClient) setThreadName(ctx context.Context, threadID, name string) error {
	_, err := c.request(ctx, "thread/name/set", map[string]any{
		"threadId": threadID,
		"name":     name,
	})
	return err
}

// applyCodexReasoningEffort writes the per-agent thinking_level into a
// Codex app-server request. The three points — thread/start.config,
// thread/resume.config, turn/start.effort — all flow through this helper
// so any future protocol/key change touches one site rather than three
// (per Trump's MUL-2339 review constraint).
//
// The shape is detected from the params keys:
//   - turn/start always carries `input`, and the schema exposes the
//     reasoning override as the top-level `effort` field.
//   - thread/start and thread/resume nest it under
//     `config.model_reasoning_effort`.
//
// Empty `level` is a no-op: we deliberately do NOT emit a key when the
// caller didn't request an override, so the upstream defaults (config
// file, account-scoped model preference) stay in charge. This also
// guarantees `effort: ""` never reaches the CLI — Codex rejects empty
// strings on this field.
func applyCodexReasoningEffort(params map[string]any, level string) {
	if params == nil || level == "" {
		return
	}
	if _, isTurnStart := params["input"]; isTurnStart {
		params["effort"] = level
		return
	}
	cfg, _ := params["config"].(map[string]any)
	if cfg == nil {
		cfg = map[string]any{}
	}
	cfg["model_reasoning_effort"] = level
	params["config"] = cfg
}

// applyCodexServiceTier writes the catalog-owned service tier to the
// app-server's top-level serviceTier field. All three execution requests
// accept the same shape. Empty is a no-op so config.toml remains authoritative.
func applyCodexServiceTier(params map[string]any, tier string) {
	if params == nil || tier == "" {
		return
	}
	params["serviceTier"] = tier
}

func resetTimer(timer *time.Timer, d time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(d)
}

func stopTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func codexFirstTurnNoProgressTimeout(semanticInactivityTimeout time.Duration) time.Duration {
	if semanticInactivityTimeout <= 0 || semanticInactivityTimeout > defaultCodexFirstTurnNoProgressTimeout {
		return defaultCodexFirstTurnNoProgressTimeout
	}
	scaled := semanticInactivityTimeout * 4 / 5
	if scaled <= 0 {
		return semanticInactivityTimeout
	}
	return scaled
}

func isCodexFirstTurnProgressActivity(activity string) bool {
	return activity != "" && activity != "status:running" && activity != "error:retry"
}

func buildCodexTimeoutDiagnosticError(diag codexTimeoutDiagnostic, stderrTail string) string {
	stderrTail = sanitizeCodexDiagnostic(stderrTail)
	var msg string
	switch diag.Kind {
	case codexTimeoutFirstTurnNoProgress:
		msg = fmt.Sprintf("%s after %s: received turn start but no item, message, tool, turn/completed, or error event (%s)",
			CodexFirstTurnNoProgressMarker,
			diag.Timeout,
			formatCodexDiagnosticFields(diag),
		)
	case codexTimeoutSemanticInactivity:
		msg = fmt.Sprintf("%s after %s without agent progress (last activity: %s; %s)",
			CodexSemanticInactivityMarker,
			diag.Timeout,
			nonEmptyCodexDiagnosticValue(diag.LastActivity),
			formatCodexDiagnosticFields(diag),
		)
	default:
		msg = "codex timed out"
	}
	msg = appendCodexKnownStderrHint(msg, stderrTail)
	return withAgentStderr(msg, "codex", stderrTail)
}

func formatCodexDiagnosticFields(diag codexTimeoutDiagnostic) string {
	return fmt.Sprintf("codex_version=%q thread_id=%q turn_id=%q model=%q",
		nonEmptyCodexDiagnosticValue(diag.CodexVersion),
		nonEmptyCodexDiagnosticValue(diag.ThreadID),
		nonEmptyCodexDiagnosticValue(diag.TurnID),
		formatCodexDiagnosticModel(diag.Model),
	)
}

func nonEmptyCodexDiagnosticValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

func formatCodexDiagnosticModel(model string) string {
	if strings.TrimSpace(model) == "" {
		return "default(empty)"
	}
	return model
}

func appendCodexKnownStderrHint(msg, stderrTail string) string {
	if strings.Contains(stderrTail, codexModelCatalogRefreshFailureSignal) {
		return msg + "; diagnosis: Codex could not load its model catalog, which blocks the first turn. This is usually a transient network failure reaching the Codex service. Check network/proxy connectivity and retry the task, or switch to another runtime while the Codex service is unreachable"
	}
	return msg
}

func detectCodexVersionForDiagnostics(ctx context.Context, execPath string, env []string, logger *slog.Logger) string {
	versionCtx, cancel := context.WithTimeout(ctx, codexVersionDiagnosticTimeout)
	defer cancel()

	cmd := exec.CommandContext(versionCtx, execPath, "--version")
	cmd.Env = env
	data, err := cmd.Output()
	if err != nil {
		if logger != nil {
			logger.Debug("codex version diagnostic failed", "error", err)
		}
		return "unknown"
	}
	version := extractVersionLine(string(data))
	if strings.TrimSpace(version) == "" {
		return "unknown"
	}
	return version
}

func trySendString(ch chan<- string, value string) {
	select {
	case ch <- value:
	default:
	}
}

func logCodexAgentMessage(logger *slog.Logger, msg Message) {
	if logger == nil {
		return
	}
	attrs := []any{
		"type", string(msg.Type),
		"tool", msg.Tool,
		"call_id", msg.CallID,
		"status", msg.Status,
		"content_len", len(msg.Content),
		"output_len", len(msg.Output),
	}
	logger.Info("codex agent message received", attrs...)
	if msg.Type == MessageToolResult {
		logger.Info("codex tool_result observed", "tool", msg.Tool, "call_id", msg.CallID, "output_len", len(msg.Output))
	}
}

func describeCodexSemanticActivity(msg Message) string {
	switch msg.Type {
	case MessageToolUse, MessageToolResult:
		if msg.Tool != "" {
			return fmt.Sprintf("%s:%s", msg.Type, msg.Tool)
		}
	case MessageStatus:
		if msg.Status != "" {
			return fmt.Sprintf("%s:%s", msg.Type, msg.Status)
		}
	}
	return string(msg.Type)
}

// ── codexClient: JSON-RPC 2.0 transport ──

type codexClient struct {
	cfg                Config
	stdin              interface{ Write([]byte) (int, error) }
	mu                 sync.Mutex
	nextID             int
	pending            map[int]*pendingRPC
	processDone        chan struct{}
	processErr         error
	handshakeTimeout   time.Duration
	pid                int
	attempt            int
	activeLaunches     int64
	threadStartSent    bool
	threadStartStarted time.Time
	threadID           string
	turnID             string
	onMessage          func(Message)
	onSemanticActivity func(description string)
	onTurnDone         func(aborted bool)
	// acceptNotification isolates the active turn from same-thread history
	// replay emitted while thread/resume is restoring prior conversation.
	// Unit-level protocol tests leave it nil and exercise dispatch directly.
	acceptNotification func(method string, params map[string]any) bool
	// onDiscardedNotification preserves out-of-band safety signals (such as
	// suppressing an initialize retry after observed activity) without letting
	// filtered history mutate current-turn output or lifecycle state.
	onDiscardedNotification func(method string, params map[string]any)

	notificationProtocol string // "unknown", "legacy", "raw"
	turnStarted          bool
	completedTurnIDs     map[string]bool

	usageMu sync.Mutex
	usage   TokenUsage // accumulated from turn events

	turnErrorMu sync.Mutex
	turnError   string // captured from turn/completed status=failed or terminal error notifications
}

// codexTurnNotificationGate keeps resume-time history replay from mutating the
// output or ending the new turn. Codex app-server can emit notifications before
// the turn/start RPC response, so the gate is armed before that request and uses
// turn/started (or legacy task_started) as the actual current-turn boundary.
// Protocols that omit those start events also omit a reliable current-turn ID;
// for compatibility, their post-arm events remain accepted. We therefore rely
// on the single stdout reader's ordering guarantee that resume history emitted
// before the thread/resume response is processed while the gate is still closed.
// A replay emitted after arm but before a start event cannot be distinguished
// from a valid legacy current-turn event without breaking those older streams.
// Its mutable lifecycle fields are only touched by the stdout reader goroutine;
// armed is atomic because the lifecycle goroutine flips it.
type codexTurnNotificationGate struct {
	armed   atomic.Bool
	started bool
	turnID  string
}

func (g *codexTurnNotificationGate) arm() {
	g.armed.Store(true)
}

func (g *codexTurnNotificationGate) accept(method string, params map[string]any) bool {
	if !g.armed.Load() {
		return false
	}

	if method == "codex/event" || strings.HasPrefix(method, "codex/event/") {
		msg, _ := params["msg"].(map[string]any)
		msgType, _ := msg["type"].(string)
		if msgType == "task_started" {
			g.started = true
			return true
		}
		// Older Codex event streams can omit task_started. Once turn/start is
		// armed, keep that compatibility; pre-arm replay is still excluded.
		return true
	}

	switch {
	case method == "turn/started":
		g.started = true
		g.turnID = extractNestedString(params, "turn", "id")
		return true
	case method == "turn/completed":
		if !g.started {
			// Older app-server versions can complete a turn without first
			// emitting turn/started. The pre-arm boundary still rejects resume
			// replay, while this keeps those versions functional.
			return true
		}
		turnID := extractNestedString(params, "turn", "id")
		return g.turnID == "" || turnID == "" || turnID == g.turnID
	case method == "thread/status/changed" || strings.HasPrefix(method, "item/"):
		if !g.started {
			return true
		}
		turnID, _ := params["turnId"].(string)
		return g.turnID == "" || turnID == "" || turnID == g.turnID
	default:
		// A terminal error may be the first notification produced by a failed
		// turn/start, so it must remain observable even without turn/started.
		return true
	}
}

func (c *codexClient) setTurnError(msg string) {
	if msg == "" {
		return
	}
	c.turnErrorMu.Lock()
	defer c.turnErrorMu.Unlock()
	if c.turnError == "" {
		c.turnError = msg
	}
}

func (c *codexClient) getTurnError() string {
	c.turnErrorMu.Lock()
	defer c.turnErrorMu.Unlock()
	return c.turnError
}

type pendingRPC struct {
	ch     chan rpcResult
	method string
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

type codexHandshakeTimeoutError struct {
	Method  string
	Timeout time.Duration
}

func (e *codexHandshakeTimeoutError) Error() string {
	return fmt.Sprintf("%s: %s did not respond after %s", CodexHandshakeTimeoutMarker, e.Method, e.Timeout)
}

func (e *codexHandshakeTimeoutError) Unwrap() error {
	return context.DeadlineExceeded
}

func isCodexHandshakeRPC(method string) bool {
	switch method {
	case "initialize", "thread/start", "thread/resume", "thread/name/set", "turn/start":
		return true
	default:
		return false
	}
}

func codexRequestContextError(ctx context.Context) error {
	var handshakeErr *codexHandshakeTimeoutError
	if errors.As(context.Cause(ctx), &handshakeErr) {
		return handshakeErr
	}
	return ctx.Err()
}

func (c *codexClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	requestCtx := ctx
	cancelRequest := func() {}
	if c.handshakeTimeout > 0 && isCodexHandshakeRPC(method) {
		timeoutErr := &codexHandshakeTimeoutError{Method: method, Timeout: c.handshakeTimeout}
		requestCtx, cancelRequest = context.WithTimeoutCause(ctx, c.handshakeTimeout, timeoutErr)
	}
	defer cancelRequest()

	c.mu.Lock()
	if c.processErr != nil {
		err := c.processErr
		c.mu.Unlock()
		return nil, err
	}
	if c.processDone == nil {
		c.processDone = make(chan struct{})
	}
	processDone := c.processDone
	c.nextID++
	id := c.nextID
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: method}
	c.pending[id] = pr
	c.mu.Unlock()

	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	data = append(data, '\n')
	if _, err := c.stdin.Write(data); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("write %s: %w", method, err)
	}
	if method == "turn/start" {
		threadID := ""
		if paramMap, ok := params.(map[string]any); ok {
			threadID, _ = paramMap["threadId"].(string)
		}
		c.cfg.Logger.Info("codex turn/start sent", "request_id", id, "thread_id", threadID)
	}

	select {
	case res := <-pr.ch:
		return res.result, res.err
	case <-processDone:
		select {
		case res := <-pr.ch:
			return res.result, res.err
		default:
		}
		c.mu.Lock()
		delete(c.pending, id)
		err := c.processErr
		c.mu.Unlock()
		if requestCtx.Err() != nil {
			return nil, codexRequestContextError(requestCtx)
		}
		if err == nil {
			err = errCodexProcessExited
		}
		return nil, err
	case <-requestCtx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, codexRequestContextError(requestCtx)
	}
}

func (c *codexClient) notify(method string) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	_, _ = c.stdin.Write(data)
}

func (c *codexClient) respond(id int, result any) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	_, _ = c.stdin.Write(data)
}

func (c *codexClient) respondError(id int, code int, message string) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	_, _ = c.stdin.Write(data)
}

func (c *codexClient) closeAllPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, pr := range c.pending {
		pr.ch <- rpcResult{err: err}
		delete(c.pending, id)
	}
}

func (c *codexClient) markProcessExited(err error) {
	if err == nil {
		err = errCodexProcessExited
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.processErr == nil {
		c.processErr = err
		if c.processDone != nil {
			close(c.processDone)
		}
	}
	for id, pr := range c.pending {
		pr.ch <- rpcResult{err: err}
		delete(c.pending, id)
	}
}

func (c *codexClient) getProcessErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.processErr
}

func isCodexTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errCodexProcessExited) {
		return true
	}
	var handshakeErr *codexHandshakeTimeoutError
	if errors.As(err, &handshakeErr) {
		return true
	}
	return strings.HasPrefix(err.Error(), "write ")
}

func (c *codexClient) handleLine(line string) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return
	}

	// Check if it's a response to our request
	if _, hasID := raw["id"]; hasID {
		if _, hasResult := raw["result"]; hasResult {
			c.handleResponse(raw)
			return
		}
		if _, hasError := raw["error"]; hasError {
			c.handleResponse(raw)
			return
		}
		// Server request (has id + method)
		if _, hasMethod := raw["method"]; hasMethod {
			c.handleServerRequest(raw)
			return
		}
	}

	// Notification (no id, has method)
	if _, hasMethod := raw["method"]; hasMethod {
		c.handleNotification(raw)
	}
}

func (c *codexClient) handleResponse(raw map[string]json.RawMessage) {
	var id int
	if err := json.Unmarshal(raw["id"], &id); err != nil {
		return
	}

	c.mu.Lock()
	pr, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.mu.Unlock()

	if !ok {
		return
	}

	if errData, hasErr := raw["error"]; hasErr {
		var rpcErr struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(errData, &rpcErr)
		pr.ch <- rpcResult{err: fmt.Errorf("%s: %s (code=%d)", pr.method, rpcErr.Message, rpcErr.Code)}
	} else {
		pr.ch <- rpcResult{result: raw["result"]}
	}
}

func (c *codexClient) handleServerRequest(raw map[string]json.RawMessage) {
	var id int
	_ = json.Unmarshal(raw["id"], &id)

	var method string
	_ = json.Unmarshal(raw["method"], &method)

	// Auto-approve all exec/patch requests in daemon mode
	switch method {
	case "item/commandExecution/requestApproval", "execCommandApproval":
		c.respond(id, map[string]any{"decision": "accept"})
	case "item/fileChange/requestApproval", "applyPatchApproval":
		c.respond(id, map[string]any{"decision": "accept"})
	case "item/permissions/requestApproval":
		c.respond(id, codexPermissionsApprovalResponse(raw["params"], c.cfg.Logger))
	case "mcpServer/elicitation/request":
		c.respond(id, map[string]any{"action": "accept", "content": nil, "_meta": nil})
	default:
		msg := fmt.Sprintf("unsupported codex app-server request: %s", method)
		c.cfg.Logger.Warn("codex: unhandled server request", "method", method, "id", id)
		c.setTurnError(msg)
		c.respondError(id, -32601, msg)
	}
}

// codexPermissionsApprovalResponse builds the auto-grant reply for a Codex
// item/permissions/requestApproval server request. In daemon mode there is no
// human to approve, so we echo back the requested network / fileSystem profile
// and scope it to the current turn, mirroring the other auto-accept branches in
// handleServerRequest.
//
// The grant is intentionally limited to the network / fileSystem keys we
// understand. A parse failure and any dropped key are logged so that a future
// app-server protocol that adds a new permission shape is visible in daemon
// logs instead of being silently narrowed away.
func codexPermissionsApprovalResponse(params json.RawMessage, logger *slog.Logger) map[string]any {
	var payload struct {
		Permissions map[string]any `json:"permissions"`
	}
	if err := json.Unmarshal(params, &payload); err != nil && logger != nil {
		logger.Warn("codex: failed to parse permission approval request; granting empty turn-scoped profile", "error", err)
	}

	granted := map[string]any{}
	var dropped []string
	for key, value := range payload.Permissions {
		switch key {
		case "network", "fileSystem":
			if value != nil {
				granted[key] = value
			}
		default:
			dropped = append(dropped, key)
		}
	}
	if len(dropped) > 0 && logger != nil {
		sort.Strings(dropped)
		logger.Warn("codex: dropping unrecognized permission keys from approval request; add explicit handling if the app-server protocol expanded", "keys", dropped)
	}

	return map[string]any{
		"permissions": granted,
		"scope":       "turn",
	}
}

func (c *codexClient) handleNotification(raw map[string]json.RawMessage) {
	var method string
	_ = json.Unmarshal(raw["method"], &method)

	var params map[string]any
	if p, ok := raw["params"]; ok {
		_ = json.Unmarshal(p, &params)
	}
	// Filter multiplexed subagent threads before the current-turn gate. The
	// gate mutates started/turnID on turn/started; letting another thread reach
	// it first can replace the main turn ID and make subsequent main-thread
	// items/completion look stale.
	if c.isNotificationFromOtherThread(params) {
		return
	}
	if c.acceptNotification != nil && !c.acceptNotification(method, params) {
		if c.onDiscardedNotification != nil {
			c.onDiscardedNotification(method, params)
		}
		return
	}

	// Legacy codex/event notifications
	if method == "codex/event" || strings.HasPrefix(method, "codex/event/") {
		c.notificationProtocol = "legacy"
		msgData, ok := params["msg"]
		if !ok {
			return
		}
		msgMap, ok := msgData.(map[string]any)
		if !ok {
			return
		}
		c.handleEvent(msgMap)
		return
	}

	// Raw v2 notifications
	if c.notificationProtocol != "legacy" {
		if c.notificationProtocol == "unknown" &&
			(method == "turn/started" || method == "turn/completed" ||
				method == "thread/started" || strings.HasPrefix(method, "item/")) {
			c.notificationProtocol = "raw"
		}

		if c.notificationProtocol == "raw" {
			c.handleRawNotification(method, params)
		}
	}
}

func (c *codexClient) handleEvent(msg map[string]any) {
	msgType, _ := msg["type"].(string)

	switch msgType {
	case "task_started":
		c.turnStarted = true
		if c.onMessage != nil {
			c.onMessage(Message{Type: MessageStatus, Status: "running", SessionID: c.threadID})
		}
	case "agent_message":
		text, _ := msg["message"].(string)
		if text != "" && c.onMessage != nil {
			c.onMessage(Message{Type: MessageText, Content: text})
		}
	case "exec_command_begin":
		callID, _ := msg["call_id"].(string)
		command, _ := msg["command"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   "exec_command",
				CallID: callID,
				Input:  map[string]any{"command": command},
			})
		}
	case "exec_command_end":
		callID, _ := msg["call_id"].(string)
		output, _ := msg["output"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolResult,
				Tool:   "exec_command",
				CallID: callID,
				Output: output,
			})
		}
	case "patch_apply_begin":
		callID, _ := msg["call_id"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   "patch_apply",
				CallID: callID,
			})
		}
	case "patch_apply_end":
		callID, _ := msg["call_id"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolResult,
				Tool:   "patch_apply",
				CallID: callID,
			})
		}
	case "task_complete":
		// Extract usage from legacy task_complete if present.
		c.extractUsageFromMap(msg)
		if c.onTurnDone != nil {
			c.onTurnDone(false)
		}
	case "turn_aborted":
		if c.onTurnDone != nil {
			c.onTurnDone(true)
		}
	}
}

func (c *codexClient) handleRawNotification(method string, params map[string]any) {
	// Ignore notifications from threads other than the one we are tracking.
	// Codex multiplexes subagent threads (e.g. memory consolidation) on the
	// same stdio pipe; only our thread should drive turn lifecycle and output.
	//
	// The v2 app-server-protocol schema guarantees a top-level threadId on
	// every notification. handleNotification performs this guard before the
	// stateful current-turn gate; retain it here as defense in depth for direct
	// callers. If a future codex revision introduces notifications without
	// threadId, they fall through — re-audit this guard when bumping codex.
	if c.isNotificationFromOtherThread(params) {
		return
	}

	switch method {
	case "turn/started":
		c.turnStarted = true
		if turnID := extractNestedString(params, "turn", "id"); turnID != "" {
			c.turnID = turnID
		}
		if c.onMessage != nil {
			c.onMessage(Message{Type: MessageStatus, Status: "running", SessionID: c.threadID})
		}

	case "turn/completed":
		turnID := extractNestedString(params, "turn", "id")
		status := extractNestedString(params, "turn", "status")
		threadID, _ := params["threadId"].(string)
		c.cfg.Logger.Info("codex turn/completed received", "thread_id", threadID, "turn_id", turnID, "status", status)
		aborted := status == "cancelled" || status == "canceled" ||
			status == "aborted" || status == "interrupted"

		// Capture the error message from failed turns so callers can surface
		// a real reason instead of falling back to "empty output".
		if status == "failed" {
			errMsg := extractNestedString(params, "turn", "error", "message")
			if errMsg == "" {
				errMsg = "codex turn failed"
			}
			c.setTurnError(errMsg)
		}

		if c.completedTurnIDs == nil {
			c.completedTurnIDs = map[string]bool{}
		}
		if turnID != "" {
			if c.completedTurnIDs[turnID] {
				return
			}
			c.completedTurnIDs[turnID] = true
		}

		// Extract usage from turn/completed if present (e.g. params.turn.usage).
		if turn, ok := params["turn"].(map[string]any); ok {
			c.extractUsageFromMap(turn)
		}

		if c.onTurnDone != nil {
			c.onTurnDone(aborted)
		}

	case "error":
		// Top-level protocol error. Retrying notifications (willRetry=true) are
		// transient reconnect attempts; only capture terminal errors so we
		// don't stomp on a real failure later with a retry placeholder.
		willRetry, _ := params["willRetry"].(bool)
		errMsg := extractNestedString(params, "error", "message")
		if errMsg == "" {
			errMsg = extractNestedString(params, "message")
		}
		if errMsg != "" {
			c.cfg.Logger.Warn("codex error notification", "message", errMsg, "will_retry", willRetry)
			if c.onSemanticActivity != nil {
				if willRetry {
					c.onSemanticActivity("error:retry")
				} else {
					c.onSemanticActivity("error:terminal")
				}
			}
			if !willRetry {
				c.setTurnError(errMsg)
				if c.onTurnDone != nil {
					c.onTurnDone(false)
				}
			}
		}

	case "thread/status/changed":
		statusType := extractNestedString(params, "status", "type")
		if statusType == "idle" && c.turnStarted {
			if c.onTurnDone != nil {
				c.onTurnDone(false)
			}
		}

	default:
		if strings.HasPrefix(method, "item/") {
			c.handleItemNotification(method, params)
		}
	}
}

func (c *codexClient) isNotificationFromOtherThread(params map[string]any) bool {
	threadID, ok := params["threadId"].(string)
	return ok && c.threadID != "" && threadID != c.threadID
}

func (c *codexClient) handleItemNotification(method string, params map[string]any) {
	item, _ := params["item"].(map[string]any)
	itemType, _ := item["type"].(string)
	itemID, _ := item["id"].(string)
	if isCodexItemProgressActivity(method) && c.onSemanticActivity != nil {
		c.onSemanticActivity(describeCodexItemProgressActivity(method, itemType, itemID))
	}
	if item == nil {
		return
	}

	switch {
	case method == "item/started" && itemType == "commandExecution":
		command, _ := item["command"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   "exec_command",
				CallID: itemID,
				Input:  map[string]any{"command": command},
			})
		}

	case method == "item/completed" && itemType == "commandExecution":
		output, _ := item["aggregatedOutput"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolResult,
				Tool:   "exec_command",
				CallID: itemID,
				Output: output,
			})
		}

	case method == "item/started" && itemType == "fileChange":
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   "patch_apply",
				CallID: itemID,
			})
		}

	case method == "item/completed" && itemType == "fileChange":
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolResult,
				Tool:   "patch_apply",
				CallID: itemID,
			})
		}

	case method == "item/completed" && itemType == "agentMessage":
		text, _ := item["text"].(string)
		if text != "" && c.onMessage != nil {
			c.onMessage(Message{Type: MessageText, Content: text})
		}
		phase, _ := item["phase"].(string)
		if phase == "final_answer" && c.turnStarted {
			if c.onTurnDone != nil {
				c.onTurnDone(false)
			}
		}
	}
}

func isCodexItemProgressActivity(method string) bool {
	return strings.HasPrefix(method, "item/")
}

func describeCodexItemProgressActivity(method, itemType, itemID string) string {
	if itemType == "" {
		itemType = "unknown"
	}
	if itemID == "" {
		return fmt.Sprintf("%s:%s", method, itemType)
	}
	return fmt.Sprintf("%s:%s:%s", method, itemType, itemID)
}

// extractUsageFromMap extracts token usage from a map that may contain
// "usage", "token_usage", or "tokens" fields. Handles various Codex formats.
func (c *codexClient) extractUsageFromMap(data map[string]any) {
	// Try common field names for usage data.
	var usageMap map[string]any
	for _, key := range []string{"usage", "token_usage", "tokens"} {
		if v, ok := data[key].(map[string]any); ok {
			usageMap = v
			break
		}
	}
	if usageMap == nil {
		return
	}

	c.usageMu.Lock()
	defer c.usageMu.Unlock()

	// Codex reports cached input as a prompt-token detail: cached_input_tokens
	// are included in input_tokens. Persist mutually-exclusive buckets so
	// dashboard cost math does not charge cached input twice.
	inputTokens := codexInt64(usageMap, "input_tokens", "input", "prompt_tokens")
	cacheReadTokens := codexInt64(usageMap, "cached_input_tokens", "cache_read_tokens", "cache_read_input_tokens")
	c.usage.InputTokens += codexUncachedInputTokens(inputTokens, cacheReadTokens)
	c.usage.OutputTokens += codexInt64(usageMap, "output_tokens", "output", "completion_tokens")
	c.usage.CacheReadTokens += cacheReadTokens
	c.usage.CacheWriteTokens += codexInt64(usageMap, "cache_write_tokens", "cache_creation_input_tokens")
}

func codexUncachedInputTokens(inputTokens, cachedInputTokens int64) int64 {
	uncached := inputTokens - cachedInputTokens
	if uncached < 0 {
		return 0
	}
	return uncached
}

// codexInt64 returns the first non-zero int64 value from the map for the given keys.
func codexInt64(m map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch v := m[key].(type) {
		case float64:
			if v != 0 {
				return int64(v)
			}
		case int64:
			if v != 0 {
				return v
			}
		}
	}
	return 0
}

// ── Codex session log scanner ──

// codexSessionUsage holds usage extracted from a Codex session JSONL file.
type codexSessionUsage struct {
	usage TokenUsage
	model string
}

// scanCodexSessionUsage extracts usage for threadID from its Codex rollout.
// Codex 0.144.4 embeds the app-server thread ID in both the rollout filename and
// the first session_meta item. Binding the scan to that ID prevents a
// concurrently-created rollout (for example a Codex subagent) from being billed
// to this task. A resumed rollout keeps its original date directory, so both flat
// and YYYY/MM/DD layouts are searched.
func scanCodexSessionUsage(startTime time.Time, codexHome, threadID string, resumed bool) *codexSessionUsage {
	root := codexSessionRoot(codexHome)
	if root == "" || strings.TrimSpace(threadID) == "" {
		return nil
	}

	type candidate struct {
		path    string
		modTime time.Time
	}
	var files []candidate
	for _, path := range findCodexSessionRollouts(root, threadID) {
		info, err := os.Stat(path)
		if err != nil || info.ModTime().Before(startTime) {
			continue
		}
		files = append(files, candidate{path: path, modTime: info.ModTime()})
	}
	if len(files) == 0 {
		return nil
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].modTime.Equal(files[j].modTime) {
			return files[i].path < files[j].path
		}
		return files[i].modTime.Before(files[j].modTime)
	})

	// Multiple paths for one thread can transiently exist during layout migration.
	// They have the same owner, so prefer the latest deterministically without ever
	// crossing into a different thread's rollout.
	result := parseCodexSessionFileSince(files[len(files)-1].path, startTime, resumed)
	if result == nil || (result.usage.InputTokens == 0 && result.usage.OutputTokens == 0 &&
		result.usage.CacheReadTokens == 0 && result.usage.CacheWriteTokens == 0) {
		return nil
	}
	return result
}

// findCodexSessionRollouts returns uncompressed rollout files owned by threadID
// in the two layouts supported by Codex 0.14x. filepath.Glob traverses a linked
// sessions root, unlike WalkDir, which treats a symlink root as a single file and
// never visits its children. The normal path uses Codex's filename contract and
// validates session_meta.id when present. If a future Codex version changes the
// filename, the metadata pass preserves exact ownership instead of silently
// dropping usage. Filtering after globbing also keeps a provider-supplied thread
// ID out of the glob expression.
func findCodexSessionRollouts(root, threadID string) []string {
	threadID = strings.TrimSpace(threadID)
	if root == "" || threadID == "" {
		return nil
	}

	patterns := []string{
		filepath.Join(root, "rollout-*.jsonl"),
		filepath.Join(root, "*", "*", "*", "rollout-*.jsonl"),
	}
	seen := make(map[string]bool)
	var candidates []string
	for _, pattern := range patterns {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, path := range paths {
			if seen[path] {
				continue
			}
			seen[path] = true
			candidates = append(candidates, path)
		}
	}

	// Fast path for the current Codex contract. Reject a filename match if its
	// canonical session_meta explicitly names a different thread.
	suffix := "-" + threadID + ".jsonl"
	var matches []string
	for _, path := range candidates {
		if !strings.HasSuffix(filepath.Base(path), suffix) {
			continue
		}
		if metadataID, ok := readCodexRolloutThreadID(path); ok && metadataID != threadID {
			continue
		}
		matches = append(matches, path)
	}
	if len(matches) > 0 {
		return matches
	}

	// Compatibility path for a future filename format: the first session_meta
	// remains the canonical owner of a rollout in Codex's own resume reader.
	for _, path := range candidates {
		if metadataID, ok := readCodexRolloutThreadID(path); ok && metadataID == threadID {
			matches = append(matches, path)
		}
	}
	return matches
}

// readCodexRolloutThreadID reads only the head of a rollout. Codex writes the
// canonical session_meta first; the line cap prevents a malformed legacy file
// without metadata from turning ownership checks into a full multi-GB scan.
func readCodexRolloutThreadID(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	var evt struct {
		Type    string `json:"type"`
		Payload *struct {
			ID string `json:"id"`
		} `json:"payload"`
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for lineCount := 0; lineCount < 64 && scanner.Scan(); lineCount++ {
		line := scanner.Bytes()
		if !bytesContainsStr(line, "session_meta") {
			continue
		}
		if err := json.Unmarshal(line, &evt); err == nil && evt.Type == "session_meta" && evt.Payload != nil && evt.Payload.ID != "" {
			return evt.Payload.ID, true
		}
	}
	return "", false
}

// codexSessionRoot returns the Codex sessions directory. It prefers the
// explicit per-task codexHome the backend is running with (so usage is read
// from the same task-local sessions Codex actually wrote to), then the ambient
// CODEX_HOME, then ~/.codex.
func codexSessionRoot(codexHome string) string {
	if codexHome = strings.TrimSpace(codexHome); codexHome == "" {
		codexHome = os.Getenv("CODEX_HOME")
	}
	if codexHome != "" {
		dir := filepath.Join(codexHome, "sessions")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	dir := filepath.Join(home, ".codex", "sessions")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

type codexRawTokenUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	CacheReadInputTokens  int64 `json:"cache_read_input_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
}

// codexSessionTokenCount represents a token_count event in Codex JSONL.
type codexSessionTokenCount struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Payload   *struct {
		Type string `json:"type"`
		Info *struct {
			TotalTokenUsage *codexRawTokenUsage `json:"total_token_usage"`
			LastTokenUsage  *codexRawTokenUsage `json:"last_token_usage"`
			Model           string              `json:"model"`
		} `json:"info"`
		Model string `json:"model"`
	} `json:"payload"`
}

// parseCodexSessionFile extracts the final token_count from a Codex session file.
func parseCodexSessionFile(path string) *codexSessionUsage {
	return parseCodexSessionFileSince(path, time.Time{}, false)
}

// parseCodexSessionFileSince extracts usage accumulated after startTime. Codex
// reports total_token_usage cumulatively for the whole resumed session, so the
// last total before startTime is subtracted from the final total. Timestamp-less
// events in a resumed rollout are baseline-only until an explicit post-start
// timestamp establishes the boundary; fresh sessions retain the previous
// whole-file behavior because every event belongs to the new task.
func parseCodexSessionFileSince(path string, startTime time.Time, resumed bool) *codexSessionUsage {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var result codexSessionUsage
	var previousTotal, accumulated, finalUsage codexRawTokenUsage
	previousTotalFound := false
	finalUsageFound := false
	afterStartBoundary := false

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		// Fast pre-filter.
		if !bytesContainsStr(line, "token_count") && !bytesContainsStr(line, "turn_context") {
			continue
		}

		var evt codexSessionTokenCount
		if err := json.Unmarshal(line, &evt); err != nil || evt.Payload == nil {
			continue
		}
		timestampAfterStart := !startTime.IsZero() && !evt.Timestamp.IsZero() && evt.Timestamp.After(startTime)
		if timestampAfterStart {
			afterStartBoundary = true
		}

		// Track model from turn_context events.
		if evt.Type == "turn_context" && evt.Payload.Model != "" {
			result.model = evt.Payload.Model
			continue
		}

		// Extract token usage from token_count events.
		if evt.Payload.Type == "token_count" && evt.Payload.Info != nil {
			afterStart := startTime.IsZero() || timestampAfterStart ||
				(evt.Timestamp.IsZero() && (!resumed || afterStartBoundary))
			if usage := evt.Payload.Info.TotalTokenUsage; usage != nil {
				current := normalizeCodexRawTokenUsage(*usage)
				if afterStart {
					delta := current
					if previousTotalFound {
						delta = subtractCodexRawTokenUsage(current, previousTotal)
					}
					accumulated = addCodexRawTokenUsage(accumulated, delta)
					finalUsage = accumulated
					finalUsageFound = true
				}
				previousTotal = current
				previousTotalFound = true
			} else if usage := evt.Payload.Info.LastTokenUsage; usage != nil && afterStart {
				// Preserve event order: a later last_token_usage is the same
				// fallback the old whole-file parser would have selected.
				finalUsage = normalizeCodexRawTokenUsage(*usage)
				finalUsageFound = true
			}
			if evt.Payload.Info.Model != "" {
				result.model = evt.Payload.Info.Model
			}
		}
	}

	if !finalUsageFound {
		return nil
	}
	cachedTokens := finalUsage.CachedInputTokens
	result.usage = TokenUsage{
		InputTokens:     codexUncachedInputTokens(finalUsage.InputTokens, cachedTokens),
		OutputTokens:    finalUsage.OutputTokens + finalUsage.ReasoningOutputTokens,
		CacheReadTokens: cachedTokens,
	}
	return &result
}

func subtractCodexRawTokenUsage(total, baseline codexRawTokenUsage) codexRawTokenUsage {
	total = normalizeCodexRawTokenUsage(total)
	baseline = normalizeCodexRawTokenUsage(baseline)
	// Guard each counter independently. Treating one field's reset as a reset of
	// the entire snapshot would re-report still-monotonic fields and recreate the
	// over-counting this fallback is meant to prevent.
	return codexRawTokenUsage{
		InputTokens:           nonNegativeTokenDelta(total.InputTokens, baseline.InputTokens),
		OutputTokens:          nonNegativeTokenDelta(total.OutputTokens, baseline.OutputTokens),
		CachedInputTokens:     nonNegativeTokenDelta(total.CachedInputTokens, baseline.CachedInputTokens),
		ReasoningOutputTokens: nonNegativeTokenDelta(total.ReasoningOutputTokens, baseline.ReasoningOutputTokens),
	}
}

func normalizeCodexRawTokenUsage(usage codexRawTokenUsage) codexRawTokenUsage {
	if usage.CachedInputTokens == 0 {
		usage.CachedInputTokens = usage.CacheReadInputTokens
	}
	usage.CacheReadInputTokens = 0
	return usage
}

func addCodexRawTokenUsage(a, b codexRawTokenUsage) codexRawTokenUsage {
	return codexRawTokenUsage{
		InputTokens:           a.InputTokens + b.InputTokens,
		OutputTokens:          a.OutputTokens + b.OutputTokens,
		CachedInputTokens:     a.CachedInputTokens + b.CachedInputTokens,
		ReasoningOutputTokens: a.ReasoningOutputTokens + b.ReasoningOutputTokens,
	}
}

func nonNegativeTokenDelta(total, baseline int64) int64 {
	if total < baseline {
		// A counter reset means the final value already belongs to the new span.
		return total
	}
	return total - baseline
}

// bytesContainsStr checks if b contains the string s (without allocating).
func bytesContainsStr(b []byte, s string) bool {
	return strings.Contains(string(b), s)
}

// ── Helpers ──

func extractThreadID(result json.RawMessage) string {
	var r struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return ""
	}
	return r.Thread.ID
}

func extractNestedString(m map[string]any, keys ...string) string {
	current := any(m)
	for _, key := range keys {
		obj, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = obj[key]
	}
	s, _ := current.(string)
	return s
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
