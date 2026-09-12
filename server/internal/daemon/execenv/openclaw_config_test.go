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
	"runtime"
	"strings"
	"testing"
	"time"
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
	// deadline is the ctx deadline this invocation ran under, zero if it had
	// none. Recorded because the budget the ceiling is derived from counts
	// deadlines, not calls: path resolution makes two invocations under one
	// shared deadline, and a test that counted calls could not tell that apart
	// from a fifth budget. See TestPrepareOpenclawConfigWorstCaseCLIBudgets.
	deadline time.Time
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

func TestPrepareOpenclawConfigUsesPairedCommandEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("env shebang fixture is Unix-specific")
	}

	const interpreterName = "multica-openclaw-env-test"
	runtimeBin := t.TempDir()
	interpreter := filepath.Join(runtimeBin, interpreterName)
	interpreterScript := `#!/bin/sh
shift
case "$*" in
  "config validate --json") printf '%s\n' '{"path":"$OPENCLAW_HOME/.openclaw/openclaw.json"}' ;;
  "config get agents.list --json") printf '%s\n' '[]' ;;
  *) exit 42 ;;
esac
`
	if err := os.WriteFile(interpreter, []byte(interpreterScript), 0o755); err != nil {
		t.Fatalf("write managed interpreter: %v", err)
	}
	target := filepath.Join(t.TempDir(), "openclaw")
	if err := os.WriteFile(target, []byte("#!/usr/bin/env "+interpreterName+"\n"), 0o755); err != nil {
		t.Fatalf("write managed openclaw target: %v", err)
	}

	openclawHome := t.TempDir()
	configPath := filepath.Join(openclawHome, ".openclaw", "openclaw.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir openclaw config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write openclaw config: %v", err)
	}
	t.Setenv("PATH", "/usr/bin:/bin")

	if _, err := execOpenclawCLI(context.Background(), target, "config", "validate", "--json"); err == nil {
		t.Fatal("managed openclaw unexpectedly ran without its paired interpreter environment")
	}

	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
		OpenclawBin: target,
		Env: map[string]string{
			"PATH":          runtimeBin + string(os.PathListSeparator) + "/usr/bin:/bin",
			"OPENCLAW_HOME": openclawHome,
		},
	})
	if err != nil {
		t.Fatalf("prepare openclaw config with paired environment: %v", err)
	}
	if result.IncludeRoot != filepath.Dir(configPath) {
		t.Fatalf("include root = %q, want config directory %q", result.IncludeRoot, filepath.Dir(configPath))
	}
}

func (s *openclawCLIStub) exec(ctx context.Context, _ map[string]string, bin string, args ...string) (string, error) {
	deadline, _ := ctx.Deadline()
	s.calls = append(s.calls, openclawCall{bin: bin, args: append([]string(nil), args...), deadline: deadline})
	key := strings.Join(args, " ")
	if resp, ok := s.responses[key]; ok {
		return resp.stdout, resp.err
	}
	if key == "config validate --json" {
		return s.derivedValidateResponse()
	}
	return "", fmt.Errorf("openclawCLIStub: unexpected args %q", key)
}

// derivedValidateResponse answers `config validate --json` from the registered
// `config file` response when a test did not register one itself.
//
// openclawActiveConfigPath asks `config validate --json` first because its answer
// arrives in a named field (see there). Almost every test in this package fixes
// the *outcome* of path resolution — which file the wrapper $includes, how many
// CLI calls a cold preparation makes, what happens when the CLI is missing — and
// not which subcommand supplies it. Deriving keeps those tests stating what they
// are about; hand-writing a second response into 30-odd maps would turn the next
// change to this boundary into a 30-site edit and bury the two tests that do care.
//
// A test that cares registers "config validate --json" explicitly:
// TestOpenclawActiveConfigPathIgnoresAPathShapedWarning and the fallback cases in
// openclaw_process_tree_test.go drive the real binary instead, so they are
// unaffected by this.
func (s *openclawCLIStub) derivedValidateResponse() (string, error) {
	resp, ok := s.responses["config file"]
	if !ok {
		return "", fmt.Errorf("openclawCLIStub: no `config file` response to derive validate from")
	}
	// An unusable `config file` means an unusable CLI for this purpose: report the
	// same failure so the test exercises whatever fallback it is about.
	if resp.err != nil {
		return "", resp.err
	}
	path := strings.TrimSpace(resp.stdout)
	if path == "" {
		return "", nil
	}
	payload, err := json.Marshal(map[string]any{"valid": true, "path": path})
	if err != nil {
		return "", err
	}
	return string(payload) + "\n", nil
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

// TestPrepareOpenclawConfigFallsBackWhenConfigFileUnsupported covers
// OpenClaw 2026.2.x builds that rejected `openclaw config file` with
// "too many arguments for 'config'". That command-shape mismatch should
// not make every task fail during execenv prep; the daemon can derive the
// active path from OpenClaw's documented and legacy config candidates and
// then continue using `config get ... --json` for resolved config data.
func TestPrepareOpenclawConfigFallsBackWhenConfigFileUnsupported(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	userConfigDir := t.TempDir()
	userConfigPath := filepath.Join(userConfigDir, "openclaw.json")
	if err := os.WriteFile(userConfigPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}
	t.Setenv("OPENCLAW_CONFIG_PATH", userConfigPath)

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {err: errors.New("openclaw config file: exit status 1 (stderr: error: too many arguments for 'config'. Expected 0 arguments but got 1.)")},
		"config get agents.list --json": {stdout: `[
			{ "id": "coder", "model": "openai/gpt-5" }
		]`},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}

	got := mustReadJSON(t, result.ConfigPath)
	include, ok := got["$include"].([]any)
	if !ok || len(include) != 1 || include[0] != userConfigPath {
		t.Errorf("$include = %v, want fallback OPENCLAW_CONFIG_PATH %q", got["$include"], userConfigPath)
	}
	if result.IncludeRoot != userConfigDir {
		t.Errorf("IncludeRoot = %q, want %q", result.IncludeRoot, userConfigDir)
	}
	agents := got["agents"].(map[string]any)
	list := agents["list"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["workspace"] != workDir {
		t.Errorf("agents.list workspace rewrite after fallback = %v, want workDir %q", list, workDir)
	}
	// Three calls, not two, and the extra one is the point of this test now: a CLI
	// too old to support `config file` is also too old for `config validate --json`,
	// so path resolution asks both before falling back to the candidate shape. That
	// is one extra invocation on exactly the hosts that were already on a
	// deprecated command shape, and it buys every current host an answer that a
	// path-shaped warning line cannot corrupt (see openclawActiveConfigPath).
	if len(stub.calls) != 3 {
		t.Fatalf("openclaw calls = %d, want 3: %+v", len(stub.calls), stub.calls)
	}
	if got := strings.Join(stub.calls[0].args, " "); got != "config validate --json" {
		t.Errorf("first openclaw call = %q, want config validate --json", got)
	}
	if got := strings.Join(stub.calls[1].args, " "); got != "config file" {
		t.Errorf("second openclaw call = %q, want config file", got)
	}
	if strings.Join(stub.calls[2].args, " ") != "config get agents.list --json" {
		t.Errorf("third openclaw call = %q, want config get agents.list --json", strings.Join(stub.calls[2].args, " "))
	}
}

func TestOpenclawActiveConfigPathFallbackSources(t *testing.T) {
	cases := map[string]struct {
		setup func(t *testing.T) string
	}{
		"config_path": {
			setup: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "custom-openclaw.json")
				t.Setenv("OPENCLAW_CONFIG_PATH", path)
				return path
			},
		},
		"legacy_config_path": {
			setup: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "custom-clawdbot.json")
				t.Setenv("CLAWDBOT_CONFIG_PATH", path)
				return path
			},
		},
		"state_dir": {
			setup: func(t *testing.T) string {
				stateDir := t.TempDir()
				path := filepath.Join(stateDir, "openclaw.json")
				t.Setenv("OPENCLAW_STATE_DIR", stateDir)
				return path
			},
		},
		"legacy_state_dir": {
			setup: func(t *testing.T) string {
				stateDir := t.TempDir()
				path := filepath.Join(stateDir, "clawdbot.json")
				t.Setenv("CLAWDBOT_STATE_DIR", stateDir)
				return path
			},
		},
		"openclaw_home": {
			setup: func(t *testing.T) string {
				home := t.TempDir()
				path := filepath.Join(home, ".openclaw", "openclaw.json")
				t.Setenv("OPENCLAW_HOME", home)
				return path
			},
		},
		"default_home": {
			setup: func(t *testing.T) string {
				home := t.TempDir()
				path := filepath.Join(home, ".openclaw", "openclaw.json")
				t.Setenv("HOME", home)
				return path
			},
		},
		"legacy_default_clawdbot": {
			setup: func(t *testing.T) string {
				home := t.TempDir()
				path := filepath.Join(home, ".clawdbot", "clawdbot.json")
				t.Setenv("HOME", home)
				return path
			},
		},
		"legacy_default_moltbot": {
			setup: func(t *testing.T) string {
				home := t.TempDir()
				path := filepath.Join(home, ".moltbot", "moltbot.json")
				t.Setenv("HOME", home)
				return path
			},
		},
		"legacy_default_moldbot": {
			setup: func(t *testing.T) string {
				home := t.TempDir()
				path := filepath.Join(home, ".moldbot", "moldbot.json")
				t.Setenv("HOME", home)
				return path
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			clearOpenclawPathEnv(t)
			want := tc.setup(t)
			if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
				t.Fatalf("mkdir config dir: %v", err)
			}
			if err := os.WriteFile(want, []byte(`{}`), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			stub := installOpenclawStub(t, map[string]openclawResponse{
				"config file": {err: openclawConfigFileUnsupportedErr()},
			})

			got, exists, err := openclawActiveConfigPath(stub.bin, openclawCLITimeout)
			if err != nil {
				t.Fatalf("openclawActiveConfigPath: %v", err)
			}
			if !exists {
				t.Fatal("exists = false, want true")
			}
			if got != want {
				t.Errorf("path = %q, want %q", got, want)
			}
		})
	}
}

