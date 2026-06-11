package execenv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// openclawCLIStub captures one or more (subcommand, response) pairs and
// installs itself into the package-level openclawExec hook for the duration
// of a test. Each call records the args it saw so assertions can verify the
// preparer hit `config file` and `config get agents.list --json`.
type openclawCLIStub struct {
	t         *testing.T
	bin       string
	responses map[string]openclawResponse
	calls     []openclawCall
}

type openclawCall struct {
	bin  string
	args []string
}

type openclawResponse struct {
	stdout string
	err    error
}

func installOpenclawStub(t *testing.T, responses map[string]openclawResponse) *openclawCLIStub {
	t.Helper()
	stub := &openclawCLIStub{
		t:         t,
		bin:       "/test/stub/openclaw",
		responses: responses,
	}
	prev := openclawExec
	openclawExec = stub.exec
	t.Cleanup(func() { openclawExec = prev })
	return stub
}

func (s *openclawCLIStub) exec(_ context.Context, bin string, args ...string) (string, error) {
	s.calls = append(s.calls, openclawCall{bin: bin, args: append([]string(nil), args...)})
	key := strings.Join(args, " ")
	resp, ok := s.responses[key]
	if !ok {
		return "", fmt.Errorf("openclawCLIStub: unexpected args %q", key)
	}
	return resp.stdout, resp.err
}

func mustReadJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read synthesized cfg: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parse synthesized cfg: %v", err)
	}
	return got
}

// TestPrepareOpenclawConfigDelegatesParsingToCLI is the headline assertion
// for the Elon must-fix: instead of re-parsing the user's openclaw.json
// with encoding/json (which can't read JSON5 / $include / env-var
// substitution), we delegate the read to the openclaw CLI. The wrapper
// $includes the user's active path so OpenClaw's own loader handles the
// JSON5 / $include resolution; we only emit workspace overrides.
func TestPrepareOpenclawConfigDelegatesParsingToCLI(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	// JSON5 user config — comments and trailing commas would break the old
	// encoding/json reader. The stub doesn't actually parse this; it just
	// proves the wrapper points the $include at the right file regardless
	// of its on-disk syntax.
	userConfigDir := t.TempDir()
	userConfigPath := filepath.Join(userConfigDir, "openclaw.json")
	json5Body := `// User config with JSON5 features the old parser couldn't read
{
  agents: {
    defaults: {
      workspace: "/Users/alice/.openclaw/workspace",
      model: { primary: "anthropic/claude-sonnet-4-6" },
    },
    list: [
      { id: "scout", workspace: "/Users/alice/projects/scout", },
      { id: "coder", model: "openai/gpt-5", },
    ],
  },
  gateway: { port: 18789 }, // trailing comma
}
`
	if err := os.WriteFile(userConfigPath, []byte(json5Body), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {stdout: userConfigPath + "\n"},
		"config get agents.list --json": {stdout: `[
			{ "id": "scout", "workspace": "/Users/alice/projects/scout" },
			{ "id": "coder", "model": "openai/gpt-5" }
		]`},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	cfgPath := result.ConfigPath
	if cfgPath != filepath.Join(envRoot, openclawConfigFile) {
		t.Errorf("cfgPath = %q, want %q", cfgPath, filepath.Join(envRoot, openclawConfigFile))
	}

	got := mustReadJSON(t, cfgPath)

	// $include must reference the user's active config so OpenClaw's own
	// loader does the JSON5 / $include / env-substitution work.
	include, ok := got["$include"].([]any)
	if !ok || len(include) != 1 || include[0] != userConfigPath {
		t.Errorf("$include = %v, want [%q]", got["$include"], userConfigPath)
	}

	// The wrapper $includes a path that lives outside envRoot. OpenClaw
	// confines $include resolution to the wrapper file's own directory
	// unless OPENCLAW_INCLUDE_ROOTS lists the target. Surface the user
	// config's dirname so the daemon can grant it.
	if result.IncludeRoot != userConfigDir {
		t.Errorf("IncludeRoot = %q, want %q (dirname of active config so wrapper can $include across dirs)", result.IncludeRoot, userConfigDir)
	}

	agents := got["agents"].(map[string]any)
	defaults := agents["defaults"].(map[string]any)
	if defaults["workspace"] != workDir {
		t.Errorf("agents.defaults.workspace = %v, want %q", defaults["workspace"], workDir)
	}

	// Per-agent workspaces must be rewritten so a host-scope agents.list[].
	// workspace cannot silently win over our defaults override. This is
	// intentional per-task isolation (see prepareOpenclawConfig doc).
	list := agents["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("agents.list length = %d, want 2", len(list))
	}
	for i, item := range list {
		entry := item.(map[string]any)
		if entry["workspace"] != workDir {
			t.Errorf("agents.list[%d].workspace = %v, want %q (per-agent overrides must be rewritten so they don't beat defaults)", i, entry["workspace"], workDir)
		}
	}
	// Non-workspace fields per entry are carried over so a sibling-replace
	// merge in OpenClaw's $include semantics doesn't silently lose them.
	if list[0].(map[string]any)["id"] != "scout" {
		t.Errorf("agents.list[0].id lost in carryover: %v", list[0])
	}
	if list[1].(map[string]any)["model"] != "openai/gpt-5" {
		t.Errorf("agents.list[1].model lost in carryover: %v", list[1])
	}
}

// TestPrepareOpenclawConfigFailsClosedOnCLIError — the headline regression
// for Elon's review. When the openclaw CLI fails (broken config, missing
// binary, etc.), prepareOpenclawConfig MUST surface the error rather than
// silently synthesize a minimal config that would mask the user's broken
// state and boot OpenClaw without their registered agents.
func TestPrepareOpenclawConfigFailsClosedOnCLIError(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {err: errors.New("exec: openclaw: no such file or directory")},
	})

	_, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err == nil {
		t.Fatal("prepareOpenclawConfig succeeded on CLI failure; expected fail closed")
	}
	if !strings.Contains(err.Error(), "locate openclaw active config") {
		t.Errorf("error message %q does not name the failed step", err.Error())
	}

	// No stale wrapper left behind.
	if _, err := os.Stat(filepath.Join(envRoot, openclawConfigFile)); !os.IsNotExist(err) {
		t.Errorf("wrapper config should not exist after fail-closed; got err = %v", err)
	}
}

