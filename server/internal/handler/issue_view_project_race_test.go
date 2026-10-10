package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Scan barriers pause after a statement has actually completed, not when
// QueryRow returns its lazy row. The read barrier also exercises the old path.
type projectViewScanRow struct {
	pgx.Row
	ctx   context.Context
	after func(context.Context) error
}

func (r projectViewScanRow) Scan(dest ...any) error {
	if err := r.Row.Scan(dest...); err != nil {
		return err
	}
	return r.after(r.ctx)
}

type projectViewProbeDB struct {
	db.DBTX
	afterRead func(context.Context) error
}

func (d projectViewProbeDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	row := d.DBTX.QueryRow(ctx, sql, args...)
	if strings.Contains(sql, "-- name: GetProjectInWorkspace") {
		return projectViewScanRow{Row: row, ctx: ctx, after: d.afterRead}
	}
	return row
}

type projectViewTxStarter struct {
	beginErr error
	wrap     func(pgx.Tx) pgx.Tx
}

func (s projectViewTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	if s.beginErr != nil {
		return nil, s.beginErr
	}
	tx, err := testPool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return s.wrap(tx), nil
}

type projectViewProbeTx struct {
	pgx.Tx
	afterInsert                   func(context.Context) error
	lockErr, insertErr, commitErr error
}