func TestOpenclawActiveConfigPathFallbackFreshInstallUsesCanonicalPath(t *testing.T) {
	clearOpenclawPathEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {err: openclawConfigFileUnsupportedErr()},
	})

	got, exists, err := openclawActiveConfigPath(stub.bin, openclawCLITimeout)
	if err != nil {
		t.Fatalf("openclawActiveConfigPath: %v", err)
	}
	want := filepath.Join(home, ".openclaw", "openclaw.json")
	if got != want {
		t.Errorf("path = %q, want canonical fresh-install path %q", got, want)
	}
	if exists {
		t.Fatal("exists = true, want false for fresh install")
	}
}

func TestOpenclawActiveConfigPathFallbackOpenclawConfigPathHardOverride(t *testing.T) {
	clearOpenclawPathEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	explicitPath := filepath.Join(t.TempDir(), "missing-openclaw.json")
	t.Setenv("OPENCLAW_CONFIG_PATH", explicitPath)
	legacyPath := filepath.Join(home, ".clawdbot", "clawdbot.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy config dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {err: openclawConfigFileUnsupportedErr()},
	})

	got, exists, err := openclawActiveConfigPath(stub.bin, openclawCLITimeout)
	if err != nil {
		t.Fatalf("openclawActiveConfigPath: %v", err)
	}
	if got != explicitPath {
		t.Errorf("path = %q, want OPENCLAW_CONFIG_PATH hard override %q", got, explicitPath)
	}
	if exists {
		t.Fatal("exists = true, want false when explicit OPENCLAW_CONFIG_PATH is missing")
	}
}

func TestOpenclawActiveConfigPathFallbackErrorIncludesOriginalCLIError(t *testing.T) {
	clearOpenclawPathEnv(t)
	badPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.MkdirAll(badPath, 0o755); err != nil {
		t.Fatalf("mkdir bad config path: %v", err)
	}
	t.Setenv("OPENCLAW_CONFIG_PATH", badPath)
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {err: openclawConfigFileUnsupportedErr()},
	})

	_, _, err := openclawActiveConfigPath(stub.bin, openclawCLITimeout)
	if err == nil {
		t.Fatal("openclawActiveConfigPath succeeded with directory config path; expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "too many arguments for 'config'") {
		t.Errorf("error %q lost original unsupported CLI stderr", msg)
	}
	if !strings.Contains(msg, "is a directory") {
		t.Errorf("error %q lost fallback failure detail", msg)
	}
}

