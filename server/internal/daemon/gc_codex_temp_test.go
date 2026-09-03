package daemon

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

const codexTempRel = "codex-home/.tmp"

func ageCodexTempFixture(t *testing.T, taskDir string, when time.Time) {
	t.Helper()
	for _, path := range []string{filepath.Join(taskDir, codexTempRel), filepath.Join(taskDir, ".gc_meta.json")} {
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatalf("age %s: %v", path, err)
		}
	}
}

func TestCodexTempGC_StaleInactiveRootRemovesOnlyExactTemp(t *testing.T) {
	t.Parallel()
	d := newGCTestDaemon(t, http.NewServeMux())
	old := time.Now().Add(-4 * 24 * time.Hour)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "stale-temp", &execenv.GCMeta{
		Kind:        execenv.GCKindIssue,
		IssueID:     "issue-stale-temp",
		WorkspaceID: "ws",
		CompletedAt: old,
	})
	writeFile(t, filepath.Join(taskDir, codexTempRel, "marketplaces/.git/objects/pack/cache.pack"), 64)
	writeFile(t, filepath.Join(taskDir, "codex-home/.sandbox-bin/codex"), 32)
	writeFile(t, filepath.Join(taskDir, "codex-home/sessions/keep.jsonl"), 16)
	writeFile(t, filepath.Join(taskDir, "workdir/repo/.tmp/user-owned"), 8)
	ageCodexTempFixture(t, taskDir, old)

	stats := &gcStats{byPattern: map[string]int{}}
	d.reclaimIdleCodexTemp(taskDir, stats)

	assertGone(t, taskDir, codexTempRel)
	assertKept(t, taskDir,
		"codex-home/.sandbox-bin/codex",
		"codex-home/sessions/keep.jsonl",
		"workdir/repo/.tmp/user-owned",
		".gc_meta.json",
	)
	if stats.codexTempDirsReclaimed != 1 || stats.artifactRemoved != 1 {
		t.Fatalf("stats = %+v, want one Codex temp directory", stats)
	}
	if stats.bytesReclaimed != 0 {
		t.Fatalf("bytes_reclaimed=%d, want 0 without a logical-size prescan", stats.bytesReclaimed)
	}
	if got := stats.byPattern[managedArtifactPatternPrefix+codexTempRel]; got != 1 {
		t.Fatalf("managed temp pattern count=%d, want 1", got)
	}
}

func TestCodexTempGC_RecentSecondRunKeepsReusableMarketplace(t *testing.T) {
	t.Parallel()
	d := newGCTestDaemon(t, http.NewServeMux())
	old := time.Now().Add(-4 * 24 * time.Hour)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "resumed-temp", &execenv.GCMeta{
		Kind:        execenv.GCKindIssue,
		IssueID:     "issue-resumed-temp",
		WorkspaceID: "ws",
		CompletedAt: old,
	})
	marketplace := filepath.Join(taskDir, codexTempRel, "marketplaces/chatcut/.git/HEAD")
	plugin := filepath.Join(taskDir, codexTempRel, "plugins/asset-import/SKILL.md")
	writeFile(t, marketplace, 16)
	writeFile(t, plugin, 16)
	ageCodexTempFixture(t, taskDir, old)

	// A second run reuses the same env root and refreshes completion metadata.
	// The marketplace and installed plugin must remain available for a nearby
	// third run; no network rehydration should be needed inside the 3-day TTL.
	if err := execenv.WriteGCMeta(taskDir, execenv.GCMeta{
		Kind:        execenv.GCKindIssue,
		IssueID:     "issue-resumed-temp",
		WorkspaceID: "ws",
		CompletedAt: time.Now().Add(-time.Hour),
	}, slog.Default()); err != nil {
		t.Fatalf("refresh GC metadata: %v", err)
	}
	d.reclaimIdleCodexTemp(taskDir, &gcStats{byPattern: map[string]int{}})

	for _, path := range []string{marketplace, plugin} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("reusable Codex content was removed: %s: %v", path, err)
		}
	}
}

func TestCodexTempGC_ActiveEnvRootAlwaysWinsOverAge(t *testing.T) {
	t.Parallel()
	d := newGCTestDaemon(t, http.NewServeMux())
	old := time.Now().Add(-30 * 24 * time.Hour)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "active-temp", &execenv.GCMeta{
		Kind:        execenv.GCKindChat,
		WorkspaceID: "ws",
		CompletedAt: old,
	})
	writeFile(t, filepath.Join(taskDir, codexTempRel, "plugins/cache.bin"), 16)
	ageCodexTempFixture(t, taskDir, old)

	d.markActiveEnvRoot(taskDir)
	d.reclaimIdleCodexTemp(taskDir, &gcStats{byPattern: map[string]int{}})

	assertKept(t, taskDir, codexTempRel+"/plugins/cache.bin")
}

func TestCodexTempGC_TTLZeroDisablesCleanup(t *testing.T) {
	t.Parallel()
	d := newGCTestDaemon(t, http.NewServeMux())
	d.cfg.GCCodexTempTTL = 0
	old := time.Now().Add(-30 * 24 * time.Hour)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "disabled-temp", &execenv.GCMeta{
		Kind:        execenv.GCKindQuickCreate,
		WorkspaceID: "ws",
		CompletedAt: old,
	})
	writeFile(t, filepath.Join(taskDir, codexTempRel, "plugins/cache.bin"), 16)
	ageCodexTempFixture(t, taskDir, old)

	d.reclaimIdleCodexTemp(taskDir, &gcStats{byPattern: map[string]int{}})

	assertKept(t, taskDir, codexTempRel+"/plugins/cache.bin")
}

func TestCodexTempGC_DoesNotFollowTempSymlink(t *testing.T) {
	t.Parallel()
	d := newGCTestDaemon(t, http.NewServeMux())
	old := time.Now().Add(-30 * 24 * time.Hour)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "linked-temp", &execenv.GCMeta{
		Kind:        execenv.GCKindIssue,
		IssueID:     "issue-linked-temp",
		WorkspaceID: "ws",
		CompletedAt: old,
	})
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep.pack")
	writeFile(t, keep, 16)
	if err := os.MkdirAll(filepath.Join(taskDir, "codex-home"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(taskDir, codexTempRel)); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if err := os.Chtimes(filepath.Join(taskDir, ".gc_meta.json"), old, old); err != nil {
		t.Fatal(err)
	}

	d.reclaimIdleCodexTemp(taskDir, &gcStats{byPattern: map[string]int{}})

	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("Codex temp symlink target was touched: %v", err)
	}
}

func TestRunGC_CodexTempSkipsActiveTask(t *testing.T) {
	t.Parallel()
	d := newGCTestDaemon(t, http.NewServeMux())
	old := time.Now().Add(-30 * 24 * time.Hour)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "running-temp", &execenv.GCMeta{
		Kind:          execenv.GCKindChat,
		ChatSessionID: "active-chat",
		WorkspaceID:   "ws",
		CompletedAt:   old,
	})
	writeFile(t, filepath.Join(taskDir, codexTempRel, "marketplaces/cache.pack"), 16)
	ageCodexTempFixture(t, taskDir, old)
	d.markActiveEnvRoot(taskDir)

	d.runGC(context.Background())

	assertKept(t, taskDir, codexTempRel+"/marketplaces/cache.pack")
}
