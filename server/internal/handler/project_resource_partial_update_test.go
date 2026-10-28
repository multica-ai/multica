package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// resourceReadPauseDB pauses the non-transactional read path, so the racing
// update test also exercises (and fails on) the pre-fix code that reads
// outside a transaction.
type resourceReadPauseDB struct {
	db.DBTX
	paused, release chan struct{}
}

func (d resourceReadPauseDB) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	row := d.DBTX.QueryRow(ctx, query, args...)
	// Prefix (not equality): matches both the plain read and the
	// FOR UPDATE variant added by the fix, whichever the handler uses.
	if strings.HasPrefix(query, "-- name: GetProjectResourceInWorkspace") {
		return &contextLockPauseRow{Row: row, paused: d.paused, release: d.release}
	}
	return row
}

// pausedUpdateHandler returns a copy of the test handler whose project
// resource read pauses until resume is called: on the pre-fix code the pause
// sits in the plain read, with the fix it sits in the FOR UPDATE read inside
// the transaction — the prefix match in resourceReadPauseDB and the
// contextLockTestStarter cover both shapes.
func pausedUpdateHandler(paused, release chan struct{}) *Handler {
	h := *testHandler
	h.Queries = db.New(resourceReadPauseDB{DBTX: testPool, paused: paused, release: release})
	h.TxStarter = &contextLockTestStarter{
		pool: testPool, pauseAfter: "GetProjectResourceInWorkspaceForUpdate",
		paused: paused, release: release,
	}
	return &h
}

// waitRefRequestGated makes the two-request race deterministic: it returns
// only once the racing ref-only update has either completed outright or is
// blocked inside its FOR UPDATE read waiting for the paused request's row
// lock. The paused request is resumed only after that point, so the write
// order is fixed instead of left to the scheduler.
//
// Which of the two happens separates the worlds. On the pre-fix code nothing
// blocks the ref request: it reads and writes while the paused request holds
// no lock, so its write has already landed when this returns, and the resumed
// write (derived from a stale snapshot) reverts it — the assertion fails. On
// the serialized code the ref request cannot complete: its FOR UPDATE read
// waits for the row lock the paused request holds, visible the way a DBA
// would see it, as wait_event_type='Lock' on the backend running that query.
// The pre-fix code never issues that query, so the lock signal cannot fire
// and completion is the only exit — the two outcomes are mutually exclusive
// rather than timing-dependent. Completion needs no deadline (however slow,
// it is observed); the deadline only bounds the fixed-world wait for the
// blocked backend to show up.
func waitRefRequestGated(t *testing.T, ctx context.Context, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return
		default:
		}
		var lockWaiters int
		if err := testPool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