func TestIsOpenclawConfigFileUnsupportedMatchesKnownShapes(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"reported_too_many_arguments": {
			err:  errors.New("openclaw config file: exit status 1 (stderr: error: too many arguments for 'config')"),
			want: true,
		},
		"reported_expected_zero_args": {
			err:  errors.New("Expected 0 arguments but got 1."),
			want: true,
		},
		"unknown_config_file": {
			err:  errors.New("unknown subcommand `file` for `openclaw config`"),
			want: true,
		},
		"real_config_validation_error": {
			err:  errors.New("openclaw config validation failed: missing gateway.auth.token"),
			want: false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := isOpenclawConfigFileUnsupported(tc.err); got != tc.want {
				t.Errorf("isOpenclawConfigFileUnsupported(%q) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}

func clearOpenclawPathEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OPENCLAW_CONFIG_PATH", "")
	t.Setenv("OPENCLAW_STATE_DIR", "")
	t.Setenv("OPENCLAW_HOME", "")
	t.Setenv("CLAWDBOT_CONFIG_PATH", "")
	t.Setenv("CLAWDBOT_STATE_DIR", "")
}

func openclawConfigFileUnsupportedErr() error {
	return errors.New("openclaw config file: exit status 1 (stderr: error: too many arguments for 'config'. Expected 0 arguments but got 1.)")
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
		// Pre-2026.6 single-agent installs with no per-agent overrides resolve
		// to an empty registry once the config-path probe reports missing.
		// (2026.6.x registry-population is covered by
		// TestPrepareOpenclawConfigNewSchemaOmitsAgentsList.)
		"agents list --json": {stdout: "null"},
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

// TestPrepareOpenclawConfigParsesPathFromUITerminalOutput — regression test
// for the case where `openclaw config file` prints terminal UI borders
// (e.g., Doctor warnings) before the actual path. The path is always the
// last non-empty line.
func TestPrepareOpenclawConfigParsesPathFromUITerminalOutput(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	userConfigDir := t.TempDir()
	userConfigPath := filepath.Join(userConfigDir, "openclaw.json")
	if err := os.WriteFile(userConfigPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	// Simulate OpenClaw's output with UI borders (Doctor warnings)
	stdoutWithUI := `│
◇  Doctor warnings ──────────────────────────────────────────────────────╮
│                                                                        │
│  - Left plugin install index in place because shared SQLite state has  │
│    conflicting plugin install metadata for: qqbot                      │
│                                                                        │
├────────────────────────────────────────────────────────────────────────╯
[state-migrations] Legacy state migration warnings:
- Left plugin install index in place because shared SQLite state has conflicting plugin install metadata for: qqbot
│
◇  Doctor warnings ──────────────────────────────────────────────────────╮
│                                                                        │
│  - Left plugin install index in place because shared SQLite state has  │
│    conflicting plugin install metadata for: qqbot                      │
│                                                                        │
├────────────────────────────────────────────────────────────────────────╯
` + userConfigPath + "\n"

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: stdoutWithUI},
		"config get agents.list --json": {stdout: "null"},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}

	got := mustReadJSON(t, result.ConfigPath)
	include := got["$include"].([]any)
	if include[0] != userConfigPath {
		t.Errorf("$include[0] = %v, want %q (path must be extracted from last non-empty line)", include[0], userConfigPath)
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
// assertion for strict replace. The user has a global `mcp.servers.global_one`
// and a same-named `shared`; the agent has a managed `shared + managed_only`
// set; the view OpenClaw resolves must be exactly the managed set.
//
// What changed is how that is achieved. It used to be a sanitized copy of the
// user's resolved config written into the task directory; it is now include
// order:
//
//	user's live config  ->  mcp.servers: null  ->  wrapper's managed servers
//
// So the assertions are the three things that composition rests on — the include
// list and its order, the reset stage's content, and the wrapper's own block —
// plus the property the copy could never have: nothing from the user's config is
// written into the task directory at all, which is what removes the redaction
// hazard rather than working around it.
func TestPrepareOpenclawConfigStrictReplacesUserMcpServers(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	// A real user config on disk, secrets included. Nothing this package writes
	// may contain any of it — asserted below by scanning envRoot.
	userCfgDir := t.TempDir()
	userCfgPath := filepath.Join(userCfgDir, "openclaw.json")
	userCfg := `{
		"mcp": {"servers": {
			"global_one": {"command": "/bin/echo", "args": ["user"]},
			"shared":     {"command": "/bin/old-version"}
		}},
		"gateway": {"port": 18789},
		"providers": {"anthropic": {"apiKey": "sk-user-secret"}}
	}`
	if err := os.WriteFile(userCfgPath, []byte(userCfg), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: userCfgPath},
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

	// Include order is the mechanism, so it is what gets pinned: the user's live
	// config first, then the reset stage. Reversed, the user's servers would
	// arrive after the null and survive.
	resetPath := filepath.Join(envRoot, openclawMcpResetFile)
	include, _ := got["$include"].([]any)
	if len(include) != 2 || include[0] != userCfgPath || include[1] != resetPath {
		t.Fatalf("wrapper $include = %#v, want [%q, %q]", got["$include"], userCfgPath, resetPath)
	}
	if body, rerr := os.ReadFile(resetPath); rerr != nil {
		t.Fatalf("read mcp reset stage: %v", rerr)
	} else if string(body) != openclawMcpResetBody {
		t.Errorf("mcp reset stage = %q, want exactly %q", body, openclawMcpResetBody)
	}

	// The include reaches the user's own directory, so that hop has to be
	// granted or OpenClaw's confinement check refuses the chain at load.
	if result.IncludeRoot != userCfgDir {
		t.Errorf("IncludeRoot = %q, want %q — the chain includes the live user config", result.IncludeRoot, userCfgDir)
	}

	// And the property that makes this design safe rather than merely working:
	// no byte of the user's configuration is copied into the task directory, so
	// there is nothing to redact, truncate or write back stale.
	assertEnvRootCarriesNoUserConfig(t, envRoot, "sk-user-secret", "global_one", "18789")
}

// assertEnvRootCarriesNoUserConfig fails if any file the preparer wrote contains
// a marker from the user's own config.
//
// A scan rather than an assertion about one named file, because the guarantee is
// about the directory: the old design's snapshot was one file, and the point of
// replacing it is that no file has that content now.
func assertEnvRootCarriesNoUserConfig(t *testing.T, envRoot string, markers ...string) {
	t.Helper()
	entries, err := os.ReadDir(envRoot)
	if err != nil {
		t.Fatalf("read envRoot: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(envRoot, entry.Name())
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("read %s: %v", path, rerr)
		}
		for _, marker := range markers {
			if strings.Contains(string(body), marker) {
				t.Errorf("%s contains %q from the user's config; this path must copy none of it", entry.Name(), marker)
			}
		}
	}
}

// TestPrepareOpenclawConfigIncludeRootGrants pins which include targets need an
// OPENCLAW_INCLUDE_ROOTS grant, since getting this wrong fails in a way no
// wrapper-shape assertion would catch: OpenClaw's include-confinement check
// rejects the chain at load time, and the task fails with a config error rather
// than anything pointing back here.
//
// The managed-MCP row is the one that changed. The flat resolved-config copy it
// replaces was self-contained, so it needed no grant; the chain reaches the
// user's real config, so it does. The reset stage needs none of its own — it sits
// in envRoot beside the wrapper that includes it.
func TestPrepareOpenclawConfigIncludeRootGrants(t *testing.T) {
	cases := []struct {
		name       string
		mcpConfig  json.RawMessage
		userConfig bool
		wantGrant  bool
	}{
		{name: "managed mcp includes the user config", mcpConfig: json.RawMessage(`{"mcpServers":{"m":{"command":"uvx"}}}`), userConfig: true, wantGrant: true},
		{name: "inherited mcp includes the user config", userConfig: true, wantGrant: true},
		{name: "fresh install has nothing to include", mcpConfig: json.RawMessage(`{"mcpServers":{"m":{"command":"uvx"}}}`), userConfig: false, wantGrant: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			envRoot := t.TempDir()
			workDir := filepath.Join(envRoot, "workdir")
			if err := os.MkdirAll(workDir, 0o755); err != nil {
				t.Fatalf("mkdir workdir: %v", err)
			}
			responses := map[string]openclawResponse{
				"config get agents.list --json": {stdout: "null"},
			}
			userCfgPath := ""
			if tc.userConfig {
				userCfgPath = filepath.Join(t.TempDir(), "openclaw.json")
				if err := os.WriteFile(userCfgPath, []byte(`{}`), 0o600); err != nil {
					t.Fatalf("write user cfg: %v", err)
				}
				responses["config file"] = openclawResponse{stdout: userCfgPath}
			} else {
				responses["config file"] = openclawResponse{stdout: filepath.Join(t.TempDir(), "absent.json")}
			}
			stub := installOpenclawStub(t, responses)

			result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
				OpenclawBin: stub.bin,
				McpConfig:   tc.mcpConfig,
			})
			if err != nil {
				t.Fatalf("prepareOpenclawConfig: %v", err)
			}
			want := ""
			if tc.wantGrant {
				want = filepath.Dir(userCfgPath)
			}
			if result.IncludeRoot != want {
				t.Errorf("IncludeRoot = %q, want %q", result.IncludeRoot, want)
			}
		})
	}
}

// TestPrepareOpenclawConfigPreservesNonServerMcpKeys — the scope assertion:
// strict replace applies to `mcp.servers` and to nothing else. Siblings under
// `mcp` (`sessionIdleTtlMs`, `apps`, anything a future release adds) must reach
// the agent exactly as the user wrote them.
//
// This used to be enforced by reading the user's resolved `mcp` block and writing
// the non-server keys back, which is where the review found the hazard: `config
// get` returns display data, so a sensitive sibling comes back as
// `__OPENCLAW_REDACTED__` and writing it back would overwrite a working value
// with a placeholder. It also had to enumerate the keys it knew about.
//
// Nothing is read now, so the guarantee is structural and that is what this
// asserts: the reset stage names exactly one key, and no file this package writes
// mentions any sibling at all. The end-to-end half — an actual sibling surviving
// an actual loader — is in openclaw_mcp_real_integration_test.go, which has to
// run against a real CLI to mean anything.
func TestPrepareOpenclawConfigPreservesNonServerMcpKeys(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	userCfgPath := filepath.Join(t.TempDir(), "openclaw.json")
	// Both shapes the sibling problem takes: a plain tuning value, and one whose
	// name would be redacted by `config get`.
	userCfg := `{
		"mcp": {
			"sessionIdleTtlMs": 300000,
			"apps": {"inspector": {"token": "sk-sibling-secret"}},
			"servers": {"global_one": {"command": "/bin/echo"}}
		}
	}`
	if err := os.WriteFile(userCfgPath, []byte(userCfg), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: userCfgPath},
		"config get agents.list --json": {stdout: "null"},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
		OpenclawBin: stub.bin,
		McpConfig:   json.RawMessage(`{"mcpServers": {"managed_only": {"command": "uvx", "args": ["m"]}}}`),
	})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}

	// The reset must name `mcp.servers` and nothing else — a reset of the whole
	// `mcp` object would take the siblings down with the servers.
	reset := mustReadJSON(t, filepath.Join(envRoot, openclawMcpResetFile))
	resetMcp, ok := reset["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("mcp reset = %#v, want an object naming servers", reset)
	}
	if len(resetMcp) != 1 {
		t.Errorf("mcp reset names %d keys (%v), want only servers — anything else resets a user setting", len(resetMcp), resetMcp)
	}
	if value, present := resetMcp["servers"]; !present || value != nil {
		t.Errorf("mcp reset servers = %#v, want explicit null", resetMcp["servers"])
	}

	// The wrapper contributes servers only, so nothing it writes can shadow a
	// sibling either.
	wrapperMcp, _ := mustReadJSON(t, result.ConfigPath)["mcp"].(map[string]any)
	if len(wrapperMcp) != 1 {
		t.Errorf("wrapper mcp block = %#v, want servers only", wrapperMcp)
	}
	servers, _ := wrapperMcp["servers"].(map[string]any)
	if _, ok := servers["managed_only"]; !ok {
		t.Errorf("wrapper missing managed_only: %v", servers)
	}
	if _, leaked := servers["global_one"]; leaked {
		t.Errorf("global_one leaked into wrapper: %v", servers)
	}

	// And no sibling was observed, copied or re-emitted anywhere.
	assertEnvRootCarriesNoUserConfig(t, envRoot, "sessionIdleTtlMs", "apps", "sk-sibling-secret")
}

