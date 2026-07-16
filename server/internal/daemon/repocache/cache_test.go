package repocache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func trustedGitForTest(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not available: %v", err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		t.Fatalf("make git path absolute: %v", err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve git path: %v", err)
	}
	return path
}

func TestWorktreeDirNameRejectsUnsafeRepoNames(t *testing.T) {
	t.Parallel()

	tests := []string{
		"",
		".",
		"..",
		"../escape",
		"org/../escape",
		"https://example.com/org/../escape.git",
		"https://example.com/org/%2e%2e/escape.git",
		"https://example.com/org/bad name.git",
		"https://example.com/org/bad\\name.git",
		"https://example.com/org/repo?.git",
	}
	for _, rawURL := range tests {
		rawURL := rawURL
		t.Run(rawURL, func(t *testing.T) {
			t.Parallel()
			if _, err := worktreeDirName(rawURL); err == nil {
				t.Fatalf("worktreeDirName(%q) succeeded, want unsafe repo name error", rawURL)
			}
		})
	}
}

func TestWorktreeDirNameDisambiguatesSameBasenameDeterministically(t *testing.T) {
	t.Parallel()

	urlA := "https://git.example.com/team-a/app.git"
	urlB := "https://git.example.com/team-b/app.git"
	nameA, err := worktreeDirName(urlA)
	if err != nil {
		t.Fatalf("worktreeDirName(%q): %v", urlA, err)
	}
	nameB, err := worktreeDirName(urlB)
	if err != nil {
		t.Fatalf("worktreeDirName(%q): %v", urlB, err)
	}
	if nameA == nameB {
		t.Fatalf("same-basename URLs share worktree dir %q", nameA)
	}
	if !strings.HasPrefix(nameA, "app-") || !strings.HasPrefix(nameB, "app-") {
		t.Fatalf("worktree dirs should retain the safe basename: A=%q B=%q", nameA, nameB)
	}
	again, err := worktreeDirName(urlA)
	if err != nil {
		t.Fatalf("second worktreeDirName(%q): %v", urlA, err)
	}
	if again != nameA {
		t.Fatalf("worktree dir is not deterministic: first=%q second=%q", nameA, again)
	}
}

func testLogger() *slog.Logger {
	return slog.Default()
}

func TestGitEnv(t *testing.T) {
	t.Setenv("HOME", "/owner/home/must-not-leak")
	t.Setenv("GH_TOKEN", "owner-token-must-not-leak")
	t.Setenv("GIT_ASKPASS", "/owner/bin/askpass")
	t.Setenv("SSH_AUTH_SOCK", "/owner/ssh-agent.sock")
	env := gitEnv()

	envHas := func(env []string, want string) bool {
		for _, e := range env {
			if e == want {
				return true
			}
		}
		return false
	}
	for _, want := range []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
	} {
		if !envHas(env, want) {
			t.Errorf("gitEnv() missing required broker setting %q", want)
		}
	}
	for _, forbidden := range []string{
		"HOME=/owner/home/must-not-leak",
		"GH_TOKEN=owner-token-must-not-leak",
		"GIT_ASKPASS=/owner/bin/askpass",
		"SSH_AUTH_SOCK=/owner/ssh-agent.sock",
	} {
		if envHas(env, forbidden) {
			t.Errorf("gitEnv() inherited owner setting %q", forbidden)
		}
	}
}

func TestGitEnvRejectsExistingOwnerConfig(t *testing.T) {
	// GIT_CONFIG_COUNT env vars are process-wide; cannot use t.Setenv in
	// parallel tests, so run sequentially.
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_0", "url.https://github.com/.insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "gh:")
	t.Setenv("GIT_CONFIG_KEY_1", "http.extraHeader")
	t.Setenv("GIT_CONFIG_VALUE_1", "Authorization: Bearer tok")

	env := gitEnv()

	envHas := func(want string) bool {
		for _, e := range env {
			if e == want {
				return true
			}
		}
		return false
	}

	for _, forbidden := range []string{
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=url.https://github.com/.insteadOf",
		"GIT_CONFIG_VALUE_0=gh:",
		"GIT_CONFIG_KEY_1=http.extraHeader",
		"GIT_CONFIG_VALUE_1=Authorization: Bearer tok",
	} {
		if envHas(forbidden) {
			t.Errorf("gitEnv() inherited owner Git config %q", forbidden)
		}
	}
}

func TestNewWithGitBrokerRejectsRelativeExecutable(t *testing.T) {
	_, err := NewWithGitBroker(t.TempDir(), testLogger(), GitBrokerOptions{
		Executable: "git",
	})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("NewWithGitBroker error = %v, want absolute executable rejection", err)
	}
}

func TestNewWithGitBrokerRejectsExecutableUnderWritableParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses a different executable trust model")
	}

	contents, err := os.ReadFile(trustedGitForTest(t))
	if err != nil {
		t.Fatalf("read trusted Git executable: %v", err)
	}
	writableParent := t.TempDir()
	gitCopy := filepath.Join(writableParent, "git")
	if err := os.WriteFile(gitCopy, contents, 0o755); err != nil {
		t.Fatalf("write Git executable copy: %v", err)
	}

	_, err = NewWithGitBroker(t.TempDir(), testLogger(), GitBrokerOptions{Executable: gitCopy})
	if err == nil || (!strings.Contains(err.Error(), "git executable is not owned by root") &&
		!strings.Contains(err.Error(), "root-owned system trust anchor")) {
		t.Fatalf("NewWithGitBroker error = %v, want writable parent trust rejection", err)
	}
}

func TestTrustedSystemGitExecutablePassesOwnerValidation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses a different executable trust model")
	}

	gitPath := trustedGitForTest(t)
	if err := validateTrustedExecutablePath(gitPath); err != nil {
		t.Fatalf("validateTrustedExecutablePath(%q): %v", gitPath, err)
	}
}

func TestGitBrokerUsesCallerContextAndAbsoluteExecutable(t *testing.T) {
	cache, err := NewWithGitBroker(t.TempDir(), testLogger(), GitBrokerOptions{
		Executable: trustedGitForTest(t),
	})
	if err != nil {
		t.Fatalf("NewWithGitBroker: %v", err)
	}
	if !filepath.IsAbs(cache.git.executable) {
		t.Fatalf("broker executable is not absolute: %q", cache.git.executable)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = cache.git.command(ctx, "--version").Run()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled broker command error = %v, want context.Canceled", err)
	}
}

func TestTaskGitIgnoresOwnerConfigHooksFiltersAndURLRewrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX hook and filter fixtures")
	}

	sourceA := createTestRepo(t)
	sourceB := createTestRepo(t)
	if err := os.WriteFile(filepath.Join(sourceA, ".gitattributes"), []byte("payload.txt filter=owner-filter\n"), 0o644); err != nil {
		t.Fatalf("write attributes: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceA, "payload.txt"), []byte("assigned-source\n"), 0o644); err != nil {
		t.Fatalf("write source A payload: %v", err)
	}
	runGitAuthored(t, sourceA, "add", ".")
	runGitAuthored(t, sourceA, "commit", "-m", "assigned payload")
	if err := os.WriteFile(filepath.Join(sourceB, "payload.txt"), []byte("rewritten-source\n"), 0o644); err != nil {
		t.Fatalf("write source B payload: %v", err)
	}
	runGitAuthored(t, sourceB, "add", ".")
	runGitAuthored(t, sourceB, "commit", "-m", "rewrite payload")

	ownerRoot := t.TempDir()
	marker := filepath.Join(ownerRoot, "owner-command-ran")
	hooksDir := filepath.Join(ownerRoot, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	hook := "#!/bin/sh\nprintf hook >" + strconv.Quote(marker) + "\n"
	if err := os.WriteFile(filepath.Join(hooksDir, "post-checkout"), []byte(hook), 0o755); err != nil {
		t.Fatalf("write owner hook: %v", err)
	}
	filter := "sh -c 'printf filter >" + marker + "; cat'"
	ownerConfig := filepath.Join(ownerRoot, "gitconfig")
	config := fmt.Sprintf("[url %q]\n\tinsteadOf = %s\n[core]\n\thooksPath = %s\n[filter \"owner-filter\"]\n\tsmudge = %s\n\tclean = %s\n[credential]\n\thelper = !sh -c 'printf credential >%s'\n",
		sourceB, sourceA, hooksDir, filter, filter, marker)
	if err := os.WriteFile(ownerConfig, []byte(config), 0o600); err != nil {
		t.Fatalf("write owner Git config: %v", err)
	}
	t.Setenv("HOME", ownerRoot)
	t.Setenv("GIT_CONFIG_GLOBAL", ownerConfig)
	t.Setenv("GIT_ASKPASS", filepath.Join(ownerRoot, "askpass"))
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "http.extraHeader")
	t.Setenv("GIT_CONFIG_VALUE_0", "Authorization: Bearer owner-token")

	cache, err := NewWithGitBroker(t.TempDir(), testLogger(), GitBrokerOptions{
		Executable: trustedGitForTest(t),
	})
	if err != nil {
		t.Fatalf("NewWithGitBroker: %v", err)
	}
	ctx := context.Background()
	if err := cache.SyncContext(ctx, "ws-1", []RepoInfo{{URL: sourceA}}); err != nil {
		t.Fatalf("SyncContext: %v", err)
	}
	result, err := cache.CreateWorktreeContext(ctx, WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceA,
		WorkDir:     t.TempDir(),
		AgentName:   "isolated",
		TaskID:      "owner-config-isolation",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeContext: %v", err)
	}
	payload, err := os.ReadFile(filepath.Join(result.Path, "payload.txt"))
	if err != nil {
		t.Fatalf("read checked-out payload: %v", err)
	}
	if got := string(payload); got != "assigned-source\n" {
		t.Fatalf("checked-out payload = %q, want assigned repository content", got)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("owner hook/filter/helper executed, marker stat error = %v", err)
	}
}

func TestGitBrokerZeroTimeoutUsesConfiguredDefault(t *testing.T) {
	const configuredTimeout = 37 * time.Second
	broker := &gitBroker{timeout: configuredTimeout}

	started := time.Now()
	ctx, cancel := broker.withTimeout(context.Background(), 0)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("broker context has no deadline")
	}
	if got := deadline.Sub(started); got <= configuredTimeout-time.Second || got >= configuredTimeout+time.Second {
		t.Fatalf("broker default timeout = %v, want approximately %v", got, configuredTimeout)
	}
}

func TestBareDirName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input, want string
	}{
		{"https://github.com/org/my-repo.git", "github.com+org+my-repo.git"},
		{"https://github.com/org/my-repo", "github.com+org+my-repo.git"},
		{"git@github.com:org/my-repo.git", "github.com+org+my-repo.git"},
		{"git@github.com:org/my-repo", "github.com+org+my-repo.git"},
		{"https://github.com/org/repo/", "github.com+org+repo.git"},
		{"ssh://git@gitlab.example.com:22/group/sub/repo.git", "gitlab.example.com%3A22+group+sub+repo.git"},
		// Basename collision: two repos sharing the basename must produce
		// distinct dirs (the original bug).
		{"ssh://git@gitlab.example.com:22/relisty/app.git", "gitlab.example.com%3A22+relisty+app.git"},
		{"ssh://git@gitlab.example.com:22/listbridge/app.git", "gitlab.example.com%3A22+listbridge+app.git"},
		{"my-repo", "my-repo.git"},
	}
	for _, tt := range tests {
		if got := bareDirName(tt.input); got != tt.want {
			t.Errorf("bareDirName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
	if _, err := bareDirNameSafe(""); err == nil {
		t.Error("bareDirNameSafe(\"\") succeeded, want unsafe repo URL error")
	}
}

// TestBareDirNameDistinctsSegmentBoundaryColliders covers the collision class
// that a naive path-flattening-with-dashes scheme would miss: two repos whose
// path segments differ only at a segment boundary flatten to the same string
// once slashes become dashes. The '+' separator can't appear inside a
// GitHub/GitLab path segment, so the boundary stays visible in the output.
func TestBareDirNameDistinctsSegmentBoundaryColliders(t *testing.T) {
	t.Parallel()
	pairs := [][2]string{
		{"git@github.com:foo/bar-baz.git", "git@github.com:foo-bar/baz.git"},
		{"https://github.com/foo/bar-baz.git", "https://github.com/foo-bar/baz.git"},
	}
	for _, p := range pairs {
		a, b := bareDirName(p[0]), bareDirName(p[1])
		if a == b {
			t.Errorf("bareDirName collision: %q and %q both → %q", p[0], p[1], a)
		}
	}
}

// TestBareDirNameDistinctsSameRepoNameAcrossHosts covers the cross-host
// collision class: the same path-with-namespace on different hosts must
// produce distinct cache dirs so an agent configured for host A can't be
// served the clone from host B.
func TestBareDirNameDistinctsSameRepoNameAcrossHosts(t *testing.T) {
	t.Parallel()
	pairs := [][2]string{
		{"git@github.com:org/repo.git", "git@gitlab.example.com:org/repo.git"},
		{"https://github.com/org/repo.git", "https://gitlab.example.com/org/repo.git"},
		{"ssh://git@github.com/org/repo.git", "ssh://git@gitlab.example.com/org/repo.git"},
	}
	for _, p := range pairs {
		a, b := bareDirName(p[0]), bareDirName(p[1])
		if a == b {
			t.Errorf("bareDirName collision across hosts: %q and %q both → %q", p[0], p[1], a)
		}
	}
}

// TestBareDirNameDistinctsHostPortFromDashedHostname covers the lossy-port
// encoding regression: a naive ':' -> '-' rewrite would collapse
// `host:port` onto a hostname that literally contains the same dash pattern,
// silently reintroducing the wrong-remote bug. We URL-encode ':' to '%3A'
// so host+port is lossless — and '%' is forbidden in valid hostnames so the
// marker can never come from a legal literal hostname.
func TestBareDirNameDistinctsHostPortFromDashedHostname(t *testing.T) {
	t.Parallel()
	pairs := [][2]string{
		// Host-with-port vs a literal hostname that looks like `host-port`.
		{"ssh://git@gitlab.example.com:22/org/repo.git", "git@gitlab.example.com-22:org/repo.git"},
		// Same again but across the URL and scp-style forms, explicit ports
		// swapped to ensure we don't rely on order.
		{"ssh://git@host.example.com:443/a/b.git", "git@host.example.com-443:a/b.git"},
	}
	for _, p := range pairs {
		a, b := bareDirName(p[0]), bareDirName(p[1])
		if a == b {
			t.Errorf("bareDirName collision between host:port and host-port: %q and %q both → %q", p[0], p[1], a)
		}
	}
}

func TestIsBareRepo(t *testing.T) {
	t.Parallel()

	// A real bare repository should be detected as bare.
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, output)
	}
	if !isBareRepo(dir) {
		t.Error("expected bare repo to be detected")
	}

	// An empty directory should not.
	emptyDir := t.TempDir()
	if isBareRepo(emptyDir) {
		t.Error("expected empty dir to not be detected as bare repo")
	}
}