// TestPrepareOpenclawConfigFailsClosedOnMalformedAgentsList — the second
// fail-closed surface. When `openclaw config get agents.list --json`
// returns junk we can't parse, we fail rather than guess.
func TestPrepareOpenclawConfigFailsClosedOnMalformedAgentsList(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	userConfigPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userConfigPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: userConfigPath},
		"config get agents.list --json": {stdout: "<<<garbage>>>"},
	})

	_, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err == nil {
		t.Fatal("prepareOpenclawConfig succeeded on malformed agents.list output; expected fail closed")
	}
	if !strings.Contains(err.Error(), "agents.list") {
		t.Errorf("error message %q does not name the failed step", err.Error())
	}
}

// TestPrepareOpenclawConfigKeyMissingTreatedAsEmpty — `config get` exits
// non-zero when a path is unset. That is not a failure; the user simply has
// no agents.list. We must produce a valid wrapper with just the defaults
// override.
func TestPrepareOpenclawConfigKeyMissingTreatedAsEmpty(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	userConfigPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userConfigPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: userConfigPath},
		"config get agents.list --json": {err: errors.New("openclaw: No value at agents.list")},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	cfgPath := result.ConfigPath
	got := mustReadJSON(t, cfgPath)
	if _, present := got["agents"].(map[string]any)["list"]; present {
		t.Errorf("agents.list should be omitted when user has none, got %v", got["agents"])
	}
	if got["agents"].(map[string]any)["defaults"].(map[string]any)["workspace"] != workDir {
		t.Errorf("defaults.workspace not set when agents.list missing")
	}
}

// TestPrepareOpenclawConfigFreshInstallNoOnDiskConfig — the only legitimate
// "synthesize minimal" case. `openclaw config file` reports a path (the
// default) but the file does not exist yet. We emit a wrapper with the
// workspace override and NO $include (there is nothing to include).
func TestPrepareOpenclawConfigFreshInstallNoOnDiskConfig(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	// CLI reports a default path that doesn't exist (fresh install).
	missingPath := filepath.Join(t.TempDir(), "openclaw.json")

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {stdout: missingPath},
		// `config get` should not be called when the file does not exist;
		// the stub will fail "unexpected args" if it is.
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	cfgPath := result.ConfigPath
	got := mustReadJSON(t, cfgPath)
	if _, present := got["$include"]; present {
		t.Errorf("$include should be absent for fresh install, got %v", got["$include"])
	}
	if got["agents"].(map[string]any)["defaults"].(map[string]any)["workspace"] != workDir {
		t.Errorf("defaults.workspace not set on fresh-install wrapper")
	}
	// Fresh install emits no $include, so no extra include root is needed
	// — the wrapper never steps outside envRoot. Daemon should leave the
	// user's OPENCLAW_INCLUDE_ROOTS alone.
	if result.IncludeRoot != "" {
		t.Errorf("IncludeRoot = %q on fresh install, want empty (no $include emitted)", result.IncludeRoot)
	}
}

