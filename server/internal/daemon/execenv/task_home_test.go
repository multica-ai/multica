package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// TestRenderManagedBlockWritableRoots verifies the workspace-write managed block
// emits writable_roots as a top-level dotted key (never a table header) and that
// the result — including the network_access dotted key sharing the same implicit
// table — is valid TOML.
func TestRenderManagedBlockWritableRoots(t *testing.T) {
	t.Parallel()
	policy := codexSandboxPolicyFor("linux", "0.121.0")
	policy.WritableRoots = []string{"/home/u/multica_workspaces/ws/abc/home", "/home/u/multica_workspaces/.repos/ws"}

	block := renderMulticaManagedBlock(policy)

	if !strings.Contains(block, `sandbox_workspace_write.writable_roots = ["/home/u/multica_workspaces/ws/abc/home", "/home/u/multica_workspaces/.repos/ws"]`) {
		t.Errorf("missing dotted-key writable_roots array, got:\n%s", block)
	}
	if strings.Contains(block, "[sandbox_workspace_write]") {
		t.Errorf("must not open a [sandbox_workspace_write] table header, got:\n%s", block)
	}

	// Both network_access and writable_roots build the same implicit table via
	// dotted keys — assert the parser accepts it.
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(block[strings.Index(block, "sandbox_mode"):]), &parsed); err != nil {
		t.Fatalf("generated block is invalid TOML: %v\nblock:\n%s", err, block)
	}
	sww, ok := parsed["sandbox_workspace_write"].(map[string]any)
	if !ok {
		t.Fatalf("sandbox_workspace_write not parsed as a table: %#v", parsed["sandbox_workspace_write"])
	}
	roots, ok := sww["writable_roots"].([]any)
	if !ok || len(roots) != 2 {
		t.Fatalf("writable_roots did not round-trip as a 2-element array: %#v", sww["writable_roots"])
	}
}

// TestRenderManagedBlockNoWritableRoots verifies that with no writable roots (or
// under danger-full-access), the writable_roots key is omitted entirely.
func TestRenderManagedBlockNoWritableRoots(t *testing.T) {
	t.Parallel()

	linux := renderMulticaManagedBlock(codexSandboxPolicyFor("linux", "0.121.0"))
	if strings.Contains(linux, "writable_roots") {
		t.Errorf("workspace-write with no roots must omit writable_roots, got:\n%s", linux)
	}

	// danger-full-access must never emit workspace-write keys even if roots set.
	darwin := codexSandboxPolicyFor("darwin", "0.121.0")
	darwin.WritableRoots = []string{"/should/not/appear"}
	block := renderMulticaManagedBlock(darwin)
	if strings.Contains(block, "writable_roots") || strings.Contains(block, "sandbox_workspace_write") {
		t.Errorf("danger-full-access must omit all workspace-write keys, got:\n%s", block)
	}
}

// TestEnsureCodexSandboxConfigWritableRoots verifies the writable_roots flow all
// the way through ensureCodexSandboxConfig into the on-disk config.toml.
func TestEnsureCodexSandboxConfigWritableRoots(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	policy := codexSandboxPolicyFor("linux", "0.121.0")
	policy.WritableRoots = []string{filepath.Join(dir, "home"), filepath.Join(dir, "repos")}
	if err := ensureCodexSandboxConfig(configPath, policy, "0.121.0", testLogger()); err != nil {
		t.Fatalf("ensureCodexSandboxConfig failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	if !strings.Contains(string(data), "sandbox_workspace_write.writable_roots = [") {
		t.Errorf("config.toml missing writable_roots, got:\n%s", data)
	}
	// Whole file must be valid TOML (the managed block coexists with any user
	// content — here there is none, so parsing the file directly is enough).
	var parsed map[string]any
	if err := toml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("on-disk config.toml is invalid TOML: %v", err)
	}
}

// TestPrepareCodexSandboxHomeLinux verifies the Linux path creates a writable
// home and returns exactly that home as the only writable root (no repo cache —
// Codex re-protects resolved gitdirs, so writable_roots can't unblock git; see
// task_home.go / #2925).
func TestPrepareCodexSandboxHomeLinux(t *testing.T) {
	// Not parallel: mutates HOME via os.UserHomeDir seeding in prepareTaskHome.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	envRoot := t.TempDir()
	home, roots, err := prepareCodexSandboxHome(envRoot, "linux", "", testLogger())
	if err != nil {
		t.Fatalf("prepareCodexSandboxHome: %v", err)
	}

	wantHome := filepath.Join(envRoot, "home")
	if home != wantHome {
		t.Errorf("home = %q, want %q", home, wantHome)
	}
	if fi, err := os.Stat(home); err != nil || !fi.IsDir() {
		t.Errorf("task home dir not created: err=%v", err)
	}
	if len(roots) != 1 || roots[0] != wantHome {
		t.Errorf("writable roots = %v, want [%q]", roots, wantHome)
	}
}

// TestPrepareCodexSandboxHomeNonLinux verifies macOS and Windows skip the
// redirect entirely (real HOME stays writable / no Landlock sandbox), and that
// no home directory is created.
func TestPrepareCodexSandboxHomeNonLinux(t *testing.T) {
	for _, goos := range []string{"darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			envRoot := t.TempDir()
			home, roots, err := prepareCodexSandboxHome(envRoot, goos, "", testLogger())
			if err != nil {
				t.Fatalf("prepareCodexSandboxHome(%s): %v", goos, err)
			}
			if home != "" || roots != nil {
				t.Fatalf("%s should skip the writable home, got home=%q roots=%v", goos, home, roots)
			}
			if _, err := os.Stat(filepath.Join(envRoot, "home")); !os.IsNotExist(err) {
				t.Errorf("%s must not create a task home dir, stat err=%v", goos, err)
			}
		})
	}
}

