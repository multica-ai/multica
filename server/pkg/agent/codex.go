package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// codexStderrTailBytes bounds the stderr tail captured for inclusion in
// error messages when codex exits before the JSON-RPC handshake (e.g. the
// user supplied a custom_args flag that the `app-server` subcommand
// rejects). Kept as its own constant so bumping codex independently of
// other agents stays easy if codex starts shipping longer failure traces.
const (
	codexStderrTailBytes                   = 2048
	defaultCodexSemanticInactivityTimeout  = 10 * time.Minute
	defaultCodexFirstTurnNoProgressTimeout = 30 * time.Second
	codexVersionDiagnosticTimeout          = 2 * time.Second
)

// CodexSemanticInactivityMarker prefixes timeout errors emitted when Codex
// stops making semantic progress while the process is still alive.
const CodexSemanticInactivityMarker = "codex semantic inactivity timeout"

// CodexFirstTurnNoProgressMarker identifies the app-server failure mode where
// Codex accepts a turn and then never emits any item, completion, or error.
const CodexFirstTurnNoProgressMarker = "codex app-server no progress timeout"

const codexModelCatalogRefreshTimeoutSignal = "failed to refresh available models: timeout waiting for child process to exit"

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
	extra := filterCustomArgs(opts.ExtraArgs, codexBlockedArgs, logger)
	custom := filterCustomArgs(opts.CustomArgs, codexBlockedArgs, logger)
	// Only claim ownership of the `mcp_servers` namespace when the agent
	// actually has a managed mcp_config in the MCP Tab. Otherwise existing
	// users who configure MCP via `custom_args: ["-c", "mcp_servers.…"]`
	// would silently lose those entries after this PR ships. With managed
	// mcp_config present, daemon-written `$CODEX_HOME/config.toml` is the
	// authoritative source and stray `-c mcp_servers.*` overrides are
	// dropped to keep last-wins from re-shadowing it.
	if hasManagedCodexMcpConfig(opts.McpConfig) {
		extra = filterCodexCustomConfigOverrides(extra, logger)
		custom = filterCodexCustomConfigOverrides(custom, logger)
	}
	args = append(args, extra...)
	args = append(args, custom...)
	return args
}

// hasManagedCodexMcpConfig reports whether the agent's mcp_config field is
// "present" in the API three-state sense: a non-null JSON value. Both
// `{}` and `{"mcpServers":{}}` count as present (the admin saved an empty
// managed set — strict mode, no global fallback); only SQL NULL or the
// literal JSON `null` count as absent (CLI default).
func hasManagedCodexMcpConfig(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	if bytes.Equal(trimmed, []byte("null")) {
		return false
	}
	return true
}

// codexManagedMcpConfigKeyRe matches the daemon-managed config namespace
// (`mcp_servers.…`) when it appears as the value of a Codex `-c` /
// `--config` flag. Used by filterCodexCustomConfigOverrides to drop user
// overrides that would otherwise shadow what the MCP Tab writes into
// `$CODEX_HOME/config.toml`.
var codexManagedMcpConfigKeyRe = regexp.MustCompile(`^\s*mcp_servers(?:\s*\.|\s*=|\s*$)`)

