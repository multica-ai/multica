package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestProjectWorkflowMigrationPreservesProgressAndViews(t *testing.T) {
	ctx := context.Background()
	ws := parseUUID(testWorkspaceID)
	seedTestCatalog(t)
	base, err := issueworkflow.EnsureDefault(ctx, testHandler.Queries, ws)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := testHandler.Queries.ListIssueWorkflowStatuses(ctx, db.ListIssueWorkflowStatusesParams{WorkspaceID: ws, WorkflowID: base.ID, IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]db.IssueWorkflowStatus{}
	for _, node := range nodes {
		byKey[node.SpecKey] = node
	}
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	project := fx.Project(t, "Migration acceptance")
	other := fx.Project(t, "Unaffected project")
	ids := []string{}
	for _, key := range []string{"todo", "in_progress", "done", "cancelled"} {
		id := fx.Issue(t, key, testutil.Cols{"project_id": project, "status": key, "workflow_id": base.ID, "workflow_status_id": byKey[key].ID})
		ids = append(ids, id)
		fx.Cleanup(t, `DELETE FROM issue_transition WHERE issue_id=$1`, id)
	}
	oldInProgress := uuidToString(byKey["in_progress"].ID)
	fx.Issue(t, "unaffected", testutil.Cols{"project_id": other, "status": "in_progress", "workflow_id": base.ID, "workflow_status_id": byKey["in_progress"].ID})
	view := fx.Insert(t, "issue_view", testutil.Cols{"workspace_id": testWorkspaceID, "owner_id": testUserID, "name": "Shared progress", "scope_type": "workspace", "visibility": "workspace", "query": `{"statusFilters":["` + oldInProgress + `"]}`, "display": `{}`})
	request := func(body map[string]any, want int) issueWorkflowApplyResponse {
		t.Helper()
		var result issueWorkflowApplyResponse
		testutil.Call(t, testHandler.UpdateProjectIssueWorkflow, withURLParam(newRequest(http.MethodPut, "/api/projects/"+project+"/issue-workflow", body), "id", project)).Want(want).JSON(&result)
		return result
	}
	preview := request(map[string]any{"mode": "custom", "dry_run": true}, 200)
	if preview.Plan.Migration.IssueCount != 4 || preview.Plan.Migration.ViewCount != 1 {
		t.Fatalf("preview=%+v", preview.Plan.Migration)
	}
	if fx.Count(t, `SELECT count(*) FROM issue WHERE project_id=$1 AND workflow_id=$2`, project, base.ID) != 4 {
		t.Fatal("preview mutated bindings")
	}
	if fx.Count(t, `SELECT count(*) FROM issue_workflow WHERE scope_type='project' AND scope_id=$1`, project) != 0 {
		t.Fatal("preview materialized workflow")
	}
	testutil.Call(t, testHandler.UpdateProjectIssueWorkflow, withURLParam(newRequest(http.MethodPut, "/", map[string]any{"mode": "custom"}), "id", project)).Want(409)
	// Changes made after preview cannot be silently included in confirmation.
	fx.Exec(t, `UPDATE issue SET revision=revision+1 WHERE id=$1`, ids[0])
	testutil.Call(t, testHandler.UpdateProjectIssueWorkflow, withURLParam(newRequest(http.MethodPut, "/", map[string]any{"mode": "custom", "confirm_migration": true, "migration_fingerprint": preview.Plan.Migration.Fingerprint}), "id", project)).Want(409)
	preview = request(map[string]any{"mode": "custom", "dry_run": true}, 200)
	applied := request(map[string]any{"mode": "custom", "confirm_migration": true, "migration_fingerprint": preview.Plan.Migration.Fingerprint}, 200)
	fx.Cleanup(t, `DELETE FROM issue_workflow WHERE id=$1`, applied.Workflow.ID)
	fx.Cleanup(t, `DELETE FROM issue_workflow_status WHERE workflow_id=$1`, applied.Workflow.ID)
	if fx.Count(t, `SELECT count(DISTINCT workflow_id) FROM issue WHERE project_id=$1`, project) != 1 {
		t.Fatal("project still has multiple workflows")
	}
	for index, key := range []string{"todo", "in_progress", "done", "cancelled"} {
		if fx.Count(t, `SELECT count(*) FROM issue WHERE id=$1 AND status=$2 AND workflow_id=$3`, ids[index], key, applied.Workflow.ID) != 1 {
			t.Fatalf("lost progress for %s", key)
		}
		if fx.Count(t, `SELECT count(*) FROM issue_transition WHERE issue_id=$1 AND cause='workflow_migrated'`, ids[index]) != 1 {
			t.Fatal("missing migration history")
		}
		if fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, ids[index]) != 0 {
			t.Fatal("migration started agent")
		}
	}
	if fx.Count(t, `SELECT count(*) FROM issue_view WHERE id=$1 AND query->'statusFilters'->>0=$2 AND query->'statusFilterMappings'->$3->>$2 IS NOT NULL`, view, oldInProgress, project) != 1 {
		t.Fatal("global view did not preserve project-scoped mapping")
	}
	// A rename retains node identity, issue revision and transition history.
	var customNodes []map[string]any
	for _, node := range applied.Statuses {
		if node.ArchivedAt != nil {
			continue
		}
		customNodes = append(customNodes, map[string]any{"key": node.SpecKey, "name": node.Name, "description": node.Description, "color": node.Color, "phase": node.Phase, "entry_policy": node.EntryPolicy})
	}
	customNodes[0]["name"] = "Renamed"
	spec := map[string]any{"api_version": 1, "name": "Renamed workflow", "initial_status": "todo", "statuses": customNodes}
	renamed := request(map[string]any{"mode": "custom", "spec": spec, "expected_revision": applied.Workflow.Revision}, 200)
	if renamed.Plan.Migration.IssueCount != 0 {
		t.Fatal("rename migrated issues")
	}
	if fx.Count(t, `SELECT count(*) FROM issue_transition WHERE issue_id=$1`, ids[0]) != 1 {
		t.Fatal("rename created transition")
	}
	// Removing an occupied node requires mapping, including its saved filters.
	var removedID string
	kept := []map[string]any{}
	for _, node := range renamed.Statuses {
		if node.SpecKey == "in_progress" {
			removedID = node.ID
		}
	}
	for _, node := range customNodes {
		if node["key"] != "in_progress" {
			kept = append(kept, node)
		}
	}
	spec["statuses"] = kept
	removedPreview := request(map[string]any{"mode": "custom", "spec": spec, "allow_archive": true, "dry_run": true}, 200)
	testutil.Call(t, testHandler.UpdateProjectIssueWorkflow, withURLParam(newRequest(http.MethodPut, "/", map[string]any{"mode": "custom", "spec": spec, "allow_archive": true, "confirm_migration": true, "migration_fingerprint": removedPreview.Plan.Migration.Fingerprint}), "id", project)).Want(409)
	request(map[string]any{"mode": "custom", "spec": spec, "allow_archive": true, "confirm_migration": true, "migration_fingerprint": removedPreview.Plan.Migration.Fingerprint, "status_mapping": map[string]string{removedID: "todo"}}, 200)
	if fx.Count(t, `SELECT count(*) FROM issue WHERE id=$1 AND status='todo'`, ids[1]) != 1 {
		t.Fatal("removal ignored explicit mapping")
	}
	if fx.Count(t, `SELECT count(*) FROM issue_workflow_status WHERE id=$1 AND archived_at IS NOT NULL`, removedID) != 1 {
		t.Fatal("removed node not archived")
	}
	// Switching back migrates every issue, including terminal work.
	preview = request(map[string]any{"mode": "default", "dry_run": true}, 200)
	request(map[string]any{"mode": "default", "confirm_migration": true, "migration_fingerprint": preview.Plan.Migration.Fingerprint}, 200)
	if fx.Count(t, `SELECT count(*) FROM issue WHERE project_id=$1 AND workflow_id=$2`, project, base.ID) != 4 {
		t.Fatal("use-default left stale bindings")
	}
}