// createTestRepo creates a local git repo with an initial commit and returns its path.
func createTestRepo(t *testing.T) string {
	t.Helper()
	return createTestRepoAt(t, t.TempDir())
}

// createTestRepoAt initializes a git repo at the given directory (which
// must already exist). Used to craft repo URLs at paths chosen by the test
// — e.g. to reproduce collision classes in name derivation.
func createTestRepoAt(t *testing.T, dir string) string {
	t.Helper()
	for _, args := range [][]string{
		{"init", dir},
		{"-C", dir, "commit", "--allow-empty", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git setup failed: %s: %v", out, err)
		}
	}
	return dir
}

func TestSyncAndLookup(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())

	// Sync should clone the repo.
	err := cache.Sync("ws-123", []RepoInfo{
		{URL: sourceRepo},
	})
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Lookup should find the cached repo.
	path := cache.Lookup("ws-123", sourceRepo)
	if path == "" {
		t.Fatal("expected to find cached repo")
	}
	if !isBareRepo(path) {
		t.Fatalf("expected bare repo at %s", path)
	}

	// Lookup for unknown URL should return empty.
	if got := cache.Lookup("ws-123", "https://github.com/org/unknown"); got != "" {
		t.Fatalf("expected empty for unknown URL, got %q", got)
	}

	// Lookup for unknown workspace should return empty.
	if got := cache.Lookup("ws-999", sourceRepo); got != "" {
		t.Fatalf("expected empty for unknown workspace, got %q", got)
	}
}

func TestCacheOperationsRejectWritableBareRepository(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses a different repository metadata trust model")
	}

	operations := []struct {
		name string
		run  func(*Cache, string, string, string) error
	}{
		{
			name: "sync",
			run: func(cache *Cache, sourceRepo, _, _ string) error {
				return cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}})
			},
		},
		{
			name: "lookup",
			run: func(cache *Cache, sourceRepo, _, _ string) error {
				if got := cache.Lookup("ws-1", sourceRepo); got != "" {
					return fmt.Errorf("Lookup returned untrusted bare cache %q", got)
				}
				return nil
			},
		},
		{
			name: "resolve",
			run: func(cache *Cache, sourceRepo, _, _ string) error {
				_, err := cache.Resolve("ws-1", sourceRepo)
				return err
			},
		},
		{
			name: "fetch",
			run: func(cache *Cache, _, barePath, _ string) error {
				return cache.Fetch(barePath)
			},
		},
		{
			name: "create-worktree",
			run: func(cache *Cache, sourceRepo, _, workDir string) error {
				_, err := cache.CreateWorktree(WorktreeParams{
					WorkspaceID: "ws-1",
					RepoURL:     sourceRepo,
					WorkDir:     workDir,
					AgentName:   "agent",
					TaskID:      "untrusted-cache",
				})
				return err
			},
		},
	}

	for _, operation := range operations {
		operation := operation
		t.Run(operation.name, func(t *testing.T) {
			sourceRepo := createTestRepo(t)
			cache := New(t.TempDir(), testLogger())
			if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
				t.Fatalf("initial Sync: %v", err)
			}
			barePath := cache.Lookup("ws-1", sourceRepo)
			if barePath == "" {
				t.Fatal("initial Lookup returned no bare cache")
			}
			beforeHead := gitOutputForTest(t, barePath, "rev-parse", "HEAD")
			if err := os.Chmod(barePath, 0o777); err != nil {
				t.Fatalf("make bare cache writable: %v", err)
			}
			t.Cleanup(func() { _ = os.Chmod(barePath, 0o755) })

			workDir := t.TempDir()
			err := operation.run(cache, sourceRepo, barePath, workDir)
			if operation.name == "lookup" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "group/world writable") {
				t.Fatalf("%s error = %v, want writable bare cache rejection", operation.name, err)
			}
			if got := gitOutputForTest(t, barePath, "rev-parse", "HEAD"); got != beforeHead {
				t.Fatalf("%s changed bare cache HEAD: got %q want %q", operation.name, got, beforeHead)
			}
			entries, readErr := os.ReadDir(workDir)
			if readErr != nil {
				t.Fatalf("read work directory: %v", readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("%s created worktree content before trust rejection: %v", operation.name, entries)
			}
		})
	}
}

func TestWorkspaceCacheTrustRejectsWritableDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses a different repository metadata trust model")
	}

	for _, target := range []string{"root", "workspace"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			sourceRepo := createTestRepo(t)
			cache := New(root, testLogger())
			if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
				t.Fatalf("initial Sync: %v", err)
			}
			path := root
			if target == "workspace" {
				path = filepath.Join(root, "ws-1")
			}
			if err := os.Chmod(path, 0o777); err != nil {
				t.Fatalf("make %s writable: %v", target, err)
			}
			t.Cleanup(func() { _ = os.Chmod(path, 0o755) })

			err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}})
			if err == nil || !strings.Contains(err.Error(), "group/world writable") {
				t.Fatalf("Sync error = %v, want writable %s rejection", err, target)
			}
			if got := cache.Lookup("ws-1", sourceRepo); got != "" {
				t.Fatalf("Lookup returned cache through writable %s: %q", target, got)
			}
		})
	}
}

func TestValidateDaemonOwnedDirectoryRejectsDifferentOwner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses a different repository metadata trust model")
	}

	path := t.TempDir()
	if err := validateDaemonOwnedDirectoryPathForUID(path, "test metadata", os.Geteuid()+1); err == nil || !strings.Contains(err.Error(), "daemon effective UID") {
		t.Fatalf("owner validation error = %v, want daemon UID mismatch", err)
	}
}

func TestValidateDaemonOwnedDirectoryStickyParentPolicy(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific root-owned sticky temp-parent policy")
	}

	child, err := os.MkdirTemp(os.TempDir(), "multica-sticky-parent-")
	if err != nil {
		t.Fatalf("create cache below system temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(child) })
	if err := os.Chmod(child, 0o755); err != nil {
		t.Fatalf("secure cache child: %v", err)
	}
	if err := validateDaemonOwnedDirectoryPathForUID(child, "test cache", os.Geteuid()); err != nil {
		t.Fatalf("root/daemon-owned sticky ancestor rejected: %v", err)
	}
	if err := validateDaemonOwnedDirectoryPathForUID(os.TempDir(), "test cache root", os.Geteuid()); err == nil {
		t.Fatal("sticky system temp directory was accepted as cache root")
	}

	parent := t.TempDir()
	child = filepath.Join(parent, "daemon-cache")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatalf("create daemon cache child: %v", err)
	}
	if err := os.Chmod(parent, 0o777); err != nil {
		t.Fatalf("make non-sticky parent writable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	if err := validateDaemonOwnedDirectoryPathForUID(child, "test cache", os.Geteuid()); err == nil || !strings.Contains(err.Error(), "group/world writable") {
		t.Fatalf("non-sticky writable ancestor error = %v, want rejection", err)
	}
}

func TestCreateWorktreeRejectsBareCacheReplacementAfterOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses a different repository metadata trust model")
	}

	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)
	if barePath == "" {
		t.Fatal("Lookup returned no bare cache")
	}
	originalPath := barePath + ".validated"
	replaced := false
	cache.bareCacheHook = func(stage, path string) {
		if stage != "after-open" || replaced {
			return
		}
		replaced = true
		if err := os.Rename(path, originalPath); err != nil {
			t.Fatalf("rename validated bare cache: %v", err)
		}
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatalf("install replacement bare cache: %v", err)
		}
		if err := os.WriteFile(filepath.Join(path, "replacement-sentinel.txt"), []byte("untouched"), 0o644); err != nil {
			t.Fatalf("write replacement sentinel: %v", err)
		}
	}

	_, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     t.TempDir(),
		AgentName:   "agent",
		TaskID:      "bare-cache-replacement",
	})
	if err == nil || !strings.Contains(err.Error(), "path changed after ownership proof") {
		t.Fatalf("CreateWorktree error = %v, want bare-cache identity rejection", err)
	}
	if !replaced {
		t.Fatal("bare-cache replacement hook did not run")
	}
	contents, readErr := os.ReadFile(filepath.Join(barePath, "replacement-sentinel.txt"))
	if readErr != nil || string(contents) != "untouched" {
		t.Fatalf("replacement bare cache was modified: contents=%q err=%v", contents, readErr)
	}
}

func TestClonePostconditionRejectsUntrustedBareRepository(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses a different repository metadata trust model")
	}

	root := t.TempDir()
	sourceRepo := createTestRepo(t)
	cache := New(root, testLogger())
	cache.bareCacheHook = func(stage, barePath string) {
		if stage == "after-clone" {
			if err := os.Chmod(barePath, 0o777); err != nil {
				t.Fatalf("make cloned bare cache writable: %v", err)
			}
		}
	}

	err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}})
	if err == nil || !strings.Contains(err.Error(), "group/world writable") {
		t.Fatalf("Sync error = %v, want clone postcondition rejection", err)
	}
	wsDir, pathErr := cache.workspaceCacheDir("ws-1", false)
	if pathErr != nil {
		t.Fatalf("workspaceCacheDir: %v", pathErr)
	}
	barePath, pathErr := cacheTargetPath(wsDir, sourceRepo)
	if pathErr != nil {
		t.Fatalf("cacheTargetPath: %v", pathErr)
	}
	if _, statErr := os.Lstat(barePath); !os.IsNotExist(statErr) {
		t.Fatalf("untrusted cloned cache was not removed: %v", statErr)
	}
}

func TestFetchRejectsBareRepositorySymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses a different repository metadata trust model")
	}

	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)
	linkPath := filepath.Join(filepath.Dir(barePath), "bare-link.git")
	if err := os.Symlink(barePath, linkPath); err != nil {
		t.Fatalf("create bare cache symlink: %v", err)
	}

	err := cache.Fetch(linkPath)
	if err == nil || !strings.Contains(err.Error(), "resolves through symlink") {
		t.Fatalf("Fetch error = %v, want bare cache symlink rejection", err)
	}
}

func TestWorktreeLauncherSelection(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux uses the kernel-bound /proc/self/exe launcher")
	}
	launcher, err := worktreeLauncherExecutable()
	if err != nil {
		t.Fatalf("worktreeLauncherExecutable: %v", err)
	}
	if launcher != "/proc/self/exe" {
		t.Fatalf("launcher = %q, want /proc/self/exe", launcher)
	}
}

func gitOutputForTest(t *testing.T, repoPath string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", repoPath}, args...)
	out, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", commandArgs, strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out))
}