// TestPrepareOpenclawConfigExpandsTilde — `openclaw config file` reports
// paths with `~` shortened. The $include in our wrapper must be absolute so
// the loader resolves it unambiguously.
func TestPrepareOpenclawConfigExpandsTilde(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	if err := os.MkdirAll(filepath.Join(fakeHome, ".openclaw"), 0o755); err != nil {
		t.Fatalf("mkdir home/.openclaw: %v", err)
	}
	realPath := filepath.Join(fakeHome, ".openclaw", "openclaw.json")
	if err := os.WriteFile(realPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: "~/.openclaw/openclaw.json\n"},
		"config get agents.list --json": {stdout: "null"},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	cfgPath := result.ConfigPath
	got := mustReadJSON(t, cfgPath)
	include := got["$include"].([]any)
	if include[0] != realPath {
		t.Errorf("$include[0] = %v, want %q (tilde must be expanded to absolute)", include[0], realPath)
	}
	// IncludeRoot must also use the expanded absolute dirname, otherwise
	// the daemon would export a `~/.openclaw`-shaped root that OpenClaw
	// would not match against the resolved absolute include target.
	wantRoot := filepath.Join(fakeHome, ".openclaw")
	if result.IncludeRoot != wantRoot {
		t.Errorf("IncludeRoot = %q, want %q (must be expanded absolute dirname)", result.IncludeRoot, wantRoot)
	}
}

// TestPrepareOpenclawConfigWrapperLoadableUnderIncludeConfinement is the
// regression test for the Elon include-confinement blocker. OpenClaw
// resolves `$include` only inside the wrapper file's own directory unless
// the target's parent dir is granted via OPENCLAW_INCLUDE_ROOTS. The
// previous PR wrote a wrapper at envRoot that $included
// `~/.openclaw/openclaw.json` (cross-directory) but never surfaced the
// dirname; OpenClaw would have refused to follow the link at runtime.
//
// This test simulates the same confinement check OpenClaw performs:
//
//   - For every `$include` target, assert filepath.Dir(target) is either
//     the wrapper's own dir OR matches the IncludeRoot we surface for the
//     daemon to grant.
//
// It does NOT shell out to a real openclaw binary — the spec is small and
// stable enough that mirroring it in-test is more reliable than depending
// on the CLI being installed in CI. If this assertion ever drifts from the
// real loader, the upstream docs are the source of truth:
// https://github.com/openclaw/openclaw/blob/main/docs/gateway/configuration.md
func TestPrepareOpenclawConfigWrapperLoadableUnderIncludeConfinement(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	// User's active config sits in its own dir, not envRoot. This is the
	// realistic shape (~/.openclaw/openclaw.json is never inside the task
	// workspace) and is the exact case the bug paper-trail flagged.
	userConfigDir := t.TempDir()
	userConfigPath := filepath.Join(userConfigDir, "openclaw.json")
	if err := os.WriteFile(userConfigPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: userConfigPath},
		"config get agents.list --json": {stdout: "null"},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}

	got := mustReadJSON(t, result.ConfigPath)
	rawIncludes, ok := got["$include"].([]any)
	if !ok || len(rawIncludes) == 0 {
		t.Fatalf("wrapper has no $include entries, but a user config is present: %v", got)
	}

	// Mirror OpenClaw's confinement check: every cross-dir $include target
	// must have its dirname covered by either the wrapper's own dir or the
	// IncludeRoot we surface.
	wrapperDir := filepath.Dir(result.ConfigPath)
	granted := []string{wrapperDir}
	if result.IncludeRoot != "" {
		granted = append(granted, result.IncludeRoot)
	}
	for _, raw := range rawIncludes {
		target, ok := raw.(string)
		if !ok {
			t.Fatalf("$include entry is not a string: %T %v", raw, raw)
		}
		targetDir := filepath.Dir(target)
		allowed := false
		for _, g := range granted {
			if targetDir == g {
				allowed = true
				break
			}
		}
		if !allowed {
			t.Errorf("$include target %q has dirname %q which is not in granted include roots %v — OpenClaw would refuse to load it",
				target, targetDir, granted)
		}
	}
}

