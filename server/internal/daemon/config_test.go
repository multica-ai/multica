package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestResolveAgentExecutablePath_PreservesDispatchShimName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires elevated privileges on Windows")
	}

	for _, shimName := range []string{"volta-shim", "vp"} {
		t.Run(shimName, func(t *testing.T) {
			managerDir := t.TempDir()
			manager := filepath.Join(managerDir, shimName)
			if err := os.WriteFile(manager, []byte("#!/bin/sh\nprintf '%s\\n' \"${0##*/}\"\n"), 0o755); err != nil {
				t.Fatalf("write dispatcher: %v", err)
			}

			binDir := t.TempDir()
			entrypoint := filepath.Join(binDir, "claude")
			if err := os.Symlink(manager, entrypoint); err != nil {
				t.Fatalf("symlink dispatcher: %v", err)
			}
			t.Setenv("PATH", binDir)

			got, err := resolveAgentExecutablePath("claude")
			if err != nil {
				t.Fatalf("resolveAgentExecutablePath: %v", err)
			}
			realBinDir, err := filepath.EvalSymlinks(binDir)
			if err != nil {
				t.Fatalf("resolve bin directory: %v", err)
			}
			want := filepath.Join(realBinDir, "claude")
			if got != want {
				t.Fatalf("resolved path = %q, want command-preserving entrypoint %q", got, want)
			}
			output, err := exec.Command(got, "--version").CombinedOutput()
			if err != nil {
				t.Fatalf("run resolved entrypoint: %v: %s", err, output)
			}
			if got := strings.TrimSpace(string(output)); got != "claude" {
				t.Fatalf("dispatcher observed command name %q, want claude", got)
			}
		})
	}
}

func TestResolveAgentExecutablePath_CanonicalizesOrdinaryVersionTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires elevated privileges on Windows")
	}

	versionDir := t.TempDir()
	target := filepath.Join(versionDir, "claude-2.1.216")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write versioned executable: %v", err)
	}

	binDir := t.TempDir()
	entrypoint := filepath.Join(binDir, "claude")
	if err := os.Symlink(target, entrypoint); err != nil {
		t.Fatalf("symlink versioned executable: %v", err)
	}
	t.Setenv("PATH", binDir)

	got, err := resolveAgentExecutablePath("claude")
	if err != nil {
		t.Fatalf("resolveAgentExecutablePath: %v", err)
	}
	want, err := filepath.EvalSymlinks(entrypoint)
	if err != nil {
		t.Fatalf("resolve versioned executable: %v", err)
	}
	if got != want {
		t.Fatalf("resolved path = %q, want pinned version target %q", got, want)
	}
}

func TestPatternsFromEnv_DefaultsWhenUnset(t *testing.T) {
	t.Setenv("MULTICA_GC_ARTIFACT_PATTERNS", "")
	defaults := []string{"node_modules", ".next", ".turbo"}
	got := patternsFromEnv("MULTICA_GC_ARTIFACT_PATTERNS", defaults)
	if !reflect.DeepEqual(got, defaults) {
		t.Fatalf("expected defaults %v, got %v", defaults, got)
	}
	// Ensure callers get a copy, not a shared backing array.
	got[0] = "mutated"
	if defaults[0] == "mutated" {
		t.Fatal("patternsFromEnv must not return a slice aliased with defaults")
	}
}

func TestDefaultGCIntervalIsTwoHours(t *testing.T) {
	if DefaultGCInterval != 2*time.Hour {
		t.Fatalf("DefaultGCInterval = %s, want 2h", DefaultGCInterval)
	}
}

// A localhost server URL is not the official cloud host, so this exercises the
// self-host branch of defaultGCCompletedTaskTTL: retention stays unbounded until
// an operator opts in, and a daemon upgrade never starts deleting on its own.
func TestLoadConfig_CompletedTaskTTLDefaultsDisabledOnSelfHostAndReadsEnv(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "missing-shell"))
	t.Setenv("MULTICA_GC_COMPLETED_TASK_TTL", "")

	overrides := Overrides{
		ServerURL:      "http://localhost:0",
		WorkspacesRoot: t.TempDir(),
	}
	cfg, err := LoadConfig(overrides)
	if err != nil {
		t.Fatalf("LoadConfig with default completed-task TTL: %v", err)
	}
	if cfg.GCCompletedTaskTTL != 0 {
		t.Fatalf("GCCompletedTaskTTL = %s, want disabled", cfg.GCCompletedTaskTTL)
	}

	t.Setenv("MULTICA_GC_COMPLETED_TASK_TTL", "36h")
	cfg, err = LoadConfig(overrides)
	if err != nil {
		t.Fatalf("LoadConfig with completed-task TTL: %v", err)
	}
	if cfg.GCCompletedTaskTTL != 36*time.Hour {
		t.Fatalf("GCCompletedTaskTTL = %s, want 36h", cfg.GCCompletedTaskTTL)
	}

	t.Setenv("MULTICA_GC_COMPLETED_TASK_TTL", "not-a-duration")
	if _, err := LoadConfig(overrides); err == nil || !strings.Contains(err.Error(), "MULTICA_GC_COMPLETED_TASK_TTL") {
		t.Fatalf("LoadConfig invalid completed-task TTL error = %v, want named validation error", err)
	}
}

func TestLoadConfig_CompletedTaskTTLDefaultsBoundedOnOfficialCloud(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "missing-shell"))
	t.Setenv("MULTICA_GC_COMPLETED_TASK_TTL", "")

	overrides := Overrides{
		ServerURL:      "https://" + officialCloudHost,
		WorkspacesRoot: t.TempDir(),
	}
	cfg, err := LoadConfig(overrides)
	if err != nil {
		t.Fatalf("LoadConfig on official cloud: %v", err)
	}
	if cfg.GCCompletedTaskTTL != 14*24*time.Hour {
		t.Fatalf("GCCompletedTaskTTL = %s, want 14d on official cloud", cfg.GCCompletedTaskTTL)
	}

	// An explicit 0 has to win on cloud too — otherwise the only way back to the
	// previous retention behavior would be downgrading the daemon.
	t.Setenv("MULTICA_GC_COMPLETED_TASK_TTL", "0")
	cfg, err = LoadConfig(overrides)
	if err != nil {
		t.Fatalf("LoadConfig with cloud opt-out: %v", err)
	}
	if cfg.GCCompletedTaskTTL != 0 {
		t.Fatalf("GCCompletedTaskTTL = %s, want an explicit 0 to disable the cloud default", cfg.GCCompletedTaskTTL)
	}

	t.Setenv("MULTICA_GC_COMPLETED_TASK_TTL", "36h")
	cfg, err = LoadConfig(overrides)
	if err != nil {
		t.Fatalf("LoadConfig with cloud override: %v", err)
	}
	if cfg.GCCompletedTaskTTL != 36*time.Hour {
		t.Fatalf("GCCompletedTaskTTL = %s, want the env override to win on cloud", cfg.GCCompletedTaskTTL)
	}
}

func TestDefaultGCCompletedTaskTTLOnlyBoundsOfficialCloudHost(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		serverURL string
		want      time.Duration
	}{
		{"official cloud", "https://api.multica.ai", DefaultGCCompletedTaskTTLCloud},
		{"official cloud with port and path", "https://API.Multica.AI:443/api", DefaultGCCompletedTaskTTLCloud},
		// Staging and previews inherit the self-host value for the same reason
		// officialCloudHost excludes them from the auto-update default.
		{"staging", "https://api-staging.multica.ai", DefaultGCCompletedTaskTTLSelfHost},
		{"self-host", "https://multica.example.com", DefaultGCCompletedTaskTTLSelfHost},
		{"localhost", "http://localhost:8080", DefaultGCCompletedTaskTTLSelfHost},
		{"unparseable", "://nope", DefaultGCCompletedTaskTTLSelfHost},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := defaultGCCompletedTaskTTL(tc.serverURL); got != tc.want {
				t.Fatalf("defaultGCCompletedTaskTTL(%q) = %s, want %s", tc.serverURL, got, tc.want)
			}
		})
	}
}