// TestPrepareOpenclawConfigStrictEmptyManagedSetDropsUserMcp — an empty managed
// set `{}` must drop the user's global mcp.servers too. Without the reset,
// OpenClaw would still resolve user-only servers through the include and the
// admin's "saved no servers" intent would be silently overridden.
func TestPrepareOpenclawConfigStrictEmptyManagedSetDropsUserMcp(t *testing.T) {
	userCfgPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userCfgPath, []byte(`{"mcp":{"servers":{"global_one":{"command":"/bin/echo"}}}}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	cases := map[string]json.RawMessage{
		"object_empty":          json.RawMessage(`{}`),
		"mcp_servers_empty_map": json.RawMessage(`{"mcpServers": {}}`),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			envRoot := t.TempDir()
			workDir := filepath.Join(envRoot, "workdir")
			if err := os.MkdirAll(workDir, 0o755); err != nil {
				t.Fatalf("mkdir workdir: %v", err)
			}
			stub := installOpenclawStub(t, map[string]openclawResponse{
				"config file":                   {stdout: userCfgPath},
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
			// An empty managed set is exactly the case that needs the reset: with
			// no server named on the wrapper, the include result is what resolves
			// unless the user's map has been nulled first.
			resetPath := filepath.Join(envRoot, openclawMcpResetFile)
			include, _ := got["$include"].([]any)
			if len(include) != 2 || include[1] != resetPath {
				t.Fatalf("wrapper $include = %#v, want the reset stage at %q", got["$include"], resetPath)
			}
		})
	}
}

// TestPrepareOpenclawConfigNullMcpConfigKeepsUserInclude — when the agent has no
// managed mcp_config (`null` / absent), the wrapper must include the live user
// config with no reset stage, so the user's global mcp.servers and everything
// else still flow through. This is the "inherit defaults" branch.
func TestPrepareOpenclawConfigNullMcpConfigKeepsUserInclude(t *testing.T) {
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
			envRoot := t.TempDir()
			workDir := filepath.Join(envRoot, "workdir")
			if err := os.MkdirAll(workDir, 0o755); err != nil {
				t.Fatalf("mkdir workdir: %v", err)
			}
			stub := installOpenclawStub(t, map[string]openclawResponse{
				"config file":                   {stdout: userCfgPath},
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
			if _, present := got["mcp"]; present {
				t.Errorf("wrapper has mcp block when mcp_config = %q: %v", name, got["mcp"])
			}
			include, _ := got["$include"].([]any)
			if len(include) != 1 || include[0] != userCfgPath {
				t.Errorf("$include = %v, want the live user config %q alone on the inherit path", got["$include"], userCfgPath)
			}
			// No managed set means nothing to enforce, so the reset stage must not
			// exist: it would null the servers the agent is supposed to inherit.
			if _, err := os.Stat(filepath.Join(envRoot, openclawMcpResetFile)); !os.IsNotExist(err) {
				t.Errorf("inherit path wrote a reset stage (should not): err=%v", err)
			}
			if result.IncludeRoot != userCfgDir {
				t.Errorf("IncludeRoot = %q, want %q (cross-dir hop for the live $include)", result.IncludeRoot, userCfgDir)
			}
		})
	}
}

// TestPrepareOpenclawConfigManagedSetFreshInstall — a managed mcp_config on a
// fresh install (no on-disk user config) has nothing to include and nothing to
// reset: the wrapper carries the managed servers as the sole MCP definition, with
// no $include and no reset stage.
func TestPrepareOpenclawConfigManagedSetFreshInstall(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	missingPath := filepath.Join(t.TempDir(), "openclaw.json")
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {stdout: missingPath},
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
	// A reset with no include to reset would leave `mcp.servers: null` as the
	// only contribution from the chain, which is a shape the wrapper's own block
	// then has to undo for no reason.
	if _, err := os.Stat(filepath.Join(envRoot, openclawMcpResetFile)); !os.IsNotExist(err) {
		t.Errorf("fresh install wrote a reset stage (should not): err=%v", err)
	}
}

// TestPrepareOpenclawConfigManagedMcpCostsNoExtraCLICall pins the removal that
// makes the deadline arithmetic simpler, and it is the test that fails if the
// config read ever comes back.
//
// A managed-MCP agent used to pay one more CLI round-trip than any other — the
// invalid pathless `config get --json`, which is what #7551 reported as a task
// that never starts. The reset stage is a file this package writes, so managed
// MCP now costs no CLI time at all, which is why
// openclawMaxCLIDeadlinesPerPreparation dropped from 4 to 3.
//
// The stub errors on any invocation it was not given, so a reintroduced read
// fails preparation here rather than silently widening the budget.
func TestPrepareOpenclawConfigManagedMcpCostsNoExtraCLICall(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	userCfgPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userCfgPath, []byte(`{"mcp":{"servers":{"user":{"command":"user"}}}}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	discovery := map[string]openclawResponse{
		"config file":                   {stdout: userCfgPath},
		"config get agents.list --json": {stdout: "null"},
	}

	baseline := installOpenclawStub(t, discovery)
	if _, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: baseline.bin}); err != nil {
		t.Fatalf("prepareOpenclawConfig (inherit): %v", err)
	}
	inheritCalls := len(baseline.calls)

	managedEnvRoot := t.TempDir()
	managedWorkDir := filepath.Join(managedEnvRoot, "workdir")
	if err := os.MkdirAll(managedWorkDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	managed := installOpenclawStub(t, discovery)
	if _, err := prepareOpenclawConfig(managedEnvRoot, managedWorkDir, OpenclawConfigPrep{
		OpenclawBin: managed.bin,
		McpConfig:   json.RawMessage(`{"mcpServers":{"managed":{"command":"managed"}}}`),
	}); err != nil {
		t.Fatalf("prepareOpenclawConfig (managed): %v", err)
	}

	var managedArgs []string
	for _, call := range managed.calls {
		managedArgs = append(managedArgs, strings.Join(call.args, " "))
	}
	if len(managed.calls) != inheritCalls {
		t.Errorf("managed MCP made %d CLI calls against %d for the same agent without it: %v",
			len(managed.calls), inheritCalls, managedArgs)
	}
	for _, args := range managedArgs {
		if strings.Contains(args, "mcp") {
			t.Errorf("managed MCP read configuration from the CLI (%q); the reset stage exists so it does not have to", args)
		}
	}
}

