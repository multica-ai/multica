package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	pruneWorkspaceID = "a05b0e10-ee7a-4603-a72d-a548b2390cb2"
	pruneTTL         = 72 * time.Hour
)

func pruneParams(root, taskID string) RootDirParams {
	return RootDirParams{
		WorkspacesRoot:  root,
		WorkspaceID:     pruneWorkspaceID,
		WorkspaceSlug:   "Asset Feed",
		TaskID:          taskID,
		IssueIdentifier: "MUL-6063",
	}
}

func pruneTaskRootIndexForTest(t *testing.T, root string, now time.Time, eligible func(string, string) bool) int {
	t.Helper()
	removed, err := PruneTaskRootIndex(root, pruneTTL, now, eligible)
	if err != nil {
		t.Fatalf("PruneTaskRootIndex: %v", err)
	}
	return removed
}

// TestPruneTaskRootIndexKeepsTheFreezeWindow is the guard that matters most
// here. ResolveRootDir installs a record BEFORE Prepare creates the directory,
// so "record with no env root" is the correct state for the window in between.
// A sweep that treated absence alone as garbage would delete a live task's
// frozen identity and hand the same task a second physical root — the exact
// orphaning the index exists to prevent.
func TestPruneTaskRootIndexKeepsTheFreezeWindow(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	params := pruneParams(root, "5c57b65b-ee7a-4603-a72d-b659c34a1dc3")
	frozen, err := ResolveRootDir(params)
	if err != nil {
		t.Fatalf("ResolveRootDir: %v", err)
	}
	if _, err := os.Stat(frozen); !os.IsNotExist(err) {
		t.Fatalf("fixture must represent the pre-Prepare window; stat err = %v", err)
	}

	if removed := pruneTaskRootIndexForTest(t, root, time.Now(), func(string, string) bool { return true }); removed != 0 {
		t.Fatalf("removed %d records inside the freeze window, want 0", removed)
	}
	again, err := ResolveRootDir(params)
	if err != nil {
		t.Fatalf("ResolveRootDir after prune: %v", err)
	}
	if again != frozen {
		t.Fatalf("task moved from %q to %q after a prune cycle", frozen, again)
	}
}

func TestPruneTaskRootIndexReclaimsOnlyStrandedRecords(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	liveParams := pruneParams(root, "5c57b65b-ee7a-4603-a72d-b659c34a1dc3")
	strandedParams := pruneParams(root, "5c57b65b-ee7a-4603-a72d-c760d45b2ed4")

	livePath, err := ResolveRootDir(liveParams)
	if err != nil {
		t.Fatalf("resolve live root: %v", err)
	}
	if err := os.MkdirAll(livePath, 0o755); err != nil {
		t.Fatalf("create live root: %v", err)
	}
	// The stranded task got a record but never reached ClaimEnvRoot, so its
	// root never materialised and cleanTaskDir will never see it.
	if _, err := ResolveRootDir(strandedParams); err != nil {
		t.Fatalf("resolve stranded root: %v", err)
	}

	past := time.Now().Add(pruneTTL + time.Hour)
	if removed := pruneTaskRootIndexForTest(t, root, past, func(string, string) bool { return true }); removed != 1 {
		t.Fatalf("removed %d records, want exactly the stranded one", removed)
	}
	if _, err := os.Stat(taskRootRecordDir(strandedParams)); !os.IsNotExist(err) {
		t.Fatalf("stranded record survived the sweep; stat err = %v", err)
	}
	resolved, err := ResolveRootDir(liveParams)
	if err != nil {
		t.Fatalf("resolve live root after sweep: %v", err)
	}
	if resolved != livePath {
		t.Fatalf("live task moved from %q to %q after the sweep", livePath, resolved)
	}
}

// A record that cannot be parsed is what keeps its task failing closed. The
// sweep must not quietly convert that into "propose a fresh root", because the
// original root may still hold work.
func TestPruneTaskRootIndexKeepsUnreadableRecords(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	params := pruneParams(root, "5c57b65b-ee7a-4603-a72d-b659c34a1dc3")
	recordDir := taskRootRecordDir(params)
	if err := os.MkdirAll(recordDir, 0o755); err != nil {
		t.Fatalf("seed record dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(recordDir, taskRootRecordFile), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed corrupt record: %v", err)
	}

	past := time.Now().Add(pruneTTL + time.Hour)
	removed, err := PruneTaskRootIndex(root, pruneTTL, past, func(string, string) bool { return true })
	if err == nil {
		t.Fatal("PruneTaskRootIndex did not report the unreadable record")
	}
	if removed != 0 {
		t.Fatalf("removed %d unreadable records, want 0", removed)
	}
	if _, err := os.Stat(filepath.Join(recordDir, taskRootRecordFile)); err != nil {
		t.Fatalf("unreadable record was reclaimed: %v", err)
	}
}