func TestRepoMaintenanceKillSwitchDefaultsOnAndCanDisable(t *testing.T) {
	t.Setenv("MULTICA_GC_REPO_MAINTENANCE_ENABLED", "")
	if !boolFromEnv("MULTICA_GC_REPO_MAINTENANCE_ENABLED", true) {
		t.Fatal("repo maintenance kill switch should default to enabled")
	}

	t.Setenv("MULTICA_GC_REPO_MAINTENANCE_ENABLED", "false")
	if boolFromEnv("MULTICA_GC_REPO_MAINTENANCE_ENABLED", true) {
		t.Fatal("repo maintenance kill switch should accept false")
	}
}

func TestPatternsFromEnv_DropsSeparatorBearingEntries(t *testing.T) {
	t.Setenv("MULTICA_GC_ARTIFACT_PATTERNS", "node_modules, .next ,foo/bar, ../etc, ,target")
	got := patternsFromEnv("MULTICA_GC_ARTIFACT_PATTERNS", nil)
	want := []string{"node_modules", ".next", "target"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestIsSafeAgentName(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"claude", true},
		{"cursor-agent", true},
		{"kiro_cli", true},
		{"v1.2", true},
		{"Claude2", true},
		{"", false},
		{"a b", false},
		{"a/b", false},
		{"a;b", false},
		{"a$b", false},
		{"a`b", false},
		{"a'b", false},
		{`a"b`, false},
	} {
		if got := isSafeAgentName(tc.in); got != tc.want {
			t.Errorf("isSafeAgentName(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestBuildLoginShellResolveScript_ShapeAndContent(t *testing.T) {
	got := buildLoginShellResolveScript([]string{"claude", "cursor-agent"})
	// Must list exactly the names we asked for, in order.
	if !strings.Contains(got, "for n in claude cursor-agent;") {
		t.Errorf("script missing expected for-loop header:\n%s", got)
	}
	// Must strip aliases AND functions before `command -v` — otherwise
	// `alias claude=...` in .zshrc shadows the real binary, which is the
	// exact case behind #2512. The order matters (unalias/unset -f BEFORE
	// command -v); we assert by relative position.
	idxUnalias := strings.Index(got, `unalias "$n" 2>/dev/null`)
	idxUnsetFn := strings.Index(got, `unset -f "$n" 2>/dev/null`)
	idxLookup := strings.Index(got, `command -v "$n"`)
	if idxUnalias < 0 || idxUnsetFn < 0 || idxLookup < 0 {
		t.Fatalf("script missing unalias/unset -f/command -v steps:\n%s", got)
	}
	if !(idxUnalias < idxLookup && idxUnsetFn < idxLookup) {
		t.Errorf("unalias/unset -f must precede command -v:\n%s", got)
	}
	// Must canonicalise via `cd ... && pwd -P` to break out of symlinked
	// per-shell prefix dirs (fnm/nvm/volta) before the spawned shell exits.
	if !strings.Contains(got, "pwd -P") {
		t.Errorf("script missing pwd -P canonicalisation:\n%s", got)
	}
	// Output must be tab-separated `<name>\t<path>` so the parser can split.
	if !strings.Contains(got, `printf '%s\t%s\n'`) {
		t.Errorf("script missing tab-separated printf:\n%s", got)
	}
}

// TestResolveAgentsViaLoginShell_ResolvesViaInteractiveShell verifies the
// motivating bug scenario: a binary that lives in a directory which is NOT on
// the daemon's PATH but IS added to PATH by the user's interactive shell rc
// file gets resolved to a canonical absolute path.
//
// We simulate this by:
//   - creating a temp dir containing an executable named "fakeclaude"
//   - removing every other dir from PATH (so exec.LookPath misses)
//   - pointing SHELL at /bin/sh and using ENV (sourced on -i) to add the dir
//
// Skipped on Windows (no POSIX shell), and skipped if /bin/sh is missing or
// doesn't honour ENV (which would defeat the simulation — not the function's
// fault).
func TestResolveAgentsViaLoginShell_ResolvesViaInteractiveShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell not available on Windows")
	}
	sh := "/bin/sh"
	if _, err := os.Stat(sh); err != nil {
		t.Skipf("no /bin/sh available: %v", err)
	}

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "fakeclaude")
	// A trivially executable script. We only need it to exist and be
	// marked +x; the resolver never runs it.
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	// Prove the precondition: with binDir absent from PATH, the daemon
	// would normally miss this binary.
	t.Setenv("PATH", "/usr/bin:/bin")
	if _, err := lookPathInPath("fakeclaude"); err == nil {
		t.Skip("PATH leak — test environment already exposes fakeclaude without shell help")
	}

	// Wire the interactive shell to add binDir to PATH on startup. POSIX
	// sh reads $ENV when invoked with -i, so we write a tiny rc file that
	// prepends binDir.
	rc := filepath.Join(t.TempDir(), "sh.rc")
	if err := os.WriteFile(rc, []byte("export PATH=\""+binDir+":$PATH\"\n"), 0o644); err != nil {
		t.Fatalf("write rc: %v", err)
	}
	t.Setenv("SHELL", sh)
	t.Setenv("ENV", rc)

	got := resolveAgentsViaLoginShell([]string{"fakeclaude", "kiro-cli"})
	resolved, ok := got["fakeclaude"]
	if !ok {
		t.Fatalf("expected fakeclaude in resolved map, got %v", got)
	}
	// Must be an absolute path, must exist, must point at our fake binary
	// (resolving any symlinks t.TempDir may have introduced — macOS's
	// /var → /private/var symlink is the usual culprit).
	if !filepath.IsAbs(resolved) {
		t.Errorf("expected absolute path, got %q", resolved)
	}
	wantCanonical, err := filepath.EvalSymlinks(binPath)
	if err != nil {
		t.Fatalf("eval symlinks for expected path: %v", err)
	}
	if resolved != wantCanonical {
		t.Errorf("resolved = %q, want canonical %q", resolved, wantCanonical)
	}
}

func TestResolveAgentsViaLoginShell_SkipsUnsupportedShell(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/fish")
	got := resolveAgentsViaLoginShell([]string{"claude"})
	if len(got) != 0 {
		t.Errorf("expected empty map for unsupported shell, got %v", got)
	}
}

func TestResolveAgentsViaLoginShell_EmptyShellNoCrash(t *testing.T) {
	t.Setenv("SHELL", "")
	got := resolveAgentsViaLoginShell([]string{"claude"})
	if len(got) != 0 {
		t.Errorf("expected empty map when SHELL unset, got %v", got)
	}
}

func TestResolveAgentsViaLoginShell_EmptyInput(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	got := resolveAgentsViaLoginShell(nil)
	if len(got) != 0 {
		t.Errorf("expected empty map for nil input, got %v", got)
	}
}

// lookPathInPath is a thin wrapper used by the test above; matches what
// exec.LookPath would do but lets the test be explicit about which call it's
// asserting against.
func lookPathInPath(name string) (string, error) {
	return exec.LookPath(name)
}

