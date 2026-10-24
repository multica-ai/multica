package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

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

// TestUpdateProjectResourceConcurrentPartialUpdate serializes a label-only
// update and a ref-only update at the read edge: the label request pauses
// right after reading the row, the ref request completes, then the label
// request resumes. The persisted row must keep the ref update — the reader's
// stale snapshot must not silently revert a field it never received
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

	paused, release := make(chan struct{}), make(chan struct{})
	h := *testHandler
	h.Queries = db.New(resourceReadPauseDB{DBTX: testPool, paused: paused, release: release})
	h.TxStarter = &contextLockTestStarter{
		pool: testPool, pauseAfter: "GetProjectResourceInWorkspaceForUpdate",
		paused: paused, release: release,
	}
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
		map[string]any{"resource_ref": map[string]any{
			"url": "https://github.com/example/repo", "ref": "release",
		}}), "id", projectID, "resourceId", resourceID)
	doneB := make(chan error, 1)
	go func() {
		testHandler.UpdateProjectResource(recB, reqB)
		doneB <- nil
	}()

	resume()
	waitContextLockResult(t, doneA, "label-only update")
	waitContextLockResult(t, doneB, "ref update")
	if recA.Code != http.StatusOK {
		t.Fatalf("label-only update status = %d, want 200", recA.Code)
	}
	if recB.Code != http.StatusOK {
		t.Fatalf("ref update status = %d, want 200", recB.Code)
	}

	stored, err := testHandler.Queries.GetProjectResourceInWorkspace(ctx, db.GetProjectResourceInWorkspaceParams{
		ID: parseUUID(resourceID), WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatal(err)
	}
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