// TestPrepareOpenclawConfigStrictReplacesUserMcpServers — the headline
// assertion for Elon's strict-replace must-fix on PR #3450. When the user
// has a global `mcp.servers.global_one` AND the agent has a managed
// `mcp.servers.shared + managed_only`, the wrapper must NOT $include the
// live user config (which would leak global_one) and must instead
// $include a sanitized snapshot that has the user's `mcp` block stripped.
// The wrapper itself carries managed servers and nothing else.
func TestPrepareOpenclawConfigStrictReplacesUserMcpServers(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	userCfgPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userCfgPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}
	// The resolved user config the CLI would return: a user global
	// mcp.servers + some other non-mcp content the snapshot must preserve.
	resolvedUser := `{
		"mcp": {"servers": {
			"global_one": {"command": "/bin/echo", "args": ["user"]},
			"shared":     {"command": "/bin/old-version"}
		}},
		"gateway": {"port": 18789},
		"providers": {"anthropic": {"apiKey": "sk-user-secret"}}
	}`
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: userCfgPath},
		"config get --json":             {stdout: resolvedUser},
		"config get agents.list --json": {stdout: "null"},
	})

	mcpConfig := json.RawMessage(`{
		"mcpServers": {
			"shared":       {"command": "/bin/new-version"},
			"managed_only": {"url": "https://mcp.example.com", "transport": "streamable-http"}
		}
	}`)

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
		OpenclawBin: stub.bin,
		McpConfig:   mcpConfig,
	})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}

	got := mustReadJSON(t, result.ConfigPath)
	mcp, ok := got["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("wrapper missing mcp block: %v", got)
	}
	servers, ok := mcp["servers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp.servers is not an object: %v", mcp)
	}
	if len(servers) != 2 {
		t.Errorf("mcp.servers has %d entries, want 2 (managed only — global_one must not leak): %v", len(servers), servers)
	}
	if _, leaked := servers["global_one"]; leaked {
		t.Errorf("mcp.servers.global_one leaked into wrapper from user config: %v", servers)
	}
	if shared, ok := servers["shared"].(map[string]any); !ok || shared["command"] != "/bin/new-version" {
		t.Errorf("mcp.servers.shared = %v, want managed `command: /bin/new-version` (managed overrides user same-name)", shared)
	}
	if managed, ok := servers["managed_only"].(map[string]any); !ok || managed["url"] != "https://mcp.example.com" {
		t.Errorf("mcp.servers.managed_only missing or wrong shape: %v", managed)
	}

	// The wrapper's $include must point at the sanitized snapshot, NOT the
	// live user config — otherwise OpenClaw would deep-merge user.mcp back in.
	include, _ := got["$include"].([]any)
	if len(include) != 1 {
		t.Fatalf("wrapper $include has %d entries, want 1: %v", len(include), include)
	}
	snapshotPath, _ := include[0].(string)
	if snapshotPath == userCfgPath {
		t.Fatalf("wrapper $includes the live user config (%q) — strict replace requires the sanitized snapshot", userCfgPath)
	}
	wantSnapshot := filepath.Join(envRoot, openclawUserSnapshotFile)
	if snapshotPath != wantSnapshot {
		t.Errorf("$include = %q, want sanitized snapshot %q", snapshotPath, wantSnapshot)
	}

	// Snapshot must exist, must drop the `mcp` block, and must preserve the
	// non-mcp keys (gateway, providers, secrets) so OpenClaw still has API
	// keys and other config the user relied on.
	snap := mustReadJSON(t, snapshotPath)
	if _, present := snap["mcp"]; present {
		t.Errorf("snapshot still contains an `mcp` block — strict replace not enforced: %v", snap["mcp"])
	}
	if gw, ok := snap["gateway"].(map[string]any); !ok || gw["port"] != float64(18789) {
		t.Errorf("snapshot lost gateway.port carryover: %v", snap["gateway"])
	}
	if _, ok := snap["providers"].(map[string]any); !ok {
		t.Errorf("snapshot lost providers carryover: %v", snap)
	}

	// The snapshot lives in envRoot alongside the wrapper, so the daemon
	// does NOT need to grant an OPENCLAW_INCLUDE_ROOTS entry for it.
	if result.IncludeRoot != "" {
		t.Errorf("IncludeRoot = %q, want empty (snapshot lives in envRoot, no cross-dir include)", result.IncludeRoot)
	}
}