func TestIsOfficialCloudServer(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		want bool
	}{
		{"canonical cloud https", "https://api.multica.ai", true},
		{"canonical cloud with trailing slash stripped", "https://api.multica.ai/", true},
		{"canonical cloud case-insensitive", "https://API.Multica.AI", true},
		{"cloud over plain http (unusual but match host)", "http://api.multica.ai", true},
		{"localhost is self-host", "http://localhost:8080", false},
		{"loopback ip is self-host", "http://127.0.0.1:8080", false},
		{"lan ip is self-host", "http://192.168.0.28:8080", false},
		{"third-party host is self-host", "https://multica.example.com", false},
		// Staging / preview / future subdomains deliberately follow the
		// safer self-host default until explicitly opted in.
		{"multica.ai apex is not the api host", "https://multica.ai", false},
		{"staging subdomain is self-host", "https://staging.multica.ai", false},
		{"preview subdomain is self-host", "https://api-preview.multica.ai", false},
		// Malformed inputs must not falsely match.
		{"empty string is self-host", "", false},
		{"garbage string is self-host", "::not a url::", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isOfficialCloudServer(tc.url); got != tc.want {
				t.Errorf("isOfficialCloudServer(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

// stageFakeAgent writes an executable `claude` script into a temp dir and
// points PATH (and the daemon-id env var) so LoadConfig can run end-to-end
// without poking the host's real agent installation. Returns the staged PATH
// so tests that need to add their own dirs can extend it.
func stageFakeAgent(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell not available on Windows")
	}
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "claude")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("MULTICA_DAEMON_ID", "11111111-1111-1111-1111-111111111111")
	// Clear any inherited env-var override so the test sees the URL-based
	// default, not whatever the developer happens to have exported.
	t.Setenv("MULTICA_DAEMON_AUTO_UPDATE", "")
	return binDir
}

func TestLoadConfig_DiscoversQwenCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture is unavailable on Windows")
	}
	binDir := stageFakeAgent(t)
	qwen := filepath.Join(binDir, "qwen")
	if err := os.WriteFile(qwen, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake qwen: %v", err)
	}
	// Avoid consulting an inherited interactive shell for all deliberately
	// absent providers; this test is about ordinary PATH discovery.
	t.Setenv("SHELL", "/usr/bin/fish")
	t.Setenv("MULTICA_QWEN_MODEL", "qwen3.8-max-preview")
	t.Setenv("MULTICA_QWEN_ARGS", "--verbose --foo=bar")

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:0",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	entry, ok := cfg.Agents["qwen"]
	if !ok {
		t.Fatalf("qwen was not discovered: %v", cfg.Agents)
	}
	wantPath, err := filepath.EvalSymlinks(qwen)
	if err != nil {
		t.Fatalf("eval symlinks for qwen: %v", err)
	}
	if entry.Path != wantPath || entry.Command != "qwen" || entry.Model != "qwen3.8-max-preview" {
		t.Fatalf("qwen entry = %+v, want path=%q command=qwen model=qwen3.8-max-preview", entry, wantPath)
	}
	if got, want := strings.Join(cfg.QwenArgs, " "), "--verbose --foo=bar"; got != want {
		t.Fatalf("QwenArgs = %q, want %q", got, want)
	}
}

func TestLoadConfig_SkipsMulticaHooksShadowingAgentBinaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell not available on Windows")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	hooksDir := filepath.Join(home, ".multica", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("create hooks dir: %v", err)
	}
	realBinDir := t.TempDir()

	for _, name := range []string{"claude", "codex", "hermes"} {
		hookPath := filepath.Join(hooksDir, name)
		hookBody := "#!/bin/sh\nexec " + name + " \"$@\"\n"
		if err := os.WriteFile(hookPath, []byte(hookBody), 0o755); err != nil {
			t.Fatalf("write hook wrapper %s: %v", name, err)
		}
		realPath := filepath.Join(realBinDir, name)
		if err := os.WriteFile(realPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write real binary %s: %v", name, err)
		}
	}

	t.Setenv("PATH", hooksDir+string(os.PathListSeparator)+realBinDir)
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))
	t.Setenv("MULTICA_DAEMON_ID", "11111111-1111-1111-1111-111111111111")

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:0",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	for provider, binary := range map[string]string{
		"claude": "claude",
		"codex":  "codex",
		"hermes": "hermes",
	} {
		got, ok := cfg.Agents[provider]
		if !ok {
			t.Fatalf("expected %s agent in config, got %#v", provider, cfg.Agents)
		}
		want := canonicalExecutablePath(filepath.Join(realBinDir, binary))
		if got.Path != want {
			t.Errorf("%s path = %q, want unshadowed real binary %q", provider, got.Path, want)
		}
		if strings.HasPrefix(got.Path, hooksDir) {
			t.Errorf("%s path still points into hooks dir: %q", provider, got.Path)
		}
	}
}

func TestLoadConfig_SkipsMulticaHooksFromLoginShellFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell not available on Windows")
	}
	sh := "/bin/sh"
	if _, err := os.Stat(sh); err != nil {
		t.Skipf("no /bin/sh available: %v", err)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	hooksDir := filepath.Join(home, ".multica", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("create hooks dir: %v", err)
	}
	realBinDir := t.TempDir()
	hookPath := filepath.Join(hooksDir, "codex")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\nexec codex \"$@\"\n"), 0o755); err != nil {
		t.Fatalf("write hook wrapper: %v", err)
	}
	realPath := filepath.Join(realBinDir, "codex")
	if err := os.WriteFile(realPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write real codex: %v", err)
	}

	t.Setenv("PATH", "/usr/bin:/bin")
	if _, err := exec.LookPath("codex"); err == nil {
		t.Skip("PATH leak - codex already visible to daemon without shell fallback")
	}
	rc := filepath.Join(t.TempDir(), "sh.rc")
	rcBody := "export PATH=\"" + hooksDir + string(os.PathListSeparator) + realBinDir + ":$PATH\"\n"
	if err := os.WriteFile(rc, []byte(rcBody), 0o644); err != nil {
		t.Fatalf("write shell rc: %v", err)
	}
	t.Setenv("SHELL", sh)
	t.Setenv("ENV", rc)
	t.Setenv("MULTICA_DAEMON_ID", "11111111-1111-1111-1111-111111111111")
	pinNonCodexAgentsToMissingPaths(t)
	oldBundlePaths := codexDesktopAppBundlePaths
	codexDesktopAppBundlePaths = func() []string { return nil }
	t.Cleanup(func() { codexDesktopAppBundlePaths = oldBundlePaths })

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:0",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	got, ok := cfg.Agents["codex"]
	if !ok {
		t.Fatalf("expected codex from login-shell fallback, got %#v", cfg.Agents)
	}
	want := canonicalExecutablePath(realPath)
	if got.Path != want {
		t.Fatalf("codex path = %q, want unshadowed real binary %q", got.Path, want)
	}
	if strings.HasPrefix(got.Path, hooksDir) {
		t.Fatalf("codex path still points into hooks dir: %q", got.Path)
	}
}

// TestLoadConfig_AutoUpdateDefault_SelfHostOff is the regression guard for
// MUL-2381: a daemon pointed at any non-cloud server URL must default
// AutoUpdateEnabled to false, because self-host operators frequently run a
// fork and the upstream GitHub release would silently overwrite it.
func TestLoadConfig_AutoUpdateDefault_SelfHostOff(t *testing.T) {
	stageFakeAgent(t)
	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AutoUpdateEnabled {
		t.Fatalf("AutoUpdateEnabled = true for self-host (localhost) server, want false")
	}
}

func TestLoadConfig_CodexHandshakeTimeout(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("MULTICA_CODEX_HANDSHAKE_TIMEOUT", "")

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with default: %v", err)
	}
	if cfg.CodexHandshakeTimeout != DefaultCodexHandshakeTimeout {
		t.Fatalf("CodexHandshakeTimeout = %s, want default %s", cfg.CodexHandshakeTimeout, DefaultCodexHandshakeTimeout)
	}

	t.Setenv("MULTICA_CODEX_HANDSHAKE_TIMEOUT", "47s")

	cfg, err = LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with env: %v", err)
	}
	if cfg.CodexHandshakeTimeout != 47*time.Second {
		t.Fatalf("CodexHandshakeTimeout = %s, want 47s from env", cfg.CodexHandshakeTimeout)
	}

	t.Setenv("MULTICA_CODEX_HANDSHAKE_TIMEOUT", "0")
	cfg, err = LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with zero env: %v", err)
	}
	if cfg.CodexHandshakeTimeout != DefaultCodexHandshakeTimeout {
		t.Fatalf("CodexHandshakeTimeout = %s, want default %s for zero env", cfg.CodexHandshakeTimeout, DefaultCodexHandshakeTimeout)
	}

	cfg, err = LoadConfig(Overrides{
		ServerURL:             "http://localhost:8080",
		WorkspacesRoot:        t.TempDir(),
		CodexHandshakeTimeout: 12 * time.Second,
	})
	if err != nil {
		t.Fatalf("LoadConfig with override: %v", err)
	}
	if cfg.CodexHandshakeTimeout != 12*time.Second {
		t.Fatalf("CodexHandshakeTimeout = %s, want 12s from override", cfg.CodexHandshakeTimeout)
	}
}

