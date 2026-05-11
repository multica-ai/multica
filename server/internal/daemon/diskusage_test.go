package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = 'x'
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestScanDiskUsage_AggregatesAndCategorizes verifies the happy-path: each
// task directory is sized, categorized by GC meta kind, and aggregated into
// per-workspace totals matching the per-task totals.
func TestScanDiskUsage_AggregatesAndCategorizes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	wsA := "11111111-1111-1111-1111-111111111111"
	wsB := "22222222-2222-2222-2222-222222222222"

	taskA1 := filepath.Join(root, wsA, "aaaaaaaa")
	writeFile(t, filepath.Join(taskA1, "workdir/main.go"), 1000)
	writeFile(t, filepath.Join(taskA1, "workdir/node_modules/dep/index.js"), 4000)
	mustWriteMeta(t, taskA1, execenv.GCMeta{
		Kind:        execenv.GCKindIssue,
		IssueID:     "issue-1",
		WorkspaceID: wsA,
		CompletedAt: time.Now().Add(-3 * time.Hour),
	})

	taskA2 := filepath.Join(root, wsA, "bbbbbbbb")
	writeFile(t, filepath.Join(taskA2, "workdir/notes.md"), 500)
	mustWriteMeta(t, taskA2, execenv.GCMeta{
		Kind:          execenv.GCKindChat,
		ChatSessionID: "chat-1",
		WorkspaceID:   wsA,
		CompletedAt:   time.Now().Add(-1 * time.Hour),
	})

	taskB1 := filepath.Join(root, wsB, "cccccccc")
	writeFile(t, filepath.Join(taskB1, "workdir/result.txt"), 2000)
	// No meta — exercises the unknown-kind / mtime-fallback path. Backdate
	// the dir mtime so the fallback produces a measurable age (a freshly
	// created dir has mtime=now, which would round to 0 seconds).
	backdate := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(taskB1, backdate, backdate); err != nil {
		t.Fatal(err)
	}

	report, err := ScanDiskUsage(root, []string{"node_modules", ".next", ".turbo"})
	if err != nil {
		t.Fatalf("ScanDiskUsage: %v", err)
	}

	if len(report.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(report.Tasks))
	}

	byShort := map[string]TaskDiskUsage{}
	for _, task := range report.Tasks {
		byShort[task.TaskShort] = task
	}

	a1 := byShort["aaaaaaaa"]
	if a1.Kind != string(execenv.GCKindIssue) {
		t.Errorf("task a1 kind = %q, want %q", a1.Kind, execenv.GCKindIssue)
	}
	// Size includes main.go (1000) + node_modules subtree (4000) + the
	// .gc_meta.json control file we wrote. Bound the meta overhead so we
	// don't drift if the meta JSON shape changes.
	if a1.SizeBytes < 5000 || a1.SizeBytes > 5000+1024 {
		t.Errorf("task a1 size = %d, want in [5000, 6024]", a1.SizeBytes)
	}
	if a1.ArtifactSizeBytes != 4000 {
		t.Errorf("task a1 artifact size = %d, want 4000", a1.ArtifactSizeBytes)
	}
	if a1.AgeSeconds < 60 {
		t.Errorf("task a1 age_seconds = %d, want >= 60 (CompletedAt -3h)", a1.AgeSeconds)
	}
	if a1.WorkspaceShort != ShortID(wsA) {
		t.Errorf("task a1 workspace_short = %q, want %q", a1.WorkspaceShort, ShortID(wsA))
	}

	a2 := byShort["bbbbbbbb"]
	if a2.Kind != string(execenv.GCKindChat) {
		t.Errorf("task a2 kind = %q, want chat", a2.Kind)
	}
	if a2.SizeBytes < 500 || a2.SizeBytes > 500+1024 {
		t.Errorf("task a2 size = %d, want in [500, 1524]", a2.SizeBytes)
	}
	if a2.ArtifactSizeBytes != 0 {
		t.Errorf("task a2 artifact size = %d, want 0", a2.ArtifactSizeBytes)
	}

	b1 := byShort["cccccccc"]
	if b1.Kind != DiskUsageKindUnknown {
		t.Errorf("task b1 kind = %q, want %q", b1.Kind, DiskUsageKindUnknown)
	}
	if b1.SizeBytes != 2000 {
		t.Errorf("task b1 size = %d, want 2000 (no meta file)", b1.SizeBytes)
	}
	if b1.AgeSeconds < 60 {
		t.Errorf("task b1 age_seconds = %d, want >= 60 (mtime backdated 2h)", b1.AgeSeconds)
	}

	if report.TotalSizeBytes != a1.SizeBytes+a2.SizeBytes+b1.SizeBytes {
		t.Errorf("total size = %d, want sum of per-task sizes (%d)",
			report.TotalSizeBytes, a1.SizeBytes+a2.SizeBytes+b1.SizeBytes)
	}
	if report.TotalArtifactSizeBytes != 4000 {
		t.Errorf("total artifact size = %d, want 4000", report.TotalArtifactSizeBytes)
	}

	wsByID := map[string]WorkspaceDiskUsage{}
	for _, ws := range report.Workspaces {
		wsByID[ws.WorkspaceID] = ws
	}
	if wsByID[wsA].SizeBytes != a1.SizeBytes+a2.SizeBytes {
		t.Errorf("workspace A size = %d, want %d (a1+a2)",
			wsByID[wsA].SizeBytes, a1.SizeBytes+a2.SizeBytes)
	}
	if wsByID[wsA].ArtifactSizeBytes != 4000 {
		t.Errorf("workspace A artifact size = %d, want 4000", wsByID[wsA].ArtifactSizeBytes)
	}
	if wsByID[wsA].TaskCount != 2 {
		t.Errorf("workspace A task count = %d, want 2", wsByID[wsA].TaskCount)
	}
	if wsByID[wsB].SizeBytes != 2000 {
		t.Errorf("workspace B size = %d, want 2000", wsByID[wsB].SizeBytes)
	}

	// Workspace A's artifact ratio: 4000 reclaimable / a1+a2 size. Match
	// within float tolerance so a small meta-file delta doesn't break it.
	wantARatio := 4000.0 / float64(a1.SizeBytes+a2.SizeBytes)
	if got := wsByID[wsA].ArtifactRatio; got < wantARatio-0.005 || got > wantARatio+0.005 {
		t.Errorf("workspace A artifact_ratio = %f, want ~%f", got, wantARatio)
	}
	// Workspace B has no artifact subtree at all → ratio must be 0, not NaN.
	if got := wsByID[wsB].ArtifactRatio; got != 0 {
		t.Errorf("workspace B artifact_ratio = %f, want 0", got)
	}

	// Scan-wide counts must reflect the full scan, not the (un-truncated
	// here) slice — they're the contract callers rely on once --top kicks in.
	if report.TotalTaskCount != 3 {
		t.Errorf("total_task_count = %d, want 3", report.TotalTaskCount)
	}
	if report.TotalWorkspaceCount != 2 {
		t.Errorf("total_workspace_count = %d, want 2", report.TotalWorkspaceCount)
	}
	if report.TotalArtifactRatio <= 0 || report.TotalArtifactRatio > 1 {
		t.Errorf("total_artifact_ratio = %f, want in (0, 1]", report.TotalArtifactRatio)
	}

	// Tasks must be sorted by size descending — the consumer treats this as
	// a stable contract for `--top N` slicing.
	for i := 1; i < len(report.Tasks); i++ {
		if report.Tasks[i-1].SizeBytes < report.Tasks[i].SizeBytes {
			t.Errorf("tasks not sorted by size desc: %d < %d at idx %d",
				report.Tasks[i-1].SizeBytes, report.Tasks[i].SizeBytes, i)
		}
	}

	// JSON round-trip — guards the field names the issue spec calls out.
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	for _, want := range []string{
		`"kind"`,
		`"parent_status"`,
		`"age_seconds"`,
		`"size_bytes"`,
		`"artifact_size_bytes"`,
		`"workspace_id"`,
		`"task_short"`,
		`"artifact_ratio"`,
		`"total_task_count"`,
		`"total_workspace_count"`,
		`"total_artifact_ratio"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("JSON missing required field %s: %s", want, raw)
		}
	}
}

// TestScanDiskUsage_EmptyWorkspaceArtifactRatio guards the total=0 edge:
// a workspace whose tasks have no measurable bytes (or no files at all) must
// still report ArtifactRatio=0, never NaN. The CLI table renders this column,
// and `NaN%` would surface in the user's terminal otherwise.
func TestScanDiskUsage_EmptyWorkspaceArtifactRatio(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	wsID := "00000000-0000-0000-0000-000000000000"
	taskDir := filepath.Join(root, wsID, "tttttttt")
	if err := os.MkdirAll(filepath.Join(taskDir, "workdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := ScanDiskUsage(root, []string{"node_modules"})
	if err != nil {
		t.Fatalf("ScanDiskUsage: %v", err)
	}
	if len(report.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(report.Workspaces))
	}
	if got := report.Workspaces[0].ArtifactRatio; got != 0 {
		t.Errorf("empty workspace artifact_ratio = %f, want 0 (no NaN)", got)
	}
	if got := report.TotalArtifactRatio; got != 0 {
		t.Errorf("empty scan total_artifact_ratio = %f, want 0 (no NaN)", got)
	}
}

// TestScanDiskUsage_DoesNotEnterGit guards the GC safety contract: anything
// inside a .git directory must not be counted, even if it would otherwise
// match an artifact basename. Reflects the same constraint cleanTaskArtifacts
// enforces so the disk-usage report stays in sync with what GC reclaims.
func TestScanDiskUsage_DoesNotEnterGit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	wsID := "wwwwwwww-wwww-wwww-wwww-wwwwwwwwwwww"
	taskDir := filepath.Join(root, wsID, "tttttttt")

	writeFile(t, filepath.Join(taskDir, "workdir/.git/objects/pack"), 9999)
	writeFile(t, filepath.Join(taskDir, "workdir/.git/node_modules/x"), 5555)
	writeFile(t, filepath.Join(taskDir, "workdir/main.go"), 100)

	report, err := ScanDiskUsage(root, []string{"node_modules"})
	if err != nil {
		t.Fatalf("ScanDiskUsage: %v", err)
	}

	if len(report.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(report.Tasks))
	}
	got := report.Tasks[0]
	if got.SizeBytes != 100 {
		t.Errorf("size_bytes = %d, want 100 (only main.go; .git tree skipped)", got.SizeBytes)
	}
	if got.ArtifactSizeBytes != 0 {
		t.Errorf("artifact_size_bytes = %d, want 0 (node_modules under .git is invisible)", got.ArtifactSizeBytes)
	}
}

// TestScanDiskUsage_DoesNotFollowSymlinks guards the second safety
// constraint. A symlinked artifact directory must not be sized — neither
// the link itself nor its target — because cleanTaskArtifacts won't reclaim
// it either.
func TestScanDiskUsage_DoesNotFollowSymlinks(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}

	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "huge.bin"), 10000)

	wsID := "ssssssss-ssss-ssss-ssss-ssssssssssss"
	taskDir := filepath.Join(root, wsID, "tttttttt")
	writeFile(t, filepath.Join(taskDir, "workdir/main.go"), 100)
	if err := os.Symlink(outside, filepath.Join(taskDir, "workdir/node_modules")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	// Symlinked regular file too — the link's target lives outside taskDir
	// and must not be summed.
	if err := os.Symlink(filepath.Join(outside, "huge.bin"), filepath.Join(taskDir, "workdir/big-link")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	report, err := ScanDiskUsage(root, []string{"node_modules"})
	if err != nil {
		t.Fatalf("ScanDiskUsage: %v", err)
	}

	if len(report.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(report.Tasks))
	}
	got := report.Tasks[0]
	if got.SizeBytes != 100 {
		t.Errorf("size_bytes = %d, want 100 (only main.go; symlinks ignored)", got.SizeBytes)
	}
	if got.ArtifactSizeBytes != 0 {
		t.Errorf("artifact_size_bytes = %d, want 0 (symlinked node_modules ignored)", got.ArtifactSizeBytes)
	}
}

// TestScanDiskUsage_MissingRoot ensures a daemon that has never run yet
// (workspaces dir doesn't exist) returns an empty report, not an error.
func TestScanDiskUsage_MissingRoot(t *testing.T) {
	t.Parallel()
	report, err := ScanDiskUsage(filepath.Join(t.TempDir(), "does-not-exist"), nil)
	if err != nil {
		t.Fatalf("ScanDiskUsage on missing root returned error: %v", err)
	}
	if len(report.Tasks) != 0 || len(report.Workspaces) != 0 {
		t.Errorf("expected empty report, got %+v", report)
	}
}

// TestScanDiskUsage_RejectsPatternsWithSeparators mirrors the GC safety check:
// a pattern containing "/" or "\\" is meaningless for basename matching and
// must be silently dropped, not interpreted as a path.
func TestScanDiskUsage_RejectsPatternsWithSeparators(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	wsID := "rrrrrrrr-rrrr-rrrr-rrrr-rrrrrrrrrrrr"
	taskDir := filepath.Join(root, wsID, "tttttttt")
	writeFile(t, filepath.Join(taskDir, "workdir/node_modules/x"), 1000)

	report, err := ScanDiskUsage(root, []string{"workdir/node_modules", "../etc"})
	if err != nil {
		t.Fatalf("ScanDiskUsage: %v", err)
	}
	if got := report.Tasks[0].ArtifactSizeBytes; got != 0 {
		t.Errorf("artifact_size_bytes = %d, want 0 (separator-bearing patterns dropped)", got)
	}
	if got := report.ArtifactPatterns; len(got) != 0 {
		t.Errorf("ArtifactPatterns = %v, want empty (all dropped)", got)
	}
}

func mustWriteMeta(t *testing.T, taskDir string, meta execenv.GCMeta) {
	t.Helper()
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, ".gc_meta.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
