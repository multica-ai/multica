package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Count point reads independently of the notification catalog seam. For these
// unassigned children, the only legitimate point read is target validation;
// transaction-local archive guards and response fillers are deliberately not
// included in the transition phase's catalog budget.
type batchTransitionDB struct {
	db.DBTX
	pointReads       int
	beforeParentRead func()
	failUpdateID     pgtype.UUID
}

func (q *batchTransitionDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "-- name: UpdateIssue :one") && q.failUpdateID.Valid && args[0] == q.failUpdateID {
		return batchTransitionWriteError{}
	}
	if strings.HasPrefix(sql, "-- name: GetIssueStatusEntryByKey :one") {
		q.pointReads++
	}
	if strings.HasPrefix(sql, "-- name: GetIssue :one") && q.beforeParentRead != nil {
		beforeRead := q.beforeParentRead
		q.beforeParentRead = nil
		beforeRead()
	}
	return q.DBTX.QueryRow(ctx, sql, args...)
}

type batchTransitionWriteError struct{}

func (batchTransitionWriteError) Scan(...any) error {
	return errors.New("test issue write failure")
}

func batchTransitionRequest(ws string, ids []string, updates map[string]any) *http.Request {
	return testutil.WithHeaders(testutil.JSONRequest(http.MethodPatch, "/api/issues/batch", map[string]any{
		"issue_ids": ids, "updates": updates,
	}), "X-Workspace-ID", ws, "X-User-ID", testUserID)
}

func TestBatchTransitionStatusResolver(t *testing.T) {
	for _, tc := range []struct {
		name, previous, current string
		noParent                bool
		transitionReads         int
		notificationReads       int
		comments                int
	}{
		{name: "built_in_done", previous: "todo", current: "done", comments: 1},
		{name: "built_in_cancelled", previous: "todo", current: "cancelled", comments: 1},
		{name: "custom_previous", previous: "working", current: "done", transitionReads: 1, comments: 1},
		{name: "custom_to_cancelled", previous: "working", current: "cancelled", transitionReads: 1, comments: 1},
		{name: "custom_target", previous: "todo", current: "approved", transitionReads: 1, notificationReads: 1, comments: 1},
		{name: "both_custom", previous: "working", current: "approved", transitionReads: 1, notificationReads: 1, comments: 1},
		{name: "custom_cancelled", previous: "working", current: "dropped", transitionReads: 1, notificationReads: 1, comments: 1},
		{name: "archived_previous", previous: "retired_working", current: "done", transitionReads: 1, comments: 1},
		{name: "archived_terminal", previous: "retired_done", current: "done", transitionReads: 1},
		{name: "terminal_to_terminal", previous: "approved", current: "dropped", transitionReads: 1},
		{name: "no_change", previous: "approved", current: "approved"},
		{name: "no_parent", previous: "working", current: "done", noParent: true},
		{name: "unknown_previous", previous: "missing", current: "done", transitionReads: 1},
	} {
		for _, size := range []int{1, 25} {
			t.Run(fmt.Sprintf("%s/%d", tc.name, size), func(t *testing.T) {
				ws := dbfx.Workspace(t, "Batch transitions", "batch-transitions")
				fx := testutil.New(testPool, ws, testUserID)
				for key, category := range map[string]string{
					"working": "in_progress", "approved": "done", "dropped": "cancelled",
					"retired_working": "in_progress", "retired_done": "done",
				} {
					cols := testutil.Cols{"workspace_id": ws, "key": key, "name": key, "category": category, "color": "#123456"}
					if strings.HasPrefix(key, "retired_") {
						cols["archived_at"] = testutil.Raw("now()")
					}
					fx.Insert(t, "issue_status", cols)
				}
				agent := fx.Agent(t, "Parent assignee", fx.Runtime(t, "Parent runtime"))
				parent := fx.Issue(t, "Parent", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})
				fx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", parent)
				fx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id = $1", parent)
				var ids []string
				for range size {
					cols := testutil.Cols{"status": tc.previous, "stage": 1}
					if !tc.noParent {
						cols["parent_issue_id"] = parent
					}
					ids = append(ids, fx.Issue(t, "Child", cols))
				}
				catalog := &childDoneCatalog{Querier: testHandler.Queries, reads: map[pgtype.UUID]int{}}
				probe := &batchTransitionDB{DBTX: testPool}
				beforeNotification := -1
				probe.beforeParentRead = func() { beforeNotification = catalog.reads[parseUUID(ws)] }
				h := *testHandler
				h.Queries = db.New(probe)
				h.IssueStatusCatalog = catalog
				var out struct{ Updated int }
				testutil.Call(t, h.BatchUpdateIssues, batchTransitionRequest(ws, ids, map[string]any{"status": tc.current})).Want(http.StatusOK).JSON(&out)
				if out.Updated != size {
					t.Errorf("updated = %d, want %d successful writes", out.Updated, size)
				}
				if got := fx.Count(t, "SELECT count(*) FROM issue WHERE workspace_id = $1 AND title = 'Child' AND status = $2", ws, tc.current); got != size {
					t.Errorf("children retaining target key %q = %d, want %d", tc.current, got, size)
				}
				if tc.comments > 0 && beforeNotification != tc.transitionReads {
					t.Errorf("transition catalog reads = %d, want %d", beforeNotification, tc.transitionReads)
				}
				if got := catalog.reads[parseUUID(ws)]; got != tc.transitionReads+tc.notificationReads {
					t.Errorf("transition + notification catalog reads = %d, want %d + %d", got, tc.transitionReads, tc.notificationReads)
				}
				if probe.pointReads != 1 || catalog.pointReads != 0 {
					t.Errorf("point reads = %d + %d, want only 1 target-validation read", probe.pointReads, catalog.pointReads)
				}
				if got := countSystemCommentsOn(t, parent); got != tc.comments {
					t.Errorf("parent comments = %d, want %d", got, tc.comments)
				}
				if got := countPendingTasksForAgent(t, parent, agent); got != tc.comments {
					t.Errorf("parent runs = %d, want %d", got, tc.comments)
				}
			})
		}
	}
}