// TestLoadConfig_CodexFirstTurnNoProgressTimeout pins the env-only
// MULTICA_CODEX_FIRST_TURN_TIMEOUT resolution (GH #3262 / #5959): unset and an
// explicit "0" both mean "keep the backend default" (0 = unset), while a positive
// value is honored verbatim. There is deliberately no Overrides/CLI parity — this
// knob is environment-only.
func TestLoadConfig_CodexFirstTurnNoProgressTimeout(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("MULTICA_CODEX_FIRST_TURN_TIMEOUT", "")

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with unset: %v", err)
	}
	if cfg.CodexFirstTurnNoProgressTimeout != 0 {
		t.Fatalf("CodexFirstTurnNoProgressTimeout = %s, want 0 when unset", cfg.CodexFirstTurnNoProgressTimeout)
	}

	t.Setenv("MULTICA_CODEX_FIRST_TURN_TIMEOUT", "30m")
	cfg, err = LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with env: %v", err)
	}
	if cfg.CodexFirstTurnNoProgressTimeout != 30*time.Minute {
		t.Fatalf("CodexFirstTurnNoProgressTimeout = %s, want 30m from env", cfg.CodexFirstTurnNoProgressTimeout)
	}

	t.Setenv("MULTICA_CODEX_FIRST_TURN_TIMEOUT", "0")
	cfg, err = LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with zero env: %v", err)
	}
	if cfg.CodexFirstTurnNoProgressTimeout != 0 {
		t.Fatalf("CodexFirstTurnNoProgressTimeout = %s, want 0 for explicit zero", cfg.CodexFirstTurnNoProgressTimeout)
	}
}

// TestLoadConfig_CodexFirstTurnTimeoutEqualToSemanticWarns pins the equality
// edge (multica-eve review on #6753): because the semantic-inactivity timer is
// armed before the first-turn timer, equal durations still let the semantic
// deadline win and drop the #3291 startup retry. LoadConfig must warn when the
// first-turn timeout is >= the semantic timeout, and stay quiet only when the
// semantic timeout is strictly greater.
func TestLoadConfig_CodexFirstTurnTimeoutEqualToSemanticWarns(t *testing.T) {
	stageFakeAgent(t)

	const warnNeedle = "MULTICA_CODEX_FIRST_TURN_TIMEOUT is greater than or equal to the semantic-inactivity timeout"

	loadWithLoggedWarnings := func(t *testing.T, semantic, firstTurn string) string {
		t.Helper()
		var buf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
		t.Cleanup(func() { slog.SetDefault(prev) })

		t.Setenv("MULTICA_CODEX_SEMANTIC_INACTIVITY_TIMEOUT", semantic)
		t.Setenv("MULTICA_CODEX_FIRST_TURN_TIMEOUT", firstTurn)
		if _, err := LoadConfig(Overrides{
			ServerURL:      "http://localhost:8080",
			WorkspacesRoot: t.TempDir(),
		}); err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		return buf.String()
	}

	// Equal durations: the semantic timer is armed first, so the retry can still
	// be lost. The warning MUST fire — this is the edge the earlier `>`-only
	// check missed.
	if logs := loadWithLoggedWarnings(t, "15m", "15m"); !strings.Contains(logs, warnNeedle) {
		t.Fatalf("equal durations must warn; logs = %q", logs)
	}

	// First-turn strictly above semantic: also warns (the original truncation case).
	if logs := loadWithLoggedWarnings(t, "10m", "30m"); !strings.Contains(logs, warnNeedle) {
		t.Fatalf("first-turn above semantic must warn; logs = %q", logs)
	}

	// Semantic strictly above first-turn: the recommended safe configuration —
	// no warning.
	if logs := loadWithLoggedWarnings(t, "30m", "10m"); strings.Contains(logs, warnNeedle) {
		t.Fatalf("semantic strictly above first-turn must not warn; logs = %q", logs)
	}
}

// TestLoadConfig_ToolWatchdogDefaultsToIdleWatchdog pins the merge: the
// in-flight-tool budget is no longer an independent constant, so an operator
// who raises the idle budget cannot accidentally leave tool calls capped at a
// lower, invisible ceiling.
func TestLoadConfig_ToolWatchdogDefaultsToIdleWatchdog(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("MULTICA_AGENT_IDLE_WATCHDOG", "")
	t.Setenv("MULTICA_AGENT_TOOL_WATCHDOG", "")

	load := func(t *testing.T) Config {
		t.Helper()
		cfg, err := LoadConfig(Overrides{
			ServerURL:      "http://localhost:8080",
			WorkspacesRoot: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		return cfg
	}

	cfg := load(t)
	if cfg.AgentIdleWatchdog != DefaultAgentIdleWatchdog {
		t.Fatalf("AgentIdleWatchdog = %s, want default %s", cfg.AgentIdleWatchdog, DefaultAgentIdleWatchdog)
	}
	if cfg.AgentToolWatchdog != cfg.AgentIdleWatchdog {
		t.Fatalf("AgentToolWatchdog = %s, want it to track AgentIdleWatchdog %s", cfg.AgentToolWatchdog, cfg.AgentIdleWatchdog)
	}

	// Raising only the idle budget must carry the tool budget with it.
	t.Setenv("MULTICA_AGENT_IDLE_WATCHDOG", "6h")
	cfg = load(t)
	if cfg.AgentIdleWatchdog != 6*time.Hour || cfg.AgentToolWatchdog != 6*time.Hour {
		t.Fatalf("idle=%s tool=%s, want both 6h", cfg.AgentIdleWatchdog, cfg.AgentToolWatchdog)
	}

	// An explicit tool override still wins, so "tools may run longer than the
	// model may think" stays expressible.
	t.Setenv("MULTICA_AGENT_TOOL_WATCHDOG", "12h")
	cfg = load(t)
	if cfg.AgentIdleWatchdog != 6*time.Hour {
		t.Fatalf("AgentIdleWatchdog = %s, want 6h", cfg.AgentIdleWatchdog)
	}
	if cfg.AgentToolWatchdog != 12*time.Hour {
		t.Fatalf("AgentToolWatchdog = %s, want 12h from env", cfg.AgentToolWatchdog)
	}

	// Zero keeps its distinct meaning: never force-stop while a tool is in
	// flight. It must NOT be re-derived from the idle budget.
	t.Setenv("MULTICA_AGENT_TOOL_WATCHDOG", "0")
	cfg = load(t)
	if cfg.AgentToolWatchdog != 0 {
		t.Fatalf("AgentToolWatchdog = %s, want 0 from env", cfg.AgentToolWatchdog)
	}

	// Disabling the suite disables both.
	t.Setenv("MULTICA_AGENT_IDLE_WATCHDOG", "0")
	t.Setenv("MULTICA_AGENT_TOOL_WATCHDOG", "")
	cfg = load(t)
	if cfg.AgentIdleWatchdog != 0 || cfg.AgentToolWatchdog != 0 {
		t.Fatalf("idle=%s tool=%s, want both 0", cfg.AgentIdleWatchdog, cfg.AgentToolWatchdog)
	}
}

func TestLoadConfig_OpenCodeIdleWatchdog(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("MULTICA_OPENCODE_IDLE_WATCHDOG", "")

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with default: %v", err)
	}
	if cfg.OpenCodeIdleWatchdog != DefaultOpenCodeIdleWatchdog {
		t.Fatalf("OpenCodeIdleWatchdog = %s, want default %s", cfg.OpenCodeIdleWatchdog, DefaultOpenCodeIdleWatchdog)
	}

	t.Setenv("MULTICA_OPENCODE_IDLE_WATCHDOG", "7m")
	cfg, err = LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with env: %v", err)
	}
	if cfg.OpenCodeIdleWatchdog != 7*time.Minute {
		t.Fatalf("OpenCodeIdleWatchdog = %s, want 7m from env", cfg.OpenCodeIdleWatchdog)
	}

	// Zero disables the OpenCode-specific override while leaving the generic
	// AgentIdleWatchdog as the fallback for OpenCode runs.
	t.Setenv("MULTICA_OPENCODE_IDLE_WATCHDOG", "0")
	cfg, err = LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with zero env: %v", err)
	}
	if cfg.OpenCodeIdleWatchdog != 0 {
		t.Fatalf("OpenCodeIdleWatchdog = %s, want zero from env", cfg.OpenCodeIdleWatchdog)
	}
}

