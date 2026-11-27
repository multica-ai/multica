package repocache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinkedExcludesAreScopedToManagedCheckout(t *testing.T) {
	identityTestEnvironment(t)
	global := filepath.Join(t.TempDir(), "user ignore")
	writeExcludeFixture(t, global, "global.tmp") // No trailing newline.
	gitIdentityCommand(t, "config", "--global", "core.excludesFile", global)

	f := newExistingCheckoutFixture(t, false)
	bare := f.cache.Lookup("ws-1", f.source)
	shared := filepath.Join(bare, "info", "exclude")
	writeExcludeFixture(t, shared, "/shared.tmp\n")
	sibling := filepath.Join(t.TempDir(), "sibling")
	gitIdentityCommand(t, "-C", bare, "worktree", "add", "-b", "unmanaged", sibling)
	managed := f.checkout(t, firstTaskID, false).Path
	fixture := createTestRepo(t)

	for _, name := range []string{"AGENTS.md", "CLAUDE.md", "nested/AGENTS.md", "nested/CLAUDE.md", "global.tmp", "shared.tmp"} {
		writeExcludeFixture(t, filepath.Join(managed, filepath.FromSlash(name)), "fixture")
	}
	for _, dir := range []string{sibling, fixture} {
		writeExcludeFixture(t, filepath.Join(dir, "AGENTS.md"), "legitimate root fixture")
		gitIdentityCommand(t, "-C", dir, "add", "-A")
		if staged := gitIdentityCommand(t, "-C", dir, "diff", "--cached", "--name-only"); staged != "AGENTS.md" {
			t.Fatalf("unexpected staged files in %s: %q", dir, staged)
		}
	}
	gitIdentityCommand(t, "-C", managed, "add", "-A")
	if staged := gitIdentityCommand(t, "-C", managed, "diff", "--cached", "--name-only"); staged != "nested/AGENTS.md\nnested/CLAUDE.md" {
		t.Fatalf("unexpected staged files in managed checkout: %q", staged)
	}
	if got := readFile(t, shared); got != "/shared.tmp\n" {
		t.Fatalf("cache's shared excludes changed: %q", got)
	}
	if got := gitIdentityCommand(t, "-C", bare, "rev-parse", "--is-bare-repository"); got != "true" {
		t.Fatalf("existing cache stopped being bare: %q", got)
	}
}

func TestLinkedExcludesReusePreservesUserSettings(t *testing.T) {
	identityTestEnvironment(t)
	f := newExistingCheckoutFixture(t, false)
	managed := f.checkout(t, firstTaskID, false).Path
	user := filepath.Join(t.TempDir(), "override")
	writeExcludeFixture(t, user, "first.tmp\n")
	gitIdentityCommand(t, "-C", managed, "config", "--worktree", "core.excludesFile", user)
	f.checkout(t, secondTaskID, false)
	assertCheckoutIgnored(t, managed, true, "first.tmp", "AGENTS.md")

	writeExcludeFixture(t, user, "second.tmp\n")
	f.checkout(t, secondTaskID, false)
	assertCheckoutIgnored(t, managed, true, "second.tmp", "AGENTS.md")
	assertCheckoutIgnored(t, managed, false, "first.tmp", "nested/AGENTS.md")
	dir := gitIdentityCommand(t, "-C", managed, "rev-parse", "--absolute-git-dir")
	configPath := filepath.Join(dir, "config.worktree")
	config := readFile(t, configPath)
	saved := gitIdentityCommand(t, "config", "--file", configPath, "--no-includes", "--get", "core.excludesFile")
	if strings.Count(config, worktreeExcludesInclude) != 1 || saved != user {
		t.Fatalf("expected one managed include and retained user config:\n%s", config)
	}
}

func TestLinkedExcludesRetainDefaultIgnoreAndTrackedRootFile(t *testing.T) {
	identityTestEnvironment(t)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeExcludeFixture(t, filepath.Join(xdg, "git", "ignore"), "/default.tmp\n")
	f := newExistingCheckoutFixture(t, false)
	writeExcludeFixture(t, filepath.Join(f.source, "AGENTS.md"), "tracked")
	gitIdentityCommand(t, "-C", f.source, "add", "AGENTS.md")
	gitIdentityCommand(t, "-C", f.source, "commit", "-m", "track legitimate root instructions")

	managed := f.checkout(t, firstTaskID, false).Path
	writeExcludeFixture(t, filepath.Join(managed, "AGENTS.md"), "edited")
	writeExcludeFixture(t, filepath.Join(managed, "CLAUDE.md"), "runtime sidecar")
	writeExcludeFixture(t, filepath.Join(managed, "default.tmp"), "user ignore")
	gitIdentityCommand(t, "-C", managed, "add", "-A")
	if staged := gitIdentityCommand(t, "-C", managed, "diff", "--cached", "--name-only"); staged != "AGENTS.md" {
		t.Fatalf("unexpected staged files in managed checkout: %q", staged)
	}
}

func writeExcludeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