func TestResolveReturnsCanonicalBareRepoIdentity(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceRepo := createTestRepo(t)
	cache := New(filepath.Join(root, "cache"), slog.Default())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	resolved, err := cache.Resolve("ws-1", sourceRepo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	wantPath, err := filepath.EvalSymlinks(cache.Lookup("ws-1", sourceRepo))
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if resolved.URL != sourceRepo {
		t.Fatalf("resolved URL = %q, want %q", resolved.URL, sourceRepo)
	}
	if resolved.BarePath != wantPath {
		t.Fatalf("resolved bare path = %q, want %q", resolved.BarePath, wantPath)
	}
	if !filepath.IsAbs(resolved.BarePath) {
		t.Fatalf("resolved bare path must be absolute: %q", resolved.BarePath)
	}
}

func TestResolveRejectsBareRepoWhoseOriginDoesNotMatchAssignment(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceA := createTestRepo(t)
	sourceB := createTestRepo(t)
	cache := New(filepath.Join(root, "cache"), slog.Default())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceA}}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceA)
	if out, err := exec.Command("git", "-C", barePath, "remote", "set-url", "origin", sourceB).CombinedOutput(); err != nil {
		t.Fatalf("replace origin: %s: %v", strings.TrimSpace(string(out)), err)
	}

	if _, err := cache.Resolve("ws-1", sourceA); err == nil || !strings.Contains(err.Error(), "origin") {
		t.Fatalf("Resolve error = %v, want origin identity mismatch", err)
	}
}

// TestSyncKeepsDistinctCachesForSegmentBoundaryColliders proves that two
// URLs differing only at a path-segment boundary don't share a bare cache
// and don't silently reuse each other's origin. Both conditions would have
// failed under a plain slashes-to-dashes flattening scheme: the two URLs
// in this test produce the same dash-joined key even though they point at
// different source repositories.
func TestSyncKeepsDistinctCachesForSegmentBoundaryColliders(t *testing.T) {
	t.Parallel()

	// Build two real source repos under a shared parent. Their filesystem
	// paths are used directly as URLs (git accepts local paths as remote
	// URLs). The path pair ".../foo/bar-baz" and ".../foo-bar/baz" would
	// flatten to the same string under slashes-to-dashes — that's the
	// class of collision we want to rule out.
	parent := t.TempDir()
	srcA := filepath.Join(parent, "foo", "bar-baz")
	srcB := filepath.Join(parent, "foo-bar", "baz")
	if err := os.MkdirAll(srcA, 0o755); err != nil {
		t.Fatalf("mkdir srcA: %v", err)
	}
	if err := os.MkdirAll(srcB, 0o755); err != nil {
		t.Fatalf("mkdir srcB: %v", err)
	}
	createTestRepoAt(t, srcA)
	createTestRepoAt(t, srcB)
	// Distinct content so a silent-reuse bug would produce the wrong file
	// in the wrong cache.
	if err := os.WriteFile(filepath.Join(srcA, "A.txt"), []byte("A\n"), 0o644); err != nil {
		t.Fatalf("write A: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcB, "B.txt"), []byte("B\n"), 0o644); err != nil {
		t.Fatalf("write B: %v", err)
	}
	runGitAuthored(t, srcA, "add", ".")
	runGitAuthored(t, srcA, "commit", "-m", "A-content")
	runGitAuthored(t, srcB, "add", ".")
	runGitAuthored(t, srcB, "commit", "-m", "B-content")

	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: srcA}, {URL: srcB}}); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	pathA := cache.Lookup("ws-1", srcA)
	pathB := cache.Lookup("ws-1", srcB)
	if pathA == "" || pathB == "" {
		t.Fatalf("missing cache entry: A=%q B=%q", pathA, pathB)
	}
	if pathA == pathB {
		t.Fatalf("collider URLs share a bare cache path: %s", pathA)
	}

	// Each bare cache must carry the origin URL of the repo it was
	// cloned from — not the other one's. A silent-reuse bug would have
	// both caches pointing at whichever URL won the race in Sync.
	if got := gitConfigGet(t, pathA, "remote.origin.url"); got != srcA {
		t.Errorf("cacheA origin.url = %q, want %q", got, srcA)
	}
	if got := gitConfigGet(t, pathB, "remote.origin.url"); got != srcB {
		t.Errorf("cacheB origin.url = %q, want %q", got, srcB)
	}

	// And each cache's content must reflect the right source.
	if !cachedRepoHasFile(t, pathA, "A.txt") {
		t.Errorf("cacheA (%s) should contain A.txt from srcA", pathA)
	}
	if !cachedRepoHasFile(t, pathB, "B.txt") {
		t.Errorf("cacheB (%s) should contain B.txt from srcB", pathB)
	}
}

// gitConfigGet reads a git config value from repoPath. Fails the test if
// the key is missing or the command errors.
func gitConfigGet(t *testing.T, repoPath, key string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoPath, "config", "--get", key).Output()
	if err != nil {
		t.Fatalf("git config --get %s in %s: %v", key, repoPath, err)
	}
	return strings.TrimSpace(string(out))
}

// cachedRepoHasFile returns true if the bare cache at barePath exposes a
// file named filename anywhere in its remote-tracking default branch.
// Walks refs/remotes/origin/* since a bare clone stores fetched heads
// there under the modern refspec.
func cachedRepoHasFile(t *testing.T, barePath, filename string) bool {
	t.Helper()
	ref := getRemoteDefaultBranch(barePath)
	if ref == "" {
		return false
	}
	out, err := exec.Command("git", "-C", barePath, "ls-tree", "-r", "--name-only", ref).Output()
	if err != nil {
		t.Fatalf("git ls-tree %s in %s: %v", ref, barePath, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == filename {
			return true
		}
	}
	return false
}

func TestSyncFetchesExisting(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())

	// First sync: clone.
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("first sync failed: %v", err)
	}

	// Record the remote-tracking default head in the cache. Under the modern
	// refspec layout, fetches write to refs/remotes/origin/*, not the bare
	// repo's own refs/heads/*, so reading the bare HEAD would return the
	// fossil snapshot from initial clone.
	barePath := cache.Lookup("ws-1", sourceRepo)
	oldHead := gitRefCommit(t, barePath, getRemoteDefaultBranch(barePath))

	// Add a commit to source.
	addEmptyCommit(t, sourceRepo, "second")
	sourceHead := gitHead(t, sourceRepo)
	if sourceHead == oldHead {
		t.Fatal("source HEAD should differ after new commit")
	}

	// Second sync: should fetch (not re-clone).
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("second sync failed: %v", err)
	}

	// Verify the cache remote-tracking ref was updated.
	newHead := gitRefCommit(t, barePath, getRemoteDefaultBranch(barePath))
	if newHead == oldHead {
		t.Fatal("expected cache remote-tracking head to be updated after fetch")
	}
	if newHead != sourceHead {
		t.Fatalf("expected cache head %s to match source head %s", newHead, sourceHead)
	}
}

func gitHead(t *testing.T, repoPath string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD failed in %s: %v", repoPath, err)
	}
	return strings.TrimSpace(string(out))
}

func TestWorktreeFromCache(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	barePath := cache.Lookup("ws-1", sourceRepo)
	if barePath == "" {
		t.Fatal("expected cached repo")
	}

	// Create a worktree from the bare cache — this is the actual use case.
	worktreeDir := filepath.Join(t.TempDir(), "work")
	cmd := exec.Command("git", "-C", barePath, "worktree", "add", "-b", "test-branch", worktreeDir, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree add failed: %s: %v", out, err)
	}
	defer exec.Command("git", "-C", barePath, "worktree", "remove", "--force", worktreeDir).Run()

	// Verify worktree exists and is on the right branch.
	cmd = exec.Command("git", "-C", worktreeDir, "branch", "--show-current")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("show branch failed: %v", err)
	}
	if got := trimLine(string(out)); got != "test-branch" {
		t.Fatalf("expected branch 'test-branch', got %q", got)
	}
}

func TestCreateWorktree(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	workDir := t.TempDir()
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "Code Reviewer",
		TaskID:      "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	// Verify the worktree was created.
	if _, err := os.Stat(result.Path); os.IsNotExist(err) {
		t.Fatalf("worktree path does not exist: %s", result.Path)
	}

	// Verify branch name format.
	if !strings.HasPrefix(result.BranchName, "agent/code-reviewer/") {
		t.Errorf("expected branch to start with 'agent/code-reviewer/', got %q", result.BranchName)
	}

	// Verify the worktree is on the correct branch.
	cmd := exec.Command("git", "-C", result.Path, "branch", "--show-current")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("show branch failed: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != result.BranchName {
		t.Errorf("expected branch %q, got %q", result.BranchName, got)
	}
}

func TestSyncRejectsUnsafeWorkspaceIDWithoutModifyingEscapeTarget(t *testing.T) {
	t.Parallel()

	sourceRepo := createTestRepo(t)
	parent := t.TempDir()
	cacheRoot := filepath.Join(parent, "cache")
	escapeTarget := filepath.Join(parent, "escape")
	if err := os.MkdirAll(escapeTarget, 0o755); err != nil {
		t.Fatalf("mkdir escape target: %v", err)
	}
	sentinel := filepath.Join(escapeTarget, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	cache := New(cacheRoot, testLogger())
	err := cache.Sync("../escape", []RepoInfo{{URL: sourceRepo}})
	if err == nil {
		t.Fatal("Sync succeeded with traversal workspace ID")
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "keep\n" {
		t.Fatalf("escape target modified: contents=%q err=%v", got, readErr)
	}
	entries, readErr := os.ReadDir(escapeTarget)
	if readErr != nil {
		t.Fatalf("read escape target: %v", readErr)
	}
	if len(entries) != 1 || entries[0].Name() != "sentinel.txt" {
		t.Fatalf("Sync wrote outside cache root: entries=%v", entries)
	}
}

func TestSyncRejectsSymlinkEscapeWithoutModifyingTarget(t *testing.T) {
	t.Parallel()

	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(cacheRoot, "ws-1")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	sentinel := filepath.Join(outside, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err == nil {
		t.Fatal("Sync succeeded through workspace symlink escape")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatalf("read outside target: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "sentinel.txt" {
		t.Fatalf("Sync modified symlink target: entries=%v", entries)
	}
}

func TestSyncPreservesExistingNonGitCacheTarget(t *testing.T) {
	t.Parallel()

	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	wsDir := filepath.Join(cacheRoot, "ws-1")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatalf("mkdir workspace cache: %v", err)
	}
	target := filepath.Join(wsDir, bareDirName(sourceRepo))
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir cache target: %v", err)
	}
	sentinel := filepath.Join(target, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}})
	if err == nil {
		t.Fatal("Sync succeeded with pre-existing non-git cache target")
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "keep\n" {
		t.Fatalf("pre-existing cache target modified: contents=%q err=%v", got, readErr)
	}
}

func TestCreateWorktreeRejectsUnsafeRepoURLBeforeLookup(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	workDir := t.TempDir()
	outside := filepath.Join(filepath.Dir(workDir), "escape")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside target: %v", err)
	}
	sentinel := filepath.Join(outside, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	_, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     "../escape",
		WorkDir:     workDir,
		AgentName:   "agent",
		TaskID:      "unsafe-repo-name",
	})
	if err == nil || !strings.Contains(err.Error(), "unsafe repo") {
		t.Fatalf("CreateWorktree error = %v, want unsafe repo error", err)
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "keep\n" {
		t.Fatalf("outside target modified: contents=%q err=%v", got, readErr)
	}
}

func TestCreateWorktreeRejectsLocalDirectoryBeforeCacheAccess(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	localDir := createTestRepo(t)
	headBefore := gitHead(t, localDir)

	_, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:    "ws-1",
		RepoURL:        "https://example.com/org/repo.git",
		WorkDir:        localDir,
		AgentName:      "agent",
		TaskID:         "local-directory",
		LocalDirectory: true,
	})
	if err == nil || !strings.Contains(err.Error(), "local_directory") {
		t.Fatalf("CreateWorktree error = %v, want local_directory prohibition", err)
	}
	if got := gitHead(t, localDir); got != headBefore {
		t.Fatalf("local directory HEAD changed: got %s want %s", got, headBefore)
	}
	entries, readErr := os.ReadDir(cacheRoot)
	if readErr != nil {
		t.Fatalf("read cache root: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("local_directory rejection touched cache: entries=%v", entries)
	}
}

func TestCreateWorktreeRejectsWorkDirThatIsExistingCheckout(t *testing.T) {
	t.Parallel()

	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	localDir := createTestRepo(t)
	headBefore := gitHead(t, localDir)

	_, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     localDir,
		AgentName:   "agent",
		TaskID:      "implicit-local-directory",
	})
	if err == nil || !strings.Contains(err.Error(), "local_directory") {
		t.Fatalf("CreateWorktree error = %v, want local_directory prohibition", err)
	}
	if got := gitHead(t, localDir); got != headBefore {
		t.Fatalf("existing checkout HEAD changed: got %s want %s", got, headBefore)
	}
}

func TestCreateWorktreeRejectsSymlinkEscapeTarget(t *testing.T) {
	t.Parallel()

	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	workDir := t.TempDir()
	dirName, err := worktreeDirName(sourceRepo)
	if err != nil {
		t.Fatalf("worktreeDirName: %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workDir, dirName)); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	sentinel := filepath.Join(outside, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	_, err = cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "agent",
		TaskID:      "symlink-escape",
	})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("CreateWorktree error = %v, want symlink escape error", err)
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "keep\n" {
		t.Fatalf("symlink target modified: contents=%q err=%v", got, readErr)
	}
}