// TestPrepareOpenclawConfigStrictPreservesNonServerMcpKeys — Elon's
// follow-up must-fix: the strict-replace path must scope only to
// `mcp.servers`, not the entire `mcp` block. OpenClaw config has
// sibling settings under `mcp` (e.g. `sessionIdleTtlMs` — see
// https://docs.openclaw.ai/gateway/configuration-reference#mcp). The
// previous implementation deleted the whole `mcp` block which silently
// reset those siblings to OpenClaw's defaults. This test fixes that
// scope: managed-MCP path drops `mcp.servers` but leaves
// `mcp.sessionIdleTtlMs` intact in the snapshot.
func TestPrepareOpenclawConfigStrictPreservesNonServerMcpKeys(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	userCfgPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userCfgPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}
	// User's resolved config has BOTH `mcp.servers` (must be stripped) and
	// `mcp.sessionIdleTtlMs` (must survive). The snapshot is what OpenClaw
	// loads via the wrapper's $include, so only the snapshot's `mcp` block
	// is consulted for non-server settings.
	resolvedUser := `{
		"mcp": {
			"sessionIdleTtlMs": 300000,
			"servers": {"global_one": {"command": "/bin/echo"}}
		},
		"gateway": {"port": 18789}
	}`
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: userCfgPath},
		"config get --json":             {stdout: resolvedUser},
		"config get agents.list --json": {stdout: "null"},
	})
	mcpConfig := json.RawMessage(`{"mcpServers": {"managed_only": {"command": "uvx", "args": ["m"]}}}`)

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
		OpenclawBin: stub.bin,
		McpConfig:   mcpConfig,
	})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}

	snapPath := filepath.Join(envRoot, openclawUserSnapshotFile)
	snap := mustReadJSON(t, snapPath)
	snapMcp, ok := snap["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("snapshot lost the mcp block entirely; mcp.sessionIdleTtlMs should have survived: %v", snap)
	}
	if _, leaked := snapMcp["servers"]; leaked {
		t.Errorf("snapshot still has mcp.servers; strict scope must drop it: %v", snapMcp)
	}
	// json.Unmarshal decodes JSON numbers as float64.
	if ttl, ok := snapMcp["sessionIdleTtlMs"].(float64); !ok || ttl != 300000 {
		t.Errorf("snapshot lost mcp.sessionIdleTtlMs (should be preserved): %v", snapMcp)
	}

	// Wrapper still emits the managed-only server set on top, so the
	// effective view post-include is exactly the managed set.
	got := mustReadJSON(t, result.ConfigPath)
	wrapperMcp, _ := got["mcp"].(map[string]any)
	servers, _ := wrapperMcp["servers"].(map[string]any)
	if _, ok := servers["managed_only"]; !ok {
		t.Errorf("wrapper missing managed_only: %v", servers)
	}
	if _, leaked := servers["global_one"]; leaked {
		t.Errorf("global_one leaked into wrapper: %v", servers)
	}
}

// TestPrepareOpenclawConfigStrictEmptyManagedSetDropsUserMcp — empty
// managed set `{}` must drop the user's global mcp.servers too. Without
// strict replace, OpenClaw would still resolve user-only servers via the
// $include and the admin's "saved no servers" intent would be silently
// overridden.
func TestPrepareOpenclawConfigStrictEmptyManagedSetDropsUserMcp(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	userCfgPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userCfgPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}
	resolvedUser := `{"mcp": {"servers": {"global_one": {"command": "/bin/echo"}}}}`

	cases := map[string]json.RawMessage{
		"object_empty":          json.RawMessage(`{}`),
		"mcp_servers_empty_map": json.RawMessage(`{"mcpServers": {}}`),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			stub := installOpenclawStub(t, map[string]openclawResponse{
				"config file":                   {stdout: userCfgPath},
				"config get --json":             {stdout: resolvedUser},
				"config get agents.list --json": {stdout: "null"},
			})
			result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
				OpenclawBin: stub.bin,
				McpConfig:   raw,
			})
			if err != nil {
				t.Fatalf("prepareOpenclawConfig: %v", err)
			}
			got := mustReadJSON(t, result.ConfigPath)
			mcp, ok := got["mcp"].(map[string]any)
			if !ok {
				t.Fatalf("wrapper missing mcp block (managed empty must still be present): %v", got)
			}
			servers, ok := mcp["servers"].(map[string]any)
			if !ok {
				t.Fatalf("mcp.servers is not an object: %v", mcp)
			}
			if len(servers) != 0 {
				t.Errorf("mcp.servers has %d entries on managed-empty, want 0 (global_one must not leak): %v", len(servers), servers)
			}
			// And the snapshot must have dropped the user's mcp block, so the
			// $include resolves with no mcp at all.
			snapPath := filepath.Join(envRoot, openclawUserSnapshotFile)
			snap := mustReadJSON(t, snapPath)
			if _, present := snap["mcp"]; present {
				t.Errorf("snapshot still has `mcp` — strict empty must drop the user block: %v", snap["mcp"])
			}
		})
	}
}