// TestPrepareOpenclawConfigResetStagePairsWithWrapperMcp pins the invariant the
// reset stage's safety hangs on: a generated chain that contains
// openclawMcpResetFile must come with an `mcp.servers` object on the wrapper
// itself.
//
// The reset works because OpenClaw's include merge treats a null source as
// replacement (`src/config/includes.ts` at v2026.7.1-2) — but the same loader
// rejects a *surviving* null at validation: `McpConfigSchema` is
// `.strict().optional()` (`src/config/zod-schema.ts`), and zod's `.optional()`
// accepts undefined, never null. The only thing standing between the reset and
// that rejection is the wrapper's own block, which the loader merges over the
// include result.
//
// Today the pairing holds by construction — the reset is written under
// `hasManagedMcp && exists`, and buildPerTaskOpenclawConfig emits `mcp.servers`
// under `hasManagedMcp` — but nothing named that coupling. A refactor that emits
// the reset without the wrapper block (an empty managed set optimized away, say)
// would fail every managed-MCP task at agent start with an `invalid_type` on
// `mcp.servers` pointing nowhere near this package. Fail-closed, but with a
// diagnostic bad enough that this test exists to keep it from ever firing.
func TestPrepareOpenclawConfigResetStagePairsWithWrapperMcp(t *testing.T) {
	cases := []struct {
		name      string
		userCfg   string
		mcpConfig json.RawMessage
	}{
		{
			name:      "user servers only",
			userCfg:   `{"mcp":{"servers":{"user-only":{"command":"user"}}}}`,
			mcpConfig: json.RawMessage(`{"mcpServers":{"managed":{"command":"managed"}}}`),
		},
		{
			name:      "user siblings alongside servers",
			userCfg:   `{"mcp":{"servers":{"user-only":{"command":"user"}},"sessionIdleTtlMs":300000}}`,
			mcpConfig: json.RawMessage(`{"mcpServers":{"managed":{"command":"managed"}}}`),
		},
		{
			// The sharpest case: nothing on the wrapper but an empty map stands
			// between the reset's null and the schema.
			name:      "empty managed set",
			userCfg:   `{"mcp":{"servers":{"user-only":{"command":"user"}}}}`,
			mcpConfig: json.RawMessage(`{"mcpServers":{}}`),
		},
		{
			name:      "user has no mcp key at all",
			userCfg:   `{"gateway":{"mode":"local"}}`,
			mcpConfig: json.RawMessage(`{"mcpServers":{"managed":{"command":"managed"}}}`),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			envRoot := t.TempDir()
			workDir := filepath.Join(envRoot, "workdir")
			if err := os.MkdirAll(workDir, 0o755); err != nil {
				t.Fatalf("mkdir workdir: %v", err)
			}
			userCfgPath := filepath.Join(t.TempDir(), "openclaw.json")
			if err := os.WriteFile(userCfgPath, []byte(tc.userCfg), 0o600); err != nil {
				t.Fatalf("write user cfg: %v", err)
			}
			stub := installOpenclawStub(t, map[string]openclawResponse{
				"config file":                   {stdout: userCfgPath},
				"config get agents.list --json": {stdout: "null"},
			})

			result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
				OpenclawBin: stub.bin,
				McpConfig:   tc.mcpConfig,
			})
			if err != nil {
				t.Fatalf("prepareOpenclawConfig: %v", err)
			}

			// The chain must actually carry the reset stage...
			resetPath := filepath.Join(envRoot, openclawMcpResetFile)
			wrapper := mustReadJSON(t, result.ConfigPath)
			include, ok := wrapper["$include"].([]any)
			if !ok || len(include) != 2 || include[1] != resetPath {
				t.Fatalf("wrapper $include = %#v, want [user config, %q]", wrapper["$include"], resetPath)
			}

			// ...and then the wrapper must carry its own `mcp.servers`. This is
			// what overwrites the chain's transient null before validation;
			// without it the loader rejects the resolved config.
			mcp, ok := wrapper["mcp"].(map[string]any)
			if !ok {
				t.Fatalf("reset stage present but wrapper has no mcp object — the "+
					"resolved chain would keep `mcp.servers: null`, which OpenClaw's "+
					"schema rejects, failing every managed-MCP task at load: %v", wrapper)
			}
			if _, ok := mcp["servers"].(map[string]any); !ok {
				t.Fatalf("wrapper mcp.servers is not an object: %#v", mcp)
			}
		})
	}
}