func TestBatchTransitionFailureSkipsWholeParent(t *testing.T) {
	for _, mode := range []string{"read_error", "unknown", "parked_read_error"} {
		for _, uncertainIndex := range []int{0, 1, 2} {
			t.Run(fmt.Sprintf("%s/uncertain=%d", mode, uncertainIndex), func(t *testing.T) {
				ws := dbfx.Workspace(t, "Batch failure isolation", "batch-transition-failure")
				fx := testutil.New(testPool, ws, testUserID)
				for key, category := range map[string]string{"working": "in_progress", "parked": "backlog"} {
					fx.Insert(t, "issue_status", testutil.Cols{"workspace_id": ws, "key": key, "name": key, "category": category, "color": "#123456"})
				}
				catalog := &childDoneCatalog{Querier: testHandler.Queries, reads: map[pgtype.UUID]int{}}
				uncertainStatus := "missing"
				if mode != "unknown" {
					uncertainStatus = "working"
					catalog.failNext = map[pgtype.UUID]bool{parseUUID(ws): true}
				}
				var ids []string
				var parents, agents []string
				wantComments := []int{0, 1, 0}
				if mode == "unknown" {
					wantComments[2] = 1 // A miss must not poison other known keys.
				}
				for group := range 3 {
					agent := fx.Agent(t, fmt.Sprintf("Parent assignee %d", group), fx.Runtime(t, fmt.Sprintf("Parent runtime %d", group)))
					parentStatus := "in_progress"
					if group == 0 && mode == "parked_read_error" {
						parentStatus = "parked"
					}
					parent := fx.Issue(t, "Parent", testutil.Cols{"status": parentStatus, "assignee_type": "agent", "assignee_id": agent})
					parents, agents = append(parents, parent), append(agents, agent)
					fx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", parent)
					fx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id = $1", parent)
					statuses := []string{"todo"}
					if group == 0 {
						statuses = []string{"todo", "todo", "todo"}
						statuses[uncertainIndex] = uncertainStatus
					} else if group == 2 {
						statuses[0] = "working"
					}
					for i, status := range statuses {
						ids = append(ids, fx.Issue(t, "Child", testutil.Cols{"status": status, "parent_issue_id": parent, "stage": i + 1}))
					}
				}
				probe := &batchTransitionDB{DBTX: testPool}
				h := *testHandler
				h.Queries, h.IssueStatusCatalog = db.New(probe), catalog
				var out struct{ Updated int }
				testutil.Call(t, h.BatchUpdateIssues, batchTransitionRequest(ws, ids, map[string]any{"status": "done"})).Want(http.StatusOK).JSON(&out)
				if out.Updated != len(ids) {
					t.Errorf("notification failure lost successful writes: updated = %d, want %d", out.Updated, len(ids))
				}
				if got := fx.Count(t, "SELECT count(*) FROM issue WHERE workspace_id = $1 AND title = 'Child' AND status = 'done'", ws); got != len(ids) {
					t.Errorf("committed terminal children = %d, want %d", got, len(ids))
				}
				for i, parent := range parents {
					if got := countSystemCommentsOn(t, parent); got != wantComments[i] {
						t.Errorf("group %d comments = %d, want %d", i, got, wantComments[i])
					}
					if got := countPendingTasksForAgent(t, parent, agents[i]); got != wantComments[i] {
						t.Errorf("group %d runs = %d, want %d", i, got, wantComments[i])
					}
				}
				if got := catalog.reads[parseUUID(ws)]; got != 1 || probe.pointReads != 1 || catalog.pointReads != 0 {
					t.Errorf("reads = catalog %d, points %d + %d; want 1, 1 + 0 (target validation only)", got, probe.pointReads, catalog.pointReads)
				}
				if mode == "read_error" && uncertainIndex == 1 {
					// A transient read failure is sticky only within one request.
					// The next genuine transition can read again and notify.
					next := fx.Issue(t, "Next request", testutil.Cols{"status": "working", "parent_issue_id": parents[2], "stage": 2})
					testutil.Call(t, h.BatchUpdateIssues, batchTransitionRequest(ws, []string{next}, map[string]any{"status": "done"})).Want(http.StatusOK).JSON(&out)
					if out.Updated != 1 || countSystemCommentsOn(t, parents[2]) != 1 || countPendingTasksForAgent(t, parents[2], agents[2]) != 1 {
						t.Fatal("next request must recover from the previous request's catalog failure")
					}
					if got := catalog.reads[parseUUID(ws)]; got != 2 {
						t.Errorf("catalog reads after recovery = %d, want 2", got)
					}
				}
			})
		}
	}
}

