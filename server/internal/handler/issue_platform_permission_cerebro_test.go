package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

const createIssuePermissionToolKey = "create_issue"

func setCreateIssueWorkspacePolicy(t *testing.T, setting toolpolicy.Setting) {
	t.Helper()
	ctx := context.Background()
	workspaceID, err := util.ParseUUID(testWorkspaceID)
	if err != nil {
		t.Fatalf("parse workspace id: %v", err)
	}
	store := toolpolicy.NewStore(testPool)
	if _, err := testPool.Exec(ctx, `DELETE FROM cerebro_tool_policy WHERE workspace_id = $1 AND tool_key = $2`, workspaceID, createIssuePermissionToolKey); err != nil {
		t.Fatalf("clear create_issue policy: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM cerebro_tool_policy WHERE workspace_id = $1 AND tool_key = $2`, workspaceID, createIssuePermissionToolKey)
	})
	if _, err := store.Set(ctx, toolpolicy.SetParams{
		WorkspaceID: workspaceID,
		ToolKey:     createIssuePermissionToolKey,
		Layer:       toolpolicy.LayerWorkspace,
		SubjectID:   workspaceID,
		Setting:     setting,
	}); err != nil {
		t.Fatalf("set create_issue workspace policy: %v", err)
	}
}

func issueCountByTitle(t *testing.T, title string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM issue WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title).Scan(&count); err != nil {
		t.Fatalf("count issues titled %q: %v", title, err)
	}
	return count
}

func cleanupIssuesByTitle(t *testing.T, title string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title)
	})
}

func agentCreateIssueRequest(t *testing.T, title string, parentID string) *http.Request {
	t.Helper()
	agentID := createHandlerTestAgent(t, "create-issue-permission-"+uuid.NewString(), []byte(`{}`))
	taskID := createHandlerTestTaskForAgent(t, agentID)
	body := map[string]any{"title": title, "allow_duplicate": true}
	if parentID != "" {
		body["parent_issue_id"] = parentID
	}
	req := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, body)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	return req
}

func createPermissionTestParent(t *testing.T) string {
	t.Helper()
	title := "Permission parent " + uuid.NewString()
	cleanupIssuesByTitle(t, title)
	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           title,
		"allow_duplicate": true,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create parent issue: status=%d body=%s", w.Code, w.Body.String())
	}
	var id string
	if err := testPool.QueryRow(context.Background(), `SELECT id FROM issue WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title).Scan(&id); err != nil {
		t.Fatalf("load parent issue: %v", err)
	}
	return id
}

func TestCreateIssuePermission_RESTDenyBlocksAgentIssueWithoutMutation(t *testing.T) {
	setCreateIssueWorkspacePolicy(t, toolpolicy.SettingDeny)
	title := "REST denied issue " + uuid.NewString()
	cleanupIssuesByTitle(t, title)

	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, agentCreateIssueRequest(t, title, ""))

	if w.Code != http.StatusForbidden {
		t.Fatalf("REST agent create under Workspace Deny: status=%d, want 403; body=%s", w.Code, w.Body.String())
	}
	if got := issueCountByTitle(t, title); got != 0 {
		t.Fatalf("REST agent create under Workspace Deny mutated %d issue rows, want 0", got)
	}
}

func TestCreateIssuePermission_RESTDenyBlocksAgentSubIssueWithoutMutation(t *testing.T) {
	parentID := createPermissionTestParent(t)
	setCreateIssueWorkspacePolicy(t, toolpolicy.SettingDeny)
	title := "REST denied sub-issue " + uuid.NewString()
	cleanupIssuesByTitle(t, title)

	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, agentCreateIssueRequest(t, title, parentID))

	if w.Code != http.StatusForbidden {
		t.Fatalf("REST agent sub-issue under Workspace Deny: status=%d, want 403; body=%s", w.Code, w.Body.String())
	}
	if got := issueCountByTitle(t, title); got != 0 {
		t.Fatalf("REST agent sub-issue under Workspace Deny mutated %d issue rows, want 0", got)
	}
}

func TestCreateIssuePermission_RESTAskReturnsPendingWithoutMutation(t *testing.T) {
	setCreateIssueWorkspacePolicy(t, toolpolicy.SettingAsk)
	title := "REST pending issue " + uuid.NewString()
	cleanupIssuesByTitle(t, title)

	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, agentCreateIssueRequest(t, title, ""))

	if w.Code != http.StatusAccepted {
		t.Fatalf("REST agent create under Workspace Ask: status=%d, want 202; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "platform_action_pending") {
		t.Fatalf("REST Ask response missing platform_action_pending: %s", w.Body.String())
	}
	if got := issueCountByTitle(t, title); got != 0 {
		t.Fatalf("REST agent create under Workspace Ask mutated %d issue rows before approval, want 0", got)
	}
}

func TestPlatformActionApprovalDoesNotReuseCreateIssueAskForDifferentPayload(t *testing.T) {
	setCreateIssueWorkspacePolicy(t, toolpolicy.SettingAsk)
	firstTitle := "REST pending payload A " + uuid.NewString()
	secondTitle := "REST pending payload B " + uuid.NewString()
	firstReq := agentCreateIssueRequest(t, firstTitle, "")

	first := httptest.NewRecorder()
	testHandler.CreateIssue(first, firstReq)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first Ask status=%d, want 202; body=%s", first.Code, first.Body.String())
	}

	secondReq := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": secondTitle, "allow_duplicate": true,
	})
	secondReq.Header.Set("X-Actor-Source", firstReq.Header.Get("X-Actor-Source"))
	secondReq.Header.Set("X-Agent-ID", firstReq.Header.Get("X-Agent-ID"))
	secondReq.Header.Set("X-Task-ID", firstReq.Header.Get("X-Task-ID"))
	second := httptest.NewRecorder()
	testHandler.CreateIssue(second, secondReq)
	if second.Code != http.StatusAccepted {
		t.Fatalf("second Ask status=%d, want 202; body=%s", second.Code, second.Body.String())
	}

	var firstBody, secondBody map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatalf("decode first Ask response: %v", err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondBody); err != nil {
		t.Fatalf("decode second Ask response: %v", err)
	}
	if firstBody["approval_id"] == secondBody["approval_id"] {
		t.Fatalf("different create payloads reused approval %v", firstBody["approval_id"])
	}
}

func TestCreateIssuePermission_RESTAllowCreatesAgentIssue(t *testing.T) {
	setCreateIssueWorkspacePolicy(t, toolpolicy.SettingAllow)
	title := "REST allowed issue " + uuid.NewString()
	cleanupIssuesByTitle(t, title)

	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, agentCreateIssueRequest(t, title, ""))

	if w.Code != http.StatusCreated {
		t.Fatalf("REST agent create under Workspace Allow: status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	if got := issueCountByTitle(t, title); got != 1 {
		t.Fatalf("REST agent create under Workspace Allow mutated %d issue rows, want 1", got)
	}
}

func TestCreateIssuePermission_RESTDenyDoesNotBlockMemberIssue(t *testing.T) {
	setCreateIssueWorkspacePolicy(t, toolpolicy.SettingDeny)
	title := "REST member issue " + uuid.NewString()
	cleanupIssuesByTitle(t, title)

	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           title,
		"allow_duplicate": true,
	}))

	if w.Code != http.StatusCreated {
		t.Fatalf("member create under agent Workspace Deny: status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	if got := issueCountByTitle(t, title); got != 1 {
		t.Fatalf("member create under agent Workspace Deny mutated %d issue rows, want 1", got)
	}
}