// TestPrepareTaskHomeSeedsCredentials verifies the per-task home is created
// writable and seeded with symlinks to the user's real credential/config files,
// skipping missing sources.
func TestPrepareTaskHomeSeedsCredentials(t *testing.T) {
	fakeHome := t.TempDir()
	// Present sources (writeFile creates parent dirs, so .ssh and .config/gh
	// become directories).
	writeFile(t, filepath.Join(fakeHome, ".gitconfig"), "[user]\n\tname = X\n")
	writeFile(t, filepath.Join(fakeHome, ".npmrc"), "//registry/:_authToken=abc\n")
	writeFile(t, filepath.Join(fakeHome, ".ssh", "id_ed25519"), "KEY\n")
	writeFile(t, filepath.Join(fakeHome, ".config", "gh", "hosts.yml"), "github.com: {}\n")
	// .netrc and .config/git deliberately absent — must be skipped.

	t.Setenv("HOME", fakeHome)

	taskHome := filepath.Join(t.TempDir(), "home")
	if err := prepareTaskHome(taskHome, testLogger()); err != nil {
		t.Fatalf("prepareTaskHome: %v", err)
	}

	assertSymlinkTo(t, filepath.Join(taskHome, ".gitconfig"), filepath.Join(fakeHome, ".gitconfig"))
	assertSymlinkTo(t, filepath.Join(taskHome, ".npmrc"), filepath.Join(fakeHome, ".npmrc"))
	assertSymlinkTo(t, filepath.Join(taskHome, ".ssh"), filepath.Join(fakeHome, ".ssh"))
	assertSymlinkTo(t, filepath.Join(taskHome, ".config", "gh"), filepath.Join(fakeHome, ".config", "gh"))

	// A read through the .gitconfig symlink resolves the real content.
	if b, err := os.ReadFile(filepath.Join(taskHome, ".gitconfig")); err != nil || !strings.Contains(string(b), "name = X") {
		t.Errorf("gitconfig not readable through symlink: %q err=%v", b, err)
	}

	// Missing sources are skipped, not linked as broken symlinks.
	if _, err := os.Lstat(filepath.Join(taskHome, ".netrc")); !os.IsNotExist(err) {
		t.Errorf(".netrc should be skipped (missing source), lstat err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(taskHome, ".config", "git")); !os.IsNotExist(err) {
		t.Errorf(".config/git should be skipped (missing source), lstat err=%v", err)
	}

	// .config must be a real, writable directory (not a symlink) so other tools
	// can write new config there.
	fi, err := os.Lstat(filepath.Join(taskHome, ".config"))
	if err != nil {
		t.Fatalf(".config not created: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error(".config must be a real directory, not a symlink")
	}
	// The task home is writable: creating a cache file under it succeeds.
	if err := os.MkdirAll(filepath.Join(taskHome, ".cache", "prisma"), 0o755); err != nil {
		t.Errorf("task home not writable: %v", err)
	}
}

// TestPrepareTaskHomeNoRealHome verifies prepareTaskHome degrades gracefully when
// there is no user home to seed from — the writable home is still created.
func TestPrepareTaskHomeNoRealHome(t *testing.T) {
	// An empty HOME makes os.UserHomeDir fail on unix.
	t.Setenv("HOME", "")

	taskHome := filepath.Join(t.TempDir(), "home")
	if err := prepareTaskHome(taskHome, testLogger()); err != nil {
		t.Fatalf("prepareTaskHome should not fail without a real home: %v", err)
	}
	if fi, err := os.Stat(taskHome); err != nil || !fi.IsDir() {
		t.Errorf("task home not created without a real home: err=%v", err)
	}
}

func TestTaskHomeEnv(t *testing.T) {
	t.Parallel()
	home := filepath.FromSlash("/tmp/task/home")
	env := TaskHomeEnv(home)

	want := map[string]string{
		"HOME":             home,
		"XDG_CACHE_HOME":   filepath.Join(home, ".cache"),
		"XDG_CONFIG_HOME":  filepath.Join(home, ".config"),
		"XDG_DATA_HOME":    filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME":   filepath.Join(home, ".local", "state"),
		"npm_config_cache": filepath.Join(home, ".npm"),
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("TaskHomeEnv[%q] = %q, want %q", k, env[k], v)
		}
	}
	if len(env) != len(want) {
		t.Errorf("TaskHomeEnv returned %d keys, want %d: %v", len(env), len(want), env)
	}
}

// --- helpers ---

func assertSymlinkTo(t *testing.T, link, wantTarget string) {
	t.Helper()
	fi, err := os.Lstat(link)
	if err != nil {
		t.Errorf("expected symlink %s: %v", link, err)
		return
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s is not a symlink", link)
		return
	}
	target, err := os.Readlink(link)
	if err != nil {
		t.Errorf("readlink %s: %v", link, err)
		return
	}
	if target != wantTarget {
		t.Errorf("symlink %s -> %s, want %s", link, target, wantTarget)
	}
}