func TestWorkflowMigrationRequiresMappingAndBlocksActiveWork(t *testing.T) {
	ctx := context.Background()
	ws := parseUUID(testWorkspaceID)
	seedTestCatalog(t)
	base, err := issueworkflow.EnsureDefault(ctx, testHandler.Queries, ws)
	if err != nil {
		t.Fatal(err)
	}
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	project := fx.Project(t, "Mapping required")
	id := fx.Issue(t, "Existing issue", testutil.Cols{"project_id": project, "status": "todo", "workflow_id": base.ID, "workflow_status_id": base.InitialStatusID})
	fx.Cleanup(t, `DELETE FROM issue_transition WHERE issue_id=$1`, id)
	spec := map[string]any{"api_version": 1, "name": "Different flow", "initial_status": "intake", "statuses": []map[string]any{{"key": "intake", "name": "Intake", "color": "#123456", "phase": "unstarted", "entry_policy": map[string]any{"executor": map[string]any{"type": "none"}}}}}
	body := map[string]any{"mode": "custom", "spec": spec, "dry_run": true}
	call := func(want int) issueWorkflowApplyResponse {
		t.Helper()
		var out issueWorkflowApplyResponse
		testutil.Call(t, testHandler.UpdateProjectIssueWorkflow, withURLParam(newRequest(http.MethodPut, "/", body), "id", project)).Want(want).JSON(&out)
		return out
	}
	preview := call(200)
	body["dry_run"] = false
	body["confirm_migration"] = true
	body["migration_fingerprint"] = preview.Plan.Migration.Fingerprint
	call(409)
	body["status_mapping"] = map[string]string{uuidToString(base.InitialStatusID): "intake"}
	runtime := fx.Runtime(t, "Migration fixture")
	agent := fx.Agent(t, "Fixture agent", runtime)
	task := fx.Task(t, agent, testutil.Cols{"issue_id": id, "status": "queued", "runtime_id": runtime})
	call(409)
	if fx.Count(t, `SELECT count(*) FROM issue WHERE id=$1 AND workflow_id=$2`, id, base.ID) != 1 {
		t.Fatal("blocked migration changed issue")
	}
	if fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id=$1 AND status='queued'`, task) != 1 {
		t.Fatal("blocked migration cancelled work")
	}
	fx.Exec(t, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, task)
	result := call(200)
	fx.Cleanup(t, `DELETE FROM issue_workflow WHERE id=$1`, result.Workflow.ID)
	fx.Cleanup(t, `DELETE FROM issue_workflow_status WHERE workflow_id=$1`, result.Workflow.ID)
}

func TestWorkflowMigrationScopedFilterDoesNotWidenOtherProjects(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	a := fx.Project(t, "Migrated")
	b := fx.Project(t, "Unrelated")
	old := "11000000-0000-0000-0000-000000000001"
	next := "11000000-0000-0000-0000-000000000002"
	wanted := fx.Issue(t, "Migrated issue", testutil.Cols{"project_id": a, "workflow_status_id": next})
	fx.Issue(t, "Unrelated issue", testutil.Cols{"project_id": b, "workflow_status_id": next})
	spec := issueTableQuerySpec{Scope: issueTableScope{Kind: "workspace"}, Filters: issueTableFiltersRequest{WorkflowStatusIDs: []string{old}, StatusMappings: map[string]map[string]string{a: {old: next}}}, Sort: issueTableSortRequest{Field: "position", Direction: "asc"}}
	w := httptest.NewRecorder()
	compiled, ok := testHandler.compileIssueTableQuery(w, newRequest(http.MethodPost, "/", nil), spec)
	if !ok {
		t.Fatal(w.Body.String())
	}
	rows, err := testPool.Query(context.Background(), "SELECT i.id::text FROM issue i WHERE "+compiled.where, compiled.args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		found = append(found, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0] != wanted {
		t.Fatalf("scoped filter matched %v", found)
	}
}

func TestIssueCreationWaitsForWorkflowMigration(t *testing.T) {
	ctx := context.Background()
	ws := parseUUID(testWorkspaceID)
	seedTestCatalog(t)
	if _, err := issueworkflow.EnsureDefault(ctx, testHandler.Queries, ws); err != nil {
		t.Fatal(err)
	}
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	project := fx.Project(t, "Concurrent migration")
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	q := testHandler.Queries.WithTx(tx)
	if err := q.LockIssueStatusCatalog(ctx, ws); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		testHandler.CreateIssue(response, newRequest(http.MethodPost, "/api/issues", map[string]any{"title": "Concurrent create", "project_id": project, "suppress_run": true}))
	}()
	select {
	case <-done:
		t.Fatalf("create bypassed migration lock: %s", response.Body.String())
	case <-time.After(100 * time.Millisecond):
	}
	custom, err := issueworkflow.CustomizeProject(ctx, q, ws, parseUUID(project))
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("create did not resume after commit")
	}
	fx.Cleanup(t, `DELETE FROM issue_workflow WHERE id=$1`, custom.ID)
	fx.Cleanup(t, `DELETE FROM issue_workflow_status WHERE workflow_id=$1`, custom.ID)
	fx.Cleanup(t, `DELETE FROM issue WHERE project_id=$1`, project)
	fx.Cleanup(t, `DELETE FROM issue_transition WHERE issue_id IN (SELECT id FROM issue WHERE project_id=$1)`, project)
	if response.Code != 201 {
		t.Fatalf("create=%d %s", response.Code, response.Body.String())
	}
	if fx.Count(t, `SELECT count(*) FROM issue WHERE project_id=$1 AND workflow_id=$2`, project, custom.ID) != 1 {
		t.Fatal("concurrent create retained old workflow")
	}
}

func TestWorkflowViewMappingComposesBackToOriginalStatus(t *testing.T) {
	project := "11000000-0000-0000-0000-000000000001"
	original := "11000000-0000-0000-0000-000000000002"
	custom := "11000000-0000-0000-0000-000000000003"
	view := db.IssueView{ScopeType: "workspace", Query: []byte(`{"statusFilters":["` + original + `"],"statusFilterMappings":{"` + project + `":{"` + original + `":"` + custom + `"}}}`)}
	raw, changed, err := migratedViewQuery(view, project, map[pgtype.UUID]db.IssueWorkflowStatus{parseUUID(custom): {ID: parseUUID(original)}})
	if err != nil || !changed {
		t.Fatalf("migration=%s changed=%v err=%v", raw, changed, err)
	}
	view.Query = raw
	values := viewStatusReferences(view, project)
	if len(values) != 1 || values[0] != original {
		t.Fatalf("returned default filter still maps to retired custom status: %v", values)
	}
}