WHERE wait_event_type = 'Lock' AND query LIKE '%GetProjectResourceInWorkspaceForUpdate%'`).Scan(&lockWaiters); err == nil && lockWaiters > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("ref update neither completed nor became blocked on the row lock")
}

// runConcurrentPartialUpdate starts a label-only update that pauses right
// after its row read, starts the caller's ref-only update against the real
// handler, and releases the paused request only once the race outcome is
// decided. Both recorders must end at 200.
func runConcurrentPartialUpdate(t *testing.T, ctx context.Context, projectID, resourceID string, refBody map[string]any) {
	t.Helper()

	paused, release := make(chan struct{}), make(chan struct{})
	h := pausedUpdateHandler(paused, release)
	var releaseOnce sync.Once
	resume := func() { releaseOnce.Do(func() { close(release) }) }
	defer resume()

	recA := httptest.NewRecorder()
	reqA := withURLParams(newRequest("PUT", "/api/projects/"+projectID+"/resources/"+resourceID,
		map[string]any{"label": "New label"}), "id", projectID, "resourceId", resourceID)
	doneA := make(chan error, 1)
	go func() {
		h.UpdateProjectResource(recA, reqA)
		doneA <- nil
	}()
	waitContextLockSignal(t, paused, "label update did not read resource")

	recB := httptest.NewRecorder()
	reqB := withURLParams(newRequest("PUT", "/api/projects/"+projectID+"/resources/"+resourceID,
		map[string]any{"resource_ref": refBody}), "id", projectID, "resourceId", resourceID)
	doneB := make(chan error, 1)
	go func() {
		testHandler.UpdateProjectResource(recB, reqB)
		doneB <- nil
		// Closed after the send so both observers of this channel —
		// waitRefRequestGated's completion probe and the final wait below —
		// can receive without draining each other: once closed, every
		// receive returns immediately.
		close(doneB)
	}()

	waitRefRequestGated(t, ctx, doneB)
	resume()
	waitContextLockResult(t, doneA, "label-only update")
	waitContextLockResult(t, doneB, "ref update")
	if recA.Code != http.StatusOK {
		t.Fatalf("label-only update status = %d, want 200", recA.Code)
	}
	if recB.Code != http.StatusOK {
		t.Fatalf("ref update status = %d, want 200", recB.Code)
	}
}

// storedProjectResource reads the row back after both updates settled.
func storedProjectResource(t *testing.T, ctx context.Context, resourceID string) db.ProjectResource {
	t.Helper()
	stored, err := testHandler.Queries.GetProjectResourceInWorkspace(ctx, db.GetProjectResourceInWorkspaceParams{
		ID: parseUUID(resourceID), WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

// TestUpdateProjectResourceConcurrentPartialUpdate serializes a label-only
// update and a ref-only update at the read edge: the label request pauses
// right after reading the row, the ref request is only released once it has
// either completed (pre-fix code) or blocked on the row lock (fix), then the
// label request resumes. The persisted row must keep the ref update — the
// reader's stale snapshot must not silently revert a field it never received
// (#9238). With the fix, the reader holds the row lock from its FOR UPDATE
// read until commit, so the ref request waits and re-reads after the label
// write lands.
func TestUpdateProjectResourceConcurrentPartialUpdate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Fatal("database-backed handler test fixture is required")
	}

	ctx := context.Background()
	projectID := dbfx.Project(t, "racing project")
	resourceID := dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    projectID,
		"workspace_id":  testWorkspaceID,
		"resource_type": "github_repo",
		"resource_ref":  `{"url":"https://github.com/example/repo","ref":"main"}`,
		"label":         "Old label",
		"position":      1,
	})

	runConcurrentPartialUpdate(t, ctx, projectID, resourceID, map[string]any{
		"url": "https://github.com/example/repo", "ref": "release",
	})

	stored := storedProjectResource(t, ctx, resourceID)
	var ref struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(stored.ResourceRef, &ref); err != nil {
		t.Fatalf("stored ref %s: %v", stored.ResourceRef, err)
	}
	if ref.Ref != "release" {
		t.Errorf("stored ref = %q, want the concurrent ref update (release)", ref.Ref)
	}
	if !stored.Label.Valid || stored.Label.String != "New label" {
		t.Errorf("stored label = %v, want New label", stored.Label)
	}
	if stored.Position != 1 {
		t.Errorf("stored position = %d, want 1", stored.Position)
	}
}

// TestUpdateProjectResourceConcurrentLabelMirrorVsExecutionMode covers the
// same stale-snapshot race through the local_directory label-mirror write: a
// label-only update races an execution_mode update, and the mirror step
// rewrites the whole ref from the row the label request read. The persisted
// ref must keep the racing mode change — a stale ref snapshot must not carry
// the old execution_mode back in behind a rename (#9238). With the fix the
// label request writes first and the mode request re-reads, so its ref (which
// intentionally omits execution_mode, i.e. in_place) lands last.
func TestUpdateProjectResourceConcurrentLabelMirrorVsExecutionMode(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Fatal("database-backed handler test fixture is required")
	}

	ctx := context.Background()
	projectID := dbfx.Project(t, "racing local directory project")
	resourceID := dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    projectID,
		"workspace_id":  testWorkspaceID,
		"resource_type": "local_directory",
		"resource_ref":  `{"local_path":"/repo","daemon_id":"daemon-1","execution_mode":"worktree"}`,
		"label":         "Old label",
		"position":      1,
	})

	runConcurrentPartialUpdate(t, ctx, projectID, resourceID, map[string]any{
		"local_path": "/repo", "daemon_id": "daemon-1",
	})

	stored := storedProjectResource(t, ctx, resourceID)
	var ref struct {
		ExecutionMode string `json:"execution_mode"`
	}
	if err := json.Unmarshal(stored.ResourceRef, &ref); err != nil {
		t.Fatalf("stored ref %s: %v", stored.ResourceRef, err)
	}
	if ref.ExecutionMode != "" {
		t.Errorf("stored execution_mode = %q, want the concurrent mode update (cleared = in_place)", ref.ExecutionMode)
	}
	if !stored.Label.Valid || stored.Label.String != "New label" {
		t.Errorf("stored label = %v, want New label", stored.Label)
	}
	if stored.Position != 1 {
		t.Errorf("stored position = %d, want 1", stored.Position)
	}
}