func TestBatchTransitionSnapshotMissAndNextRequest(t *testing.T) {
	ws := dbfx.Workspace(t, "Batch snapshot", "batch-transition-snapshot")
	fx := testutil.New(testPool, ws, testUserID)
	fx.Insert(t, "issue_status", testutil.Cols{"workspace_id": ws, "key": "working", "name": "Working", "category": "in_progress", "color": "#123456"})
	agent := fx.Agent(t, "Parent assignee", fx.Runtime(t, "Parent runtime"))
	parent := fx.Issue(t, "Parent", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})
	fx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", parent)
	fx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id = $1", parent)
	first := fx.Issue(t, "Earlier candidate", testutil.Cols{"status": "working", "parent_issue_id": parent, "stage": 1})
	later := fx.Issue(t, "Later candidate", testutil.Cols{"status": "todo", "parent_issue_id": parent, "stage": 2})
	last := fx.Issue(t, "Last candidate", testutil.Cols{"status": "todo", "parent_issue_id": parent, "stage": 3})
	mutated := false
	catalog := &childDoneCatalog{Querier: testHandler.Queries, reads: map[pgtype.UUID]int{}, afterRead: func() {
		// Commit after the catalog rows were read but before the next child row
		// is loaded. All reads succeed; no sleep or concurrent fixture is needed.
		fx.Insert(t, "issue_status", testutil.Cols{"workspace_id": ws, "key": "new_working", "name": "New working", "category": "in_progress", "color": "#123456"})
		fx.Exec(t, "UPDATE issue SET status = 'new_working' WHERE id = $1", later)
		mutated = true
	}}
	probe := &batchTransitionDB{DBTX: testPool}
	h := *testHandler
	h.Queries, h.IssueStatusCatalog = db.New(probe), catalog
	var out struct{ Updated int }
	testutil.Call(t, h.BatchUpdateIssues, batchTransitionRequest(ws, []string{first, later, last}, map[string]any{"status": "done"})).Want(http.StatusOK).JSON(&out)
	if !mutated || out.Updated != 3 {
		t.Fatalf("snapshot fixture ran = %t, updated = %d; want true, 3", mutated, out.Updated)
	}
	if got := countSystemCommentsOn(t, parent); got != 0 {
		t.Errorf("stale transition snapshot produced %d comments", got)
	}
	if got := countPendingTasksForAgent(t, parent, agent); got != 0 {
		t.Errorf("stale transition snapshot queued %d parent runs", got)
	}
	if got := fx.Count(t, "SELECT count(*) FROM issue WHERE parent_issue_id = $1 AND status = 'done'", parent); got != 3 {
		t.Errorf("committed children = %d, want 3", got)
	}
	// Re-saving the committed statuses does not replay the skipped event.
	testutil.Call(t, h.BatchUpdateIssues, batchTransitionRequest(ws, []string{first, later, last}, map[string]any{"status": "done"})).Want(http.StatusOK)
	if got := countSystemCommentsOn(t, parent); got != 0 {
		t.Errorf("unchanged statuses replayed %d comments", got)
	}
	// A real transition in a new request uses a fresh catalog, including the
	// status created during the previous request. It is not an automatic retry.
	newChild := fx.Issue(t, "Next request", testutil.Cols{"status": "new_working", "parent_issue_id": parent, "stage": 4})
	testutil.Call(t, h.BatchUpdateIssues, batchTransitionRequest(ws, []string{newChild}, map[string]any{"status": "done"})).Want(http.StatusOK).JSON(&out)
	if out.Updated != 1 || countSystemCommentsOn(t, parent) != 1 || countPendingTasksForAgent(t, parent, agent) != 1 {
		t.Fatal("fresh request did not resolve the new key and notify once")
	}
	if got := catalog.reads[parseUUID(ws)]; got != 2 || probe.pointReads != 3 {
		t.Errorf("catalog reads = %d, points = %d; want 2 and 3 target validations", got, probe.pointReads)
	}
}

