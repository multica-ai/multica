package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// prepareWithSharedHome points CODEX_HOME at sharedHome and prepares a fresh
// per-task codex-home, returning its path. Cannot be used with t.Parallel()
// because it sets an env var.
func prepareWithSharedHome(t *testing.T, sharedHome string) string {
	t.Helper()
	t.Setenv("CODEX_HOME", sharedHome)
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if err := prepareCodexHome(codexHome, discardLogger()); err != nil {
		t.Fatalf("prepareCodexHome: %v", err)
	}
	return codexHome
}

func readThrough(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// T1: prepare exposes both hooks.json and the hooks/ helper scripts.
func TestPrepareCodexHomeExposesHooks(t *testing.T) {
	sharedHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(sharedHome, "hooks.json"), []byte(`{"hooks":true}`), 0o644); err != nil {
		t.Fatalf("write shared hooks.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sharedHome, "hooks"), 0o755); err != nil {
		t.Fatalf("create shared hooks dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sharedHome, "hooks", "notify.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("write shared hook helper: %v", err)
	}

	codexHome := prepareWithSharedHome(t, sharedHome)

	if got := readThrough(t, filepath.Join(codexHome, "hooks.json")); got != `{"hooks":true}` {
		t.Fatalf("per-task hooks.json content = %q", got)
	}
	if got := readThrough(t, filepath.Join(codexHome, "hooks", "notify.sh")); !strings.Contains(got, "echo hi") {
		t.Fatalf("per-task hooks/notify.sh content = %q", got)
	}
}

// T3: a shared home with no hooks must not create empty per-task resources.
func TestPrepareCodexHomeSkipsMissingHooks(t *testing.T) {
	sharedHome := t.TempDir()

	codexHome := prepareWithSharedHome(t, sharedHome)

	if _, err := os.Lstat(filepath.Join(codexHome, "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no per-task hooks.json, lstat err = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(codexHome, "hooks")); !os.IsNotExist(err) {
		t.Fatalf("expected no per-task hooks/ dir, lstat err = %v", err)
	}
}

// T2: once the shared hooks are removed, a second prepare (workspace reuse)
// clears the stale per-task hooks.json / hooks/ residue.
func TestPrepareCodexHomeClearsStaleHooksOnReuse(t *testing.T) {
	sharedHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(sharedHome, "hooks.json"), []byte(`{"hooks":true}`), 0o644); err != nil {
		t.Fatalf("write shared hooks.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sharedHome, "hooks"), 0o755); err != nil {
		t.Fatalf("create shared hooks dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sharedHome, "hooks", "notify.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write shared hook helper: %v", err)
	}

	t.Setenv("CODEX_HOME", sharedHome)
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if err := prepareCodexHome(codexHome, discardLogger()); err != nil {
		t.Fatalf("first prepareCodexHome: %v", err)
	}
	// Sanity: exposed after first prepare.
	if _, err := os.Lstat(filepath.Join(codexHome, "hooks.json")); err != nil {
		t.Fatalf("hooks.json should exist after first prepare: %v", err)
	}

	// Shared hooks removed (user deleted them between runs).
	if err := os.Remove(filepath.Join(sharedHome, "hooks.json")); err != nil {
		t.Fatalf("remove shared hooks.json: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(sharedHome, "hooks")); err != nil {
		t.Fatalf("remove shared hooks dir: %v", err)
	}

	if err := prepareCodexHome(codexHome, discardLogger()); err != nil {
		t.Fatalf("second prepareCodexHome: %v", err)
	}

	if _, err := os.Lstat(filepath.Join(codexHome, "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("stale per-task hooks.json should be cleared, lstat err = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(codexHome, "hooks")); !os.IsNotExist(err) {
		t.Fatalf("stale per-task hooks/ should be cleared, lstat err = %v", err)
	}
	// The shared hooks dir must NOT have been deleted through a link (D5).
	if _, err := os.Lstat(sharedHome); err != nil {
		t.Fatalf("shared home must survive stale cleanup: %v", err)
	}
}

// T4 (D2, blocking): a non-ENOENT stat error on the shared source fails closed —
// the stale per-task resource is cleared and the error is surfaced, so a
// removed/unreadable source can never leave an old hook loadable.
func TestOptionalSymlinkFailsClosedOnStatError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// A regular file standing where a directory component is expected makes
	// os.Stat(src) return ENOTDIR (not ENOENT).
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	t.Run("file", func(t *testing.T) {
		src := filepath.Join(blocker, "hooks.json") // stat → ENOTDIR
		dst := filepath.Join(dir, "task-hooks.json")
		if err := os.WriteFile(dst, []byte("stale"), 0o644); err != nil {
			t.Fatalf("seed stale dst: %v", err)
		}
		err := ensureOptionalFileSymlink(src, dst)
		if err == nil {
			t.Fatal("expected a fail-closed error on non-ENOENT stat")
		}
		if _, statErr := os.Lstat(dst); !os.IsNotExist(statErr) {
			t.Fatalf("stale dst should be cleared even on stat error, lstat err = %v", statErr)
		}
	})

	t.Run("dir", func(t *testing.T) {
		src := filepath.Join(blocker, "hooks") // stat → ENOTDIR
		dst := filepath.Join(dir, "task-hooks")
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatalf("seed stale dst dir: %v", err)
		}
		err := ensureExistingDirSymlink(src, dst)
		if err == nil {
			t.Fatal("expected a fail-closed error on non-ENOENT stat")
		}
		if _, statErr := os.Lstat(dst); !os.IsNotExist(statErr) {
			t.Fatalf("stale dst dir should be cleared even on stat error, lstat err = %v", statErr)
		}
	})
}

// T11: the isolation boundary is not widened — only hooks.json / hooks/ are
// added, and an unrelated file in the shared home is never exposed.
func TestPrepareCodexHomeDoesNotExposeWholeCodexHome(t *testing.T) {
	sharedHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(sharedHome, "hooks.json"), []byte(`{"hooks":true}`), 0o644); err != nil {
		t.Fatalf("write shared hooks.json: %v", err)
	}
	// A private, non-hook file that must never leak into the per-task home.
	if err := os.WriteFile(filepath.Join(sharedHome, "secret.txt"), []byte("flux-cookie"), 0o600); err != nil {
		t.Fatalf("write shared secret: %v", err)
	}

	codexHome := prepareWithSharedHome(t, sharedHome)

	// hooks.json is exposed; the arbitrary secret is not.
	if _, err := os.Lstat(filepath.Join(codexHome, "hooks.json")); err != nil {
		t.Fatalf("hooks.json should be exposed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(codexHome, "secret.txt")); !os.IsNotExist(err) {
		t.Fatalf("non-hook shared file must not be exposed, lstat err = %v", err)
	}
	// The per-task home must not be a link to the whole shared home.
	if fi, err := os.Lstat(codexHome); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("per-task codex-home must not be a symlink to the shared home")
	}
}

// T12 (D4, blocking): the mapped hook-trust block coexists with the sandbox,
// multi-agent and memory managed blocks, and none of them duplicate across a
// reuse (second prepare).
func TestPrepareCodexHomeHookTrustCoexistsWithManagedBlocks(t *testing.T) {
	sharedHome := t.TempDir()
	sharedHooksPath := filepath.Join(sharedHome, "hooks.json")
	if err := os.WriteFile(sharedHooksPath, []byte(`{"hooks":true}`), 0o644); err != nil {
		t.Fatalf("write shared hooks.json: %v", err)
	}
	sharedConfig := hookStateHeader(sharedHooksPath, ":pre_tool_use:0:0") + "\ntrusted_hash = \"sha256:coexist\"\n"
	if err := os.WriteFile(filepath.Join(sharedHome, "config.toml"), []byte(sharedConfig), 0o644); err != nil {
		t.Fatalf("write shared config.toml: %v", err)
	}

	t.Setenv("CODEX_HOME", sharedHome)
	codexHome := filepath.Join(t.TempDir(), "codex-home")

	assertOnce := func(t *testing.T, content, marker string) {
		t.Helper()
		if n := strings.Count(content, marker); n != 1 {
			t.Fatalf("expected exactly one %q, got %d:\n%s", marker, n, content)
		}
	}

	check := func(t *testing.T, label string) {
		if err := prepareCodexHome(codexHome, discardLogger()); err != nil {
			t.Fatalf("%s prepareCodexHome: %v", label, err)
		}
		content := readThrough(t, filepath.Join(codexHome, "config.toml"))
		assertOnce(t, content, multicaManagedBeginMarker)          // sandbox
		assertOnce(t, content, multicaMultiAgentBeginMarker)       // multi-agent
		assertOnce(t, content, multicaMemoryFeatureBeginMarker)    // memory feature
		assertOnce(t, content, multicaMemoryConfigBeginMarker)     // memory config
		taskHooksPath := filepath.Join(codexHome, "hooks.json")
		assertOnce(t, content, hookStateHeader(taskHooksPath, ":pre_tool_use:0:0")) // mapped hook trust
		assertConfigContains(t, content, `trusted_hash = "sha256:coexist"`)
	}

	check(t, "first")
	check(t, "reuse")
}
