package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

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
	probe, err := exec.Command(sh, "-ilc", "alias fakeclaude 2>/dev/null").Output()
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