func (tx projectViewProbeTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "-- name: LockProjectForIssueViewCreate") && tx.lockErr != nil {
		return errorRow{tx.lockErr}
	}
	if strings.Contains(sql, "-- name: CreateIssueView") && tx.insertErr != nil {
		return errorRow{tx.insertErr}
	}
	row := tx.Tx.QueryRow(ctx, sql, args...)
	if strings.Contains(sql, "-- name: CreateIssueView") && tx.afterInsert != nil {
		return projectViewScanRow{Row: row, ctx: ctx, after: tx.afterInsert}
	}
	return row
}
func (tx projectViewProbeTx) Commit(ctx context.Context) error {
	if tx.commitErr != nil {
		return tx.commitErr
	}
	return tx.Tx.Commit(ctx)
}
func projectViewRequest(projectID string) *http.Request {
	return newRequest("POST", "/api/issue-views", map[string]any{
		"name": "Project race view", "scope_type": "project", "scope_id": projectID, "query": map[string]any{},
	})
}
func assertNoProjectViews(t *testing.T, projectID string) {
	t.Helper()
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM issue_view WHERE scope_type = 'project' AND scope_id = $1`, projectID).Scan(&count)
	if count != 0 {
		t.Fatalf("project has %d orphan views, want 0", count)
	}
}
func awaitProjectViewResponse(t *testing.T, done <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case response := <-done:
		return response
	case <-time.After(10 * time.Second):
		t.Fatal("handler did not finish within bounded wait")
		return nil
	}
}
func TestProjectIssueViewCreateDeleteFirst(t *testing.T) {
	projectID := dbfx.Project(t, "Delete first view race")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	pid := holderBackendPID(t, ctx, tx)
	qtx := testHandler.Queries.WithTx(tx)
	if _, err := qtx.LockProjectForDelete(ctx, db.LockProjectForDeleteParams{ID: parseUUID(projectID), WorkspaceID: parseUUID(testWorkspaceID)}); err != nil {
		t.Fatal(err)
	}
	if err := qtx.DeleteIssueViewsByProjectScope(ctx, db.DeleteIssueViewsByProjectScopeParams{WorkspaceID: parseUUID(testWorkspaceID), ScopeID: parseUUID(projectID)}); err != nil {
		t.Fatal(err)
	}
	if err := qtx.DeleteProject(ctx, db.DeleteProjectParams{ID: parseUUID(projectID), WorkspaceID: parseUUID(testWorkspaceID)}); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{}, 1)
	defer close(release)
	h := *testHandler
	h.Queries = db.New(projectViewProbeDB{DBTX: testPool, afterRead: func(ctx context.Context) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		h.CreateIssueView(w, projectViewRequest(projectID).WithContext(ctx))
		done <- w
	}()
	blocked := waitForWaiterBlockedBy(t, pid, 2*time.Second)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// The old implementation read the still-visible row without locking it.
	release <- struct{}{}
	w := awaitProjectViewResponse(t, done)
	if !blocked {
		t.Errorf("creator did not wait on the deleting transaction's project lock")
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("create = %d (%s), want 404", w.Code, w.Body.String())
	}
	assertNoProjectViews(t, projectID)
}
func TestProjectIssueViewCreateFirstHoldsDelete(t *testing.T) {
	projectID := dbfx.Project(t, "Create first view race")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	inserted := make(chan int, 1)
	release := make(chan struct{}, 1)
	defer close(release)
	h := *testHandler
	h.TxStarter = projectViewTxStarter{wrap: func(tx pgx.Tx) pgx.Tx {
		pid := int(tx.Conn().PgConn().PID())
		return projectViewProbeTx{Tx: tx, afterInsert: func(ctx context.Context) error {
			inserted <- pid
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}}
	}}
	created := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		h.CreateIssueView(w, projectViewRequest(projectID).WithContext(ctx))
		created <- w
	}()
	var pid int
	select {
	case pid = <-inserted:
	case w := <-created:
		t.Fatalf("creator returned %d before transactional insert barrier", w.Code)
	case <-ctx.Done():
		t.Fatal("creator did not reach insert barrier")
	}
	deleted := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		req := withURLParam(newRequest("DELETE", "/api/projects/"+projectID, nil).WithContext(ctx), "id", projectID)
		testHandler.DeleteProject(w, req)
		deleted <- w
	}()
	blocked := waitForWaiterBlockedBy(t, pid, 2*time.Second)
	if !blocked {
		t.Error("delete did not wait on creator's project key-share lock")
	}
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM issue_view WHERE scope_id = $1`, projectID).Scan(&count)
	if count != 0 {
		t.Errorf("uncommitted view visible: %d", count)
	}
	release <- struct{}{}
	w := awaitProjectViewResponse(t, created)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	w = awaitProjectViewResponse(t, deleted)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", w.Code, w.Body.String())
	}
	assertNoProjectViews(t, projectID)
}
func TestProjectIssueViewCreateRollback(t *testing.T) {
	for _, stage := range []string{"begin", "lock", "insert", "commit", "missing"} {
		t.Run(stage, func(t *testing.T) {
			projectID := dbfx.Project(t, "Rollback view "+stage)
			h := *testHandler
			injected := errors.New("injected project view failure")
			starter := projectViewTxStarter{wrap: func(tx pgx.Tx) pgx.Tx {
				probe := projectViewProbeTx{Tx: tx}
				switch stage {
				case "lock":
					probe.lockErr = injected
				case "insert":
					probe.insertErr = injected
				case "commit":
					probe.commitErr = injected
				case "missing":
					probe.lockErr = pgx.ErrNoRows
				}
				return probe
			}}
			if stage == "begin" {
				starter.beginErr = injected
			}
			h.TxStarter = starter
			want := http.StatusInternalServerError
			if stage == "missing" {
				want = http.StatusNotFound
			}
			testutil.Call(t, h.CreateIssueView, projectViewRequest(projectID)).Want(want)
			assertNoProjectViews(t, projectID)
			// Errors must release the project lock as well as roll back the insert.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			testutil.Call(t, testHandler.DeleteProject, withURLParam(newRequest("DELETE", "/api/projects/"+projectID, nil).WithContext(ctx), "id", projectID)).Want(http.StatusNoContent)
		})
	}
}
func TestProjectIssueViewForeignWorkspaceAndSequentialSweep(t *testing.T) {
	foreignWS := dbfx.Insert(t, "workspace", testutil.Cols{"name": "Foreign view workspace", "slug": "foreign-view-" + strings.ToLower(t.Name()), "issue_prefix": "FVW"})
	foreignProject := dbfx.Project(t, "Foreign view project", testutil.Cols{"workspace_id": foreignWS})
	testutil.Call(t, testHandler.CreateIssueView, projectViewRequest(foreignProject)).Want(http.StatusNotFound)
	assertNoProjectViews(t, foreignProject)
	projectID := dbfx.Project(t, "Sequential view cleanup")
	var view IssueViewResponse
	testutil.Call(t, testHandler.CreateIssueView, projectViewRequest(projectID)).Want(http.StatusCreated).JSON(&view)
	dbfx.Insert(t, "pinned_item", testutil.Cols{"workspace_id": testWorkspaceID, "user_id": testUserID, "item_type": "view", "item_id": view.ID})
	testutil.Call(t, testHandler.DeleteProject, withURLParam(newRequest("DELETE", "/api/projects/"+projectID, nil), "id", projectID)).Want(http.StatusNoContent)
	assertNoProjectViews(t, projectID)
	var pins int
	dbfx.QueryRow(t, `SELECT count(*) FROM pinned_item WHERE item_type = 'view' AND item_id = $1`, view.ID).Scan(&pins)
	if pins != 0 {
		t.Fatalf("deleted project view has %d pins", pins)
	}
	var projects int
	dbfx.QueryRow(t, `SELECT count(*) FROM project WHERE id = $1`, projectID).Scan(&projects)
	if projects != 0 {
		t.Fatal("deleted project still exists")
	}
}