func TestBatchTransitionPartialWritesAndDuplicateIDs(t *testing.T) {
	ws := dbfx.Workspace(t, "Partial batch", "batch-transition-partial")
	fx := testutil.New(testPool, ws, testUserID)
	fx.Insert(t, "issue_status", testutil.Cols{"workspace_id": ws, "key": "working", "name": "Working", "category": "in_progress", "color": "#123456"})
	agent := fx.Agent(t, "Parent assignee", fx.Runtime(t, "Parent runtime"))
	parent := fx.Issue(t, "Parent", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})
	fx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", parent)
	fx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id = $1", parent)
	first := fx.Issue(t, "Stage 1", testutil.Cols{"parent_issue_id": parent, "status": "working", "stage": 1})
	failed := fx.Issue(t, "Stage 2", testutil.Cols{"parent_issue_id": parent, "status": "working", "stage": 2})
	last := fx.Issue(t, "Stage 3", testutil.Cols{"parent_issue_id": parent, "status": "working", "stage": 3})
	orphan := fx.Issue(t, "No parent", testutil.Cols{"status": "missing"})
	foreignWS := dbfx.Workspace(t, "Foreign batch", "batch-transition-foreign")
	foreign := fx.Issue(t, "Foreign child", testutil.Cols{"workspace_id": foreignWS, "status": "todo"})
	missing := fx.Issue(t, "Deleted child")
	fx.Exec(t, "DELETE FROM issue WHERE id = $1", missing)
	ids := []string{first, "not-a-uuid", failed, foreign, first, missing, last, orphan}
	probe := &batchTransitionDB{DBTX: testPool, failUpdateID: parseUUID(failed)}
	catalog := &childDoneCatalog{Querier: testHandler.Queries, reads: map[pgtype.UUID]int{}}
	h := *testHandler
	h.Queries, h.IssueStatusCatalog = db.New(probe), catalog
	var out struct{ Updated int }
	testutil.Call(t, h.BatchUpdateIssues, batchTransitionRequest(ws, ids, map[string]any{"status": "done"})).Want(http.StatusOK).JSON(&out)
	// The existing response counts successful UPDATEs, including the duplicate
	// no-op, not unique changed rows. A write failure is not a resolver failure.
	if out.Updated != 4 {
		t.Errorf("updated = %d, want 4 (first twice, last, and orphan)", out.Updated)
	}
	for id, want := range map[string]string{first: "done", failed: "working", last: "done", orphan: "done", foreign: "todo"} {
		var status string
		fx.QueryRow(t, "SELECT status FROM issue WHERE id = $1", id).Scan(&status)
		if status != want {
			t.Errorf("issue %s status = %q, want %q", id, status, want)
		}
	}
	if countSystemCommentsOn(t, parent) != 1 || countPendingTasksForAgent(t, parent, agent) != 1 {
		t.Fatal("partial batch should close only stage 1, with one comment and run")
	}
	var content string
	fx.QueryRow(t, "SELECT content FROM comment WHERE issue_id = $1 AND author_type = 'system'", parent).Scan(&content)
	if !strings.Contains(content, "Stage 1 of this issue is complete") || !strings.Contains(content, "Stage 2 is next") {
		t.Errorf("incorrect final stage summary: %s", content)
	}
	if got := catalog.reads[parseUUID(ws)]; got != 2 || len(catalog.reads) != 1 || probe.pointReads != 1 {
		t.Errorf("transition + notification catalogs = %v, points = %d; want only this workspace twice and one validation", catalog.reads, probe.pointReads)
	}
}

