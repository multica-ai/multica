package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func seedHandlerWorkflowDefinition(t *testing.T, name string, stages int) string {
	t.Helper()
	items := make([]map[string]string, stages)
	for i := range items {
		items[i] = map[string]string{"key": fmt.Sprintf("stage_%d", i+1), "name": fmt.Sprintf("Stage %d", i+1)}
	}
	raw, err := json.Marshal(map[string]any{"schema_version": 1, "stages": items})
	if err != nil {
		t.Fatal(err)
	}
	return dbfx.Insert(t, "workflow_definition", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         name,
		"version":      1,
		"definition":   testutil.Raw("'" + string(raw) + "'::jsonb"),
		"created_by":   testUserID,
	})
}

func TestCreateWorkflowDefinitionResponseAndValidation(t *testing.T) {
	valid := map[string]any{
		"name": "Handler Flow",
		"definition": map[string]any{
			"schema_version": 1,
			"stages":         []map[string]string{{"key": "build", "name": "Build"}},
		},
	}
	w := httptest.NewRecorder()
	testHandler.CreateWorkflowDefinition(w, newRequest(http.MethodPost, "/api/workflow-definitions", valid))
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}
	var created struct {
		ID         string          `json:"id"`
		Name       string          `json:"name"`
		Version    int32           `json:"version"`
		Definition json.RawMessage `json:"definition"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Name != "Handler Flow" || created.Version != 1 || len(created.Definition) == 0 {
		t.Fatalf("created = %+v", created)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workflow_definition WHERE id = $1`, created.ID)
	})

	bad := map[string]any{
		"name":       "Bad",
		"definition": map[string]any{"schema_version": 1, "stages": []any{}},
	}
	w = httptest.NewRecorder()
	testHandler.CreateWorkflowDefinition(w, newRequest(http.MethodPost, "/api/workflow-definitions", bad))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetWorkflowDefinitionForeignWorkspaceReturns404(t *testing.T) {
	definitionID := seedHandlerWorkflowDefinition(t, "Scoped", 1)
	foreignWorkspaceID := dbfx.Workspace(t, "Foreign Workflow", fmt.Sprintf("foreign-workflow-%d", time.Now().UnixNano()))
	req := newRequest(http.MethodGet, "/api/workflow-definitions/"+definitionID, nil)
	req.Header.Set("X-Workspace-ID", foreignWorkspaceID)
	req = withURLParam(req, "id", definitionID)
	w := httptest.NewRecorder()
	testHandler.GetWorkflowDefinition(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestStartIssueWorkflowTopologyConflictReturns409(t *testing.T) {
	definitionID := seedHandlerWorkflowDefinition(t, "Conflict", 1)
	issueID := dbfx.Issue(t, "Workflow parent backlog", testutil.Cols{"status": "backlog"})
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/workflow/start", map[string]any{
		"workflow_definition_id": definitionID,
	})
	req = withURLParam(req, "id", issueID)
	w := httptest.NewRecorder()
	testHandler.StartIssueWorkflow(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetIssueWorkflowNotFoundReturns404(t *testing.T) {
	issueID := dbfx.Issue(t, "No workflow run")
	req := withURLParam(newRequest(http.MethodGet, "/api/issues/"+issueID+"/workflow", nil), "id", issueID)
	w := httptest.NewRecorder()
	testHandler.GetIssueWorkflow(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestStartIssueWorkflowDispatchesPromotedAssignedChild(t *testing.T) {
	definitionID := seedHandlerWorkflowDefinition(t, "Dispatch", 1)
	parentID := dbfx.Issue(t, "Workflow dispatch parent", testutil.Cols{"status": "todo"})
	var agentID string
	if err := testPool.QueryRow(context.Background(), `SELECT id::text FROM agent WHERE workspace_id=$1 ORDER BY created_at LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	childID := dbfx.Issue(t, "Workflow dispatch child", testutil.Cols{
		"parent_issue_id": parentID,
		"stage":           1,
		"status":          "backlog",
		"assignee_type":   "agent",
		"assignee_id":     agentID,
	})
	req := newRequest(http.MethodPost, "/api/issues/"+parentID+"/workflow/start", map[string]any{"workflow_definition_id": definitionID})
	req = withURLParam(req, "id", parentID)
	w := httptest.NewRecorder()
	testHandler.StartIssueWorkflow(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2 AND status IN ('queued','dispatched')`, childID, agentID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("pending workflow-dispatched tasks = %d, want 1", count)
	}
}
