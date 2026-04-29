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
	if _, err := os.Stat(filepath.Join(target, ".repos-"+profile, "github.com", "foo", "bar", "HEAD")); err != nil {
		t.Fatalf("expected legacy .repos to be relocated to .repos-<profile>, got err=%v", err)
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

func TestMigrateLegacyWorkspacesRoot_MergesNonConflictingTasksUnderSameWorkspace(t *testing.T) {
	home := withFakeHome(t)
	profile := "desktop-api.multica.ai"

	legacy := filepath.Join(home, "multica_workspaces_"+profile)
	target := filepath.Join(home, "multica_workspaces")

	// Same workspace_id present in both roots, but different task_short subdirs.
	// This is the realistic case: Web/CLI created task-web at the new path, the
	// Desktop daemon created task-desktop at the old per-profile path. Both
	// must end up under target/<ws>/.
	mustWriteFile(t, filepath.Join(target, "ws-uuid", "task-web", "marker"), "from-web")
	mustWriteFile(t, filepath.Join(legacy, "ws-uuid", "task-desktop", "marker"), "from-desktop")

	cfg := Config{
		Profile:        profile,
		WorkspacesRoot: target,
	}
	MigrateLegacyWorkspacesRoot(cfg, newSilentLogger())

	if b, err := os.ReadFile(filepath.Join(target, "ws-uuid", "task-web", "marker")); err != nil || string(b) != "from-web" {
		t.Fatalf("pre-existing target task should be preserved, got %q err=%v", string(b), err)
	}
	if b, err := os.ReadFile(filepath.Join(target, "ws-uuid", "task-desktop", "marker")); err != nil || string(b) != "from-desktop" {
		t.Fatalf("legacy task should be merged into target, got %q err=%v", string(b), err)
	}
	// Legacy root and its workspace dir should be cleaned up since everything migrated.
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy root should be removed after full migration, err=%v", err)
	}
}