func TestCreateWorktreeRejectsExistingWorktreeFromDifferentBareCache(t *testing.T) {
	t.Parallel()

	sourceA := createTestRepo(t)
	sourceB := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceA}, {URL: sourceB}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	bareB := cache.Lookup("ws-1", sourceB)
	workDir := t.TempDir()
	dirName, err := worktreeDirName(sourceA)
	if err != nil {
		t.Fatalf("worktreeDirName: %v", err)
	}
	foreignPath := filepath.Join(workDir, dirName)
	if out, err := exec.Command("git", "-C", bareB, "worktree", "add", "-b", "foreign-worktree", foreignPath, "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("create foreign worktree: %s: %v", out, err)
	}
	foreignHead := gitHead(t, foreignPath)
	if err := os.WriteFile(filepath.Join(foreignPath, "untracked.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("write foreign untracked file: %v", err)
	}

	_, err = cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceA,
		WorkDir:     workDir,
		AgentName:   "agent",
		TaskID:      "foreign-worktree",
	})
	if err == nil || !strings.Contains(err.Error(), "different bare cache") {
		t.Fatalf("CreateWorktree error = %v, want ownership error", err)
	}
	if got := gitHead(t, foreignPath); got != foreignHead {
		t.Fatalf("foreign worktree HEAD changed: got %s want %s", got, foreignHead)
	}
	if got, readErr := os.ReadFile(filepath.Join(foreignPath, "untracked.txt")); readErr != nil || string(got) != "keep\n" {
		t.Fatalf("foreign worktree modified before ownership proof: contents=%q err=%v", got, readErr)
	}
}

func TestCreateWorktreeRejectsBorrowedWorktreeAdminDir(t *testing.T) {
	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	firstWorkDir := t.TempDir()
	first, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     firstWorkDir,
		AgentName:   "agent",
		TaskID:      "owner",
	})
	if err != nil {
		t.Fatalf("create owned worktree: %v", err)
	}
	gitFile, err := os.ReadFile(filepath.Join(first.Path, ".git"))
	if err != nil {
		t.Fatalf("read owned worktree git file: %v", err)
	}

	secondWorkDir := t.TempDir()
	dirName, err := worktreeDirName(sourceRepo)
	if err != nil {
		t.Fatalf("worktreeDirName: %v", err)
	}
	borrowerPath := filepath.Join(secondWorkDir, dirName)
	if err := os.MkdirAll(borrowerPath, 0o755); err != nil {
		t.Fatalf("create borrower checkout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(borrowerPath, ".git"), gitFile, 0o644); err != nil {
		t.Fatalf("borrow worktree admin dir: %v", err)
	}
	sentinel := filepath.Join(borrowerPath, "borrower-sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("write borrower sentinel: %v", err)
	}

	_, err = cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     secondWorkDir,
		AgentName:   "agent",
		TaskID:      "borrower",
	})
	if err == nil || !strings.Contains(err.Error(), "backlink identifies a different checkout") {
		t.Fatalf("CreateWorktree error = %v, want git-dir backlink ownership error", err)
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "keep\n" {
		t.Fatalf("borrower checkout was modified: contents=%q err=%v", got, readErr)
	}
}

func TestCreateWorktreeRejectsExecutableRepositoryConfigBeforeUpdate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("identity-bound existing worktree updates are unsupported on windows")
	}

	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	workDir := t.TempDir()
	first, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "agent",
		TaskID:      "config-first",
	})
	if err != nil {
		t.Fatalf("create initial worktree: %v", err)
	}

	marker := filepath.Join(t.TempDir(), "filter-process-ran")
	filter := "sh -c 'printf filter >" + marker + "; cat'"
	runGitAuthored(t, first.Path, "config", "--local", "filter.task-process.process", filter)
	sentinel := filepath.Join(first.Path, "untracked-sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("write untracked sentinel: %v", err)
	}

	_, err = cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "agent",
		TaskID:      "config-second",
	})
	if err == nil || !strings.Contains(err.Error(), "filter.task-process.process") {
		t.Fatalf("CreateWorktree error = %v, want executable repository config rejection", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("repository filter process executed, marker stat error = %v", statErr)
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "keep\n" {
		t.Fatalf("worktree was modified before config rejection: contents=%q err=%v", got, readErr)
	}
}

func TestCreateWorktreeRejectsRepositoryConfigIncludesBeforeUpdate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("identity-bound existing worktree updates are unsupported on windows")
	}

	for _, includeKey := range []string{
		"include.path",
		"includeIf.gitdir:**.path",
	} {
		t.Run(includeKey, func(t *testing.T) {
			sourceRepo := createTestRepo(t)
			if err := os.WriteFile(filepath.Join(sourceRepo, ".gitattributes"), []byte("payload.txt filter=included-filter\n"), 0o644); err != nil {
				t.Fatalf("write attributes: %v", err)
			}
			if err := os.WriteFile(filepath.Join(sourceRepo, "payload.txt"), []byte("source\n"), 0o644); err != nil {
				t.Fatalf("write payload: %v", err)
			}
			runGitAuthored(t, sourceRepo, "add", ".")
			runGitAuthored(t, sourceRepo, "commit", "-m", "add filtered payload")

			cache := New(t.TempDir(), testLogger())
			if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
				t.Fatalf("sync failed: %v", err)
			}
			workDir := t.TempDir()
			first, err := cache.CreateWorktree(WorktreeParams{
				WorkspaceID: "ws-1",
				RepoURL:     sourceRepo,
				WorkDir:     workDir,
				AgentName:   "agent",
				TaskID:      "include-first",
			})
			if err != nil {
				t.Fatalf("create initial worktree: %v", err)
			}

			marker := filepath.Join(t.TempDir(), "included-filter-ran")
			includedConfig := filepath.Join(t.TempDir(), "included-config")
			filter := "sh -c 'printf filter >" + marker + "; cat'"
			contents := fmt.Sprintf("[filter \"included-filter\"]\n\tsmudge = %s\n\tclean = %s\n", filter, filter)
			if err := os.WriteFile(includedConfig, []byte(contents), 0o600); err != nil {
				t.Fatalf("write included config: %v", err)
			}
			runGitAuthored(t, first.Path, "config", "--local", includeKey, includedConfig)
			if err := os.WriteFile(filepath.Join(first.Path, "payload.txt"), []byte("dirty\n"), 0o644); err != nil {
				t.Fatalf("dirty filtered payload: %v", err)
			}
			sentinel := filepath.Join(first.Path, "untracked-sentinel.txt")
			if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
				t.Fatalf("write untracked sentinel: %v", err)
			}

			_, err = cache.CreateWorktree(WorktreeParams{
				WorkspaceID: "ws-1",
				RepoURL:     sourceRepo,
				WorkDir:     workDir,
				AgentName:   "agent",
				TaskID:      "include-second",
			})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(includeKey)) {
				t.Fatalf("CreateWorktree error = %v, want repository include config rejection", err)
			}
			if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
				t.Fatalf("included repository filter executed, marker stat error = %v", statErr)
			}
			if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "keep\n" {
				t.Fatalf("worktree was modified before include rejection: contents=%q err=%v", got, readErr)
			}
		})
	}
}

func TestCreateWorktreeRejectsWritableRepositoryMetadataBeforeUpdate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("identity-bound existing worktree updates are unsupported on windows")
	}

	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	workDir := t.TempDir()
	first, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "agent",
		TaskID:      "metadata-mode-first",
	})
	if err != nil {
		t.Fatalf("create initial worktree: %v", err)
	}

	gitFile, err := os.ReadFile(filepath.Join(first.Path, ".git"))
	if err != nil {
		t.Fatalf("read worktree .git file: %v", err)
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(gitFile)), "gitdir: "))
	if err := os.Chmod(gitDir, 0o777); err != nil {
		t.Fatalf("make worktree metadata writable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(gitDir, 0o755) })
	sentinel := filepath.Join(first.Path, "untracked-sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("write untracked sentinel: %v", err)
	}

	_, err = cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "agent",
		TaskID:      "metadata-mode-second",
	})
	if err == nil || !strings.Contains(err.Error(), "group/world writable") {
		t.Fatalf("CreateWorktree error = %v, want writable metadata rejection", err)
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "keep\n" {
		t.Fatalf("worktree was modified before metadata rejection: contents=%q err=%v", got, readErr)
	}
}

func TestCreateWorktreeRejectsWorktreeReplacementDuringUpdate(t *testing.T) {
	for _, replacementKind := range []string{"worktree", "parent"} {
		for _, replacementStage := range []string{"after-owner-proof", "after-reset", "after-clean", "after-checkout", "after-checkout-retry"} {
			t.Run(replacementKind+"/"+replacementStage, func(t *testing.T) {
				sourceRepo := createTestRepo(t)
				cache := New(t.TempDir(), testLogger())
				if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
					t.Fatalf("sync failed: %v", err)
				}
				workDir := t.TempDir()
				first, err := cache.CreateWorktree(WorktreeParams{
					WorkspaceID: "ws-1",
					RepoURL:     sourceRepo,
					WorkDir:     workDir,
					AgentName:   "agent",
					TaskID:      "replacement-first",
				})
				if err != nil {
					t.Fatalf("create initial worktree: %v", err)
				}

				movedPath := first.Path + "-moved"
				movedParent := workDir + "-moved"
				foreignSource := createTestRepo(t)
				foreignSourceSentinel := filepath.Join(foreignSource, "foreign-sentinel.txt")
				if err := os.WriteFile(foreignSourceSentinel, []byte("keep\n"), 0o644); err != nil {
					t.Fatalf("write foreign source sentinel: %v", err)
				}
				runGitAuthored(t, foreignSource, "add", "foreign-sentinel.txt")
				runGitAuthored(t, foreignSource, "commit", "-m", "add foreign sentinel")
				foreignSentinel := filepath.Join(first.Path, "foreign-sentinel.txt")
				replaced := false
				cache.existingWorktreeHook = func(stage, worktreePath string) {
					if stage != replacementStage || replaced {
						return
					}
					replaced = true
					if replacementKind == "parent" {
						if err := os.Rename(workDir, movedParent); err != nil {
							t.Fatalf("rename validated worktree parent: %v", err)
						}
						if err := os.MkdirAll(workDir, 0o755); err != nil {
							t.Fatalf("install replacement parent: %v", err)
						}
					} else if err := os.Rename(worktreePath, movedPath); err != nil {
						t.Fatalf("rename validated worktree: %v", err)
					}
					if out, err := exec.Command("git", "clone", foreignSource, worktreePath).CombinedOutput(); err != nil {
						t.Fatalf("install replacement checkout: %s: %v", out, err)
					}
				}

				agentName := "agent"
				if replacementStage == "after-checkout" {
					agentName = "fresh-agent"
				}
				_, err = cache.CreateWorktree(WorktreeParams{
					WorkspaceID: "ws-1",
					RepoURL:     sourceRepo,
					WorkDir:     workDir,
					AgentName:   agentName,
					TaskID:      "replacement-second",
				})
				if err == nil {
					t.Fatal("CreateWorktree succeeded after the validated worktree path was replaced")
				}
				if !replaced {
					t.Fatalf("replacement hook did not run at %s: create error=%v", replacementStage, err)
				}
				if got, readErr := os.ReadFile(foreignSentinel); readErr != nil || string(got) != "keep\n" {
					t.Fatalf("replacement checkout was modified: contents=%q err=%v create error=%v", got, readErr, err)
				}
			})
		}
	}
}

func TestCreateWorktreeRejectsRepositoryMetadataReplacementDuringUpdate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("identity-bound existing worktree updates are unsupported on windows")
	}

	for _, replacementKind := range []string{"worktree-git-dir", "git-common-dir"} {
		for _, replacementStage := range []string{"after-owner-proof", "after-reset", "after-clean", "after-checkout", "after-checkout-retry"} {
			t.Run(replacementKind+"/"+replacementStage, func(t *testing.T) {
				sourceRepo := createTestRepo(t)
				cache := New(t.TempDir(), testLogger())
				if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
					t.Fatalf("sync failed: %v", err)
				}
				workDir := t.TempDir()
				first, err := cache.CreateWorktree(WorktreeParams{
					WorkspaceID: "ws-1",
					RepoURL:     sourceRepo,
					WorkDir:     workDir,
					AgentName:   "agent",
					TaskID:      "metadata-first",
				})
				if err != nil {
					t.Fatalf("create initial worktree: %v", err)
				}

				gitFile, err := os.ReadFile(filepath.Join(first.Path, ".git"))
				if err != nil {
					t.Fatalf("read worktree .git file: %v", err)
				}
				gitDir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(gitFile)), "gitdir: "))
				if !filepath.IsAbs(gitDir) {
					gitDir = filepath.Join(first.Path, gitDir)
				}
				gitDir, err = filepath.EvalSymlinks(gitDir)
				if err != nil {
					t.Fatalf("resolve worktree git dir: %v", err)
				}
				commonFile, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
				if err != nil {
					t.Fatalf("read worktree common dir: %v", err)
				}
				commonDir := strings.TrimSpace(string(commonFile))
				if !filepath.IsAbs(commonDir) {
					commonDir = filepath.Join(gitDir, commonDir)
				}
				commonDir, err = filepath.EvalSymlinks(commonDir)
				if err != nil {
					t.Fatalf("resolve git common dir: %v", err)
				}

				target := gitDir
				if replacementKind == "git-common-dir" {
					target = commonDir
				}
				movedTarget := target + "-moved"
				sentinel := filepath.Join(target, "replacement-sentinel.txt")
				replaced := false
				cache.existingWorktreeHook = func(stage, _ string) {
					if stage != replacementStage || replaced {
						return
					}
					replaced = true
					if err := os.Rename(target, movedTarget); err != nil {
						t.Fatalf("rename validated %s: %v", replacementKind, err)
					}
					if err := os.MkdirAll(target, 0o755); err != nil {
						t.Fatalf("install replacement %s: %v", replacementKind, err)
					}
					if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
						t.Fatalf("write replacement sentinel: %v", err)
					}
				}

				agentName := "agent"
				if replacementStage == "after-checkout" {
					agentName = "fresh-agent"
				}
				_, err = cache.CreateWorktree(WorktreeParams{
					WorkspaceID: "ws-1",
					RepoURL:     sourceRepo,
					WorkDir:     workDir,
					AgentName:   agentName,
					TaskID:      "metadata-second",
				})
				if err == nil {
					t.Fatalf("CreateWorktree succeeded after %s replacement", replacementKind)
				}
				if !replaced {
					t.Fatalf("replacement hook did not run at %s: %v", replacementStage, err)
				}
				if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "keep\n" {
					t.Fatalf("replacement metadata directory was modified: contents=%q err=%v create error=%v", got, readErr, err)
				}
			})
		}
	}
}