// TestLoadConfig_AutoUpdateDefault_CloudOn confirms the symmetric case: a
// daemon pointed at Multica's hosted cloud keeps the historical opt-in
// auto-update default. We pass the WSS form of the URL to also exercise that
// NormalizeServerBaseURL maps it through to the http host the detector
// inspects.
func TestLoadConfig_AutoUpdateDefault_CloudOn(t *testing.T) {
	stageFakeAgent(t)
	cfg, err := LoadConfig(Overrides{
		ServerURL:      "wss://api.multica.ai/ws",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.AutoUpdateEnabled {
		t.Fatalf("AutoUpdateEnabled = false for Multica Cloud server, want true")
	}
}

// TestLoadConfig_AutoUpdateEnv_ForcesOnForSelfHost lets a self-host operator
// re-enable auto-update via env var, overriding the new conservative default.
func TestLoadConfig_AutoUpdateEnv_ForcesOnForSelfHost(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("MULTICA_DAEMON_AUTO_UPDATE", "true")
	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.AutoUpdateEnabled {
		t.Fatalf("AutoUpdateEnabled = false after explicit MULTICA_DAEMON_AUTO_UPDATE=true, want true")
	}
}

// TestLoadConfig_AutoUpdateEnv_ForcesOffForCloud covers the inverse: a cloud
// user can still opt out via env var.
func TestLoadConfig_AutoUpdateEnv_ForcesOffForCloud(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("MULTICA_DAEMON_AUTO_UPDATE", "false")
	cfg, err := LoadConfig(Overrides{
		ServerURL:      "https://api.multica.ai",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AutoUpdateEnabled {
		t.Fatalf("AutoUpdateEnabled = true after explicit MULTICA_DAEMON_AUTO_UPDATE=false, want false")
	}
}

// TestLoadConfig_AutoUpdate_NoFlagWinsOverCloudDefault keeps the legacy CLI
// flag working: --no-auto-update (translated into overrides.DisableAutoUpdate)
// forces auto-update off even when the cloud default and env var would enable.
func TestLoadConfig_AutoUpdate_NoFlagWinsOverCloudDefault(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("MULTICA_DAEMON_AUTO_UPDATE", "true")
	cfg, err := LoadConfig(Overrides{
		ServerURL:         "https://api.multica.ai",
		WorkspacesRoot:    t.TempDir(),
		DisableAutoUpdate: true,
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AutoUpdateEnabled {
		t.Fatalf("AutoUpdateEnabled = true with --no-auto-update set; flag must win")
	}
}

// TestLoadConfig_AutoReload_DefaultsOnEvenForSelfHost is the review's first
// product decision, encoded: "don't pull new versions from GitHub" and "follow
// the binary I replaced myself" are separate concerns. Self-host defaults
// auto-update OFF (MUL-2381) because upgrading a fork from an upstream release
// would clobber it — an argument that says nothing about a binary the operator
// installed by hand.
func TestLoadConfig_AutoReload_DefaultsOnEvenForSelfHost(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("MULTICA_DAEMON_AUTO_UPDATE", "")
	t.Setenv("MULTICA_DAEMON_AUTO_RELOAD", "")
	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AutoUpdateEnabled {
		t.Fatalf("AutoUpdateEnabled = true for self-host, want false (MUL-2381)")
	}
	if !cfg.AutoReloadEnabled {
		t.Fatalf("AutoReloadEnabled = false for self-host; the on-disk watcher must not ride on the auto-update default")
	}
}

// TestLoadConfig_AutoReload_NotGatedOnAutoUpdateEnv is the same decoupling at
// the env layer: turning GitHub polling off must not silently stop the daemon
// from following a hand-installed binary.
func TestLoadConfig_AutoReload_NotGatedOnAutoUpdateEnv(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("MULTICA_DAEMON_AUTO_UPDATE", "false")
	t.Setenv("MULTICA_DAEMON_AUTO_RELOAD", "")
	cfg, err := LoadConfig(Overrides{
		ServerURL:      "https://api.multica.ai",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.AutoReloadEnabled {
		t.Fatalf("MULTICA_DAEMON_AUTO_UPDATE=false disabled auto-reload; the two switches are independent")
	}
}

// TestLoadConfig_AutoReload_OffSwitches pins the escape hatch across both layers
// it can be turned off from. The config-file layer resolves to
// overrides.DisableAutoReload in cmd_daemon.go, so it is covered by the same
// assertion as the flag.
func TestLoadConfig_AutoReload_OffSwitches(t *testing.T) {
	cases := []struct {
		name      string
		env       string
		overrides Overrides
	}{
		{name: "env false", env: "false"},
		{name: "env 0", env: "0"},
		{name: "env off", env: "off"},
		{name: "flag or config file", env: "", overrides: Overrides{DisableAutoReload: true}},
		{name: "flag beats a truthy env", env: "true", overrides: Overrides{DisableAutoReload: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stageFakeAgent(t)
			t.Setenv("MULTICA_DAEMON_AUTO_RELOAD", tc.env)
			overrides := tc.overrides
			overrides.ServerURL = "https://api.multica.ai"
			overrides.WorkspacesRoot = t.TempDir()
			cfg, err := LoadConfig(overrides)
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if cfg.AutoReloadEnabled {
				t.Fatalf("AutoReloadEnabled = true, want false")
			}
		})
	}
}

// TestResolveAgentsViaLoginShell_StripsAliasShadowing locks down the fix for
// #2512: when the user's rc file declares an alias with the same name as the
// agent CLI, the resolver must still return the real binary on PATH, not the
// alias text. The previous revision of this code passed the rest of the test
// suite but silently dropped this case (alias text is not absolute, so the
// `case "$p" in /*)` filter rejected it).
func TestResolveAgentsViaLoginShell_StripsAliasShadowing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell not available on Windows")
	}
	sh := "/bin/sh"
	if _, err := os.Stat(sh); err != nil {
		t.Skipf("no /bin/sh available: %v", err)
	}

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "fakeclaude")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	// rc adds binDir to PATH AND defines an alias that shadows the bare
	// name with a non-existent path. The pre-fix script would see the
	// alias, see that its target isn't absolute, and silently drop the
	// agent. With unalias/unset -f in place, command -v falls through to
	// the PATH search and finds binPath.
	rc := filepath.Join(t.TempDir(), "sh.rc")
	rcBody := "export PATH=\"" + binDir + ":$PATH\"\n" +
		"alias fakeclaude=\"/nonexistent/wrapper-from-rc\"\n"
	if err := os.WriteFile(rc, []byte(rcBody), 0o644); err != nil {
		t.Fatalf("write rc: %v", err)
	}

	// Strip PATH so exec.LookPath misses fakeclaude — same precondition as
	// the happy-path test, so we know the shell did the resolution.
	t.Setenv("PATH", "/usr/bin:/bin")
	if _, err := lookPathInPath("fakeclaude"); err == nil {
		t.Skip("PATH leak — fakeclaude already visible to the daemon without shell help")
	}
	// Sanity-check that the simulated environment can actually load aliases.
	// If the host /bin/sh doesn't honour $ENV in -i mode (rare but possible
	// on minimal Linux images), skipping is more honest than asserting on a
	// scenario the test couldn't actually set up.
	t.Setenv("SHELL", sh)
	t.Setenv("ENV", rc)
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer probeCancel()
	probeCmd := exec.CommandContext(probeCtx, sh, "-ilc", "alias fakeclaude 2>/dev/null")
	probeCmd.Stdin = strings.NewReader("")
	probe, err := probeCmd.Output()
	if err != nil || !strings.Contains(string(probe), "fakeclaude") {
		t.Skipf("test host's /bin/sh did not load alias from $ENV; cannot simulate shadowing (probe=%q err=%v)", string(probe), err)
	}

	got := resolveAgentsViaLoginShell([]string{"fakeclaude"})
	resolved, ok := got["fakeclaude"]
	if !ok {
		t.Fatalf("expected fakeclaude in resolved map despite alias shadowing, got %v", got)
	}
	wantCanonical, err := filepath.EvalSymlinks(binPath)
	if err != nil {
		t.Fatalf("eval symlinks for expected path: %v", err)
	}
	if resolved != wantCanonical {
		t.Errorf("resolved = %q, want canonical %q (got the alias instead of the PATH binary?)", resolved, wantCanonical)
	}
}

// TestResolveAgentsViaLoginShell_HardTimeoutOnBackgroundedStdout exercises the
// failure mode Cmd.WaitDelay guards against: an rc file that backgrounds a
// long-running process inheriting stdout. Killing the shell on context
// cancel does not close the inherited pipe, so cmd.Output() would hang on
// EOF until the survivor exits. The hard deadline must be roughly
// loginShellResolveTimeout + loginShellResolveWaitDelay, not the survivor's
// lifetime.
func TestResolveAgentsViaLoginShell_HardTimeoutOnBackgroundedStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell not available on Windows")
	}
	sh := "/bin/sh"
	if _, err := os.Stat(sh); err != nil {
		t.Skipf("no /bin/sh available: %v", err)
	}

	// rc backgrounds a sleeper that holds stdout for far longer than any
	// reasonable WaitDelay. The resolver script never gets to print
	// anything (we never even reach the for-loop because rc is still
	// being sourced when the sleeper forks), but that's exactly the
	// scenario we care about — we don't want to leak time-to-startup.
	rc := filepath.Join(t.TempDir(), "sh.rc")
	rcBody := "( sleep 60 ) &\n"
	if err := os.WriteFile(rc, []byte(rcBody), 0o644); err != nil {
		t.Fatalf("write rc: %v", err)
	}
	t.Setenv("SHELL", sh)
	t.Setenv("ENV", rc)

	// Cap = context timeout + wait delay + generous slack for goroutine
	// scheduling. A bug that disables WaitDelay would blow past 60s here.
	cap := loginShellResolveTimeout + loginShellResolveWaitDelay + 3*time.Second
	start := time.Now()
	done := make(chan struct{})
	go func() {
		_ = resolveAgentsViaLoginShell([]string{"claude"})
		close(done)
	}()
	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > cap {
			t.Errorf("resolver took %v, expected <= %v (WaitDelay leak?)", elapsed, cap)
		}
	case <-time.After(cap):
		t.Fatalf("resolver did not return within %v — WaitDelay is not enforcing a hard ceiling", cap)
	}
}

// TestLoadConfig_SkipsLoginShellWhenLookPathSucceeds proves the laziness
// requirement: if every agent CLI the operator cares about is already
// resolvable via the daemon's PATH (or pinned to an explicit MULTICA_*_PATH),
// the shell-fallback path must not run. We assert this by pointing SHELL at
// a sentinel script that touches a marker file when invoked.
func TestLoadConfig_SkipsLoginShellWhenLookPathSucceeds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell not available on Windows")
	}

	// Stage 1: a fake `claude` binary the daemon's bare exec.LookPath
	// definitely sees, so the probe loop never has reason to consult
	// shellResolved.
	pathDir := t.TempDir()
	fakeClaude := filepath.Join(pathDir, "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	// Stage 2: a SHELL that writes a marker file when invoked. If
	// LoadConfig's getShellResolved closure fires, the marker appears.
	shellDir := t.TempDir()
	shellPath := filepath.Join(shellDir, "bash") // pick a name the resolver's allowlist accepts
	marker := filepath.Join(shellDir, "invoked.marker")
	shellBody := "#!/bin/sh\ntouch \"" + marker + "\"\n"
	if err := os.WriteFile(shellPath, []byte(shellBody), 0o755); err != nil {
		t.Fatalf("write sentinel shell: %v", err)
	}

	t.Setenv("PATH", pathDir)
	t.Setenv("SHELL", shellPath)
	// Pin a non-existent agent to a bare name so it would normally trip
	// the fallback — except `claude` already resolves, and the user hasn't
	// configured anything else, so the probe loop should be satisfied
	// after the first probe alone.
	t.Setenv("MULTICA_DAEMON_ID", "11111111-1111-1111-1111-111111111111")

	if _, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:0",
		WorkspacesRoot: t.TempDir(),
	}); err != nil {
		// Some daemon-id / workspace bookkeeping outside our concern may
		// fail in CI; the marker assertion below is what matters either
		// way, so we don't fail on LoadConfig errors directly.
		t.Logf("LoadConfig returned %v (non-fatal for this test)", err)
	}
	// Brief wait for any goroutine the resolver might have leaked. The
	// sync.Once-guarded resolver runs synchronously today, so this should
	// be immediate; the sleep is just to avoid a flake if that ever
	// changes.
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("login shell was invoked even though exec.LookPath found every agent — laziness broken")
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected error stat-ing marker file: %v", err)
	}
}