func TestMigrateLegacyWorkspacesRoot_PreservesConflictingTaskShort(t *testing.T) {
	home := withFakeHome(t)
	profile := "desktop-foo"

	legacy := filepath.Join(home, "multica_workspaces_"+profile)
	target := filepath.Join(home, "multica_workspaces")

	// Same workspace AND same task_short on both sides. We must not overwrite.
	mustWriteFile(t, filepath.Join(legacy, "ws-uuid", "task-x", "marker"), "from-legacy")
	mustWriteFile(t, filepath.Join(target, "ws-uuid", "task-x", "marker"), "from-target")
	// Plus a non-conflicting task_short under the same workspace and a fresh workspace.
	mustWriteFile(t, filepath.Join(legacy, "ws-uuid", "task-y", "marker"), "merged")
	mustWriteFile(t, filepath.Join(legacy, "ws-uuid-other", "task-z", "marker"), "moved")

	cfg := Config{
		Profile:        profile,
		WorkspacesRoot: target,
	}
	MigrateLegacyWorkspacesRoot(cfg, newSilentLogger())

	// Same task_short on both sides → target wins, legacy copy stays put.
	if b, err := os.ReadFile(filepath.Join(target, "ws-uuid", "task-x", "marker")); err != nil || string(b) != "from-target" {
		t.Fatalf("target task_short should be preserved, got %q err=%v", string(b), err)
	}
	if b, err := os.ReadFile(filepath.Join(legacy, "ws-uuid", "task-x", "marker")); err != nil || string(b) != "from-legacy" {
		t.Fatalf("conflicting legacy task_short should remain in place, got %q err=%v", string(b), err)
	}

	// Non-conflicting task_short under same workspace was merged.
	if b, err := os.ReadFile(filepath.Join(target, "ws-uuid", "task-y", "marker")); err != nil || string(b) != "merged" {
		t.Fatalf("non-conflicting task_short should be merged into target, got %q err=%v", string(b), err)
	}

	// Fresh workspace was renamed wholesale.
	if _, err := os.Stat(filepath.Join(legacy, "ws-uuid-other")); !os.IsNotExist(err) {
		t.Fatalf("fresh legacy workspace should be moved, err=%v", err)
	}
	if b, err := os.ReadFile(filepath.Join(target, "ws-uuid-other", "task-z", "marker")); err != nil || string(b) != "moved" {
		t.Fatalf("fresh workspace should arrive at target, got %q err=%v", string(b), err)
	}

	// Legacy root retained because the conflicting task_short still lives there.
	if _, err := os.Stat(filepath.Join(legacy, "ws-uuid", "task-x")); err != nil {
		t.Fatalf("legacy conflicting task_short should remain, err=%v", err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy root should be retained when residual entries exist: %v", err)
	}
}

func TestMigrateLegacyWorkspacesRoot_RelocatesReposNextToExistingTargetCache(t *testing.T) {
	home := withFakeHome(t)
	profile := "desktop-foo"

	legacy := filepath.Join(home, "multica_workspaces_"+profile)
	target := filepath.Join(home, "multica_workspaces")

	// Both daemons have populated bare-repo caches. Each must keep its own.
	// Legacy `.repos` belongs to the profiled daemon and is relocated to
	// `.repos-<profile>`; the default-profile daemon's existing `.repos` at
	// the target stays untouched.
	mustWriteFile(t, filepath.Join(legacy, ".repos", "github.com", "foo", "HEAD"), "legacy")
	mustWriteFile(t, filepath.Join(target, ".repos", "github.com", "bar", "HEAD"), "target")

	cfg := Config{
		Profile:        profile,
		WorkspacesRoot: target,
	}
	MigrateLegacyWorkspacesRoot(cfg, newSilentLogger())

	if _, err := os.Stat(filepath.Join(target, ".repos-"+profile, "github.com", "foo", "HEAD")); err != nil {
		t.Fatalf("legacy .repos should land at .repos-<profile>, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(target, ".repos", "github.com", "bar", "HEAD")); err != nil {
		t.Fatalf("default-profile .repos should be untouched, got err=%v", err)
	}
	// Target's default .repos must not have absorbed any legacy entries.
	if _, err := os.Stat(filepath.Join(target, ".repos", "github.com", "foo")); !os.IsNotExist(err) {
		t.Fatalf("legacy entries must not leak into target .repos, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(legacy, ".repos")); !os.IsNotExist(err) {
		t.Fatalf("legacy .repos should be moved, err=%v", err)
	}
}

func TestRepoCacheRoot_PerProfile(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantDir string
	}{
		{
			name:    "default profile",
			cfg:     Config{WorkspacesRoot: "/ws", Profile: ""},
			wantDir: filepath.Join("/ws", ".repos"),
		},
		{
			name:    "named profile",
			cfg:     Config{WorkspacesRoot: "/ws", Profile: "desktop-api.multica.ai"},
			wantDir: filepath.Join("/ws", ".repos-desktop-api.multica.ai"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := repoCacheRoot(tc.cfg); got != tc.wantDir {
				t.Fatalf("repoCacheRoot = %q, want %q", got, tc.wantDir)
			}
		})
	}
}

func TestMigrateLegacyWorkspacesRoot_DoesNotOverwriteExistingPerProfileRepos(t *testing.T) {
	home := withFakeHome(t)
	profile := "desktop-foo"

	legacy := filepath.Join(home, "multica_workspaces_"+profile)
	target := filepath.Join(home, "multica_workspaces")

	// A previous migration already populated .repos-<profile>. The new
	// migration must not clobber it; the residual legacy .repos stays put
	// for manual reconciliation.
	mustWriteFile(t, filepath.Join(legacy, ".repos", "github.com", "foo", "HEAD"), "legacy")
	mustWriteFile(t, filepath.Join(target, ".repos-"+profile, "github.com", "old", "HEAD"), "already-migrated")

	cfg := Config{
		Profile:        profile,
		WorkspacesRoot: target,
	}
	MigrateLegacyWorkspacesRoot(cfg, newSilentLogger())

	if b, err := os.ReadFile(filepath.Join(target, ".repos-"+profile, "github.com", "old", "HEAD")); err != nil || string(b) != "already-migrated" {
		t.Fatalf("existing per-profile cache must be preserved, got %q err=%v", string(b), err)
	}
	if _, err := os.Stat(filepath.Join(legacy, ".repos", "github.com", "foo", "HEAD")); err != nil {
		t.Fatalf("legacy .repos should remain when per-profile cache already exists, err=%v", err)
	}
}