// TestPrepareOpenclawConfigNullMcpConfigKeepsUserInclude — when the agent
// has no managed mcp_config (`null` / absent), the wrapper must NOT write
// a sanitized snapshot and must $include the live user config so the
// user's global mcp.servers and other config still flow through. This is
// the "inherit defaults" branch — must remain a no-op vs. the previous
// implementation.
func TestPrepareOpenclawConfigNullMcpConfigKeepsUserInclude(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	userCfgDir := t.TempDir()
	userCfgPath := filepath.Join(userCfgDir, "openclaw.json")
	if err := os.WriteFile(userCfgPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	cases := map[string]json.RawMessage{
		"nil":   nil,
		"empty": json.RawMessage(""),
		"null":  json.RawMessage("null"),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			stub := installOpenclawStub(t, map[string]openclawResponse{
				"config file":                   {stdout: userCfgPath},
				"config get agents.list --json": {stdout: "null"},
				// Note: no `config get --json` stub — the inherit path must
				// not call it (would burn an extra CLI roundtrip per task).
			})
			result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
				OpenclawBin: stub.bin,
				McpConfig:   raw,
			})
			if err != nil {
				t.Fatalf("prepareOpenclawConfig: %v", err)
			}
			got := mustReadJSON(t, result.ConfigPath)
			if _, present := got["mcp"]; present {
				t.Errorf("wrapper has mcp block when mcp_config = %q: %v", name, got["mcp"])
			}
			include, _ := got["$include"].([]any)
			if len(include) != 1 || include[0] != userCfgPath {
				t.Errorf("$include = %v, want live user config %q on inherit path", got["$include"], userCfgPath)
			}
			if _, err := os.Stat(filepath.Join(envRoot, openclawUserSnapshotFile)); !os.IsNotExist(err) {
				t.Errorf("inherit path wrote a snapshot file (should not): err=%v", err)
			}
			if result.IncludeRoot != userCfgDir {
				t.Errorf("IncludeRoot = %q, want %q (cross-dir hop for live $include)", result.IncludeRoot, userCfgDir)
			}
		})
	}
}

// TestPrepareOpenclawConfigManagedSetFreshInstall — managed mcp_config on
// a fresh install (no on-disk user config) must NOT call `config get
// --json` (there is nothing to snapshot) and must write a wrapper that
// carries managed servers as the sole MCP definition with no $include.
func TestPrepareOpenclawConfigManagedSetFreshInstall(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	missingPath := filepath.Join(t.TempDir(), "openclaw.json")
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {stdout: missingPath},
		// No `config get --json` stub — fresh install must not call it.
	})
	mcpConfig := json.RawMessage(`{"mcpServers": {"context7": {"command": "uvx", "args": ["context7-mcp"]}}}`)

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
		OpenclawBin: stub.bin,
		McpConfig:   mcpConfig,
	})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	got := mustReadJSON(t, result.ConfigPath)
	mcp, ok := got["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("wrapper missing mcp block: %v", got)
	}
	servers, ok := mcp["servers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp.servers is not an object: %v", mcp)
	}
	entry, _ := servers["context7"].(map[string]any)
	if entry == nil || entry["command"] != "uvx" {
		t.Errorf("context7 entry missing/wrong on fresh install: %v", servers)
	}
	args, _ := entry["args"].([]any)
	if len(args) != 1 || args[0] != "context7-mcp" {
		t.Errorf("context7.args = %v", args)
	}
	if _, present := got["$include"]; present {
		t.Errorf("fresh install should not emit $include: %v", got["$include"])
	}
}

// TestPrepareOpenclawConfigFailsClosedOnResolvedConfigError — when the
// user has a config on disk and the agent has managed mcp_config but
// `openclaw config get --json` errors, the preparer must NOT fall back to
// `$include`ing the live user file (which would leak global mcp.servers).
// Fail closed instead, mirroring the existing fail-closed posture.
func TestPrepareOpenclawConfigFailsClosedOnResolvedConfigError(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	userCfgPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userCfgPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: userCfgPath},
		"config get agents.list --json": {stdout: "null"},
		"config get --json":             {err: errors.New("openclaw: schema validation failed")},
	})
	mcpConfig := json.RawMessage(`{"mcpServers": {"context7": {"command": "uvx"}}}`)

	_, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
		OpenclawBin: stub.bin,
		McpConfig:   mcpConfig,
	})
	if err == nil {
		t.Fatal("prepareOpenclawConfig succeeded when `config get --json` errored; expected fail closed")
	}
	if !strings.Contains(err.Error(), "resolved config") {
		t.Errorf("error %q does not name the resolved-config step", err.Error())
	}
	// No stale wrapper / snapshot left behind.
	if _, err := os.Stat(filepath.Join(envRoot, openclawConfigFile)); !os.IsNotExist(err) {
		t.Errorf("wrapper exists after fail-closed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(envRoot, openclawUserSnapshotFile)); !os.IsNotExist(err) {
		t.Errorf("snapshot exists after fail-closed: %v", err)
	}
}