func TestCreateWorktreeRejectsNewWorktreeReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("identity-bound new worktree validation is unsupported on windows")
	}

	for _, replacementKind := range []string{"worktree", "worktree-git-dir", "git-common-dir"} {
		for _, replacementStage := range []string{"after-worktree-add", "before-exclude", "before-hook", "before-return"} {
			t.Run(replacementKind+"/"+replacementStage, func(t *testing.T) {
				sourceRepo := createTestRepo(t)
				cache := New(t.TempDir(), testLogger())
				if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
					t.Fatalf("sync failed: %v", err)
				}
				workDir := t.TempDir()
				dirName, err := worktreeDirName(sourceRepo)
				if err != nil {
					t.Fatalf("derive worktree directory name: %v", err)
				}
				worktreePath, err := canonicalWorktreeTarget(workDir, dirName)
				if err != nil {
					t.Fatalf("derive canonical worktree target: %v", err)
				}
				foreignSource := createTestRepo(t)
				foreignSourceSentinel := filepath.Join(foreignSource, "foreign-sentinel.txt")
				if err := os.WriteFile(foreignSourceSentinel, []byte("keep\n"), 0o644); err != nil {
					t.Fatalf("write foreign source sentinel: %v", err)
				}
				runGitAuthored(t, foreignSource, "add", "foreign-sentinel.txt")
				runGitAuthored(t, foreignSource, "commit", "-m", "add foreign sentinel")

				replaced := false
				var replacementSentinel string
				cache.existingWorktreeHook = func(stage, worktreePath string) {
					if stage != replacementStage || replaced {
						return
					}
					replaced = true
					gitFile, err := os.ReadFile(filepath.Join(worktreePath, ".git"))
					if err != nil {
						t.Fatalf("read new worktree .git file: %v", err)
					}
					gitDir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(gitFile)), "gitdir: "))
					if !filepath.IsAbs(gitDir) {
						gitDir = filepath.Join(worktreePath, gitDir)
					}
					gitDir, err = filepath.EvalSymlinks(gitDir)
					if err != nil {
						t.Fatalf("resolve new worktree git dir: %v", err)
					}
					commonFile, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
					if err != nil {
						t.Fatalf("read new worktree common dir: %v", err)
					}
					commonDir := strings.TrimSpace(string(commonFile))
					if !filepath.IsAbs(commonDir) {
						commonDir = filepath.Join(gitDir, commonDir)
					}
					commonDir, err = filepath.EvalSymlinks(commonDir)
					if err != nil {
						t.Fatalf("resolve new worktree common dir: %v", err)
					}

					switch replacementKind {
					case "worktree":
						if err := os.Rename(worktreePath, worktreePath+"-moved"); err != nil {
							t.Fatalf("rename new worktree: %v", err)
						}
						if out, err := exec.Command("git", "clone", foreignSource, worktreePath).CombinedOutput(); err != nil {
							t.Fatalf("install replacement checkout: %s: %v", out, err)
						}
						replacementSentinel = filepath.Join(worktreePath, "foreign-sentinel.txt")
					case "worktree-git-dir", "git-common-dir":
						target := gitDir
						if replacementKind == "git-common-dir" {
							target = commonDir
						}
						if err := os.Rename(target, target+"-moved"); err != nil {
							t.Fatalf("rename new %s: %v", replacementKind, err)
						}
						if err := os.MkdirAll(target, 0o755); err != nil {
							t.Fatalf("install replacement %s: %v", replacementKind, err)
						}
						replacementSentinel = filepath.Join(target, "replacement-sentinel.txt")
						if err := os.WriteFile(replacementSentinel, []byte("keep\n"), 0o644); err != nil {
							t.Fatalf("write replacement sentinel: %v", err)
						}
					}
				}

				_, err = cache.CreateWorktree(WorktreeParams{
					WorkspaceID:         "ws-1",
					RepoURL:             sourceRepo,
					WorkDir:             workDir,
					AgentName:           "agent",
					TaskID:              "new-replacement",
					CoAuthoredByEnabled: true,
				})
				if err == nil {
					t.Fatal("CreateWorktree succeeded after new worktree identity replacement")
				}
				if !replaced {
					t.Fatalf("replacement hook did not run at %s: create error=%v", replacementStage, err)
				}
				if got, readErr := os.ReadFile(replacementSentinel); readErr != nil || string(got) != "keep\n" {
					t.Fatalf("replacement target was modified: contents=%q err=%v create error=%v", got, readErr, err)
				}
				if replacementKind == "worktree" {
					entries, readErr := os.ReadDir(worktreePath + "-moved")
					if readErr != nil || len(entries) != 0 {
						t.Fatalf("displaced provisional checkout was not emptied: entries=%v err=%v create error=%v", entries, readErr, err)
					}
				}
			})
		}
	}
}

func TestCreateWorktreeDoesNotCreateInsideReplacementDuringWorktreeAdd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("identity-bound worktree publication is unsupported on windows")
	}

	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	workDir := t.TempDir()
	dirName, err := worktreeDirName(sourceRepo)
	if err != nil {
		t.Fatalf("derive worktree directory name: %v", err)
	}
	worktreePath, err := canonicalWorktreeTarget(workDir, dirName)
	if err != nil {
		t.Fatalf("derive canonical worktree target: %v", err)
	}
	attackerTarget := t.TempDir()
	attackerSentinel := filepath.Join(attackerTarget, "keep.txt")
	if err := os.WriteFile(attackerSentinel, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("write attacker sentinel: %v", err)
	}

	hookRan := false
	cache.worktreeAddHook = func(stage, addPath string) {
		if stage != "after-start" || hookRan {
			return
		}
		hookRan = true
		if addPath == worktreePath {
			t.Fatalf("Git received the task-visible final worktree path: %s", addPath)
		}
		stagingRoot, canonicalErr := canonicalExistingDir(filepath.Join(cache.root, ".worktree-staging"))
		if canonicalErr != nil {
			t.Fatalf("resolve staging root: %v", canonicalErr)
		}
		canonicalAddParent, canonicalErr := canonicalExistingDir(filepath.Dir(addPath))
		if canonicalErr != nil || canonicalAddParent != stagingRoot {
			t.Fatalf("Git worktree path is outside daemon staging: path=%s parent=%s root=%s err=%v", addPath, canonicalAddParent, stagingRoot, canonicalErr)
		}
		if err := os.Symlink(attackerTarget, worktreePath); err != nil {
			t.Fatalf("replace worktree target during add: %v", err)
		}
	}

	_, err = cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "agent",
		TaskID:      "during-add-replacement",
	})
	if err == nil {
		t.Fatal("CreateWorktree succeeded after the final target was replaced during git worktree add")
	}
	if !hookRan {
		t.Fatalf("worktree add hook did not run: %v", err)
	}
	if got, readErr := os.ReadFile(attackerSentinel); readErr != nil || string(got) != "keep\n" {
		t.Fatalf("attacker sentinel changed: contents=%q err=%v create error=%v", got, readErr, err)
	}
	entries, readErr := os.ReadDir(attackerTarget)
	if readErr != nil {
		t.Fatalf("read attacker target: %v", readErr)
	}
	if len(entries) != 1 || entries[0].Name() != "keep.txt" {
		t.Fatalf("Git wrote into attacker replacement: entries=%v create error=%v", entries, err)
	}
	barePath, lookupErr := cache.lookupPath(context.Background(), "ws-1", sourceRepo)
	if lookupErr != nil {
		t.Fatalf("lookup bare cache: %v", lookupErr)
	}
	branch := "refs/heads/agent/agent/during-a"
	if out, refErr := exec.Command("git", "-C", barePath, "rev-parse", "--verify", "--quiet", branch).CombinedOutput(); refErr == nil {
		t.Fatalf("unpublished branch remains: ref=%s output=%s", branch, out)
	}
	worktreeEntries, readErr := os.ReadDir(filepath.Join(barePath, "worktrees"))
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read bare worktree metadata: %v", readErr)
	}
	if len(worktreeEntries) != 0 {
		t.Fatalf("unpublished worktree metadata remains: %v", worktreeEntries)
	}
	stagingEntries, readErr := os.ReadDir(filepath.Join(cache.root, ".worktree-staging"))
	if readErr != nil {
		t.Fatalf("read staging root after rejected publication: %v", readErr)
	}
	if len(stagingEntries) != 0 {
		t.Fatalf("unpublished staging entries remain: %v", stagingEntries)
	}
}

func TestCreateWorktreePublishesFinalBacklinksWithoutStagingIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("identity-bound worktree publication is unsupported on windows")
	}

	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     t.TempDir(),
		AgentName:   "agent",
		TaskID:      "published-backlinks",
	})
	if err != nil {
		t.Fatalf("create worktree: %v", err)
	}
	gitFile, err := os.ReadFile(filepath.Join(result.Path, ".git"))
	if err != nil {
		t.Fatalf("read published .git file: %v", err)
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(gitFile)), "gitdir: "))
	if !filepath.IsAbs(gitDir) {
		t.Fatalf("published .git path is not absolute: %q", gitDir)
	}
	backlink, err := os.ReadFile(filepath.Join(gitDir, "gitdir"))
	if err != nil {
		t.Fatalf("read published git-dir backlink: %v", err)
	}
	wantBacklink := filepath.Join(result.Path, ".git")
	if got := strings.TrimSpace(string(backlink)); got != wantBacklink {
		t.Fatalf("published git-dir backlink mismatch: got %q want %q", got, wantBacklink)
	}
	if strings.Contains(string(gitFile), ".worktree-staging") || strings.Contains(string(backlink), ".worktree-staging") {
		t.Fatalf("published metadata retains staging identity: gitFile=%q backlink=%q", gitFile, backlink)
	}
	entries, err := os.ReadDir(filepath.Join(cache.root, ".worktree-staging"))
	if err != nil {
		t.Fatalf("read staging root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("published staging root is not empty: %v", entries)
	}
}

func TestRemoveDirectoryContentsAtFDRejectsEntryReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("descriptor-relative cleanup is unsupported on windows")
	}

	for _, kind := range []string{"file", "directory"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			entryPath := filepath.Join(root, "owned")
			movedPath := filepath.Join(root, "owned-moved")
			replacementSentinel := filepath.Join(entryPath, "keep.txt")
			if kind == "directory" {
				if err := os.Mkdir(entryPath, 0o755); err != nil {
					t.Fatalf("create owned directory: %v", err)
				}
			} else if err := os.WriteFile(entryPath, []byte("owned\n"), 0o644); err != nil {
				t.Fatalf("create owned file: %v", err)
			}
			rootHandle, err := openDirectoryHandle(root)
			if err != nil {
				t.Fatalf("open cleanup root: %v", err)
			}
			defer rootHandle.file.Close()
			replaced := false
			err = removeDirectoryContentsAtFDWithHook(int(rootHandle.file.Fd()), func(_ int, name string) {
				if replaced || name != "owned" {
					return
				}
				replaced = true
				if err := os.Rename(entryPath, movedPath); err != nil {
					t.Fatalf("move owned cleanup entry: %v", err)
				}
				if kind == "directory" {
					if err := os.Mkdir(entryPath, 0o755); err != nil {
						t.Fatalf("create replacement directory: %v", err)
					}
					if err := os.WriteFile(replacementSentinel, []byte("keep\n"), 0o644); err != nil {
						t.Fatalf("write replacement sentinel: %v", err)
					}
					return
				}
				if err := os.WriteFile(entryPath, []byte("keep\n"), 0o644); err != nil {
					t.Fatalf("create replacement file: %v", err)
				}
			})
			if err == nil || !strings.Contains(err.Error(), "entry changed before removal") {
				t.Fatalf("cleanup did not reject entry replacement: %v", err)
			}
			if !replaced {
				t.Fatal("replacement hook did not run")
			}
			if kind == "directory" {
				if got, readErr := os.ReadFile(replacementSentinel); readErr != nil || string(got) != "keep\n" {
					t.Fatalf("replacement directory was modified: contents=%q err=%v", got, readErr)
				}
				return
			}
			if got, readErr := os.ReadFile(entryPath); readErr != nil || string(got) != "keep\n" {
				t.Fatalf("replacement file was modified: contents=%q err=%v", got, readErr)
			}
		})
	}
}