func TestLoadConfig_UsesCodexDesktopAppBundleFallback(t *testing.T) {
	pathDir := t.TempDir()
	fakeCodex := filepath.Join(pathDir, "Codex.app", "Contents", "Resources", "codex")
	if err := os.MkdirAll(filepath.Dir(fakeCodex), 0o755); err != nil {
		t.Fatalf("mkdir fake Codex bundle: %v", err)
	}
	if err := os.WriteFile(fakeCodex, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake Codex bundle CLI: %v", err)
	}

	oldBundlePaths := codexDesktopAppBundlePaths
	codexDesktopAppBundlePaths = func() []string { return []string{fakeCodex} }
	t.Cleanup(func() { codexDesktopAppBundlePaths = oldBundlePaths })

	t.Setenv("PATH", t.TempDir())
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))
	t.Setenv("MULTICA_DAEMON_ID", "11111111-1111-1111-1111-111111111111")
	t.Setenv("MULTICA_CODEX_MODEL", "gpt-5")
	pinNonCodexAgentsToMissingPaths(t)

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:0",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	got, ok := cfg.Agents["codex"]
	if !ok {
		t.Fatalf("expected codex agent from Desktop app bundle fallback, got %#v", cfg.Agents)
	}
	if got.Path != fakeCodex {
		t.Fatalf("codex path = %q, want %q", got.Path, fakeCodex)
	}
	if got.Model != "gpt-5" {
		t.Fatalf("codex model = %q, want gpt-5", got.Model)
	}
}

// Regression for #5205: after OpenAI moved the Desktop app to ChatGPT.app,
// Multica must resolve the bundled CLI under ChatGPT.app (and prefer it over
// the legacy Codex.app path when both exist).
func TestLoadConfig_UsesChatGPTAppBundleCodexPath(t *testing.T) {
	pathDir := t.TempDir()
	fakeChatGPT := filepath.Join(pathDir, "ChatGPT.app", "Contents", "Resources", "codex")
	fakeLegacy := filepath.Join(pathDir, "Codex.app", "Contents", "Resources", "codex")
	for _, p := range []string{fakeChatGPT, fakeLegacy} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write fake CLI: %v", err)
		}
	}

	oldBundlePaths := codexDesktopAppBundlePaths
	// Prefer ChatGPT first, matching production ordering.
	codexDesktopAppBundlePaths = func() []string { return []string{fakeChatGPT, fakeLegacy} }
	t.Cleanup(func() { codexDesktopAppBundlePaths = oldBundlePaths })

	t.Setenv("PATH", t.TempDir())
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))
	t.Setenv("MULTICA_DAEMON_ID", "11111111-1111-1111-1111-111111111111")
	pinNonCodexAgentsToMissingPaths(t)

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:0",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	got, ok := cfg.Agents["codex"]
	if !ok {
		t.Fatalf("expected codex agent from ChatGPT.app bundle, got %#v", cfg.Agents)
	}
	if got.Path != fakeChatGPT {
		t.Fatalf("codex path = %q, want ChatGPT.app path %q", got.Path, fakeChatGPT)
	}
}