// TestPrepareOpenclawConfigFallsBackToRegistryOnEnvelopeWithoutExit — the
// completeness rule accepts a JSON error envelope as a finished answer (it is
// valid JSON), so a CLI that prints one and then lingers past the idle grace
// yields err == nil with the envelope in stdout. The missing-key verdict must be
// the same as when the CLI exits non-zero: select the registry rather than
// decoding the envelope as an agent list and failing the whole preparation.
func TestPrepareOpenclawConfigFallsBackToRegistryOnEnvelopeWithoutExit(t *testing.T) {
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
		"config file": {stdout: userCfgPath},
		// Envelope, no error: the CLI answered and then failed to exit.
		"config get agents.list --json": {stdout: `{"error":"Config path not found: agents.list"}`},
		"agents list --json":            {stdout: `[{"id":"scout","workspace":"/old"}]`},
	})

	// Without the envelope check this decodes as an agent list, fails to
	// unmarshal into []any, and takes the whole preparation down.
	if _, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{}); err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	var called []string
	registryConsulted := false
	for _, call := range stub.calls {
		joined := strings.Join(call.args, " ")
		called = append(called, joined)
		if joined == "agents list --json" {
			registryConsulted = true
		}
	}
	if !registryConsulted {
		t.Errorf("registry was never consulted (calls: %v), so the envelope was not recognized", called)
	}
	wrapper, err := os.ReadFile(filepath.Join(envRoot, openclawConfigFile))
	if err != nil {
		t.Fatalf("read wrapper: %v", err)
	}
	// A registry-sourced list is deliberately not written back as
	// `agents.list` (see buildPerTaskOpenclawConfig), but the envelope must not
	// reach the wrapper by any route either.
	if strings.Contains(string(wrapper), "Config path not found") {
		t.Errorf("wrapper %s carries the CLI error envelope", wrapper)
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
		"claude":   "aaaaaaaa-1111-2222-3333-4444444444aa",
		"opencode": "bbbbbbbb-1111-2222-3333-4444444444bb",
		"hermes":   "cccccccc-1111-2222-3333-4444444444cc",
		"kiro":     "dddddddd-1111-2222-3333-4444444444dd",
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

// ── Gateway endpoint pinning (issue #3260) ──
//
// When a multica agent is configured for gateway-mode openclaw and the
// runtime_config carries a Gateway endpoint, the per-task wrapper must pin
// that endpoint in its `gateway` block. OpenClaw deep-merges sibling object
// keys after $include, so the wrapper's `gateway.*` settings override
// whatever the user's global openclaw.json carried.

func TestBuildPerTaskOpenclawConfigOmitsGatewayWhenZero(t *testing.T) {
	t.Parallel()

	cfg := buildPerTaskOpenclawConfig(
		"", false, "", nil, false, "/workdir", nil, false,
		OpenclawGatewayPin{},
	)
	if _, present := cfg["gateway"]; present {
		t.Errorf("zero gateway must not emit a gateway block, got %v", cfg["gateway"])
	}
}

func TestBuildPerTaskOpenclawConfigWritesGatewayBlock(t *testing.T) {
	t.Parallel()

	pin := OpenclawGatewayPin{
		Host:  "gw.internal",
		Port:  18789,
		Token: "secret-token",
		TLS:   true,
	}
	cfg := buildPerTaskOpenclawConfig(
		"", false, "", nil, false, "/workdir", nil, false,
		pin,
	)

	gw, ok := cfg["gateway"].(map[string]any)
	if !ok {
		t.Fatalf("expected gateway map, got %T: %v", cfg["gateway"], cfg["gateway"])
	}
	if gw["host"] != "gw.internal" {
		t.Errorf("gateway.host = %v, want %q", gw["host"], "gw.internal")
	}
	if gw["port"] != 18789 {
		t.Errorf("gateway.port = %v, want %d", gw["port"], 18789)
	}
	// Token nests under gateway.auth.{mode,token} to match OpenClaw's own
	// config shape (see ~/.openclaw/openclaw.json `gateway.auth`).
	auth, ok := gw["auth"].(map[string]any)
	if !ok {
		t.Fatalf("expected gateway.auth map, got %T: %v", gw["auth"], gw["auth"])
	}
	if auth["mode"] != "token" {
		t.Errorf("gateway.auth.mode = %v, want %q", auth["mode"], "token")
	}
	if auth["token"] != "secret-token" {
		t.Errorf("gateway.auth.token = %v, want %q", auth["token"], "secret-token")
	}
	if gw["tls"] != true {
		t.Errorf("gateway.tls = %v, want true", gw["tls"])
	}
}

func TestBuildPerTaskOpenclawConfigPartialGatewayOmitsZeroFields(t *testing.T) {
	t.Parallel()

	// Users may pin only host/port and rely on the user's local openclaw.json
	// for the token (which still flows in via the $include). Zero-valued
	// fields must not land in the wrapper as empty strings/zeros — that
	// would override the user's value with junk.
	cfg := buildPerTaskOpenclawConfig(
		"", false, "", nil, false, "/workdir", nil, false,
		OpenclawGatewayPin{Host: "gw.internal", Port: 18789},
	)
	gw := cfg["gateway"].(map[string]any)
	if _, present := gw["auth"]; present {
		t.Errorf("auth block must be omitted when token is empty, got %v", gw["auth"])
	}
	if _, present := gw["tls"]; present {
		t.Errorf("tls field must be omitted when false, got %v", gw["tls"])
	}
}

// TestIsOpenclawKeyMissing covers the "key not found" wordings the CLI has
// emitted across versions. The 2026.6.x string ("Config path not found:
// agents.list", lowercase "path") is the regression from upstream #3028:
// the matcher used to compare case-sensitively against "Path not found" and
// silently stopped recognizing this, turning the intended graceful-skip
// into a fail-closed error that broke every OpenClaw 2026.6.x runtime.
func TestIsOpenclawKeyMissing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"pre-2026.6 No value at", errors.New("openclaw: No value at agents.list"), true},
		{"pre-2026.6 Path not found", errors.New("openclaw config get agents.list --json: Path not found"), true},
		{"not set", errors.New("agents.list is not set"), true},
		{"missing key", errors.New("missing key: agents.list"), true},
		{
			"2026.6.x Config path not found (verbatim #3028)",
			errors.New("openclaw config get agents.list --json: exit status 1 (stderr: Config path not found: agents.list. Run openclaw config validate to inspect config shape.)"),
			true,
		},
		{"real failure stays an error", errors.New("openclaw: failed to read config: permission denied"), false},
		{"malformed json is not a missing key", errors.New("parse output: invalid character 'x'"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isOpenclawKeyMissing(tc.err); got != tc.want {
				t.Errorf("isOpenclawKeyMissing(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestIsOpenclawKeyMissingResult covers both JSON error contracts observed in
// OpenClaw: 2026.7.2-beta.7 writes a string `error`, while 2026.8.1-beta.3
// writes `error.message` and distinguishes valid-but-unset from unknown paths.
// Only a structured missing-path message for the requested path may trigger a
// fallback; other failures must preserve the preparer's fail-closed posture.
func TestIsOpenclawKeyMissingResult(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		stdout string
		err    error
		path   string
		want   bool
	}{
		{
			name:   "2026.7.2-beta.7 stdout json missing path",
			stdout: `{"error":"Config path not found: agents.list"}`,
			err:    errors.New("openclaw config get agents.list --json: exit status 1"),
			path:   "agents.list",
			want:   true,
		},
		{
			name:   "2026.8.1-beta.3 nested json unknown path",
			stdout: `{"ok":false,"error":{"type":"cli_error","message":"Unknown config path: agents.list. Run openclaw config schema to inspect valid paths."}}`,
			err:    errors.New("openclaw config get agents.list --json: exit status 1"),
			path:   "agents.list",
			want:   true,
		},
		{
			name:   "2026.8.1-beta.3 nested json valid but unset",
			stdout: `{"ok":false,"error":{"type":"cli_error","message":"Config path is valid but unset: mcp. The runtime default applies."}}`,
			err:    errors.New("openclaw config get mcp --json: exit status 1"),
			path:   "mcp",
			want:   true,
		},
		{
			name: "2026.6.x stderr missing path remains supported",
			err:  errors.New("openclaw config get agents.list --json: exit status 1 (stderr: Config path not found: agents.list)"),
			path: "agents.list",
			want: true,
		},
		{
			name:   "other json error stays an error",
			stdout: `{"error":"OpenClaw config is invalid"}`,
			err:    errors.New("openclaw config get agents.list --json: exit status 1"),
			path:   "agents.list",
			want:   false,
		},
		{
			name:   "unrelated not-set json error stays an error",
			stdout: `{"error":"OPENAI_API_KEY is not set"}`,
			err:    errors.New("openclaw config get agents.list --json: exit status 1"),
			path:   "agents.list",
			want:   false,
		},
		{
			name: "unrelated not-set process error stays an error",
			err:  errors.New("openclaw config get agents.list --json: exit status 1 (stderr: OPENAI_API_KEY is not set)"),
			path: "agents.list",
			want: false,
		},
		{
			name: "environment variable suffix resembling path stays an error",
			err:  errors.New("openclaw config get mcp --json: exit status 1 (stderr: OPENCLAW_MCP is not set)"),
			path: "mcp",
			want: false,
		},
		{
			name:   "parent path suffix resembling path stays an error",
			stdout: `{"error":"parent.mcp is not set"}`,
			err:    errors.New("openclaw config get mcp --json: exit status 1"),
			path:   "mcp",
			want:   false,
		},
		{
			name:   "agents-list not-set json error remains compatible",
			stdout: `{"error":"agents.list is not set"}`,
			err:    errors.New("openclaw config get agents.list --json: exit status 1"),
			path:   "agents.list",
			want:   true,
		},
		{
			name:   "different missing path stays an error",
			stdout: `{"error":"Config path not found: agents.list"}`,
			err:    errors.New("openclaw config get mcp --json: exit status 1"),
			path:   "mcp",
			want:   false,
		},
		{
			name:   "longer path sharing a prefix stays an error",
			stdout: `{"error":"Config path not found: mcp.apps"}`,
			err:    errors.New("openclaw config get mcp --json: exit status 1"),
			path:   "mcp",
			want:   false,
		},
		{
			name:   "malformed json stays an error",
			stdout: `{"error":`,
			err:    errors.New("openclaw config get agents.list --json: exit status 1"),
			path:   "agents.list",
			want:   false,
		},
		{
			name:   "successful output is never reclassified",
			stdout: `{"error":"Config path not found: agents.list"}`,
			path:   "agents.list",
			want:   false,
		},
		{
			name:   "timeout remains a timeout",
			stdout: `{"error":"Config path not found: agents.list"}`,
			err:    fmt.Errorf("openclaw config get agents.list --json: %w (stderr: Config path not found: agents.list)", context.DeadlineExceeded),
			path:   "agents.list",
			want:   false,
		},
		{
			name:   "cancellation remains a cancellation",
			stdout: `{"error":"Config path not found: mcp"}`,
			err:    fmt.Errorf("openclaw config get mcp --json: %w", context.Canceled),
			path:   "mcp",
			want:   false,
		},
		// Measured 2026-08-26. `config get` requires a path, so the pathless form
		// the old resolved-root read used is rejected as a usage error on every
		// current channel. It names no path, and must not be read as "the key is
		// absent" — that would turn a broken invocation into a silent "user has no
		// mcp block" and hand the task a wrapper built on a false premise.
		{
			name: "2026.6.34 / 2026.7.1-2 pathless usage error is not a missing key",
			err:  errors.New(`openclaw config get --json: exit status 1 (stderr: Missing required argument "path". Try: openclaw config get --help)`),
			path: "mcp",
			want: false,
		},
		{
			name:   "2026.8.1-beta.3 pathless usage envelope is not a missing key",
			stdout: `{"ok":false,"error":{"type":"cli_error","message":"Missing required argument \"path\".\nTry: openclaw config get --help"}}`,
			err:    errors.New("exit status 1"),
			path:   "mcp",
			want:   false,
		},
		// Also measured: beta.3 reports a schema violation through the same
		// envelope, with the offending key in a sibling `issues` array. An invalid
		// config is the opposite of an absent key — treating it as absent would
		// proceed with a config the loader has already refused.
		{
			name:   "2026.8.1-beta.3 invalid-config envelope is not a missing key",
			stdout: `{"ok":false,"error":{"type":"cli_error","message":"OpenClaw config is invalid: wrapper.json"},"issues":[{"path":"mcp","message":"Unrecognized key: \"sessionIdleTtlMs\""}]}`,
			err:    errors.New("exit status 1"),
			path:   "mcp",
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isOpenclawKeyMissingResult(tc.stdout, tc.err, tc.path); got != tc.want {
				t.Errorf("isOpenclawKeyMissingResult(%q, %v, %q) = %v, want %v", tc.stdout, tc.err, tc.path, got, tc.want)
			}
		})
	}
}

func TestAnnotateOpenclawJSONError(t *testing.T) {
	t.Parallel()
	t.Run("keeps only a normalized error field and the cause", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("exit status 1")
		got := annotateOpenclawJSONError(
			cause,
			`{"error":"  schema\nvalidation failed  ","resolved":{"apiKey":"must-not-leak"}}`,
		)
		if !errors.Is(got, cause) {
			t.Fatalf("annotated error lost its cause: %v", got)
		}
		if !strings.Contains(got.Error(), "json error: schema validation failed") {
			t.Fatalf("annotated error omitted or failed to normalize the diagnostic: %v", got)
		}
		if strings.Contains(got.Error(), "must-not-leak") {
			t.Fatalf("annotated error leaked a sibling JSON field: %v", got)
		}
	})

	t.Run("bounds the diagnostic", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("exit status 1")
		prefix := strings.Repeat("界", openclawJSONErrorMaxRunes)
		got := annotateOpenclawJSONError(
			cause,
			`{"error":"`+prefix+`extra"}`,
		)
		if !strings.Contains(got.Error(), "json error: "+prefix+"…") {
			t.Fatalf("annotated error was not truncated at the rune limit: %v", got)
		}
		if strings.Contains(got.Error(), "extra") {
			t.Fatalf("annotated error exceeded its bound: %v", got)
		}
	})

	t.Run("ignores malformed envelopes", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("exit status 1")
		if got := annotateOpenclawJSONError(cause, `{"error":`); got != cause {
			t.Fatalf("malformed JSON changed the original error: %v", got)
		}
	})
}

