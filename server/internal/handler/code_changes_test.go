package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/codechanges"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func handlerCodeChangesFixture(t *testing.T) (string, string, codechanges.ExecutionRef) {
	t.Helper()
	if testHandler == nil {
		t.Skip("requires isolated database")
	}
	ctx := context.Background()
	runtime := createProviderRuntime(t, "codex")
	agent := dbfx.Agent(t, "code changes fixture", runtime)
	issue := dbfx.Issue(t, "saved code changes")
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "issue_id": issue, "status": "running", "dispatched_at": testutil.Raw("now()"), "started_at": testutil.Raw("now()"), "max_attempts": 1})
	row, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(task))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := testHandler.Queries.GetWorkspace(ctx, parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, "DELETE FROM agent_task_code_changes WHERE task_id=$1", task)
		_, _ = testPool.Exec(ctx, "UPDATE workspace SET settings=$2 WHERE id=$1", workspace.ID, workspace.Settings)
	})
	if _, err := testHandler.Queries.UpdateWorkspaceCodeChanges(ctx, db.UpdateWorkspaceCodeChangesParams{ID: workspace.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	return issue, task, codechanges.ExecutionRef{RuntimeID: runtime, DispatchedAt: row.DispatchedAt.Time.Format(time.RFC3339Nano)}
}
func handlerCodeSnapshot() *codechanges.Snapshot {
	return &codechanges.Snapshot{Version: 1, BaseRef: strings.Repeat("a", 40), HeadRef: strings.Repeat("b", 40), Files: []codechanges.File{{Path: "src/file.go", Status: "M", Additions: 1, Deletions: 1, Diff: "@@ -1 +1 @@\n-before\n+private-source"}}}
}
func codeChangesGet(t *testing.T, issue, task string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/code-changes"
	if task != "" {
		path += "?task_id=" + task
	}
	w := httptest.NewRecorder()
	testHandler.GetIssueCodeChanges(w, withURLParam(newRequest(http.MethodGet, path, nil), "id", issue))
	if w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("code changes response is cacheable")
	}
	return w
}
func TestCodeChangesHTTPArtifactReceiptScopeAndIdentity(t *testing.T) {
	issue, task, ref := handlerCodeChangesFixture(t)
	snapshot := handlerCodeSnapshot()
	complete := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/complete", TaskCompleteRequest{Output: "done", CodeChanges: snapshot, CodeChangesRef: ref}), "taskId", task)
	w := httptest.NewRecorder()
	testHandler.CompleteTask(w, complete)
	if w.Code != 200 {
		t.Fatalf("complete=%d %s", w.Code, w.Body.String())
	}
	encoded, err := codechanges.Encode(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if w.Header().Get(codechanges.ReceiptHeader) != "saved:"+codechanges.DigestEncoded(encoded) {
		t.Fatal("missing saved receipt")
	}
	if strings.Contains(w.Body.String(), "private-source") {
		t.Fatal("source code escaped through task response")
	}
	row, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(task))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(row.Result), "private-source") || strings.Contains(string(row.Result), "code_changes_ref") {
		t.Fatal("artifact metadata leaked into public task result")
	}
	if get := codeChangesGet(t, issue, task); get.Code != 200 || !strings.Contains(get.Body.String(), "private-source") {
		t.Fatalf("saved GET=%d %s", get.Code, get.Body.String())
	}
	foreign := newRequest(http.MethodGet, "/code-changes", nil)
	foreign.Header.Set("X-Workspace-ID", "99999999-9999-4999-8999-999999999999")
	denied := httptest.NewRecorder()
	testHandler.GetIssueCodeChanges(denied, withURLParam(foreign, "id", issue))
	if denied.Code != 404 {
		t.Fatalf("cross workspace=%d", denied.Code)
	}
	dbfx.Exec(t, "UPDATE agent_task_queue SET dispatched_at=dispatched_at+interval '1 second' WHERE id=$1", task)
	if get := codeChangesGet(t, issue, task); get.Code != 404 {
		t.Fatalf("changed identity exposed old snapshot=%d", get.Code)
	}
}
func TestCodeChangesDisabledAndStaleTerminalReports(t *testing.T) {
	issue, task, ref := handlerCodeChangesFixture(t)
	stale := ref
	at, _ := time.Parse(time.RFC3339Nano, ref.DispatchedAt)
	stale.DispatchedAt = at.Add(time.Second).Format(time.RFC3339Nano)
	request := func(reference codechanges.ExecutionRef) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.CompleteTask(w, withURLParam(newOwnerDaemonRequest(http.MethodPost, "/complete", TaskCompleteRequest{Output: "done", CodeChanges: handlerCodeSnapshot(), CodeChangesRef: reference}), "taskId", task))
		return w
	}
	if staleReply := request(stale); staleReply.Code != 409 {
		t.Fatalf("stale complete=%d %s", staleReply.Code, staleReply.Body.String())
	}
	current, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(task))
	if err != nil || current.Status != "running" {
		t.Fatal("stale callback changed execution")
	}
	if _, err := testHandler.Queries.UpdateWorkspaceCodeChanges(context.Background(), db.UpdateWorkspaceCodeChangesParams{ID: parseUUID(testWorkspaceID), Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if done := request(ref); done.Code != 200 || done.Header().Get(codechanges.ReceiptHeader) != "discarded" {
		t.Fatalf("disabled completion=%d receipt=%s", done.Code, done.Header().Get(codechanges.ReceiptHeader))
	}
	get := codeChangesGet(t, issue, "")
	if get.Code != 200 || strings.Contains(get.Body.String(), "private-source") || !strings.Contains(get.Body.String(), "disabled") {
		t.Fatal("disabled code source returned")
	}
}
func TestCodeChangesOnlyManagersCanChangeIndependentSetting(t *testing.T) {
	_, _, _ = handlerCodeChangesFixture(t)
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{WorkspaceID: parseUUID(testWorkspaceID), UserID: parseUUID(testUserID)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), "UPDATE member SET role=$3 WHERE workspace_id=$1 AND user_id=$2", testWorkspaceID, testUserID, member.Role)
	})
	update := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.UpdateWorkspaceCodeChanges(w, withURLParam(newRequest(http.MethodPut, "/code-changes", map[string]any{"enabled": false}), "id", testWorkspaceID))
		return w
	}
	dbfx.Exec(t, "UPDATE member SET role='member' WHERE workspace_id=$1 AND user_id=$2", testWorkspaceID, testUserID)
	if w := update(); w.Code != 403 {
		t.Fatalf("member setting=%d", w.Code)
	}
	dbfx.Exec(t, "UPDATE member SET role='admin' WHERE workspace_id=$1 AND user_id=$2", testWorkspaceID, testUserID)
	if w := update(); w.Code != 200 {
		t.Fatalf("admin setting=%d %s", w.Code, w.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(update().Body.Bytes(), &response); err != nil || response["enabled"] != false {
		t.Fatal("false setting not acknowledged")
	}
}

func TestCodeChangesSavedHistorySurvivesRuntimeUnbind(t *testing.T) {
	issue, task, ref := handlerCodeChangesFixture(t)
	snapshot := handlerCodeSnapshot()
	w := httptest.NewRecorder()
	testHandler.CompleteTask(w, withURLParam(newOwnerDaemonRequest(http.MethodPost, "/complete", TaskCompleteRequest{Output: "done", CodeChanges: snapshot, CodeChangesRef: ref}), "taskId", task))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	dbfx.Exec(t, "UPDATE agent_task_queue SET runtime_id=NULL WHERE id=$1", task)
	if get := codeChangesGet(t, issue, task); get.Code != 200 || !strings.Contains(get.Body.String(), "private-source") {
		t.Fatalf("unbound history=%d %s", get.Code, get.Body.String())
	}
	otherRuntime := createProviderRuntime(t, "codex")
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), "UPDATE agent_task_queue SET runtime_id=NULL WHERE id=$1", task)
	})
	dbfx.Exec(t, "UPDATE agent_task_queue SET runtime_id=$2 WHERE id=$1", task, otherRuntime)
	if get := codeChangesGet(t, issue, task); get.Code != 404 {
		t.Fatalf("different runtime read=%d", get.Code)
	}
}