func TestCodexDesktopAppBundlePaths_IncludesChatGPTAndLegacy(t *testing.T) {
	paths := codexDesktopAppBundlePaths()
	var hasChatGPT, hasLegacy bool
	for _, p := range paths {
		if strings.Contains(p, "ChatGPT.app") && strings.HasSuffix(filepath.ToSlash(p), "Contents/Resources/codex") {
			hasChatGPT = true
		}
		if strings.Contains(p, "Codex.app") && strings.HasSuffix(filepath.ToSlash(p), "Contents/Resources/codex") {
			hasLegacy = true
		}
	}
	if !hasChatGPT {
		t.Fatalf("codexDesktopAppBundlePaths missing ChatGPT.app entry: %#v", paths)
	}
	if !hasLegacy {
		t.Fatalf("codexDesktopAppBundlePaths missing legacy Codex.app entry: %#v", paths)
	}
	// New path must be preferred (listed before legacy).
	chatgptIdx, legacyIdx := -1, -1
	for i, p := range paths {
		if chatgptIdx < 0 && strings.Contains(p, "ChatGPT.app") {
			chatgptIdx = i
		}
		if legacyIdx < 0 && strings.Contains(p, "Codex.app") {
			legacyIdx = i
		}
	}
	if chatgptIdx < 0 || legacyIdx < 0 || chatgptIdx > legacyIdx {
		t.Fatalf("expected ChatGPT.app before Codex.app, got indices chat=%d legacy=%d paths=%#v", chatgptIdx, legacyIdx, paths)
	}
}

func TestLoadConfig_CodexDesktopFallbackDoesNotOverrideExplicitPath(t *testing.T) {
	pathDir := t.TempDir()
	fakeCodex := filepath.Join(pathDir, "Codex.app", "Contents", "Resources", "codex")
	if err := os.MkdirAll(filepath.Dir(fakeCodex), 0o755); err != nil {
		t.Fatalf("mkdir fake Codex bundle: %v", err)
	}
	if err := os.WriteFile(fakeCodex, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake Codex bundle CLI: %v", err)
	}

	oldBundlePaths := codexDesktopAppBundlePaths
	codexDesktopAppBundlePaths = func() []string { return []string{fakeCodex} }
	t.Cleanup(func() { codexDesktopAppBundlePaths = oldBundlePaths })

	t.Setenv("PATH", t.TempDir())
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))
	t.Setenv("MULTICA_DAEMON_ID", "11111111-1111-1111-1111-111111111111")
	t.Setenv("MULTICA_CODEX_PATH", filepath.Join(t.TempDir(), "missing-codex"))
	pinNonCodexAgentsToMissingPaths(t)
	fakeClaude := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	t.Setenv("MULTICA_CLAUDE_PATH", fakeClaude)

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:0",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got, ok := cfg.Agents["codex"]; ok {
		t.Fatalf("explicit missing MULTICA_CODEX_PATH should not fall back to Desktop bundle, got %#v", got)
	}
}

func pinNonCodexAgentsToMissingPaths(t *testing.T) {
	t.Helper()
	missingDir := t.TempDir()
	for _, name := range []string{
		"MULTICA_CLAUDE_PATH",
		"MULTICA_OPENCODE_PATH",
		"MULTICA_OPENCLAW_PATH",
		"MULTICA_HERMES_PATH",
		"MULTICA_PI_PATH",
		"MULTICA_CURSOR_PATH",
		"MULTICA_COPILOT_PATH",
		"MULTICA_KIMI_PATH",
		"MULTICA_REASONIX_PATH",
		"MULTICA_DSH_PATH",
		"MULTICA_KIRO_PATH",
		"MULTICA_GROK_PATH",
	} {
		t.Setenv(name, filepath.Join(missingDir, strings.ToLower(name)))
	}
}

// =============================================================================
// CLI config Backends.OpenClaw overrides (issue #3875)
// =============================================================================

// writeCLIConfigForProfile is a minimal helper for the override tests:
// stages a HOME, writes a config.json under the given profile (empty profile
// = default), and returns the resolved path so tests can assert against it.
func writeCLIConfigForProfile(t *testing.T, profile string, cfg cli.CLIConfig) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		t.Fatalf("write cli config: %v", err)
	}
}

// TestApplyOpenclawOverride_DoesNothingWhenNil verifies the early-return
// path. A daemon started with no override should not Setenv anything; the
// existing probe / spawn flow remains undisturbed.
func TestApplyOpenclawOverride_DoesNothingWhenNil(t *testing.T) {
	// Pre-set both env vars to known values; verify they survive untouched.
	t.Setenv("MULTICA_OPENCLAW_PATH", "/before/openclaw")
	t.Setenv("OPENCLAW_STATE_DIR", "/before/state")

	applyOpenclawOverride(nil)

	if got := os.Getenv("MULTICA_OPENCLAW_PATH"); got != "/before/openclaw" {
		t.Errorf("MULTICA_OPENCLAW_PATH mutated: got %q, want /before/openclaw", got)
	}
	if got := os.Getenv("OPENCLAW_STATE_DIR"); got != "/before/state" {
		t.Errorf("OPENCLAW_STATE_DIR mutated: got %q, want /before/state", got)
	}
}

// TestApplyOpenclawOverride_SetsBothWhenEnvUnset verifies the happy path:
// neither env var is set, the override has both fields, both env vars get
// set to the override values.
func TestApplyOpenclawOverride_SetsBothWhenEnvUnset(t *testing.T) {
	t.Setenv("MULTICA_OPENCLAW_PATH", "")
	t.Setenv("OPENCLAW_STATE_DIR", "")
	os.Unsetenv("MULTICA_OPENCLAW_PATH")
	os.Unsetenv("OPENCLAW_STATE_DIR")
	t.Cleanup(func() {
		os.Unsetenv("MULTICA_OPENCLAW_PATH")
		os.Unsetenv("OPENCLAW_STATE_DIR")
	})

	applyOpenclawOverride(&cli.OpenClawOverride{
		BinaryPath: "/from/config/openclaw",
		StateDir:   "/from/config/state",
	})

	if got := os.Getenv("MULTICA_OPENCLAW_PATH"); got != "/from/config/openclaw" {
		t.Errorf("MULTICA_OPENCLAW_PATH: got %q, want /from/config/openclaw", got)
	}
	if got := os.Getenv("OPENCLAW_STATE_DIR"); got != "/from/config/state" {
		t.Errorf("OPENCLAW_STATE_DIR: got %q, want /from/config/state", got)
	}
}

// TestApplyOpenclawOverride_EnvWinsOverConfig is the precedence test
// agreed with @YOMXXX in #3875 review: an env var set upstream by the user
// (shell export, launchctl, systemd unit) MUST take precedence over the
// config-file value. This is the back-compat contract — anyone with
// MULTICA_OPENCLAW_PATH already in their environment must not see the
// daemon silently change its meaning when they later add a config file.
func TestApplyOpenclawOverride_EnvWinsOverConfig(t *testing.T) {
	// User has already exported these in their shell.
	t.Setenv("MULTICA_OPENCLAW_PATH", "/from/env/openclaw")
	t.Setenv("OPENCLAW_STATE_DIR", "/from/env/state")

	applyOpenclawOverride(&cli.OpenClawOverride{
		BinaryPath: "/from/config/openclaw",
		StateDir:   "/from/config/state",
	})

	if got := os.Getenv("MULTICA_OPENCLAW_PATH"); got != "/from/env/openclaw" {
		t.Errorf("MULTICA_OPENCLAW_PATH: env should win, got %q want /from/env/openclaw", got)
	}
	if got := os.Getenv("OPENCLAW_STATE_DIR"); got != "/from/env/state" {
		t.Errorf("OPENCLAW_STATE_DIR: env should win, got %q want /from/env/state", got)
	}
}

// TestApplyOpenclawOverride_PartialFields_OnlySetsConfigured verifies that
// an override with only one field set leaves the other env var alone (does
// not Setenv to ""). This matters: a user who only configures state_dir
// must not have their MULTICA_OPENCLAW_PATH discovery path forcibly
// short-circuited to an empty string.
func TestApplyOpenclawOverride_PartialFields_OnlySetsConfigured(t *testing.T) {
	os.Unsetenv("MULTICA_OPENCLAW_PATH")
	os.Unsetenv("OPENCLAW_STATE_DIR")
	t.Cleanup(func() {
		os.Unsetenv("MULTICA_OPENCLAW_PATH")
		os.Unsetenv("OPENCLAW_STATE_DIR")
	})

	applyOpenclawOverride(&cli.OpenClawOverride{
		StateDir: "/from/config/state",
		// BinaryPath intentionally empty — must NOT call Setenv("MULTICA_OPENCLAW_PATH", "")
	})

	if _, set := os.LookupEnv("MULTICA_OPENCLAW_PATH"); set {
		t.Errorf("MULTICA_OPENCLAW_PATH should remain unset when BinaryPath is empty; got %q", os.Getenv("MULTICA_OPENCLAW_PATH"))
	}
	if got := os.Getenv("OPENCLAW_STATE_DIR"); got != "/from/config/state" {
		t.Errorf("OPENCLAW_STATE_DIR: got %q, want /from/config/state", got)
	}
}