// TestPrepareOpenclawConfigNewSchemaOmitsAgentsList — OpenClaw 2026.6.x
// removed the `agents.list` config path and OpenClaw 2026.7.2-beta.7 reports
// that missing path as a JSON error on stdout. In both versions the agents
// live in a sqlite registry reachable via the `openclaw agents list --json`
// subcommand.
//
// The preparer must (a) treat the config-path error as "missing, fall back"
// (read-side, #3028 first half) and (b) NOT write the registry-sourced agents
// back into the wrapper as `agents.list` (write-side, #3028 second half).
// `agents.list` is not a valid 2026.6.x config path — its schema validator
// rejects the registry shape ("agents.list.0: Invalid input") and fails
// closed before the agent runs. Per-task workspace pinning for the new schema
// rides on `agents.defaults.workspace` alone, which OpenClaw applies to the
// agent it selects from the registry.
func TestPrepareOpenclawConfigNewSchemaOmitsAgentsList(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	userConfigPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userConfigPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	// Real registry shape from `openclaw agents list --json` on 2026.6.8 —
	// carries CLI-only fields (identityName, agentDir, bindings, isDefault)
	// that the config schema rejects if written back as agents.list[].
	registry := `[{"id":"main","identityName":"Beau","identitySource":"identity","workspace":"/Users/cob/.openclaw/workspace","agentDir":"/Users/cob/.openclaw/agents/main/agent","model":"anthropic/claude-sonnet-4-6","bindings":0,"isDefault":true}]`
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {stdout: userConfigPath},
		// OpenClaw 2026.7.2-beta.7 writes this JSON error to stdout and
		// leaves stderr empty, so the process error carries no missing-path
		// text of its own (#7130).
		"config get agents.list --json": {
			stdout: `{"error":"Config path not found: agents.list"}`,
			err:    errors.New("openclaw config get agents.list --json: exit status 1"),
		},
		// Registry subcommand returns the real agents.
		"agents list --json": {stdout: registry},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	got := mustReadJSON(t, result.ConfigPath)
	agents := got["agents"].(map[string]any)
	if agents["defaults"].(map[string]any)["workspace"] != workDir {
		t.Errorf("defaults.workspace not pinned to workDir")
	}
	if _, present := agents["list"]; present {
		t.Fatalf("agents.list must be omitted for a registry-sourced (2026.6.x) host — OpenClaw rejects it; got %v", agents["list"])
	}
}

// TestPrepareOpenclawConfigNewSchemaEmptyRegistry — new-schema config-path
// error plus an empty registry (`[]`) is the 2026.6.x equivalent of "no
// agents.list": emit defaults.workspace only, omit agents.list, no error.
func TestPrepareOpenclawConfigNewSchemaEmptyRegistry(t *testing.T) {
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
		"config get agents.list --json": {err: errors.New("Config path not found: agents.list")},
		"agents list --json":            {stdout: "[]"},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	got := mustReadJSON(t, result.ConfigPath)
	agents := got["agents"].(map[string]any)
	if _, present := agents["list"]; present {
		t.Errorf("agents.list should be omitted for empty registry, got %v", agents["list"])
	}
	if agents["defaults"].(map[string]any)["workspace"] != workDir {
		t.Errorf("defaults.workspace not set")
	}
}

// TestExpandOpenclawPathTildeSeparators — `openclaw config file` shortens the
// home prefix using its host OS's separator, so Windows reports
// `~\.openclaw\openclaw.json`. Matching only `~/` left that form unexpanded;
// because `~\...` is not absolute it was then joined onto the daemon's working
// directory, yielding a path that could never exist. The resulting stat miss
// was indistinguishable from a fresh install, so the wrapper dropped the
// user's $include and every task lost its model providers and auth profiles
// (issue #6630).
func TestExpandOpenclawPathTildeSeparators(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("USERPROFILE", fakeHome)

	cases := []struct {
		name string
		in   string
	}{
		{name: "posix separator", in: "~/.openclaw/openclaw.json"},
		{name: "windows separator", in: `~\.openclaw\openclaw.json`},
		{name: "bare tilde", in: "~"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := expandOpenclawPath(tc.in)
			if err != nil {
				t.Fatalf("expandOpenclawPath(%q): %v", tc.in, err)
			}
			if strings.Contains(got, "~") {
				t.Errorf("expandOpenclawPath(%q) = %q, want the tilde expanded (a literal ~ can never stat)", tc.in, got)
			}
			if !strings.HasPrefix(got, fakeHome) {
				t.Errorf("expandOpenclawPath(%q) = %q, want it rooted at the home dir %q", tc.in, got, fakeHome)
			}
		})
	}
}