// TestPrepareOpenclawConfigFailsClosedOnMalformedMcpConfig — keeping with
// the fail-closed posture used for the rest of the preparer: a malformed
// mcp_config must not write any wrapper file, so the daemon surfaces the
// error instead of booting OpenClaw with an empty / inherited MCP set the
// admin didn't expect.
func TestPrepareOpenclawConfigFailsClosedOnMalformedMcpConfig(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	userCfgPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userCfgPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	cases := map[string]json.RawMessage{
		"unparseable_json":      json.RawMessage(`{not-json}`),
		"entry_missing_command": json.RawMessage(`{"mcpServers": {"bad": {}}}`),
		"entry_wrong_shape":     json.RawMessage(`{"mcpServers": {"bad": "not-an-object"}}`),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			stub := installOpenclawStub(t, map[string]openclawResponse{
				"config file":                   {stdout: userCfgPath},
				"config get agents.list --json": {stdout: "null"},
			})
			_, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
				OpenclawBin: stub.bin,
				McpConfig:   raw,
			})
			if err == nil {
				t.Fatalf("prepareOpenclawConfig succeeded on %s; expected fail closed", name)
			}
			if !strings.Contains(err.Error(), "mcp_config") && !strings.Contains(err.Error(), "mcp_servers") {
				t.Errorf("error %q does not name the mcp_config step", err.Error())
			}
		})
	}
}

// TestPrepareOpenclawSkillWriteMatchesScanPath is the regression test the
// MUL-2219 DoD calls out: the directory Multica writes skills into MUST be
// the same directory the OpenClaw scanner reads from. We assert this by
// resolving the workspaceDir the way OpenClaw does (agents.defaults.workspace
// from the synthesized config) and proving {workspaceDir}/skills/ holds the
// skill we wrote. Previous fixes asserted "we wrote a file" without checking
// the scanner would ever see it; that is why MUL-2213 / #2621 needed a
// follow-up.
func TestPrepareOpenclawSkillWriteMatchesScanPath(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	for _, sub := range []string{workDir, filepath.Join(envRoot, "output"), filepath.Join(envRoot, "logs")} {
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		// Fresh install — no user config on disk. Wrapper carries only the
		// workspace override, which is what the scanner reads.
		"config file": {stdout: filepath.Join(t.TempDir(), "absent-openclaw.json")},
	})

	skills := []SkillContextForEnv{
		{Name: "Issue Review", Content: "Review issues thoroughly."},
		{Name: "Local Dev", Content: "Spin up the local dev env."},
	}

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	cfgPath := result.ConfigPath
	if err := writeContextFiles(workDir, "openclaw", TaskContextForEnv{
		IssueID:     "issue-1",
		AgentSkills: skills,
	}, nil); err != nil {
		t.Fatalf("writeContextFiles: %v", err)
	}

	cfg := mustReadJSON(t, cfgPath)
	wsDir := cfg["agents"].(map[string]any)["defaults"].(map[string]any)["workspace"].(string)
	for _, s := range skills {
		want := filepath.Join(wsDir, "skills", sanitizeSkillName(s.Name), "SKILL.md")
		if _, err := os.Stat(want); err != nil {
			t.Errorf("openclaw scan target %s missing — Multica's write path and the openclaw scanner are out of sync: %v", want, err)
		}
	}
}

// TestPrepareEnvironmentOpenclawWiresConfigPath — end-to-end: Prepare sets
// env.OpenclawConfigPath so the daemon can export OPENCLAW_CONFIG_PATH, and
// the path resolves to a file with the correct workspace override. With
// fail-closed semantics, Prepare itself errors when the CLI is unavailable;
// a stub here keeps the happy path observable.
func TestPrepareEnvironmentOpenclawWiresConfigPath(t *testing.T) {
	wsRoot := t.TempDir()

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {stdout: filepath.Join(t.TempDir(), "absent.json")},
	})

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: wsRoot,
		WorkspaceID:    "ws-1",
		TaskID:         "11111111-2222-3333-4444-555555555555",
		AgentName:      "scout",
		Provider:       "openclaw",
		OpenclawBin:    stub.bin,
		Task: TaskContextForEnv{
			IssueID: "issue-1",
		},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if env.OpenclawConfigPath == "" {
		t.Fatal("Prepare(openclaw) did not set OpenclawConfigPath")
	}
	got := mustReadJSON(t, env.OpenclawConfigPath)
	workspace := got["agents"].(map[string]any)["defaults"].(map[string]any)["workspace"]
	if workspace != env.WorkDir {
		t.Errorf("agents.defaults.workspace = %v, want %q", workspace, env.WorkDir)
	}
	// Fresh install path emits no $include, so the Environment should
	// leave OpenclawIncludeRoot empty — the daemon must NOT spuriously
	// grant include roots when no cross-dir hop is being made.
	if env.OpenclawIncludeRoot != "" {
		t.Errorf("OpenclawIncludeRoot = %q on fresh install, want empty", env.OpenclawIncludeRoot)
	}
}