func TestBatchTransitionUsesResultingParent(t *testing.T) {
	for _, unresolved := range []bool{false, true} {
		t.Run(fmt.Sprintf("unresolved=%t", unresolved), func(t *testing.T) {
			ws := dbfx.Workspace(t, "Reparent batch", "batch-transition-reparent")
			fx := testutil.New(testPool, ws, testUserID)
			fx.Insert(t, "issue_status", testutil.Cols{"workspace_id": ws, "key": "working", "name": "Working", "category": "in_progress", "color": "#123456"})
			agent := fx.Agent(t, "Parent assignee", fx.Runtime(t, "Parent runtime"))
			var parents []string
			for range 2 {
				parent := fx.Issue(t, "Parent", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})
				parents = append(parents, parent)
				fx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", parent)
				fx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id = $1", parent)
			}
			first := fx.Issue(t, "Earlier candidate", testutil.Cols{"parent_issue_id": parents[0], "status": "todo"})
			previous := "working"
			if unresolved {
				previous = "missing"
			}
			last := fx.Issue(t, "Later candidate", testutil.Cols{"parent_issue_id": parents[0], "status": previous})
			catalog := &childDoneCatalog{Querier: testHandler.Queries, reads: map[pgtype.UUID]int{}}
			h := *testHandler
			h.IssueStatusCatalog = catalog
			var out struct{ Updated int }
			testutil.Call(t, h.BatchUpdateIssues, batchTransitionRequest(ws, []string{first, last}, map[string]any{
				"status": "done", "parent_issue_id": parents[1],
			})).Want(http.StatusOK).JSON(&out)
			if out.Updated != 2 || fx.Count(t, "SELECT count(*) FROM issue WHERE parent_issue_id = $1 AND status = 'done'", parents[1]) != 2 {
				t.Fatal("both children must be committed under their new parent")
			}
			wantNew := 1
			if unresolved {
				wantNew = 0
			}
			for i, want := range []int{0, wantNew} {
				if countSystemCommentsOn(t, parents[i]) != want || countPendingTasksForAgent(t, parents[i], agent) != want {
					t.Errorf("parent %d: want %d comments and runs", i, want)
				}
			}
		})
	}
}

func TestBatchTransitionWorkspaceIsolation(t *testing.T) {
	h := *testHandler
	catalog := &childDoneCatalog{Querier: testHandler.Queries, reads: map[pgtype.UUID]int{}}
	h.IssueStatusCatalog = catalog
	for _, category := range []string{"in_progress", "done"} {
		ws := dbfx.Workspace(t, "Workspace categories", "batch-transition-"+category)
		fx := testutil.New(testPool, ws, testUserID)
		fx.Insert(t, "issue_status", testutil.Cols{"workspace_id": ws, "key": "shared_key", "name": "Shared key", "category": category, "color": "#123456"})
		parent := fx.Issue(t, "Parent", testutil.Cols{"status": "in_progress"})
		fx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", parent)
		child := fx.Issue(t, "Child", testutil.Cols{"status": "shared_key", "parent_issue_id": parent})
		testutil.Call(t, h.BatchUpdateIssues, batchTransitionRequest(ws, []string{child}, map[string]any{"status": "done"})).Want(http.StatusOK)
		want := 0
		if category == "in_progress" {
			want = 1
		}
		if got := countSystemCommentsOn(t, parent); got != want {
			t.Errorf("workspace category %s comments = %d, want %d", category, got, want)
		}
		if got := catalog.reads[parseUUID(ws)]; got != 1 {
			t.Errorf("workspace category %s catalog reads = %d, want 1", category, got)
		}
	}
}
