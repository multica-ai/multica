package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
		Kind:           execenv.GCKindIssue,
		IssueID:        "issue-1",
		TaskID:         "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		WorkspaceID:    wsA,
		CompletedAt:    time.Now().Add(-3 * time.Hour),
		LocalDirectory: true,
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
	if a1.TaskID != "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" || a1.CompletedAt == nil || a1.LocalDirectory == nil || !*a1.LocalDirectory {
		t.Errorf("task a1 lifecycle metadata = task_id:%q completed_at:%v local_directory:%v",
			a1.TaskID, a1.CompletedAt, a1.LocalDirectory)
	}
	if a1.ResumeCandidate == nil || *a1.ResumeCandidate {
		t.Errorf("local_directory resume_candidate = %v, want false", a1.ResumeCandidate)
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
	if a2.LocalDirectory == nil || *a2.LocalDirectory {
		t.Errorf("managed task local_directory = %v, want false", a2.LocalDirectory)
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
	if b1.LocalDirectory != nil {
		t.Errorf("metadata-free task local_directory = %v, want unknown", b1.LocalDirectory)
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
		`"task_id"`,
		`"parent_status"`,
		`"completed_at"`,
		`"local_directory"`,
		`"resume_candidate"`,
		`"retention_reason"`,
		`"age_seconds"`,
		`"size_bytes"`,
		`"artifact_size_bytes"`,
		`"workspace_id"`,
		`"task_short"`,
		`"artifact_ratio"`,
		`"managed_artifact_subpaths"`,
		`"total_task_count"`,
		`"total_workspace_count"`,
		`"total_artifact_ratio"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("JSON missing required field %s: %s", want, raw)
		}
	}
}

func TestScanDiskUsage_MixedLayoutsUseMetadataIdentity(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workspaceID := "a05b0e10-ee7a-4603-a72d-a548b2390cb2"
	readableTaskID := "22222222-ee7a-4603-a72d-b659c34a1dc3"
	legacyTaskSegment := "a548b2390cb2"
	readableTaskSegment := "mul-6063-b659c34a1dc3"

	legacyDir := filepath.Join(root, workspaceID, legacyTaskSegment)
	writeFile(t, filepath.Join(legacyDir, "workdir", "legacy.txt"), 10)
	mustWriteMeta(t, legacyDir, execenv.GCMeta{
		Kind:        execenv.GCKindIssue,
		IssueID:     "issue-legacy",
		WorkspaceID: workspaceID,
		CompletedAt: time.Now().Add(-time.Hour),
	})

	readableDir := filepath.Join(root, "asset-feed-a548b2390cb2", readableTaskSegment)
	writeFile(t, filepath.Join(readableDir, "workdir", "readable.txt"), 20)
	mustWriteMeta(t, readableDir, execenv.GCMeta{
		Kind:        execenv.GCKindIssue,
		IssueID:     "issue-readable",
		TaskID:      readableTaskID,
		WorkspaceID: workspaceID,
		CompletedAt: time.Now().Add(-time.Hour),
	})

	report, err := ScanDiskUsage(root, nil)
	if err != nil {
		t.Fatalf("ScanDiskUsage: %v", err)
	}
	if report.TotalTaskCount != 2 || report.TotalWorkspaceCount != 1 {
		t.Fatalf("totals = tasks:%d workspaces:%d, want 2/1", report.TotalTaskCount, report.TotalWorkspaceCount)
	}
	for _, task := range report.Tasks {
		if task.WorkspaceID != workspaceID {
			t.Errorf("task %q workspace_id = %q, want metadata id %q", task.Path, task.WorkspaceID, workspaceID)
		}
	}
	byShort := map[string]TaskDiskUsage{}
	for _, task := range report.Tasks {
		byShort[task.TaskShort] = task
	}
	if _, ok := byShort[legacyTaskSegment]; !ok {
		t.Errorf("legacy task was not reported under physical directory segment %q", legacyTaskSegment)
	}
	if _, ok := byShort[readableTaskSegment]; !ok {
		t.Errorf("readable task was not reported under physical directory segment %q", readableTaskSegment)
	}
}

func TestScanDiskUsage_ReadableActiveRootUsesOwnerIdentityWithoutGCMeta(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workspaceID := "a05b0e10-ee7a-4603-a72d-a548b2390cb2"
	taskID := "22222222-ee7a-4603-a72d-b659c34a1dc3"
	env, err := execenv.Prepare(execenv.PrepareParams{
		WorkspacesRoot:  root,
		WorkspaceID:     workspaceID,
		WorkspaceSlug:   "Asset Feed",
		TaskID:          taskID,
		IssueIdentifier: "MUL-6063",
		AgentName:       "Active Task",
	}, slog.Default())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer env.Cleanup(true)
	if _, err := os.Stat(filepath.Join(env.RootDir, ".gc_meta.json")); !os.IsNotExist(err) {
		t.Fatalf("fixture must represent an active root without completion metadata; stat err = %v", err)
	}

	report, err := ScanDiskUsage(root, nil)
	if err != nil {
		t.Fatalf("ScanDiskUsage: %v", err)
	}
	if len(report.Tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(report.Tasks))
	}
	usage := report.Tasks[0]
	if usage.WorkspaceID != workspaceID {
		t.Fatalf("workspace_id = %q, want owner identity %q", usage.WorkspaceID, workspaceID)
	}
	wantTaskSegment := filepath.Base(env.RootDir)
	if usage.TaskShort != wantTaskSegment {
		t.Fatalf("task_short = %q, want physical directory segment %q", usage.TaskShort, wantTaskSegment)
	}
	if usage.TaskID != taskID {
		t.Fatalf("task_id = %q, want owner identity %q", usage.TaskID, taskID)
	}
	if usage.LocalDirectory != nil {
		t.Fatalf("local_directory = %v, want unknown without completion metadata", usage.LocalDirectory)
	}
	if len(report.Workspaces) != 1 || report.Workspaces[0].WorkspaceID != workspaceID {
		t.Fatalf("workspace aggregate = %+v, want authoritative workspace %q", report.Workspaces, workspaceID)
	}
}

func TestScanDiskUsage_ManagedCodexSandboxIsExactAndDeduplicated(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	wsID := "mmmmmmmm-mmmm-mmmm-mmmm-mmmmmmmmmmmm"
	taskDir := filepath.Join(root, wsID, "tttttttt")
	writeFile(t, filepath.Join(taskDir, "codex-home/.sandbox-bin/codex.exe"), 300)
	writeFile(t, filepath.Join(taskDir, "workdir/repo/.sandbox-bin/cache"), 400)

	report, err := ScanDiskUsage(root, nil)
	if err != nil {
		t.Fatalf("ScanDiskUsage managed-only: %v", err)
	}
	if got := report.Tasks[0].SizeBytes; got != 700 {
		t.Fatalf("size_bytes=%d, want 700", got)
	}
	if got := report.Tasks[0].ArtifactSizeBytes; got != 300 {
		t.Fatalf("artifact_size_bytes=%d, want exact managed 300", got)
	}
	if got := strings.Join(report.ManagedArtifactSubpaths, ","); got != "codex-home/.sandbox-bin" {
		t.Fatalf("managed artifact paths=%q, want codex-home/.sandbox-bin", got)
	}

	report, err = ScanDiskUsage(root, []string{".sandbox-bin"})
	if err != nil {
		t.Fatalf("ScanDiskUsage broad basename: %v", err)
	}
	if got := report.Tasks[0].ArtifactSizeBytes; got != 700 {
		t.Fatalf("artifact_size_bytes=%d, want 700 without double counting", got)
	}
}

func TestScanDiskUsage_LegacyMetaAgeUsesMetaFileMTime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	wsID := "llllllll-llll-llll-llll-llllllllllll"
	taskDir := filepath.Join(root, wsID, "tttttttt")
	writeFile(t, filepath.Join(taskDir, "workdir/main.go"), 10)
	mustWriteMeta(t, taskDir, execenv.GCMeta{
		Kind: execenv.GCKindIssue, IssueID: "issue-legacy", WorkspaceID: wsID,
	})
	oldRoot := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(taskDir, oldRoot, oldRoot); err != nil {
		t.Fatal(err)
	}
	recentMeta := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(taskDir, ".gc_meta.json"), recentMeta, recentMeta); err != nil {
		t.Fatal(err)
	}

	report, err := ScanDiskUsage(root, nil)
	if err != nil {
		t.Fatalf("ScanDiskUsage: %v", err)
	}
	if got := report.Tasks[0].AgeSeconds; got < int64(time.Hour.Seconds()) || got > int64((3*time.Hour).Seconds()) {
		t.Fatalf("age_seconds=%d, want metadata age near 2h instead of stale task root age", got)
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

// TestScanDiskUsage_CountsGitButNeverAsArtifact pins the two halves of the
// .git rule. A .git subtree IS real footprint, so it counts toward size_bytes
// (a full task-dir reclaim frees it, and dirSize reports it that way). But it
// must never count as an artifact, even when something inside it matches an
// artifact basename, because cleanTaskArtifacts refuses to reclaim in there.
func TestScanDiskUsage_CountsGitButNeverAsArtifact(t *testing.T) {
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
	const wantSize = 100 + 9999 + 5555
	if got.SizeBytes != wantSize {
		t.Errorf("size_bytes = %d, want %d (main.go plus the whole .git tree)", got.SizeBytes, wantSize)
	}
	if got.ArtifactSizeBytes != 0 {
		t.Errorf("artifact_size_bytes = %d, want 0 (node_modules under .git is not reclaimable)", got.ArtifactSizeBytes)
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

// TestScanDiskUsageRoots_SumsAcrossRoots verifies the cross-root aggregate:
// each root keeps its own labeled report and the grand totals are the sum of
// every root's full scan. A missing root contributes an empty report, not an
// error, so a never-used profile root doesn't break the aggregate.
func TestScanDiskUsageRoots_SumsAcrossRoots(t *testing.T) {
	t.Parallel()

	rootA := t.TempDir()
	writeFile(t, filepath.Join(rootA, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "t1", "workdir/main.go"), 100)

	rootB := t.TempDir()
	writeFile(t, filepath.Join(rootB, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "t1", "workdir/big"), 300)
	writeFile(t, filepath.Join(rootB, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "t2", "workdir/main.go"), 50)

	missing := filepath.Join(t.TempDir(), "never-ran")

	agg, err := ScanDiskUsageRoots([]DiskUsageRoot{
		{Profile: "", Root: rootA},
		{Profile: "desktop-host", Root: rootB},
		{Profile: "never-ran", Root: missing},
	}, []string{"node_modules"})
	if err != nil {
		t.Fatalf("ScanDiskUsageRoots: %v", err)
	}

	if len(agg.Roots) != 3 {
		t.Fatalf("Roots len = %d, want 3 (missing root still listed, empty)", len(agg.Roots))
	}
	if agg.Roots[0].Profile != "" || agg.Roots[1].Profile != "desktop-host" {
		t.Fatalf("root profiles not preserved in order: %+v", agg.Roots)
	}
	if agg.Roots[2].Report.TotalTaskCount != 0 {
		t.Fatalf("missing root TotalTaskCount = %d, want 0", agg.Roots[2].Report.TotalTaskCount)
	}
	if agg.TotalTaskCount != 3 {
		t.Fatalf("TotalTaskCount = %d, want 3 across roots", agg.TotalTaskCount)
	}
	if agg.TotalSizeBytes != 450 {
		t.Fatalf("TotalSizeBytes = %d, want 450 (100 + 300 + 50)", agg.TotalSizeBytes)
	}
	if agg.TotalWorkspaceCount != 2 {
		t.Fatalf("TotalWorkspaceCount = %d, want 2", agg.TotalWorkspaceCount)
	}
	if got := strings.Join(agg.ManagedArtifactSubpaths, ","); got != "codex-home/.sandbox-bin" {
		t.Fatalf("managed artifact paths=%q, want codex-home/.sandbox-bin", got)
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

// issueTask builds a minimal issue-kind task row for ResolveParentStatuses.
func issueTask(wsID, taskShort, issueID string) TaskDiskUsage {
	return TaskDiskUsage{
		WorkspaceID: wsID,
		TaskShort:   taskShort,
		TaskID:      taskShort,
		Kind:        string(execenv.GCKindIssue),
		ParentID:    issueID,
	}
}

func foundIssueResults(statuses map[string]string) map[string]IssueGCCheckResult {
	results := make(map[string]IssueGCCheckResult, len(statuses))
	for id, status := range statuses {
		results[id] = IssueGCCheckResult{ID: id, Found: true, Status: status}
	}
	return results
}

// TestResolveParentStatuses_FillsIssueTasks covers the main path: statuses land
// on the right rows, ids shared by several task dirs are asked for once, each
// workspace is queried separately, and non-issue kinds are left alone.
func TestResolveParentStatuses_FillsIssueTasks(t *testing.T) {
	t.Parallel()

	wsA := "11111111-1111-1111-1111-111111111111"
	wsB := "22222222-2222-2222-2222-222222222222"
	report := &DiskUsageReport{Tasks: []TaskDiskUsage{
		issueTask(wsA, "aaaa1111", "issue-1"),
		// Same issue, second workdir — must resolve without a second ask.
		issueTask(wsA, "aaaa2222", "issue-1"),
		issueTask(wsA, "aaaa3333", "issue-2"),
		issueTask(wsB, "bbbb1111", "issue-3"),
		{WorkspaceID: wsA, TaskShort: "cccc1111", Kind: string(execenv.GCKindChat), ParentID: "chat-1"},
		{WorkspaceID: wsA, TaskShort: "dddd1111", Kind: DiskUsageKindUnknown},
	}}

	asked := map[string][]string{}
	fetch := func(_ context.Context, workspaceID string, issueIDs, taskIDs []string) (map[string]IssueGCCheckResult, map[string]TaskEnvironmentGCCheckResult, error) {
		asked[workspaceID] = append(asked[workspaceID], issueIDs...)
		statuses := map[string]string{}
		for _, id := range issueIDs {
			switch id {
			case "issue-1":
				statuses[id] = "done"
			case "issue-2":
				statuses[id] = "in_progress"
			case "issue-3":
				statuses[id] = "cancelled"
			}
		}
		candidates := make(map[string]TaskEnvironmentGCCheckResult, len(taskIDs))
		for _, id := range taskIDs {
			candidates[id] = TaskEnvironmentGCCheckResult{TaskID: id, Found: true, ResumeCandidate: id == "aaaa2222"}
		}
		return foundIssueResults(statuses), candidates, nil
	}

	if err := ResolveParentStatuses(context.Background(), report, fetch); err != nil {
		t.Fatalf("ResolveParentStatuses: %v", err)
	}

	want := []string{"done", "done", "in_progress", "cancelled", "", ""}
	for i, wantStatus := range want {
		if got := report.Tasks[i].ParentStatus; got != wantStatus {
			t.Errorf("task[%d] (%s) parent_status = %q, want %q",
				i, report.Tasks[i].TaskShort, got, wantStatus)
		}
	}
	if got := report.Tasks[0].ResumeCandidate; got == nil || *got {
		t.Errorf("first issue-1 task resume_candidate = %v, want false", got)
	}
	if got := report.Tasks[1].ResumeCandidate; got == nil || !*got {
		t.Errorf("second issue-1 task resume_candidate = %v, want true", got)
	}

	if len(asked[wsA]) != 2 {
		t.Errorf("workspace A asked for %v, want exactly 2 de-duplicated ids", asked[wsA])
	}
	if len(asked[wsB]) != 1 {
		t.Errorf("workspace B asked for %v, want exactly 1 id", asked[wsB])
	}
}

// TestResolveParentStatuses_UnresolvedStaysBlank ensures an id the server does
// not return (deleted issue, or one this token cannot see) reads as unknown
// rather than being filled with a placeholder that looks like a real status.
func TestResolveParentStatuses_UnresolvedStaysBlank(t *testing.T) {
	t.Parallel()

	wsID := "11111111-1111-1111-1111-111111111111"
	report := &DiskUsageReport{Tasks: []TaskDiskUsage{
		issueTask(wsID, "aaaa1111", "issue-known"),
		issueTask(wsID, "aaaa2222", "issue-missing"),
	}}

	fetch := func(_ context.Context, _ string, _ []string, _ []string) (map[string]IssueGCCheckResult, map[string]TaskEnvironmentGCCheckResult, error) {
		return map[string]IssueGCCheckResult{
			"issue-known":   {ID: "issue-known", Found: true, Status: "todo"},
			"issue-missing": {ID: "issue-missing", Found: false},
		}, nil, nil
	}

	if err := ResolveParentStatuses(context.Background(), report, fetch); err != nil {
		t.Fatalf("ResolveParentStatuses: %v", err)
	}
	if report.Tasks[0].ParentStatus != "todo" {
		t.Errorf("known issue status = %q, want todo", report.Tasks[0].ParentStatus)
	}
	if report.Tasks[1].ParentStatus != "" {
		t.Errorf("missing issue status = %q, want empty", report.Tasks[1].ParentStatus)
	}
	if report.Tasks[1].parentFound == nil || *report.Tasks[1].parentFound {
		t.Errorf("missing issue parentFound = %v, want false", report.Tasks[1].parentFound)
	}
}

// TestResolveParentStatuses_ChunksLargeWorkspaces verifies a root with more
// issues than the server's batch cap is split the same way the GC loop splits
// it, instead of being sent as one oversized request.
func TestResolveParentStatuses_ChunksLargeWorkspaces(t *testing.T) {
	t.Parallel()

	wsID := "11111111-1111-1111-1111-111111111111"
	total := issueGCBatchSize + 1
	tasks := make([]TaskDiskUsage, 0, total)
	for i := range total {
		tasks = append(tasks, issueTask(wsID, fmt.Sprintf("task%04d", i), fmt.Sprintf("issue-%04d", i)))
	}
	report := &DiskUsageReport{Tasks: tasks}

	var chunkSizes []int
	fetch := func(_ context.Context, _ string, issueIDs, _ []string) (map[string]IssueGCCheckResult, map[string]TaskEnvironmentGCCheckResult, error) {
		chunkSizes = append(chunkSizes, len(issueIDs))
		out := make(map[string]string, len(issueIDs))
		for _, id := range issueIDs {
			out[id] = "done"
		}
		return foundIssueResults(out), nil, nil
	}

	if err := ResolveParentStatuses(context.Background(), report, fetch); err != nil {
		t.Fatalf("ResolveParentStatuses: %v", err)
	}

	if len(chunkSizes) != 2 {
		t.Fatalf("chunk sizes = %v, want 2 chunks", chunkSizes)
	}
	if chunkSizes[0] != issueGCBatchSize || chunkSizes[1] != 1 {
		t.Errorf("chunk sizes = %v, want [%d 1]", chunkSizes, issueGCBatchSize)
	}
	for i := range report.Tasks {
		if report.Tasks[i].ParentStatus != "done" {
			t.Fatalf("task[%d] parent_status = %q, want done", i, report.Tasks[i].ParentStatus)
		}
	}
}

// TestResolveParentStatuses_PartialFailureKeepsOtherWorkspaces pins the
// best-effort contract: one workspace failing must not blank out the rest, and
// the error still surfaces so the CLI can warn.
func TestResolveParentStatuses_PartialFailureKeepsOtherWorkspaces(t *testing.T) {
	t.Parallel()

	wsGood := "11111111-1111-1111-1111-111111111111"
	wsBad := "22222222-2222-2222-2222-222222222222"
	report := &DiskUsageReport{Tasks: []TaskDiskUsage{
		issueTask(wsGood, "aaaa1111", "issue-good"),
		issueTask(wsBad, "bbbb1111", "issue-bad"),
	}}

	fetch := func(_ context.Context, workspaceID string, _ []string, _ []string) (map[string]IssueGCCheckResult, map[string]TaskEnvironmentGCCheckResult, error) {
		if workspaceID == wsBad {
			return nil, nil, errors.New("boom")
		}
		return foundIssueResults(map[string]string{"issue-good": "done"}), nil, nil
	}

	err := ResolveParentStatuses(context.Background(), report, fetch)
	if err == nil {
		t.Fatal("expected the failing workspace's error to surface")
	}
	if report.Tasks[0].ParentStatus != "done" {
		t.Errorf("healthy workspace status = %q, want done", report.Tasks[0].ParentStatus)
	}
	if report.Tasks[1].ParentStatus != "" {
		t.Errorf("failed workspace status = %q, want empty", report.Tasks[1].ParentStatus)
	}
}

// TestResolveParentStatuses_NoFetcherIsNoOp keeps `disk-usage` usable offline
// or logged out: with no way to resolve, the scan result passes through
// untouched instead of erroring.
func TestResolveParentStatuses_NoFetcherIsNoOp(t *testing.T) {
	t.Parallel()

	report := &DiskUsageReport{Tasks: []TaskDiskUsage{
		issueTask("11111111-1111-1111-1111-111111111111", "aaaa1111", "issue-1"),
	}}
	if err := ResolveParentStatuses(context.Background(), report, nil); err != nil {
		t.Fatalf("nil fetcher should be a no-op, got %v", err)
	}
	if report.Tasks[0].ParentStatus != "" {
		t.Errorf("parent_status = %q, want empty", report.Tasks[0].ParentStatus)
	}
	if err := ResolveParentStatuses(context.Background(), nil, func(context.Context, string, []string, []string) (map[string]IssueGCCheckResult, map[string]TaskEnvironmentGCCheckResult, error) {
		t.Fatal("fetcher must not run for a nil report")
		return nil, nil, nil
	}); err != nil {
		t.Fatalf("nil report should be a no-op, got %v", err)
	}
}

func TestResolveParentStatuses_MissingManagedWorkDirCannotResume(t *testing.T) {
	t.Parallel()

	task := issueTask("11111111-1111-1111-1111-111111111111", "aaaa1111", "issue-1")
	task.managedWorkDirSeen = boolPointer(false)
	report := &DiskUsageReport{Tasks: []TaskDiskUsage{task}}
	fetch := func(_ context.Context, _ string, issueIDs, taskIDs []string) (map[string]IssueGCCheckResult, map[string]TaskEnvironmentGCCheckResult, error) {
		return foundIssueResults(map[string]string{issueIDs[0]: "todo"}), map[string]TaskEnvironmentGCCheckResult{
			taskIDs[0]: {TaskID: taskIDs[0], Found: true, ResumeCandidate: true},
		}, nil
	}

	if err := ResolveParentStatuses(context.Background(), report, fetch); err != nil {
		t.Fatalf("ResolveParentStatuses: %v", err)
	}
	if got := report.Tasks[0].ResumeCandidate; got == nil || *got {
		t.Fatalf("resume_candidate = %v, want false when the managed workdir is gone", got)
	}
}

func TestApplyDiskUsageRetentionPolicy(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	basePolicy := &GCPolicySnapshot{
		Enabled:                 true,
		TTLSeconds:              int64((72 * time.Hour) / time.Second),
		CompletedTaskTTLSeconds: int64((14 * 24 * time.Hour) / time.Second),
		OrphanTTLSeconds:        int64((7 * 24 * time.Hour) / time.Second),
	}
	completedRecent := now.Add(-24 * time.Hour)
	completedOld := now.Add(-15 * 24 * time.Hour)
	terminalRecent := now.Add(-24 * time.Hour)
	parentMissing := false

	tests := []struct {
		name          string
		task          TaskDiskUsage
		policy        *GCPolicySnapshot
		wantReason    string
		wantCleanupAt *time.Time
	}{
		{
			name:       "policy unavailable",
			task:       issueTask("ws", "task", "issue"),
			policy:     nil,
			wantReason: RetentionPolicyUnavailable,
		},
		{
			name:       "gc disabled",
			task:       issueTask("ws", "task", "issue"),
			policy:     &GCPolicySnapshot{},
			wantReason: RetentionGCDisabled,
		},
		{
			name:       "local directory is never fully removed",
			task:       TaskDiskUsage{Kind: string(execenv.GCKindIssue), ParentID: "issue", LocalDirectory: boolPointer(true)},
			policy:     nil,
			wantReason: RetentionLocalDirectory,
		},
		{
			name:          "unknown metadata uses orphan mtime estimate",
			task:          TaskDiskUsage{Kind: DiskUsageKindUnknown, AgeSeconds: int64((6 * 24 * time.Hour) / time.Second), orphanAgeSeconds: int64((6 * 24 * time.Hour) / time.Second), orphanAgeKnown: true},
			policy:        basePolicy,
			wantReason:    RetentionMetadataUnavailable,
			wantCleanupAt: timePtr(now.Add(24 * time.Hour)),
		},
		{
			name:          "inaccessible parent past orphan ttl is eligible",
			task:          TaskDiskUsage{Kind: string(execenv.GCKindIssue), ParentID: "issue", AgeSeconds: int64((20 * 24 * time.Hour) / time.Second), orphanAgeSeconds: int64((8 * 24 * time.Hour) / time.Second), orphanAgeKnown: true, parentFound: &parentMissing},
			policy:        basePolicy,
			wantReason:    RetentionCleanupEligible,
			wantCleanupAt: timePtr(now.Add(-24 * time.Hour)),
		},
		{
			name:       "offline parent status is fail closed",
			task:       issueTask("ws", "task", "issue"),
			policy:     basePolicy,
			wantReason: RetentionParentStatusUnavailable,
		},
		{
			name:          "open issue uses completed task bound",
			task:          TaskDiskUsage{Kind: string(execenv.GCKindIssue), ParentID: "issue", ParentStatus: "in_progress", CompletedAt: &completedRecent},
			policy:        basePolicy,
			wantReason:    RetentionCompletedTaskGrace,
			wantCleanupAt: timePtr(completedRecent.Add(14 * 24 * time.Hour)),
		},
		{
			name:          "terminal issue uses terminal grace",
			task:          TaskDiskUsage{Kind: string(execenv.GCKindIssue), ParentID: "issue", ParentStatus: "done", ParentUpdatedAt: &terminalRecent, CompletedAt: &completedRecent},
			policy:        basePolicy,
			wantReason:    RetentionTerminalParentGrace,
			wantCleanupAt: timePtr(terminalRecent.Add(72 * time.Hour)),
		},
		{
			name:          "earliest full cleanup policy wins",
			task:          TaskDiskUsage{Kind: string(execenv.GCKindIssue), ParentID: "issue", ParentStatus: "done", ParentUpdatedAt: timePtr(now.Add(-time.Hour)), CompletedAt: timePtr(now.Add(-13*24*time.Hour - 23*time.Hour))},
			policy:        basePolicy,
			wantReason:    RetentionCompletedTaskGrace,
			wantCleanupAt: timePtr(now.Add(time.Hour)),
		},
		{
			name:          "completed task past bound is eligible",
			task:          TaskDiskUsage{Kind: string(execenv.GCKindIssue), ParentID: "issue", ParentStatus: "todo", CompletedAt: &completedOld},
			policy:        basePolicy,
			wantReason:    RetentionCleanupEligible,
			wantCleanupAt: timePtr(completedOld.Add(14 * 24 * time.Hour)),
		},
		{
			name:       "non issue policy remains explicit",
			task:       TaskDiskUsage{Kind: string(execenv.GCKindChat), ParentID: "chat"},
			policy:     basePolicy,
			wantReason: RetentionKindPolicyUnavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := &DiskUsageReport{GeneratedAt: now, Tasks: []TaskDiskUsage{tc.task}}
			ApplyDiskUsageRetentionPolicy(report, tc.policy, now)
			got := report.Tasks[0]
			if got.RetentionReason != tc.wantReason {
				t.Errorf("retention_reason = %q, want %q", got.RetentionReason, tc.wantReason)
			}
			if tc.wantReason == RetentionLocalDirectory && (got.ResumeCandidate == nil || *got.ResumeCandidate) {
				t.Errorf("local_directory resume_candidate = %v, want false", got.ResumeCandidate)
			}
			if tc.wantCleanupAt == nil {
				if got.EstimatedCleanupAt != nil {
					t.Errorf("estimated_cleanup_at = %s, want nil", got.EstimatedCleanupAt)
				}
			} else if got.EstimatedCleanupAt == nil || !got.EstimatedCleanupAt.Equal(*tc.wantCleanupAt) {
				t.Errorf("estimated_cleanup_at = %v, want %v", got.EstimatedCleanupAt, tc.wantCleanupAt)
			}
			if report.GCPolicy != tc.policy {
				t.Error("report did not retain the effective policy snapshot")
			}
		})
	}
}

func boolPointer(value bool) *bool {
	return &value
}

// TestScanDiskUsage_ReportsRepoCacheSeparately pins the accounting split: the
// bare-repo cache is measured (it used to be invisible, which made the reported
// total silently disagree with the user's file manager) but kept out of the
// task totals, since every task checks out from it and none contains it.
func TestScanDiskUsage_ReportsRepoCacheSeparately(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	wsID := "11111111-1111-1111-1111-111111111111"
	writeFile(t, filepath.Join(root, wsID, "aaaaaaaa", "workdir/main.go"), 1000)

	// .repos/<workspace>/<repo>/... — the repo dir is the unit the GC evicts.
	writeFile(t, filepath.Join(root, ".repos", wsID, "widgets.git", "objects/pack/x"), 4000)
	writeFile(t, filepath.Join(root, ".repos", wsID, "gadgets.git", "objects/pack/y"), 2000)

	report, err := ScanDiskUsage(root, nil)
	if err != nil {
		t.Fatalf("ScanDiskUsage: %v", err)
	}

	if report.RepoCacheSizeBytes != 6000 {
		t.Errorf("repo_cache_size_bytes = %d, want 6000", report.RepoCacheSizeBytes)
	}
	if report.RepoCacheCount != 2 {
		t.Errorf("repo_cache_count = %d, want 2", report.RepoCacheCount)
	}
	if report.TotalSizeBytes != 1000 {
		t.Errorf("total_size_bytes = %d, want 1000 (task dirs only, cache excluded)", report.TotalSizeBytes)
	}
	if report.TotalWorkspaceCount != 1 {
		t.Errorf("total_workspace_count = %d, want 1 (.repos is not a workspace)", report.TotalWorkspaceCount)
	}
}

// TestScanDiskUsage_SkipsDaemonInternalDotDirs keeps caches like .skill-cache
// out of the per-workspace table, where they used to surface as bogus
// workspace rows alongside the real ones.
func TestScanDiskUsage_SkipsDaemonInternalDotDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	wsID := "11111111-1111-1111-1111-111111111111"
	writeFile(t, filepath.Join(root, wsID, "aaaaaaaa", "workdir/main.go"), 1000)
	writeFile(t, filepath.Join(root, ".skill-cache", "v1", "bundle", "skill.md"), 500)

	report, err := ScanDiskUsage(root, nil)
	if err != nil {
		t.Fatalf("ScanDiskUsage: %v", err)
	}

	if report.TotalWorkspaceCount != 1 {
		t.Fatalf("total_workspace_count = %d, want 1", report.TotalWorkspaceCount)
	}
	for _, ws := range report.Workspaces {
		if strings.HasPrefix(ws.WorkspaceID, ".") {
			t.Errorf("dot-directory %q reported as a workspace", ws.WorkspaceID)
		}
	}
	if report.TotalSizeBytes != 1000 {
		t.Errorf("total_size_bytes = %d, want 1000 (skill cache excluded)", report.TotalSizeBytes)
	}
}