// TestPrepareEnvironmentOpenclawWiresIncludeRoot — when the user has an
// on-disk active config (the common non-fresh-install case), Prepare must
// surface the active config's dirname on the Environment so the daemon
// can export OPENCLAW_INCLUDE_ROOTS. Without this, the wrapper's
// $include into ~/.openclaw/openclaw.json is rejected at runtime.
func TestPrepareEnvironmentOpenclawWiresIncludeRoot(t *testing.T) {
	wsRoot := t.TempDir()

	userCfgDir := t.TempDir()
	userCfgPath := filepath.Join(userCfgDir, "openclaw.json")
	if err := os.WriteFile(userCfgPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: userCfgPath},
		"config get agents.list --json": {stdout: "null"},
	})

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: wsRoot,
		WorkspaceID:    "ws-1",
		TaskID:         "33333333-2222-3333-4444-555555555555",
		AgentName:      "scout",
		Provider:       "openclaw",
		OpenclawBin:    stub.bin,
		Task:           TaskContextForEnv{IssueID: "issue-1"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if env.OpenclawIncludeRoot != userCfgDir {
		t.Errorf("OpenclawIncludeRoot = %q, want %q (dirname of active config so daemon can grant OPENCLAW_INCLUDE_ROOTS)", env.OpenclawIncludeRoot, userCfgDir)
	}
}

// TestPrepareEnvironmentOpenclawFailsClosed — when the openclaw CLI errors
// during Prepare, the whole call must fail. Previously the preparer logged
// a warning and continued with no config; we have removed that path.
func TestPrepareEnvironmentOpenclawFailsClosed(t *testing.T) {
	wsRoot := t.TempDir()

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {err: errors.New("openclaw config validation failed")},
	})

	_, err := Prepare(PrepareParams{
		WorkspacesRoot: wsRoot,
		WorkspaceID:    "ws-1",
		TaskID:         "22222222-2222-3333-4444-555555555555",
		AgentName:      "scout",
		Provider:       "openclaw",
		OpenclawBin:    stub.bin,
		Task:           TaskContextForEnv{IssueID: "issue-1"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("Prepare(openclaw) succeeded when CLI errored; expected fail closed")
	}
	if !strings.Contains(err.Error(), "prepare openclaw config") {
		t.Errorf("error message %q does not name the openclaw config step", err.Error())
	}
}

// TestPrepareEnvironmentNonOpenclawSkipsConfig — non-openclaw providers
// must not get a synthesized openclaw config (it would be dead weight on
// disk and confuse the GC reaper's idea of what an env contains). They
// also must NOT shell out to the openclaw CLI, so the stub here records
// zero calls.
func TestPrepareEnvironmentNonOpenclawSkipsConfig(t *testing.T) {
	wsRoot := t.TempDir()

	stub := installOpenclawStub(t, map[string]openclawResponse{})

	taskIDs := map[string]string{
		"claude":   "aaaaaaaa-1111-2222-3333-444444444444",
		"opencode": "bbbbbbbb-1111-2222-3333-444444444444",
		"hermes":   "cccccccc-1111-2222-3333-444444444444",
		"kiro":     "dddddddd-1111-2222-3333-444444444444",
	}
	for provider, taskID := range taskIDs {
		t.Run(provider, func(t *testing.T) {
			env, err := Prepare(PrepareParams{
				WorkspacesRoot: wsRoot,
				WorkspaceID:    "ws-1",
				TaskID:         taskID,
				AgentName:      "scout",
				Provider:       provider,
				Task:           TaskContextForEnv{IssueID: "issue-1"},
			}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatalf("Prepare(%s): %v", provider, err)
			}
			if env.OpenclawConfigPath != "" {
				t.Errorf("provider %s should not get an OpenclawConfigPath, got %q", provider, env.OpenclawConfigPath)
			}
			if _, err := os.Stat(filepath.Join(env.RootDir, openclawConfigFile)); !os.IsNotExist(err) {
				t.Errorf("provider %s left a stray openclaw-config.json", provider)
			}
		})
	}
	if len(stub.calls) != 0 {
		t.Errorf("non-openclaw providers shelled out to openclaw CLI %d times: %+v", len(stub.calls), stub.calls)
	}
}