// installTaskRootRecord stages into .pending-* and renames. A staging directory
// that outlives the gate is an install that died before the rename, so it was
// never authoritative for any task.
func TestPruneTaskRootIndexReclaimsAbandonedStagingDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	indexDir := filepath.Join(root, taskRootIndexDir)
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatalf("seed index: %v", err)
	}
	pending, err := os.MkdirTemp(indexDir, taskRootPendingPrefix)
	if err != nil {
		t.Fatalf("seed staging dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pending, taskRootRecordFile), []byte(`{"workspace_id":"w"}`), 0o644); err != nil {
		t.Fatalf("seed staging record: %v", err)
	}

	if removed := pruneTaskRootIndexForTest(t, root, time.Now(), nil); removed != 0 {
		t.Fatalf("removed %d staging dirs inside the gate, want 0", removed)
	}
	past := time.Now().Add(pruneTTL + time.Hour)
	if removed := pruneTaskRootIndexForTest(t, root, past, nil); removed != 1 {
		t.Fatalf("removed %d staging dirs past the gate, want 1", removed)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("abandoned staging dir survived; stat err = %v", err)
	}
}

func TestPruneTaskRootIndexReclaimsEmptyFinalRecordDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	params := pruneParams(root, "5c57b65b-ee7a-4603-a72d-b659c34a1dc3")
	recordDir := taskRootRecordDir(params)
	if err := os.MkdirAll(recordDir, 0o755); err != nil {
		t.Fatalf("seed empty final record dir: %v", err)
	}

	past := time.Now().Add(pruneTTL + time.Hour)
	if removed := pruneTaskRootIndexForTest(t, root, past, nil); removed != 1 {
		t.Fatalf("removed %d empty final record dirs, want 1", removed)
	}
	if _, err := os.Stat(recordDir); !os.IsNotExist(err) {
		t.Fatalf("empty final record dir survived; stat err = %v", err)
	}
}

// writeEnvRootOwner stages into a temp file and renames. A crash in between
// leaves a root holding nothing but that temp — no task content, and no owner
// marker either. findOwnedTaskRoot inspects candidates without the env root
// lock, so it must read that root as adoptable rather than refusing it as
// "holds files but has no owner", which would wedge the task permanently.
func TestResolveRootDirAdoptsRootHoldingOnlyAnUnpublishedOwnerTemp(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	const taskID = "5c57b65b-ee7a-4603-a72d-b659c34a1dc3"
	params := pruneParams(root, taskID)
	original := PredictRootDir(params)
	if err := os.MkdirAll(original, 0o755); err != nil {
		t.Fatalf("seed env root: %v", err)
	}
	tmp, err := os.CreateTemp(original, envRootOwnerTempPrefix+"*"+envRootOwnerTempSuffix)
	if err != nil {
		t.Fatalf("seed unpublished owner temp: %v", err)
	}
	if _, err := tmp.WriteString(`{"workspace_id":"` + pruneWorkspaceID + `","task_id":"` + taskID + `"}`); err != nil {
		t.Fatalf("write unpublished owner temp: %v", err)
	}
	tmp.Close()
	if _, err := os.Stat(filepath.Join(original, envRootOwnerFile)); !os.IsNotExist(err) {
		t.Fatalf("fixture must have no published owner marker; stat err = %v", err)
	}

	resolved, err := ResolveRootDir(params)
	if err != nil {
		t.Fatalf("ResolveRootDir refused a root holding only an unpublished owner temp: %v", err)
	}
	if resolved != original {
		t.Fatalf("resolved %q, want the existing root %q", resolved, original)
	}

	claim, err := ClaimEnvRoot(params)
	if err != nil {
		t.Fatalf("ClaimEnvRoot: %v", err)
	}
	defer claim.Release()
	owner, err := ReadEnvRootOwner(original)
	if err != nil {
		t.Fatalf("ReadEnvRootOwner: %v", err)
	}
	if owner.TaskID != taskID || owner.WorkspaceID != pruneWorkspaceID {
		t.Fatalf("owner = %+v, want the claiming task's identity", owner)
	}
	entries, err := os.ReadDir(original)
	if err != nil {
		t.Fatalf("read env root: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), envRootOwnerTempPrefix) {
			t.Fatalf("claim left an unpublished owner temp behind: %s", e.Name())
		}
	}
}

// Every fail-closed error must name the record directory: the refusal is
// permanent until an operator removes it, and they need the path to do that.
func TestValidateTaskRootRecordErrorsNameTheRecordDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	params := pruneParams(root, "5c57b65b-ee7a-4603-a72d-b659c34a1dc3")
	recordDir := taskRootRecordDir(params)

	cases := []struct {
		name   string
		record taskRootRecord
	}{
		{
			name:   "identity mismatch",
			record: taskRootRecord{WorkspaceID: "other", TaskID: params.TaskID, RelativePath: filepath.Join("a", "b")},
		},
		{
			name:   "invalid relative path",
			record: taskRootRecord{WorkspaceID: params.WorkspaceID, TaskID: params.TaskID, RelativePath: "."},
		},
		{
			name:   "outside stable identity",
			record: taskRootRecord{WorkspaceID: params.WorkspaceID, TaskID: params.TaskID, RelativePath: filepath.Join("unrelated-workspace", "unrelated-task")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateTaskRootRecord(params, recordDir, tc.record)
			if err == nil {
				t.Fatal("validateTaskRootRecord accepted an invalid record")
			}
			if !strings.Contains(err.Error(), recordDir) {
				t.Fatalf("error %q does not name the record dir %q", err, recordDir)
			}
		})
	}
}
