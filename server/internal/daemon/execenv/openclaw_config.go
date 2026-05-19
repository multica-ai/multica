package execenv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// openclawConfigFile is the per-task synthesized OpenClaw config the daemon
// points the openclaw CLI at via OPENCLAW_CONFIG_PATH. It sits in the env
// root (alongside workdir/, output/, logs/) so the GC reaper sweeps it with
// the rest of the task env.
const openclawConfigFile = "openclaw-config.json"

// openclawCLITimeout caps each `openclaw config ...` invocation during task
// setup. The CLI is fast (<200ms normal); 5s leaves headroom for a cold
// node start without letting a hung CLI stall task dispatch indefinitely.
const openclawCLITimeout = 5 * time.Second

// OpenclawConfigPrep is the input to prepareOpenclawConfig. Only OpenclawBin
// is meaningful in production — Timeout is here for tests that need a tight
// cap to assert error paths.
type OpenclawConfigPrep struct {
	// OpenclawBin is the openclaw CLI binary to invoke for config introspection.
	// Empty means resolve "openclaw" from PATH at exec time.
	OpenclawBin string
	// Timeout caps each CLI invocation. Zero falls back to openclawCLITimeout.
	Timeout time.Duration
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
//  1. Run `openclaw config file` to find the user's active config path
//     (handles OPENCLAW_CONFIG_PATH, OPENCLAW_STATE_DIR, OPENCLAW_HOME, and
//     the default location).
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
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = openclawCLITimeout
	}

	activePath, exists, err := openclawActiveConfigPath(bin, timeout)
	if err != nil {
		return OpenclawConfigResult{}, fmt.Errorf("locate openclaw active config: %w", err)
	}

	var resolvedList []any
	if exists {
		resolvedList, err = openclawResolvedAgentsList(bin, timeout)
		if err != nil {
			return OpenclawConfigResult{}, fmt.Errorf("read openclaw agents.list: %w", err)
		}
	}

	cfg := buildPerTaskOpenclawConfig(activePath, exists, resolvedList, workDir)

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
	if exists {
		// Only emit an include root when we actually emit a $include line
		// (i.e. the user has an on-disk config). On fresh install the
		// wrapper is self-contained and OpenClaw never needs to step out
		// of envRoot, so no extra root is required.
		result.IncludeRoot = filepath.Dir(activePath)
	}
	return result, nil
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
func buildPerTaskOpenclawConfig(activePath string, exists bool, resolvedList []any, workDir string) map[string]any {
	agents := map[string]any{
		"defaults": map[string]any{"workspace": workDir},
	}
	if rewritten := rewriteAgentsListWorkspaces(resolvedList, workDir); rewritten != nil {
		agents["list"] = rewritten
	}
	cfg := map[string]any{
		"agents": agents,
	}
	if exists {
		// Array form (not single-file form) so OpenClaw deep-merges the
		// included object with our sibling keys rather than letting the
		// include replace the whole containing object.
		cfg["$include"] = []any{activePath}
	}
	return cfg
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

// openclawActiveConfigPath runs `openclaw config file` to discover the path
// the openclaw CLI considers active. Returns (absolutePath, exists, error).
//
// The CLI handles the full resolution chain — OPENCLAW_CONFIG_PATH, the
// state directory (OPENCLAW_STATE_DIR / OPENCLAW_HOME / default), legacy
// migration, and `~` expansion — so we don't re-implement it here.
//
// The reported path uses `~` shorthand for the user's home; we expand it
// so the $include reference we write is unambiguous absolute.
func openclawActiveConfigPath(bin string, timeout time.Duration) (string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := openclawExec(ctx, bin, "config", "file")
	if err != nil {
		return "", false, err
	}
	path := strings.TrimSpace(out)
	if path == "" {
		return "", false, fmt.Errorf("`openclaw config file` returned empty output")
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", false, fmt.Errorf("expand `~` in openclaw config path: %w", herr)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	if !filepath.IsAbs(path) {
		return "", false, fmt.Errorf("openclaw reported non-absolute config path %q", path)
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("stat openclaw config %s: %w", path, err)
	}
	if info.IsDir() {
		return "", false, fmt.Errorf("openclaw config path %s is a directory, not a file", path)
	}
	return path, true, nil
}

// openclawResolvedAgentsList fetches the user's resolved agents.list via
// `openclaw config get agents.list --json`. The CLI returns the post-
// include, post-env-substitution view of the array, which is exactly the
// shape we need to rewrite each entry's workspace.
//
// Returns nil (not an error) when agents.list is unset.
func openclawResolvedAgentsList(bin string, timeout time.Duration) ([]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := openclawExec(ctx, bin, "config", "get", "agents.list", "--json")
	if err != nil {
		if isOpenclawKeyMissing(err) {
			return nil, nil
		}
		return nil, err
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var list []any
	if err := json.Unmarshal([]byte(trimmed), &list); err != nil {
		return nil, fmt.Errorf("parse `openclaw config get agents.list --json` output: %w", err)
	}
	return list, nil
}

// openclawExec is the runtime hook prepareOpenclawConfig uses to invoke the
// openclaw CLI. Production points at execOpenclawCLI; tests swap in a stub
// to avoid spawning a real binary. Production code never reassigns it.
var openclawExec = execOpenclawCLI

// execOpenclawCLI executes an openclaw subcommand and returns its stdout.
// The daemon's environment is inherited so OPENCLAW_CONFIG_PATH /
// OPENCLAW_STATE_DIR / OPENCLAW_HOME / OPENCLAW_INCLUDE_ROOTS pass through.
//
// stderr is captured separately and appended to error messages — failures
// here surface up to the daemon log, and a `openclaw doctor` hint there is
// more useful than just an exit code.
func execOpenclawCLI(ctx context.Context, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = os.Environ()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		stderrMsg := strings.TrimSpace(stderr.String())
		if stderrMsg != "" {
			return "", fmt.Errorf("openclaw %s: %w (stderr: %s)", strings.Join(args, " "), err, stderrMsg)
		}
		return "", fmt.Errorf("openclaw %s: %w", strings.Join(args, " "), err)
	}
	return string(raw), nil
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
	msg := err.Error()
	return strings.Contains(msg, "No value at ") ||
		strings.Contains(msg, "not set") ||
		strings.Contains(msg, "missing key") ||
		strings.Contains(msg, "Path not found")
}