// TestExpandOpenclawPathOpenclawHome — the same failure as #6630 in a second
// shape. When OPENCLAW_HOME is set, current releases print the variable name
// instead of its value, and an unexpanded `$OPENCLAW_HOME\...` line is not
// absolute, so it lands under the daemon's working directory and stats as
// missing — reported as a fresh install, wrapper without the user's $include.
func TestExpandOpenclawPathOpenclawHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OPENCLAW_HOME", home)

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "dollar Windows separator", in: `$OPENCLAW_HOME\.openclaw\openclaw.json`, want: filepath.Join(home, `.openclaw\openclaw.json`)},
		{name: "braced POSIX separator", in: `${OPENCLAW_HOME}/.openclaw/openclaw.json`, want: filepath.Join(home, ".openclaw", "openclaw.json")},
		{name: "bare variable", in: `$OPENCLAW_HOME`, want: home},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := expandOpenclawPath(tc.in)
			if err != nil {
				t.Fatalf("expandOpenclawPath(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("expandOpenclawPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestExpandOpenclawPathOpenclawHomeUnsetFailsLoudly — the variable form with
// nothing to expand it to must be an error, not a relative-path fallback.
// Falling through would resolve the literal `$OPENCLAW_HOME` segment against
// the daemon's working directory and hand back a confident absolute path to a
// file that cannot exist, which is exactly the silent-fresh-install failure
// this shape causes in the first place.
func TestExpandOpenclawPathOpenclawHomeUnsetFailsLoudly(t *testing.T) {
	t.Setenv("OPENCLAW_HOME", "")

	got, err := expandOpenclawPath(`$OPENCLAW_HOME/.openclaw/openclaw.json`)
	if err == nil {
		t.Fatalf("expandOpenclawPath returned %q, want an error when OPENCLAW_HOME is empty", got)
	}
	if !strings.Contains(err.Error(), "OPENCLAW_HOME") {
		t.Errorf("error %q does not name the variable that could not be expanded", err.Error())
	}
	// The path is what the reader of a daemon log needs in order to act.
	if !strings.Contains(err.Error(), `"$OPENCLAW_HOME/.openclaw/openclaw.json"`) {
		t.Errorf("error %q does not name the path being expanded", err.Error())
	}
}

// TestPrepareOpenclawConfigExpandsOpenclawHome — end-to-end guard, mirroring
// TestPrepareOpenclawConfigExpandsWindowsTilde below: the banner-then-path
// output shape with the variable form must still produce a wrapper that
// $includes the user's config, and the include-root grant that goes with it.
func TestPrepareOpenclawConfigExpandsOpenclawHome(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	openclawHome := t.TempDir()
	t.Setenv("OPENCLAW_HOME", openclawHome)

	// Built with filepath.Join for the reason the tilde test gives: the
	// remainder arrives with the CLI's separators and production normalizes it
	// the same way, so on a non-Windows host this is one oddly-named file.
	wantPath := filepath.Join(openclawHome, `.openclaw\openclaw.json`)
	if err := os.MkdirAll(filepath.Dir(wantPath), 0o755); err != nil {
		t.Fatalf("mkdir user cfg dir: %v", err)
	}
	if err := os.WriteFile(wantPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	banner := "|\no  Config warnings ---------------------------------+\n" +
		"|  - plugins.entries.duckduckgo: plugin not found  |\n" +
		"+--------------------------------------------------+\n" +
		`$OPENCLAW_HOME\.openclaw\openclaw.json` + "\n"
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: banner},
		"config get agents.list --json": {stdout: "null"},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	got := mustReadJSON(t, result.ConfigPath)
	include, ok := got["$include"].([]any)
	if !ok {
		t.Fatalf("wrapper has no $include — the user's models and auth profiles would be lost: %#v", got)
	}
	if include[0] != wantPath {
		t.Errorf("$include[0] = %v, want %q", include[0], wantPath)
	}
	if result.IncludeRoot != filepath.Dir(wantPath) {
		t.Errorf("IncludeRoot = %q, want %q", result.IncludeRoot, filepath.Dir(wantPath))
	}
}

// TestExpandOpenclawPathOpenclawHomeIsItselfATilde — the variable's *value* may
// be a tilde path. Upstream documents that and expands it before computing the
// home `config file` then shortens (`docs/help/environment.md` and
// `src/infra/home-dir.ts:41-47` at `v2026.5.27`), so the printed
// `$OPENCLAW_HOME` stands for `<os-home>/svc` and the daemon has to land on the
// same file. Joining the raw value leaves the `~` embedded, filepath.Abs makes
// that absolute under the daemon's working directory, and the stat miss is once
// again reported as a fresh install — the failure this whole branch removes,
// reached through the branch itself.
func TestExpandOpenclawPathOpenclawHomeIsItselfATilde(t *testing.T) {
	osHome := t.TempDir()
	t.Setenv("HOME", osHome)
	t.Setenv("USERPROFILE", osHome)

	cases := []struct {
		name string
		env  string
		in   string
		want string
	}{
		{
			name: "tilde value, posix remainder",
			env:  "~/svc",
			in:   `$OPENCLAW_HOME/.openclaw/openclaw.json`,
			want: filepath.Join(osHome, "svc", ".openclaw", "openclaw.json"),
		},
		{
			name: "tilde value, windows remainder",
			env:  "~/svc",
			in:   `$OPENCLAW_HOME\.openclaw\openclaw.json`,
			want: filepath.Join(osHome, "svc", `.openclaw\openclaw.json`),
		},
		{
			name: "tilde value with windows separator",
			env:  `~\svc`,
			in:   `$OPENCLAW_HOME/.openclaw/openclaw.json`,
			want: filepath.Join(osHome, "svc", ".openclaw", "openclaw.json"),
		},
		{
			name: "bare tilde value",
			env:  "~",
			in:   `$OPENCLAW_HOME/.openclaw/openclaw.json`,
			want: filepath.Join(osHome, ".openclaw", "openclaw.json"),
		},
		{
			name: "bare variable, tilde value",
			env:  "~/svc",
			in:   `$OPENCLAW_HOME`,
			want: filepath.Join(osHome, "svc"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPENCLAW_HOME", tc.env)
			got, err := expandOpenclawPath(tc.in)
			if err != nil {
				t.Fatalf("expandOpenclawPath(%q) with OPENCLAW_HOME=%q: %v", tc.in, tc.env, err)
			}
			if strings.Contains(got, "~") {
				t.Errorf("expandOpenclawPath(%q) = %q, want the value's `~` expanded (a literal ~ can never stat)", tc.in, got)
			}
			if got != tc.want {
				t.Errorf("expandOpenclawPath(%q) with OPENCLAW_HOME=%q = %q, want %q", tc.in, tc.env, got, tc.want)
			}
		})
	}
}

// TestExpandOpenclawPathLeavesLookalikePrefixesAlone — the risk claim is that no
// path which works today changes, and this is what guards it. A variable whose
// name merely starts with OPENCLAW_HOME must not be treated as the shape.
func TestExpandOpenclawPathLeavesLookalikePrefixesAlone(t *testing.T) {
	t.Setenv("OPENCLAW_HOME", t.TempDir())

	for _, in := range []string{
		`$OPENCLAW_HOMEX/y`,
		`$OPENCLAW_HOME_EXTRA/y`,
		`${OPENCLAW_HOMEX}/y`,
		`$OPENCLAW/y`,
	} {
		t.Run(in, func(t *testing.T) {
			got, err := expandOpenclawPath(in)
			if err != nil {
				t.Fatalf("expandOpenclawPath(%q): %v", in, err)
			}
			// Untouched by the new branch: still resolved as an ordinary
			// relative path, exactly as on `main`.
			want, aerr := filepath.Abs(in)
			if aerr != nil {
				t.Fatalf("filepath.Abs(%q): %v", in, aerr)
			}
			if got != want {
				t.Errorf("expandOpenclawPath(%q) = %q, want it left as a relative path -> %q", in, got, want)
			}
		})
	}
}

// TestPrepareOpenclawConfigExpandsTildeValuedOpenclawHome — the same defect at
// the level that decides what the user actually gets. A unit assertion on
// expandOpenclawPath would not have caught it: the value's `~` survived into a
// path that looks absolute, so only following it through to the wrapper shows
// the `$include` going missing.
func TestPrepareOpenclawConfigExpandsTildeValuedOpenclawHome(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	osHome := t.TempDir()
	t.Setenv("HOME", osHome)
	t.Setenv("USERPROFILE", osHome)
	t.Setenv("OPENCLAW_HOME", "~/svc")

	wantPath := filepath.Join(osHome, "svc", ".openclaw", "openclaw.json")
	if err := os.MkdirAll(filepath.Dir(wantPath), 0o755); err != nil {
		t.Fatalf("mkdir user cfg dir: %v", err)
	}
	if err := os.WriteFile(wantPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: `$OPENCLAW_HOME/.openclaw/openclaw.json` + "\n"},
		"config get agents.list --json": {stdout: "null"},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	got := mustReadJSON(t, result.ConfigPath)
	include, ok := got["$include"].([]any)
	if !ok {
		t.Fatalf("wrapper has no $include — the user's models and auth profiles would be lost: %#v", got)
	}
	if include[0] != wantPath {
		t.Errorf("$include[0] = %v, want %q", include[0], wantPath)
	}
	if result.IncludeRoot != filepath.Dir(wantPath) {
		t.Errorf("IncludeRoot = %q, want %q", result.IncludeRoot, filepath.Dir(wantPath))
	}
}

// TestPrepareOpenclawConfigExpandsWindowsTilde — end-to-end guard for #6630:
// the reporter's exact `openclaw config file` output (a config-warning banner
// followed by a Windows-shortened path) must still produce a wrapper that
// $includes the user's config.
func TestPrepareOpenclawConfigExpandsWindowsTilde(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("USERPROFILE", fakeHome)

	// filepath.Join normalizes the reported remainder to the host separator,
	// so build the expected target the same way the production path does and
	// materialize it. On non-Windows hosts that is a single oddly-named file;
	// the assertion under test is the tilde expansion, not the separator.
	wantPath := filepath.Join(fakeHome, `.openclaw\openclaw.json`)
	if err := os.MkdirAll(filepath.Dir(wantPath), 0o755); err != nil {
		t.Fatalf("mkdir user cfg dir: %v", err)
	}
	if err := os.WriteFile(wantPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	banner := "|\no  Config warnings ---------------------------------+\n" +
		"|  - plugins.entries.duckduckgo: plugin not found  |\n" +
		"+--------------------------------------------------+\n" +
		`~\.openclaw\openclaw.json` + "\n"
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: banner},
		"config get agents.list --json": {stdout: "null"},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	got := mustReadJSON(t, result.ConfigPath)
	include, ok := got["$include"].([]any)
	if !ok {
		t.Fatalf("wrapper has no $include — the user's models and auth profiles would be lost: %#v", got)
	}
	if include[0] != wantPath {
		t.Errorf("$include[0] = %v, want %q", include[0], wantPath)
	}
	if result.IncludeRoot != filepath.Dir(wantPath) {
		t.Errorf("IncludeRoot = %q, want %q", result.IncludeRoot, filepath.Dir(wantPath))
	}
}

// TestPrepareOpenclawConfigWarnsWhenActiveConfigMissing — discovery used to be
// silent, so a wrapper written without $include left no trace in the daemon
// log and could only be diagnosed by reading the generated file. A fresh
// install legitimately lands here too, but the consequence (no user models or
// auth profiles for this task) is worth a warning either way.
func TestPrepareOpenclawConfigWarnsWhenActiveConfigMissing(t *testing.T) {
	envRoot := t.TempDir()
	workDir := t.TempDir()
	missing := filepath.Join(t.TempDir(), "absent", "openclaw.json")

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {stdout: missing + "\n"},
	})

	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin, Logger: logger})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	out := logs.String()
	if !strings.Contains(out, "openclaw active config not found") {
		t.Errorf("missing active config was not warned about; log was:\n%s", out)
	}
	if !strings.Contains(out, "include_target=none") {
		t.Errorf("prepared-config log should record include_target=none; log was:\n%s", out)
	}
	if result.IncludeRoot != "" {
		t.Errorf("IncludeRoot = %q, want empty", result.IncludeRoot)
	}
}