// TestOpenclawOverrideFrom_NavigationCases verifies the nullable-pointer
// chain into Backends.OpenClaw. Three cases that all must safely return
// nil without panicking: nil Backends, nil OpenClaw inside Backends, and
// the happy path (returns the inner override unchanged).
func TestOpenclawOverrideFrom_NavigationCases(t *testing.T) {
	if got := openclawOverrideFrom(cli.CLIConfig{}); got != nil {
		t.Errorf("nil Backends should produce nil override, got %+v", got)
	}
	if got := openclawOverrideFrom(cli.CLIConfig{Backends: &cli.BackendOverrides{}}); got != nil {
		t.Errorf("nil OpenClaw inside Backends should produce nil override, got %+v", got)
	}
	want := &cli.OpenClawOverride{StateDir: "/x"}
	got := openclawOverrideFrom(cli.CLIConfig{Backends: &cli.BackendOverrides{OpenClaw: want}})
	if got != want {
		t.Errorf("happy path should return inner pointer; got %p want %p", got, want)
	}
}

// TestLoadConfig_AppliesBackendOverridesFromConfigFile is the integration
// test that ties commit 1's schema to commit 2's wire-up: write a config
// file with backends.openclaw.{binary_path,state_dir}, call LoadConfig
// (with no env vars set), and verify the openclaw probe picked up the
// configured BinaryPath and the OPENCLAW_STATE_DIR env var was injected.
func TestLoadConfig_AppliesBackendOverridesFromConfigFile(t *testing.T) {
	stageFakeAgent(t)
	// stageFakeAgent left "claude" on PATH; we also need a fake "openclaw"
	// at a custom path that the config file points at (mimicking a non-default
	// installation: another bundled / isolated / CI deployment, etc).
	customDir := t.TempDir()
	customOpenclaw := filepath.Join(customDir, "non-default-openclaw")
	if err := os.WriteFile(customOpenclaw, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake openclaw: %v", err)
	}

	// Make sure no env-var override is leaking in from the test runner.
	os.Unsetenv("MULTICA_OPENCLAW_PATH")
	os.Unsetenv("OPENCLAW_STATE_DIR")
	t.Cleanup(func() {
		os.Unsetenv("MULTICA_OPENCLAW_PATH")
		os.Unsetenv("OPENCLAW_STATE_DIR")
	})

	// Drop a CLI config under the user's HOME (already pointed at TempDir
	// by stageFakeAgent's t.Setenv chain — but reassert here for clarity).
	homeForCLIConfig := t.TempDir()
	t.Setenv("HOME", homeForCLIConfig)
	cfg := cli.CLIConfig{
		ServerURL: "http://localhost:8080",
		Backends: &cli.BackendOverrides{
			OpenClaw: &cli.OpenClawOverride{
				BinaryPath: customOpenclaw,
				StateDir:   "/var/lib/openclaw-isolated",
			},
		},
	}
	if err := cli.SaveCLIConfig(cfg); err != nil {
		t.Fatalf("save cli config: %v", err)
	}

	loaded, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	openclaw, ok := loaded.Agents["openclaw"]
	if !ok {
		t.Fatalf("agents map missing 'openclaw' key; got keys=%v", agentKeys(loaded.Agents))
	}
	if openclaw.Path != customOpenclaw {
		t.Errorf("openclaw.Path: got %q, want %q (the binary configured in CLI config)", openclaw.Path, customOpenclaw)
	}
	if got := os.Getenv("OPENCLAW_STATE_DIR"); got != "/var/lib/openclaw-isolated" {
		t.Errorf("OPENCLAW_STATE_DIR: got %q, want injected from config", got)
	}
}

// TestLoadConfig_BackendOverrides_BackwardCompat_NoConfigFile verifies that
// the override mechanism is purely additive: a daemon started without any
// CLI config file (or with an empty one) behaves identically to before
// commit 1 — agents discovered from PATH, no env injection.
func TestLoadConfig_BackendOverrides_BackwardCompat_NoConfigFile(t *testing.T) {
	stageFakeAgent(t)

	// Point HOME at an empty dir — no config.json present.
	t.Setenv("HOME", t.TempDir())
	os.Unsetenv("MULTICA_OPENCLAW_PATH")
	os.Unsetenv("OPENCLAW_STATE_DIR")
	t.Cleanup(func() {
		os.Unsetenv("MULTICA_OPENCLAW_PATH")
		os.Unsetenv("OPENCLAW_STATE_DIR")
	})

	_, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with no config file should not fail: %v", err)
	}

	if _, set := os.LookupEnv("OPENCLAW_STATE_DIR"); set {
		t.Errorf("OPENCLAW_STATE_DIR should remain unset when no config file is present; got %q", os.Getenv("OPENCLAW_STATE_DIR"))
	}
}

// TestLoadConfig_BackendOverrides_MalformedConfigFileNonFatal verifies the
// fail-soft contract documented inline in LoadConfig: a corrupt config.json
// must not prevent daemon startup. This matters for diskcorruption /
// partial-write recovery — the daemon should log and proceed using
// env-var-only configuration.
func TestLoadConfig_BackendOverrides_MalformedConfigFileNonFatal(t *testing.T) {
	stageFakeAgent(t)
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Write malformed JSON.
	cfgDir := filepath.Join(homeDir, ".multica")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig should not fail on malformed config.json: %v", err)
	}
	// Should also have logged a slog Warn — we don't assert on the log
	// output here (avoids brittle string matching), but the build does
	// make sure log/slog stays imported.
}

// agentKeys is a tiny helper to make agent-map missing-key error messages
// readable. Returns sorted keys.
func agentKeys(m map[string]AgentEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestApplyOpenclawOverride_CLITimeout covers the #7112 knob on the same
// precedence contract as binary_path / state_dir: the config file supplies it
// when the environment does not, and an environment value the user exported
// upstream always wins.
func TestApplyOpenclawOverride_CLITimeout(t *testing.T) {
	t.Run("config file supplies the value", func(t *testing.T) {
		os.Unsetenv(execenv.OpenclawCLITimeoutEnv)
		t.Cleanup(func() { os.Unsetenv(execenv.OpenclawCLITimeoutEnv) })

		applyOpenclawOverride(&cli.OpenClawOverride{CLITimeout: "45s"})

		if got := os.Getenv(execenv.OpenclawCLITimeoutEnv); got != "45s" {
			t.Errorf("%s: got %q, want 45s", execenv.OpenclawCLITimeoutEnv, got)
		}
	})

	t.Run("env wins over config", func(t *testing.T) {
		t.Setenv(execenv.OpenclawCLITimeoutEnv, "20s")

		applyOpenclawOverride(&cli.OpenClawOverride{CLITimeout: "45s"})

		if got := os.Getenv(execenv.OpenclawCLITimeoutEnv); got != "20s" {
			t.Errorf("%s: env should win, got %q want 20s", execenv.OpenclawCLITimeoutEnv, got)
		}
	})

	t.Run("unset field leaves the env alone", func(t *testing.T) {
		os.Unsetenv(execenv.OpenclawCLITimeoutEnv)
		t.Cleanup(func() { os.Unsetenv(execenv.OpenclawCLITimeoutEnv) })

		applyOpenclawOverride(&cli.OpenClawOverride{StateDir: "/from/config/state"})

		if _, set := os.LookupEnv(execenv.OpenclawCLITimeoutEnv); set {
			t.Errorf("%s must not be set when the field is empty", execenv.OpenclawCLITimeoutEnv)
		}
	})
}