func TestCreateWorktreeRejectsFinalParentReplacementBeforePublication(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("identity-bound worktree publication is unsupported on windows")
	}

	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	workDir := t.TempDir()
	replacementSentinel := filepath.Join(workDir, "replacement-sentinel.txt")
	hookRan := false
	cache.worktreePublicationHook = func(stage, _ string) {
		if stage != "before-publication" || hookRan {
			return
		}
		hookRan = true
		if err := os.Rename(workDir, workDir+"-moved"); err != nil {
			t.Fatalf("rename final parent: %v", err)
		}
		if err := os.Mkdir(workDir, 0o755); err != nil {
			t.Fatalf("install replacement final parent: %v", err)
		}
		if err := os.WriteFile(replacementSentinel, []byte("keep\n"), 0o644); err != nil {
			t.Fatalf("write replacement parent sentinel: %v", err)
		}
	}

	_, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "agent",
		TaskID:      "parent-replacement",
	})
	if err == nil {
		t.Fatal("CreateWorktree succeeded after final parent replacement")
	}
	if !hookRan {
		t.Fatalf("publication hook did not run: %v", err)
	}
	if got, readErr := os.ReadFile(replacementSentinel); readErr != nil || string(got) != "keep\n" {
		t.Fatalf("replacement parent was modified: contents=%q err=%v create error=%v", got, readErr, err)
	}
	entries, readErr := os.ReadDir(workDir)
	if readErr != nil {
		t.Fatalf("read replacement parent: %v", readErr)
	}
	if len(entries) != 1 || entries[0].Name() != "replacement-sentinel.txt" {
		t.Fatalf("published into replacement parent: entries=%v create error=%v", entries, err)
	}
}

func TestCreateWorktreeRollsBackWhenFinalParentChangesAfterPublication(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("identity-bound worktree publication is unsupported on windows")
	}

	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	workDir := t.TempDir()
	movedWorkDir := workDir + "-moved"
	replacementSentinel := filepath.Join(workDir, "replacement-sentinel.txt")
	hookRan := false
	cache.worktreePublicationHook = func(stage, _ string) {
		if stage != "after-publication" || hookRan {
			return
		}
		hookRan = true
		if err := os.Rename(workDir, movedWorkDir); err != nil {
			t.Fatalf("rename published final parent: %v", err)
		}
		if err := os.Mkdir(workDir, 0o755); err != nil {
			t.Fatalf("install replacement final parent: %v", err)
		}
		if err := os.WriteFile(replacementSentinel, []byte("keep\n"), 0o644); err != nil {
			t.Fatalf("write replacement parent sentinel: %v", err)
		}
	}

	_, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "agent",
		TaskID:      "post-publish-parent-replacement",
	})
	if err == nil {
		t.Fatal("CreateWorktree succeeded after post-publication parent replacement")
	}
	if !hookRan {
		t.Fatalf("post-publication hook did not run: %v", err)
	}
	if got, readErr := os.ReadFile(replacementSentinel); readErr != nil || string(got) != "keep\n" {
		t.Fatalf("replacement parent was modified: contents=%q err=%v create error=%v", got, readErr, err)
	}
	dirName, nameErr := worktreeDirName(sourceRepo)
	if nameErr != nil {
		t.Fatalf("derive worktree directory name: %v", nameErr)
	}
	if _, statErr := os.Lstat(filepath.Join(movedWorkDir, dirName)); !os.IsNotExist(statErr) {
		t.Fatalf("provisional checkout remains in moved final parent: err=%v create error=%v", statErr, err)
	}
	barePath, lookupErr := cache.lookupPath(context.Background(), "ws-1", sourceRepo)
	if lookupErr != nil {
		t.Fatalf("lookup bare cache: %v", lookupErr)
	}
	branch := "refs/heads/agent/agent/post-pub"
	if out, refErr := exec.Command("git", "-C", barePath, "rev-parse", "--verify", "--quiet", branch).CombinedOutput(); refErr == nil {
		t.Fatalf("rolled-back branch remains: ref=%s output=%s", branch, out)
	}
	worktreeEntries, readErr := os.ReadDir(filepath.Join(barePath, "worktrees"))
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read bare worktree metadata: %v", readErr)
	}
	if len(worktreeEntries) != 0 {
		t.Fatalf("rolled-back worktree metadata remains: %v", worktreeEntries)
	}
	stagingEntries, readErr := os.ReadDir(filepath.Join(cache.root, ".worktree-staging"))
	if readErr != nil {
		t.Fatalf("read staging root after rollback: %v", readErr)
	}
	if len(stagingEntries) != 0 {
		t.Fatalf("rolled-back staging entries remain: %v", stagingEntries)
	}
}

func TestCreateWorktreeRejectsFinalTargetRaceBeforePublication(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("identity-bound worktree publication is unsupported on windows")
	}

	for _, replacementKind := range []string{"directory", "symlink"} {
		t.Run(replacementKind, func(t *testing.T) {
			sourceRepo := createTestRepo(t)
			cache := New(t.TempDir(), testLogger())
			if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
				t.Fatalf("sync failed: %v", err)
			}
			workDir := t.TempDir()
			dirName, err := worktreeDirName(sourceRepo)
			if err != nil {
				t.Fatalf("derive worktree directory name: %v", err)
			}
			worktreePath, err := canonicalWorktreeTarget(workDir, dirName)
			if err != nil {
				t.Fatalf("derive canonical worktree target: %v", err)
			}
			attackerTarget := t.TempDir()
			sentinel := filepath.Join(attackerTarget, "keep.txt")
			if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
				t.Fatalf("write attacker sentinel: %v", err)
			}
			cache.worktreePublicationHook = func(stage, _ string) {
				if stage != "before-publication" {
					return
				}
				if replacementKind == "directory" {
					if err := os.Mkdir(worktreePath, 0o755); err != nil {
						t.Fatalf("install target directory: %v", err)
					}
					if err := os.WriteFile(filepath.Join(worktreePath, "keep.txt"), []byte("keep\n"), 0o644); err != nil {
						t.Fatalf("write target directory sentinel: %v", err)
					}
					return
				}
				if err := os.Symlink(attackerTarget, worktreePath); err != nil {
					t.Fatalf("install target symlink: %v", err)
				}
			}

			_, err = cache.CreateWorktree(WorktreeParams{
				WorkspaceID: "ws-1",
				RepoURL:     sourceRepo,
				WorkDir:     workDir,
				AgentName:   "agent",
				TaskID:      "target-race-" + replacementKind,
			})
			if err == nil {
				t.Fatal("CreateWorktree succeeded after final target race")
			}
			if replacementKind == "symlink" {
				entries, readErr := os.ReadDir(attackerTarget)
				if readErr != nil || len(entries) != 1 || entries[0].Name() != "keep.txt" {
					t.Fatalf("symlink target was modified: entries=%v err=%v create error=%v", entries, readErr, err)
				}
			} else if got, readErr := os.ReadFile(filepath.Join(worktreePath, "keep.txt")); readErr != nil || string(got) != "keep\n" {
				t.Fatalf("target directory was modified: contents=%q err=%v create error=%v", got, readErr, err)
			}
		})
	}
}

func TestCreateWorktreeExcludesOpenCodeSkills(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	workDir := t.TempDir()
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "OpenCode",
		TaskID:      "opencode-exclude-test",
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	exclude := gitInfoExclude(t, result.Path)
	if !strings.Contains(exclude, ".opencode\n") {
		t.Fatalf("expected .git/info/exclude to contain .opencode, got:\n%s", exclude)
	}
	if strings.Contains(exclude, ".config/opencode") {
		t.Fatalf("expected .git/info/exclude to not contain stale .config/opencode, got:\n%s", exclude)
	}
}

// TestCreateWorktreeExcludesCodebuddySidecars is the regression guard for
// PR #5224's review feedback: once the daemon started writing
// .codebuddy/skills/ and CODEBUDDY.md into the task workdir (instead of
// reusing Claude's .claude/CLAUDE.md, which were already excluded), the
// repo-cache worktree needed the new CodeBuddy sidecar paths added to
// .git/info/exclude too — otherwise these daemon-injected files show up in
// `git status` and risk being committed by the agent.
func TestCreateWorktreeExcludesCodebuddySidecars(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	workDir := t.TempDir()
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "CodeBuddy",
		TaskID:      "codebuddy-exclude-test",
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	exclude := gitInfoExclude(t, result.Path)
	if !strings.Contains(exclude, ".codebuddy\n") {
		t.Fatalf("expected .git/info/exclude to contain .codebuddy, got:\n%s", exclude)
	}
	if !strings.Contains(exclude, "CODEBUDDY.md\n") {
		t.Fatalf("expected .git/info/exclude to contain CODEBUDDY.md, got:\n%s", exclude)
	}
}

func gitInfoExclude(t *testing.T, worktreePath string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--git-dir")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse --git-dir failed in %s: %v", worktreePath, err)
	}
	gitDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreePath, gitDir)
	}
	data, err := os.ReadFile(filepath.Join(gitDir, "info", "exclude"))
	if err != nil {
		t.Fatalf("read .git/info/exclude failed: %v", err)
	}
	return string(data)
}

func TestCreateWorktreeNotCached(t *testing.T) {
	t.Parallel()
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())

	_, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     "https://github.com/org/nonexistent",
		WorkDir:     t.TempDir(),
		AgentName:   "Agent",
		TaskID:      "test-task-id",
	})
	if err == nil {
		t.Fatal("expected error for uncached repo")
	}
	if !strings.Contains(err.Error(), "not found in cache") {
		t.Errorf("expected 'not found in cache' error, got: %v", err)
	}
}

func TestCreateWorktreeWithRequestedBranchRef(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	defaultHead := gitHead(t, sourceRepo)

	runGitAuthored(t, sourceRepo, "checkout", "-b", "review-branch")
	if err := os.WriteFile(filepath.Join(sourceRepo, "review.txt"), []byte("review\n"), 0o644); err != nil {
		t.Fatalf("write review file: %v", err)
	}
	runGitAuthored(t, sourceRepo, "add", ".")
	runGitAuthored(t, sourceRepo, "commit", "-m", "review branch commit")
	reviewHead := gitHead(t, sourceRepo)
	if reviewHead == defaultHead {
		t.Fatal("test setup failed: review branch did not advance")
	}

	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     t.TempDir(),
		Ref:         "review-branch",
		AgentName:   "Reviewer",
		TaskID:      "review-task-id",
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	if got := gitHead(t, result.Path); got != reviewHead {
		t.Fatalf("worktree HEAD = %s, want requested branch head %s", got, reviewHead)
	}
	if _, err := os.Stat(filepath.Join(result.Path, "review.txt")); err != nil {
		t.Fatalf("requested branch file missing: %v", err)
	}
}

func TestCreateWorktreeWithRequestedCommitRef(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	firstCommit := gitHead(t, sourceRepo)
	addEmptyCommit(t, sourceRepo, "second commit")

	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     t.TempDir(),
		Ref:         firstCommit,
		AgentName:   "Reviewer",
		TaskID:      "commit-task-id",
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	if got := gitHead(t, result.Path); got != firstCommit {
		t.Fatalf("worktree HEAD = %s, want requested commit %s", got, firstCommit)
	}
}

func TestCreateWorktreeWithRequestedTagRef(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	taggedCommit := gitHead(t, sourceRepo)
	runGitAuthored(t, sourceRepo, "tag", "v1")
	// Advance the default branch past the tag so worktree HEAD == taggedCommit
	// can only be true if the tag was actually resolved (vs falling back to
	// the default branch tip).
	addEmptyCommit(t, sourceRepo, "post-tag commit")

	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     t.TempDir(),
		Ref:         "v1",
		AgentName:   "Reviewer",
		TaskID:      "tag-task-id",
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	if got := gitHead(t, result.Path); got != taggedCommit {
		t.Fatalf("worktree HEAD = %s, want tagged commit %s", got, taggedCommit)
	}
}

func TestCreateWorktreeWithUnknownRequestedRef(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	_, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     t.TempDir(),
		Ref:         "missing-ref",
		AgentName:   "Reviewer",
		TaskID:      "missing-ref-task-id",
	})
	if err == nil {
		t.Fatal("expected unknown ref error")
	}
	if !strings.Contains(err.Error(), "cannot resolve requested ref") {
		t.Fatalf("expected requested ref error, got: %v", err)
	}
}