// filterCodexCustomConfigOverrides drops `-c mcp_servers.…=` and
// `--config mcp_servers.…=` entries from custom args. Codex's `-c` is
// last-wins (verified against codex-cli 0.132.0), so without this filter a
// user-written `-c mcp_servers.fetch=…` in custom_args would silently
// override whatever the MCP Tab saved into the per-task config.toml. We
// own the `mcp_servers` namespace via the managed block, so user attempts
// to write into it are dropped with a warning rather than allowed to win.
// Other `-c`/`--config` keys (e.g. `-c model="o3"`) pass through unchanged.
func filterCodexCustomConfigOverrides(args []string, logger *slog.Logger) []string {
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
			if codexManagedMcpConfigKeyRe.MatchString(value) {
				if logger != nil {
					// Log the key only, never the value — mcp_servers.<name>.env
					// is allowed to carry secrets and the whole point of moving
					// this to config.toml is to keep raw values out of logs/argv.
					key := value
					if eqIdx := strings.Index(value, "="); eqIdx >= 0 {
						key = value[:eqIdx]
					}
					logger.Warn("custom_args: blocked mcp_servers override; daemon manages this via CODEX_HOME/config.toml",
						"flag", flag, "key", strings.TrimSpace(key))
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
// Claude-style camelCase keys (`args`, `env`, `command`, `url`) pass
// through verbatim — Codex's config schema happens to use the same
// names today. If they ever diverge, rename here rather than in the UI.
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
	if codexHome := strings.TrimSpace(b.cfg.Env["CODEX_HOME"]); codexHome != "" {
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

	codexArgs := buildCodexArgs(opts, b.cfg.Logger)
	cmd := exec.CommandContext(runCtx, execPath, codexArgs...)
	hideAgentWindow(cmd)
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
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[codex:stderr] "), codexStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start codex: %w", err)
	}

	b.cfg.Logger.Info("codex started app-server", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	semanticActivityCh := make(chan string, 256)

	var outputMu sync.Mutex
	var output strings.Builder

	// turnDone is set before starting the reader goroutine so there is no
	// race between the lifecycle goroutine writing and the reader reading.
	turnDone := make(chan bool, 1) // true = aborted

	c := &codexClient{
		cfg:                  b.cfg,
		stdin:                stdin,
		pending:              make(map[int]*pendingRPC),
		notificationProtocol: "unknown",
		onMessage: func(msg Message) {
			logCodexAgentMessage(b.cfg.Logger, msg)
			if msg.Type == MessageText {
				outputMu.Lock()
				output.WriteString(msg.Content)
				outputMu.Unlock()
			}
			trySend(msgCh, msg)
			trySendString(semanticActivityCh, describeCodexSemanticActivity(msg))
		},
		onSemanticActivity: func(description string) {
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
		c.closeAllPending(fmt.Errorf("codex process exited"))
	}()

	// drainAndWait closes stdin so codex shuts down, then joins cmd.Wait().
	// cmd.Wait() is the only Go-stdlib-documented synchronization point for
	// os/exec's internal stderr/stdout copy goroutines — until it returns,
	// stderrBuf may not have observed every byte codex wrote before it
	// exited, and stderrBuf.Tail() can come back empty or truncated. Any
	// code that reads stderrBuf.Tail() must call drainAndWait() first.
	// sync.Once makes it safe to call from both error paths and the deferred
	// cleanup.
	var waitOnce sync.Once
	drainAndWait := func() {
		waitOnce.Do(func() {
			stdin.Close()
			_ = cmd.Wait()
		})
	}

	// Drive the session lifecycle in a goroutine.
	// Shutdown sequence: lifecycle goroutine closes stdin + cancels context →
	// codex process exits → reader goroutine's scanner.Scan() returns false →
	// readerDone closes → lifecycle goroutine collects final output and sends Result.
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer drainAndWait()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string

		// 1. Initialize handshake
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
			drainAndWait() // flush os/exec stderr goroutine before sampling Tail
			finalStatus = "failed"
			finalError = withAgentStderr(fmt.Sprintf("codex initialize failed: %v", err), "codex", stderrBuf.Tail())
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		c.notify("initialized")

		// 2. Start a new thread, or resume the prior one for this issue. When
		// resume fails (thread GCed on the server, schema drift, etc.) we fall
		// back to a fresh thread so the task still makes progress.
		threadID, resumed, err := c.startOrResumeThread(runCtx, opts, b.cfg.Logger)
		if err != nil {
			drainAndWait() // flush os/exec stderr goroutine before sampling Tail
			finalStatus = "failed"
			finalError = withAgentStderr(err.Error(), "codex", stderrBuf.Tail())
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		c.threadID = threadID
		if resumed {
			b.cfg.Logger.Info("codex thread resumed", "thread_id", threadID)
		} else {
			b.cfg.Logger.Info("codex thread started", "thread_id", threadID)
		}

		// 3. Send turn and wait for completion
		turnParams := map[string]any{
			"threadId": threadID,
			"input": []map[string]any{
				{"type": "text", "text": prompt},
			},
		}
		// Per-turn reasoning override. Mirrors the per-thread injection in
		// startOrResumeThread; keeping both in sync is enforced by the
		// shared `codexReasoningInjection` fixture in codex_test.go (see
		// MUL-2339 — Trump's constraint that the three injection points
		// must not drift independently).
		applyCodexReasoningEffort(turnParams, opts.ThinkingLevel)
		_, err = c.request(runCtx, "turn/start", turnParams)
		if err != nil {
			drainAndWait() // flush os/exec stderr goroutine before sampling Tail
			finalStatus = "failed"
			finalError = withAgentStderr(fmt.Sprintf("codex turn/start failed: %v", err), "codex", stderrBuf.Tail())
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
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

		waitingForTurn := true
		var timeoutDiagnostic codexTimeoutDiagnostic
		for waitingForTurn {
			select {
			case aborted := <-turnDone:
				waitingForTurn = false
				switch {
				case aborted:
					finalStatus = "aborted"
					finalError = "turn was aborted"
				default:
					if errMsg := c.getTurnError(); errMsg != "" {
						finalStatus = "failed"
						finalError = errMsg
					}
				}
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
				waitingForTurn = false
				if runCtx.Err() == context.DeadlineExceeded {
					finalStatus = "timeout"
					finalError = fmt.Sprintf("codex timed out after %s", timeout)
				} else {
					finalStatus = "aborted"
					finalError = "execution cancelled"
				}
			}
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("codex finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		// Close stdin and cancel context to signal the app-server to exit.
		// Without this, the long-running codex process keeps stdout open and
		// the reader goroutine blocks forever on scanner.Scan().
		stdin.Close()
		cancel()

		// Wait for the reader goroutine to finish so all output is accumulated.
		<-readerDone
		drainAndWait()

		if timeoutDiagnostic.Kind != codexTimeoutNone {
			timeoutDiagnostic.CodexVersion = detectCodexVersionForDiagnostics(context.Background(), execPath, cmd.Env, b.cfg.Logger)
			finalError = buildCodexTimeoutDiagnosticError(timeoutDiagnostic, stderrBuf.Tail())
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
		// Codex writes token_count events to ~/.codex/sessions/YYYY/MM/DD/*.jsonl.
		if u.InputTokens == 0 && u.OutputTokens == 0 {
			if scanned := scanCodexSessionUsage(startTime); scanned != nil {
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
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			SessionID:  threadID,
			DurationMs: duration.Milliseconds(),
			Usage:      usageMap,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// startOrResumeThread picks between Codex's thread/resume and thread/start
// based on opts.ResumeSessionID. When a prior thread ID is provided it first
// tries thread/resume; any error (unknown thread, schema mismatch, transport
// failure) is logged and the method falls back to thread/start so the task
// still executes. The returned threadID is what subsequent turn/start calls
// must reference, and resumed indicates whether the prior thread was picked
// up (only useful for logging).
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
		resumeResult, err := c.request(ctx, "thread/resume", resumeParams)
		if err == nil {
			if threadID := extractThreadID(resumeResult); threadID != "" {
				return threadID, true, nil
			}
			logger.Warn("codex thread/resume returned no thread ID; falling back to thread/start", "prior_thread_id", priorThreadID)
		} else {
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
	startResult, err := c.request(ctx, "thread/start", startParams)
	if err != nil {
		return "", false, fmt.Errorf("codex thread/start failed: %w", err)
	}
	threadID := extractThreadID(startResult)
	if threadID == "" {
		return "", false, fmt.Errorf("codex thread/start returned no thread ID")
	}
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
	if strings.Contains(stderrTail, codexModelCatalogRefreshTimeoutSignal) {
		return msg + "; diagnosis: Codex stderr shows the model catalog refresh timed out. Try setting an explicit model, switching Codex CLI versions, or using another runtime while Codex app-server recovers"
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
	threadID           string
	turnID             string
	onMessage          func(Message)
	onSemanticActivity func(description string)
	onTurnDone         func(aborted bool)

	notificationProtocol string // "unknown", "legacy", "raw"
	turnStarted          bool
	completedTurnIDs     map[string]bool

	usageMu sync.Mutex
	usage   TokenUsage // accumulated from turn events

	turnErrorMu sync.Mutex
	turnError   string // captured from turn/completed status=failed or terminal error notifications
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

func (c *codexClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
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
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
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
	case "mcpServer/elicitation/request":
		c.respond(id, map[string]any{"action": "accept", "content": nil, "_meta": nil})
	default:
		c.cfg.Logger.Warn("codex: unhandled server request", "method", method, "id", id)
		c.respondError(id, -32601, fmt.Sprintf("unhandled server request: %s", method))
	}
}

func (c *codexClient) handleNotification(raw map[string]json.RawMessage) {
	var method string
	_ = json.Unmarshal(raw["method"], &method)

	var params map[string]any
	if p, ok := raw["params"]; ok {
		_ = json.Unmarshal(p, &params)
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
	// every notification, so this dispatch-level guard transparently covers
	// every handler below. If a future codex revision introduces notifications
	// without threadId, they fall through (ok=false) — re-audit this guard
	// when bumping codex.
	if threadID, ok := params["threadId"].(string); ok && c.threadID != "" && threadID != c.threadID {
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

	// Try various key conventions.
	c.usage.InputTokens += codexInt64(usageMap, "input_tokens", "input", "prompt_tokens")
	c.usage.OutputTokens += codexInt64(usageMap, "output_tokens", "output", "completion_tokens")
	c.usage.CacheReadTokens += codexInt64(usageMap, "cache_read_tokens", "cache_read_input_tokens")
	c.usage.CacheWriteTokens += codexInt64(usageMap, "cache_write_tokens", "cache_creation_input_tokens")
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

// scanCodexSessionUsage scans Codex session JSONL files written after startTime
// to extract token usage. Codex writes token_count events to
// ~/.codex/sessions/YYYY/MM/DD/*.jsonl.
func scanCodexSessionUsage(startTime time.Time) *codexSessionUsage {
	root := codexSessionRoot()
	if root == "" {
		return nil
	}

	// Look in today's session directory.
	dateDir := filepath.Join(root,
		fmt.Sprintf("%04d", startTime.Year()),
		fmt.Sprintf("%02d", int(startTime.Month())),
		fmt.Sprintf("%02d", startTime.Day()),
	)

	files, err := filepath.Glob(filepath.Join(dateDir, "*.jsonl"))
	if err != nil || len(files) == 0 {
		return nil
	}

	// Only scan files modified after startTime (this task's session).
	var result codexSessionUsage
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil || info.ModTime().Before(startTime) {
			continue
		}
		if u := parseCodexSessionFile(f); u != nil {
			// Take the last matching file's data (usually there's only one per task).
			result = *u
		}
	}

	if result.usage.InputTokens == 0 && result.usage.OutputTokens == 0 {
		return nil
	}
	return &result
}

// codexSessionRoot returns the Codex sessions directory.
func codexSessionRoot() string {
	if codexHome := os.Getenv("CODEX_HOME"); codexHome != "" {
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

// codexSessionTokenCount represents a token_count event in Codex JSONL.
type codexSessionTokenCount struct {
	Type    string `json:"type"`
	Payload *struct {
		Type string `json:"type"`
		Info *struct {
			TotalTokenUsage *struct {
				InputTokens           int64 `json:"input_tokens"`
				OutputTokens          int64 `json:"output_tokens"`
				CachedInputTokens     int64 `json:"cached_input_tokens"`
				CacheReadInputTokens  int64 `json:"cache_read_input_tokens"`
				ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
			} `json:"total_token_usage"`
			LastTokenUsage *struct {
				InputTokens           int64 `json:"input_tokens"`
				OutputTokens          int64 `json:"output_tokens"`
				CachedInputTokens     int64 `json:"cached_input_tokens"`
				CacheReadInputTokens  int64 `json:"cache_read_input_tokens"`
				ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
			} `json:"last_token_usage"`
			Model string `json:"model"`
		} `json:"info"`
		Model string `json:"model"`
	} `json:"payload"`
}

// parseCodexSessionFile extracts the final token_count from a Codex session file.
func parseCodexSessionFile(path string) *codexSessionUsage {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var result codexSessionUsage
	found := false

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

		// Track model from turn_context events.
		if evt.Type == "turn_context" && evt.Payload.Model != "" {
			result.model = evt.Payload.Model
			continue
		}

		// Extract token usage from token_count events.
		if evt.Payload.Type == "token_count" && evt.Payload.Info != nil {
			usage := evt.Payload.Info.TotalTokenUsage
			if usage == nil {
				usage = evt.Payload.Info.LastTokenUsage
			}
			if usage != nil {
				cachedTokens := usage.CachedInputTokens
				if cachedTokens == 0 {
					cachedTokens = usage.CacheReadInputTokens
				}
				result.usage = TokenUsage{
					InputTokens:     usage.InputTokens,
					OutputTokens:    usage.OutputTokens + usage.ReasoningOutputTokens,
					CacheReadTokens: cachedTokens,
				}
				if evt.Payload.Info.Model != "" {
					result.model = evt.Payload.Info.Model
				}
				found = true
			}
		}
	}

	if !found {
		return nil
	}
	return &result
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
