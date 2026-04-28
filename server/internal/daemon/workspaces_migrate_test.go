package daemon

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func newSilentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// withFakeHome points os.UserHomeDir() at a temp dir for the duration of the
// subtest by setting HOME (Unix) and USERPROFILE (Windows).
func withFakeHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	// MULTICA_WORKSPACES_ROOT must be unset for the migration to run.
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")
	return dir
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestMigrateLegacyWorkspacesRoot_MovesContentsAndRemovesLegacyDir(t *testing.T) {
	home := withFakeHome(t)
	profile := "desktop-api.multica.ai"

	legacy := filepath.Join(home, "multica_workspaces_"+profile)
	target := filepath.Join(home, "multica_workspaces")
	mustWriteFile(t, filepath.Join(legacy, "ws-uuid-1", "task-abc", "workdir", "file.txt"), "hello")
	mustWriteFile(t, filepath.Join(legacy, ".repos", "github.com", "foo", "bar", "HEAD"), "ref: x")

	cfg := Config{
		Profile:        profile,
		WorkspacesRoot: target, // matches what LoadConfig would produce when no env/override
	}
	MigrateLegacyWorkspacesRoot(cfg, newSilentLogger())

	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("expected legacy dir to be removed, got err=%v", err)
	}
	moved := filepath.Join(target, "ws-uuid-1", "task-abc", "workdir", "file.txt")
	if b, err := os.ReadFile(moved); err != nil || string(b) != "hello" {
		t.Fatalf("expected moved file with content 'hello', got %q err=%v", string(b), err)
	}
	if _, err := os.Stat(filepath.Join(target, ".repos", "github.com", "foo", "bar", "HEAD")); err != nil {
		t.Fatalf("expected .repos cache to be moved, got err=%v", err)
	}
}

func TestMigrateLegacyWorkspacesRoot_SkipsWhenProfileEmpty(t *testing.T) {
	home := withFakeHome(t)
	// A "_" suffix file shouldn't be touched when no profile is in play.
	stray := filepath.Join(home, "multica_workspaces_some-profile", "ws", "task", "marker")
	mustWriteFile(t, stray, "x")

	cfg := Config{
		Profile:        "",
		WorkspacesRoot: filepath.Join(home, "multica_workspaces"),
	}
	MigrateLegacyWorkspacesRoot(cfg, newSilentLogger())

	if _, err := os.Stat(stray); err != nil {
		t.Fatalf("legacy file should be untouched when profile is empty: %v", err)
	}
}

func TestMigrateLegacyWorkspacesRoot_SkipsWhenEnvOverrideSet(t *testing.T) {
	home := withFakeHome(t)
	t.Setenv("MULTICA_WORKSPACES_ROOT", filepath.Join(home, "custom"))

	legacy := filepath.Join(home, "multica_workspaces_dev-foo")
	mustWriteFile(t, filepath.Join(legacy, "ws", "task", "marker"), "x")

	cfg := Config{
		Profile:        "dev-foo",
		WorkspacesRoot: filepath.Join(home, "custom"),
	}
	MigrateLegacyWorkspacesRoot(cfg, newSilentLogger())

	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy dir should be untouched when env override is set: %v", err)
	}
}

func TestMigrateLegacyWorkspacesRoot_SkipsWhenWorkspacesRootIsNotDefault(t *testing.T) {
	home := withFakeHome(t)

	legacy := filepath.Join(home, "multica_workspaces_desktop-foo")
	mustWriteFile(t, filepath.Join(legacy, "ws", "task", "marker"), "x")

	cfg := Config{
		Profile:        "desktop-foo",
		WorkspacesRoot: filepath.Join(home, "elsewhere"),
	}
	MigrateLegacyWorkspacesRoot(cfg, newSilentLogger())

	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy dir should be untouched when workspaces root is overridden: %v", err)
	}
}

func TestMigrateLegacyWorkspacesRoot_NoLegacyDirNoop(t *testing.T) {
	home := withFakeHome(t)

	cfg := Config{
		Profile:        "desktop-foo",
		WorkspacesRoot: filepath.Join(home, "multica_workspaces"),
	}
	// Should not panic, should not create the target dir.
	MigrateLegacyWorkspacesRoot(cfg, newSilentLogger())

	if _, err := os.Stat(filepath.Join(home, "multica_workspaces")); !os.IsNotExist(err) {
		t.Fatalf("target dir should not be eagerly created when nothing to migrate, err=%v", err)
	}
}

func TestMigrateLegacyWorkspacesRoot_PreservesExistingTargetEntry(t *testing.T) {
	home := withFakeHome(t)
	profile := "desktop-foo"

	legacy := filepath.Join(home, "multica_workspaces_"+profile)
	target := filepath.Join(home, "multica_workspaces")

	// Same workspace UUID exists in both. The migration must not overwrite the
	// target — it must leave the legacy copy in place for manual reconciliation.
	mustWriteFile(t, filepath.Join(legacy, "ws-uuid", "task-x", "marker"), "from-legacy")
	mustWriteFile(t, filepath.Join(target, "ws-uuid", "task-y", "marker"), "from-target")
	// Distinct entry that should be moved cleanly.
	mustWriteFile(t, filepath.Join(legacy, "ws-uuid-other", "task-z", "marker"), "moved")

	cfg := Config{
		Profile:        profile,
		WorkspacesRoot: target,
	}
	MigrateLegacyWorkspacesRoot(cfg, newSilentLogger())

	// Conflicting entry stays in legacy; target version preserved.
	if b, err := os.ReadFile(filepath.Join(target, "ws-uuid", "task-y", "marker")); err != nil || string(b) != "from-target" {
		t.Fatalf("target entry should be preserved, got %q err=%v", string(b), err)
	}
	if b, err := os.ReadFile(filepath.Join(legacy, "ws-uuid", "task-x", "marker")); err != nil || string(b) != "from-legacy" {
		t.Fatalf("conflicting legacy entry should remain in place, got %q err=%v", string(b), err)
	}

	// Distinct entry was moved.
	if _, err := os.Stat(filepath.Join(legacy, "ws-uuid-other")); !os.IsNotExist(err) {
		t.Fatalf("non-conflicting legacy entry should be moved, err=%v", err)
	}
	if b, err := os.ReadFile(filepath.Join(target, "ws-uuid-other", "task-z", "marker")); err != nil || string(b) != "moved" {
		t.Fatalf("non-conflicting entry should arrive at target, got %q err=%v", string(b), err)
	}

	// Legacy root retained because it still has a residual entry.
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy root should be retained when residual entries exist: %v", err)
	}
}