func trimLine(s string) string {
	return strings.TrimSpace(s)
}

// gitRefCommit resolves a git ref to its commit SHA in repoPath.
func gitRefCommit(t *testing.T, repoPath, ref string) string {
	t.Helper()
	if ref == "" {
		t.Fatalf("empty ref in %s", repoPath)
	}
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", ref)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse %s failed in %s: %v", ref, repoPath, err)
	}
	return strings.TrimSpace(string(out))
}

// addEmptyCommit adds an empty commit on the current branch of repoPath.
func addEmptyCommit(t *testing.T, repoPath, message string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repoPath, "commit", "--allow-empty", "-m", message)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed in %s: %s: %v", repoPath, out, err)
	}
}

// runGitAuthored runs `git -C repoPath <args...>` with the test author env set.
func runGitAuthored(t *testing.T, repoPath string, args ...string) {
	t.Helper()
	full := append([]string{"-C", repoPath}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %s: %v", args, repoPath, out, err)
	}
}

// TestCreateWorktreeFetchesDespiteAgentBranchOnRemote reproduces the original
// stale-cache bug. Under the legacy mirror refspec (+refs/heads/*:refs/heads/*)
// the sequence below would break on the second CreateWorktree because `git
// fetch` tries to overwrite refs/heads/agent/... which is locked by the first
// worktree, and the whole fetch aborts — silently discarding the main-branch
// update too. Under the modern remote-tracking refspec, fetched heads land in
// refs/remotes/origin/* and no longer collide with worktree-locked refs.
func TestCreateWorktreeFetchesDespiteAgentBranchOnRemote(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	// Capture the default branch BEFORE any detach/commit/checkout dance — we
	// need its name later to add new commits to the correct branch.
	defaultBranch := currentBranchName(t, sourceRepo)

	// Put source repo on a detached HEAD so the first worktree's agent branch
	// can be pushed back to it as a regular update (non-bare repos refuse to
	// push to the currently checked-out branch).
	runGitAuthored(t, sourceRepo, "checkout", "--detach", "HEAD")

	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	// First worktree creates refs/heads/agent/... inside the bare cache.
	workDir1 := t.TempDir()
	result1, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir1,
		AgentName:   "agent",
		TaskID:      "t1111111-0000-0000-0000-000000000000",
	})
	if err != nil {
		t.Fatalf("first CreateWorktree failed: %v", err)
	}

	// Simulate the agent pushing its branch back to origin (i.e. opening a PR).
	// Now sourceRepo has refs/heads/agent/... matching the locked ref in the
	// bare cache, which is the condition that triggered the legacy bug.
	if err := os.WriteFile(filepath.Join(result1.Path, "hello.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGitAuthored(t, result1.Path, "add", ".")
	runGitAuthored(t, result1.Path, "commit", "-m", "first task")
	runGitAuthored(t, result1.Path, "push", "origin", result1.BranchName)

	// Add a new commit to source's default branch (not the agent branch we
	// just pushed). Then re-detach so future pushes to other branches still work.
	runGitAuthored(t, sourceRepo, "checkout", defaultBranch)
	addEmptyCommit(t, sourceRepo, "new commit on default branch")
	sourceHead := gitRefCommit(t, sourceRepo, "refs/heads/"+defaultBranch)
	runGitAuthored(t, sourceRepo, "checkout", "--detach", "HEAD")

	// Second worktree: CreateWorktree fetches first. Under the legacy refspec
	// this fetch would fail (refusing to fetch into locked refs/heads/agent/...)
	// and the worktree would be based on the stale snapshot. Under the modern
	// refspec this succeeds and the new worktree sees sourceHead.
	workDir2 := t.TempDir()
	result2, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir2,
		AgentName:   "agent",
		TaskID:      "t2222222-0000-0000-0000-000000000000",
	})
	if err != nil {
		t.Fatalf("second CreateWorktree failed: %v", err)
	}

	if got := gitHead(t, result2.Path); got != sourceHead {
		t.Fatalf("second worktree HEAD = %s, want %s (remote default head after new commit)", got, sourceHead)
	}
}

// currentBranchName returns the branch name that HEAD points at in repoPath.
// Fails the test if HEAD is detached.
func currentBranchName(t *testing.T, repoPath string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoPath, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf("symbolic-ref --short HEAD in %s: %v", repoPath, err)
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		t.Fatalf("empty branch name in %s", repoPath)
	}
	return name
}

// TestEnsureRemoteTrackingLayoutMigratesLegacyCache verifies that a cache
// created with the legacy mirror refspec is migrated in place on next use:
// the refspec is rewritten to the modern remote-tracking layout and
// refs/remotes/origin/* gets backfilled so getRemoteDefaultBranch can resolve
// the remote default.
func TestEnsureRemoteTrackingLayoutMigratesLegacyCache(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	// Reset to the legacy mirror refspec to simulate a cache created by an
	// older version of the daemon.
	if err := setFetchRefspec(barePath, "+refs/heads/*:refs/heads/*"); err != nil {
		t.Fatalf("set legacy refspec: %v", err)
	}
	// Wipe any refs/remotes/origin/* that may have been populated by the initial clone.
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/HEAD").Run()
	if err := exec.Command("sh", "-c", "rm -rf '"+filepath.Join(barePath, "refs", "remotes")+"'").Run(); err != nil {
		t.Fatalf("wipe refs/remotes: %v", err)
	}

	// Sanity check: we've successfully forced the cache into legacy state.
	if cur, _ := readFetchRefspec(barePath); cur != "+refs/heads/*:refs/heads/*" {
		t.Fatalf("precondition failed: refspec is %q, want legacy mirror", cur)
	}

	// ensureRemoteTrackingLayout should migrate: rewrite refspec, backfill
	// refs/remotes/origin/*, and set origin HEAD.
	if err := ensureRemoteTrackingLayout(barePath); err != nil {
		t.Fatalf("ensureRemoteTrackingLayout failed: %v", err)
	}

	cur, err := readFetchRefspec(barePath)
	if err != nil {
		t.Fatalf("read refspec after migration: %v", err)
	}
	if cur != modernFetchRefspec {
		t.Errorf("refspec = %q, want %q", cur, modernFetchRefspec)
	}

	// getRemoteDefaultBranch should now return a refs/remotes/origin/<branch>.
	ref := getRemoteDefaultBranch(barePath)
	if !strings.HasPrefix(ref, "refs/remotes/origin/") {
		t.Errorf("getRemoteDefaultBranch = %q, want refs/remotes/origin/*", ref)
	}
}

// TestCreateWorktreePathCollisionDoesNotLeakBranch verifies the secondary bug
// fix: when the worktree path already exists as a non-worktree (e.g. a plain
// directory), createWorktree must fail cleanly without leaking a branch into
// the bare repo. Previously the "already exists" retry logic would
// misclassify path collisions as branch collisions and create a second
// timestamp-suffixed branch before hitting the same path error.
func TestCreateWorktreePathCollisionDoesNotLeakBranch(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	// Pre-create the target worktree path as a plain non-empty directory.
	workDir := t.TempDir()
	dirName := repoNameFromURL(sourceRepo)
	worktreePath := filepath.Join(workDir, dirName)
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("pre-create worktree path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreePath, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write stray file: %v", err)
	}

	_, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "agent",
		TaskID:      "t1111111-0000-0000-0000-000000000000",
	})
	if err == nil {
		t.Fatal("expected CreateWorktree to fail when path exists as non-worktree")
	}

	// No agent/* branches should have been created in the bare repo as a
	// side effect of the failed call.
	out, runErr := exec.Command("git", "-C", barePath, "for-each-ref", "--format=%(refname)", "refs/heads/agent").Output()
	if runErr != nil {
		t.Fatalf("for-each-ref failed: %v", runErr)
	}
	if leaked := strings.TrimSpace(string(out)); leaked != "" {
		t.Errorf("branch leaked into bare repo after path-collision failure:\n%s", leaked)
	}
}

// TestGetRemoteDefaultBranchScansForCustomDefault verifies fallback (3) of
// getRemoteDefaultBranch: when the cache has refs/remotes/origin/<custom>
// (e.g. develop, trunk) but no refs/remotes/origin/HEAD and no main/master,
// the function picks the custom branch instead of returning empty.
func TestGetRemoteDefaultBranchScansForCustomDefault(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	// Resolve the existing default branch's commit so we can repoint a
	// custom-named ref at it, then wipe the standard refs to force the
	// fallback path.
	existing := getRemoteDefaultBranch(barePath)
	if existing == "" {
		t.Fatalf("precondition: cache should have a default branch right after sync")
	}
	commit := gitRefCommit(t, barePath, existing)

	// Create refs/remotes/origin/develop pointing at that commit.
	runGitAuthored(t, barePath, "update-ref", "refs/remotes/origin/develop", commit)
	// Now wipe origin/HEAD (symbolic-ref -d removes the symref file itself)
	// and the common defaults so steps 1 and 2 of the resolver miss and we
	// fall through to the for-each-ref scan.
	_ = exec.Command("git", "-C", barePath, "symbolic-ref", "-d", "refs/remotes/origin/HEAD").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/main").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/master").Run()

	got := getRemoteDefaultBranch(barePath)
	if got != "refs/remotes/origin/develop" {
		t.Fatalf("getRemoteDefaultBranch = %q, want refs/remotes/origin/develop", got)
	}
}

// TestGetRemoteDefaultBranchFallsBackToBareHead verifies fallback (5):
// a legacy / migration-pending cache that has no refs/remotes/origin/* at all
// but still has its bare HEAD pointing at refs/heads/<branch> (the snapshot
// from the original mirror clone) should resolve to that local head instead
// of failing. This protects against transient backfill-fetch failures during
// the legacy → modern refspec migration. Gated on refs/remotes/origin/* being
// completely empty — with any modern remote-tracking refs present, the
// resolver refuses to reach back into the stale bare heads.
func TestGetRemoteDefaultBranchFallsBackToBareHead(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	// Force the cache into a state that mimics "legacy mirror clone whose
	// post-migration backfill fetch failed":
	//   - bare HEAD still points at refs/heads/<default>
	//   - refs/remotes/origin/* is empty
	if err := exec.Command("sh", "-c", "rm -rf '"+filepath.Join(barePath, "refs", "remotes")+"'").Run(); err != nil {
		t.Fatalf("wipe refs/remotes: %v", err)
	}

	// Sanity: origin/* is gone, HEAD is still a symbolic ref to refs/heads/*.
	if out, err := exec.Command("git", "-C", barePath, "for-each-ref", "refs/remotes/origin/").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		t.Fatalf("precondition failed: refs/remotes/origin/* should be empty, got %s", out)
	}

	got := getRemoteDefaultBranch(barePath)
	if !strings.HasPrefix(got, "refs/heads/") {
		t.Fatalf("getRemoteDefaultBranch = %q, want refs/heads/* fallback", got)
	}

	// And the resolved ref must actually exist — verifying bareHeadBranch's
	// rev-parse guard kicked in correctly.
	if err := exec.Command("git", "-C", barePath, "rev-parse", "--verify", got).Run(); err != nil {
		t.Fatalf("resolved ref %q does not exist: %v", got, err)
	}
}

// TestGitFetchRefreshesOriginHeadAfterDefaultChange verifies that an
// already-modern cache picks up a remote default-branch change. Plain `git
// fetch` never refreshes refs/remotes/origin/HEAD on its own, so without
// gitFetch's explicit `git remote set-head origin --auto` call the resolver
// would keep returning the original default branch forever after the
// upstream flipped (e.g. master → main on a long-lived repo). This guards
// against the "already-modern cache never refreshes origin/HEAD" regression.
func TestGitFetchRefreshesOriginHeadAfterDefaultChange(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	initialBranch := currentBranchName(t, sourceRepo)

	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	// Precondition: cache is already modern and origin/HEAD points at the
	// source's initial default branch.
	if got := getRemoteDefaultBranch(barePath); got != "refs/remotes/origin/"+initialBranch {
		t.Fatalf("precondition: getRemoteDefaultBranch = %q, want refs/remotes/origin/%s", got, initialBranch)
	}

	// Flip the source's default: create a new branch, commit on it, stay
	// checked out on it so the source's HEAD reflects the new default. A
	// subsequent `git ls-remote` against the source advertises this new
	// HEAD, which is what set-head --auto consumes.
	runGitAuthored(t, sourceRepo, "checkout", "-b", "new-default")
	addEmptyCommit(t, sourceRepo, "new-default commit")

	// Fetch via the cache's code path. Without the set-head call, origin/HEAD
	// would still point at the old default here.
	if err := gitFetch(barePath); err != nil {
		t.Fatalf("gitFetch failed: %v", err)
	}

	// refs/remotes/origin/HEAD must now point at the new default branch.
	out, err := exec.Command("git", "-C", barePath, "symbolic-ref", "refs/remotes/origin/HEAD").Output()
	if err != nil {
		t.Fatalf("symbolic-ref origin/HEAD after fetch: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "refs/remotes/origin/new-default" {
		t.Fatalf("origin/HEAD after fetch = %q, want refs/remotes/origin/new-default", got)
	}

	// And getRemoteDefaultBranch must resolve through step 1 (verified
	// origin/HEAD) to the new default — not through step 2 where origin/main
	// or origin/master could accidentally match the old branch.
	if got := getRemoteDefaultBranch(barePath); got != "refs/remotes/origin/new-default" {
		t.Fatalf("getRemoteDefaultBranch after fetch = %q, want refs/remotes/origin/new-default", got)
	}
}

// TestGetRemoteDefaultBranchUsesBareHeadHintForCustomDefault verifies step 3
// of the resolver: when the cache has a non-standard default branch name
// (trunk, develop, …) and `git remote set-head origin --auto` didn't
// populate refs/remotes/origin/HEAD, the resolver must use the bare repo's
// own HEAD as a hint to pick refs/remotes/origin/<same name> — NOT fall
// through to a refname-order scan that would pick the wrong branch.
func TestGetRemoteDefaultBranchUsesBareHeadHintForCustomDefault(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	existing := getRemoteDefaultBranch(barePath)
	if existing == "" {
		t.Fatalf("precondition: cache should have a default branch right after sync")
	}
	commit := gitRefCommit(t, barePath, existing)

	// Simulate a custom default branch: create refs/heads/trunk in the bare
	// repo and point HEAD at it. `git clone --bare` would do the equivalent
	// when the remote's default was "trunk", so this matches real-world
	// state for such remotes.
	runGitAuthored(t, barePath, "update-ref", "refs/heads/trunk", commit)
	runGitAuthored(t, barePath, "symbolic-ref", "HEAD", "refs/heads/trunk")

	// Populate two refs/remotes/origin/* entries. "feature-alpha" is
	// alphabetically earlier than "trunk" — a refname-order scan (the old
	// bug) would return feature-alpha, not trunk.
	runGitAuthored(t, barePath, "update-ref", "refs/remotes/origin/trunk", commit)
	runGitAuthored(t, barePath, "update-ref", "refs/remotes/origin/feature-alpha", commit)

	// Knock out the ahead-of-step-3 fallbacks so resolution must rely on
	// the bare-HEAD hint.
	_ = exec.Command("git", "-C", barePath, "symbolic-ref", "-d", "refs/remotes/origin/HEAD").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/main").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/master").Run()

	got := getRemoteDefaultBranch(barePath)
	if got != "refs/remotes/origin/trunk" {
		t.Fatalf("getRemoteDefaultBranch = %q, want refs/remotes/origin/trunk (via bare-HEAD hint)", got)
	}
}

// TestCreateWorktreeInstallsCoAuthoredByHook verifies that CreateWorktree
// installs a prepare-commit-msg hook that appends a Co-authored-by trailer
// for the Multica Agent to every commit made in the worktree.
func TestCreateWorktreeInstallsCoAuthoredByHook(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	workDir := t.TempDir()
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir,
		AgentName:           "Test Agent",
		TaskID:              "a1b2c3d4-0000-0000-0000-000000000000",
		CoAuthoredByEnabled: true,
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	// Make a commit in the worktree and verify the hook appends the trailer.
	if err := os.WriteFile(filepath.Join(result.Path, "test.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	runGitAuthored(t, result.Path, "add", ".")
	runGitAuthored(t, result.Path, "commit", "-m", "test commit")

	// Read the commit message.
	out, err := exec.Command("git", "-C", result.Path, "log", "-1", "--format=%B").Output()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	commitMsg := string(out)
	expectedTrailer := "Co-authored-by: multica-agent <github@multica.ai>"
	if !strings.Contains(commitMsg, expectedTrailer) {
		t.Errorf("commit message missing Co-authored-by trailer.\ngot:\n%s", commitMsg)
	}
}

// TestCoAuthoredByHookIdempotent verifies that the hook does not add a
// duplicate Co-authored-by trailer if one is already present in the message.
func TestCoAuthoredByHookIdempotent(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	workDir := t.TempDir()
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir,
		AgentName:           "Test Agent",
		TaskID:              "b2c3d4e5-0000-0000-0000-000000000000",
		CoAuthoredByEnabled: true,
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	// Commit with the trailer already in the message.
	trailer := "Co-authored-by: multica-agent <github@multica.ai>"
	if err := os.WriteFile(filepath.Join(result.Path, "test.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	runGitAuthored(t, result.Path, "add", ".")
	runGitAuthored(t, result.Path, "commit", "-m", "test commit\n\n"+trailer)

	out, err := exec.Command("git", "-C", result.Path, "log", "-1", "--format=%B").Output()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	commitMsg := string(out)

	// Count occurrences — should appear exactly once.
	count := strings.Count(commitMsg, trailer)
	if count != 1 {
		t.Errorf("expected exactly 1 Co-authored-by trailer, found %d.\ngot:\n%s", count, commitMsg)
	}
}

// TestCreateWorktreeRemovesCoAuthoredByHookWhenDisabled verifies the toggle-off
// path: a bare cache that already carries the Multica prepare-commit-msg hook
// (e.g. from a prior worktree created with the setting on) must drop the hook
// when the next CreateWorktree call passes CoAuthoredByEnabled=false.
// Otherwise commits keep getting the trailer even after the user disables the
// workspace setting.
func TestCreateWorktreeRemovesCoAuthoredByHookWhenDisabled(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	// First worktree: setting enabled → hook installed in the bare cache's
	// shared hooks dir.
	workDir1 := t.TempDir()
	if _, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir1,
		AgentName:           "Test Agent",
		TaskID:              "11111111-0000-0000-0000-000000000000",
		CoAuthoredByEnabled: true,
	}); err != nil {
		t.Fatalf("CreateWorktree (enabled) failed: %v", err)
	}

	barePath := cache.Lookup("ws-1", sourceRepo)
	hookPath := filepath.Join(barePath, "hooks", "prepare-commit-msg")
	if _, err := os.Stat(hookPath); err != nil {
		t.Fatalf("precondition: expected hook to be installed at %s: %v", hookPath, err)
	}

	// Second worktree on the same bare cache: setting disabled → hook must
	// be removed and a commit in the new worktree must NOT carry the
	// trailer.
	workDir2 := t.TempDir()
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir2,
		AgentName:           "Test Agent",
		TaskID:              "22222222-0000-0000-0000-000000000000",
		CoAuthoredByEnabled: false,
	})
	if err != nil {
		t.Fatalf("CreateWorktree (disabled) failed: %v", err)
	}

	if _, err := os.Stat(hookPath); !os.IsNotExist(err) {
		t.Errorf("expected hook to be removed at %s, stat err=%v", hookPath, err)
	}

	if err := os.WriteFile(filepath.Join(result.Path, "test.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	runGitAuthored(t, result.Path, "add", ".")
	runGitAuthored(t, result.Path, "commit", "-m", "test commit")

	out, err := exec.Command("git", "-C", result.Path, "log", "-1", "--format=%B").Output()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	commitMsg := string(out)
	if strings.Contains(commitMsg, "Co-authored-by: multica-agent") {
		t.Errorf("commit unexpectedly carries the Co-authored-by trailer with setting disabled.\ngot:\n%s", commitMsg)
	}
}

// TestCreateWorktreeRemovesLegacyCoAuthoredByHook verifies the migration
// path: bare clones already on disk from previous daemon versions carry a
// prepare-commit-msg hook that does NOT include the multicaHookMarker
// sentinel — only the older `# Installed by the Multica daemon.` comment.
// Toggling the workspace setting off must still remove those legacy hooks,
// otherwise users who flip the toggle in production keep seeing the trailer
// indefinitely (the exact bug reported in MUL-1704).
func TestCreateWorktreeRemovesLegacyCoAuthoredByHook(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	// Seed the bare cache with the exact hook content shipped by the
	// previous daemon release (no multicaHookMarker line). Keeping a
	// verbatim copy here means the test fails if recognition logic ever
	// drifts away from what production hosts actually have on disk.
	const legacyHook = `#!/bin/sh
# Multica: add Co-authored-by trailer for the Multica Agent.
# Installed by the Multica daemon. Do not edit — it will be overwritten.

COMMIT_MSG_FILE="$1"
COMMIT_SOURCE="$2"

# Skip merge and squash commits.
case "$COMMIT_SOURCE" in
  merge|squash) exit 0 ;;
esac

TRAILER="Co-authored-by: multica-agent <github@multica.ai>"

# Don't add if already present.
if grep -qF "$TRAILER" "$COMMIT_MSG_FILE"; then
  exit 0
fi

# Use git interpret-trailers for proper formatting.
git interpret-trailers --in-place --trailer "$TRAILER" "$COMMIT_MSG_FILE"
`

	barePath := cache.Lookup("ws-1", sourceRepo)
	hooksDir := filepath.Join(barePath, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("create hooks dir: %v", err)
	}
	hookPath := filepath.Join(hooksDir, "prepare-commit-msg")
	if err := os.WriteFile(hookPath, []byte(legacyHook), 0o755); err != nil {
		t.Fatalf("seed legacy hook: %v", err)
	}

	workDir := t.TempDir()
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir,
		AgentName:           "Test Agent",
		TaskID:              "44444444-0000-0000-0000-000000000000",
		CoAuthoredByEnabled: false,
	})
	if err != nil {
		t.Fatalf("CreateWorktree (disabled) failed: %v", err)
	}

	if _, err := os.Stat(hookPath); !os.IsNotExist(err) {
		t.Errorf("expected legacy hook to be removed at %s, stat err=%v", hookPath, err)
	}

	if err := os.WriteFile(filepath.Join(result.Path, "test.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	runGitAuthored(t, result.Path, "add", ".")
	runGitAuthored(t, result.Path, "commit", "-m", "test commit")

	out, err := exec.Command("git", "-C", result.Path, "log", "-1", "--format=%B").Output()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	if commitMsg := string(out); strings.Contains(commitMsg, "Co-authored-by: multica-agent") {
		t.Errorf("commit unexpectedly carries the Co-authored-by trailer after legacy hook removal.\ngot:\n%s", commitMsg)
	}
}

// TestRemoveCoAuthoredByHookPreservesUserHook verifies that the disable path
// only deletes hooks installed by the daemon. A prepare-commit-msg hook
// without the Multica marker (e.g. one a user added manually) must be left
// untouched even when CoAuthoredByEnabled=false.
func TestRemoveCoAuthoredByHookPreservesUserHook(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	barePath := cache.Lookup("ws-1", sourceRepo)
	hooksDir := filepath.Join(barePath, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("create hooks dir: %v", err)
	}
	hookPath := filepath.Join(hooksDir, "prepare-commit-msg")
	userHook := "#!/bin/sh\n# user hook, not Multica\nexit 0\n"
	if err := os.WriteFile(hookPath, []byte(userHook), 0o755); err != nil {
		t.Fatalf("seed user hook: %v", err)
	}

	workDir := t.TempDir()
	if _, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir,
		AgentName:           "Test Agent",
		TaskID:              "33333333-0000-0000-0000-000000000000",
		CoAuthoredByEnabled: false,
	}); err != nil {
		t.Fatalf("CreateWorktree (disabled) failed: %v", err)
	}

	got, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("user hook unexpectedly removed: %v", err)
	}
	if string(got) != userHook {
		t.Errorf("user hook contents changed.\nwant:\n%s\ngot:\n%s", userHook, string(got))
	}
}

// TestGetRemoteDefaultBranchAmbiguousOriginReturnsEmpty verifies step 4's
// safe-scan gating: when the cache has multiple refs/remotes/origin/*
// entries, none match the common defaults, and none match the bare HEAD
// either, the resolver must refuse to guess and return "". The caller
// surfaces this as a hard error instead of silently basing new agent work
// on an arbitrary refname-order-first candidate.
func TestGetRemoteDefaultBranchAmbiguousOriginReturnsEmpty(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	existing := getRemoteDefaultBranch(barePath)
	if existing == "" {
		t.Fatalf("precondition: cache should have a default branch right after sync")
	}
	commit := gitRefCommit(t, barePath, existing)

	// Populate two unrelated origin branches (none of which match any of
	// the step 1-3 fallbacks).
	runGitAuthored(t, barePath, "update-ref", "refs/remotes/origin/feature-a", commit)
	runGitAuthored(t, barePath, "update-ref", "refs/remotes/origin/feature-b", commit)

	// Wipe every ref a step 1-3 fallback could pick up:
	//   step 1: origin/HEAD
	//   step 2: origin/main, origin/master
	//   step 3: the origin/<bareHEAD-name> bridge
	_ = exec.Command("git", "-C", barePath, "symbolic-ref", "-d", "refs/remotes/origin/HEAD").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/main").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/master").Run()
	if bareRef := bareHeadBranch(barePath); bareRef != "" {
		sameName := strings.TrimPrefix(bareRef, "refs/heads/")
		_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/"+sameName).Run()
	}

	got := getRemoteDefaultBranch(barePath)
	if got != "" {
		t.Fatalf("getRemoteDefaultBranch = %q, want \"\" (ambiguous origin/* must not guess)", got)
	}
}
