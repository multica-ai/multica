package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/cerebro/taskmandate"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

const createIssuePermissionToolKey = "create_issue"

func setCreateIssueWorkspacePolicy(t *testing.T, setting toolpolicy.Setting) {
	setPlatformActionWorkspacePolicy(t, createIssuePermissionToolKey, setting)
}

func setPlatformActionWorkspacePolicy(t *testing.T, toolKey string, setting toolpolicy.Setting) {
	t.Helper()
	ctx := context.Background()
	workspaceID, err := util.ParseUUID(testWorkspaceID)
	if err != nil {
		t.Fatalf("parse workspace id: %v", err)
	}
	store := toolpolicy.NewStore(testPool)
	if _, err := testPool.Exec(ctx, `DELETE FROM cerebro_tool_policy WHERE workspace_id = $1 AND tool_key = $2`, workspaceID, toolKey); err != nil {
		t.Fatalf("clear %s policy: %v", toolKey, err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM cerebro_tool_policy WHERE workspace_id = $1 AND tool_key = $2`, workspaceID, toolKey)
	})
	if _, err := store.Set(ctx, toolpolicy.SetParams{
		WorkspaceID: workspaceID,
		ToolKey:     toolKey,
		Layer:       toolpolicy.LayerWorkspace,
		SubjectID:   workspaceID,
		Setting:     setting,
	}); err != nil {
		t.Fatalf("set %s workspace policy: %v", toolKey, err)
	}
}

func issuePlatformActionMandate(t *testing.T, taskID, agentID string, actions ...string) {
	t.Helper()
	taskUUID, err := util.ParseUUID(taskID)
	if err != nil {
		t.Fatalf("parse task id: %v", err)
	}
	workspaceUUID, err := util.ParseUUID(testWorkspaceID)
	if err != nil {
		t.Fatalf("parse workspace id: %v", err)
	}
	agentUUID, err := util.ParseUUID(agentID)
	if err != nil {
		t.Fatalf("parse agent id: %v", err)
	}
	if err := taskmandate.NewStore(testPool).Issue(
		context.Background(),
		taskUUID,
		workspaceUUID,
		agentUUID,
		actions,
		time.Now().Add(time.Hour),
	); err != nil {
		t.Fatalf("issue task mandate: %v", err)
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

func approvalIDFromResponse(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode approval response: %v", err)
	}
	id, _ := body["approval_id"].(string)
	if id == "" {
		t.Fatalf("approval response missing approval_id: %s", rec.Body.String())
	}
	return id
}

func retryAgentCreateIssueRequest(t *testing.T, original *http.Request, title, parentID, approvalID string) *http.Request {
	t.Helper()
	body := map[string]any{"title": title, "allow_duplicate": true}
	if parentID != "" {
		body["parent_issue_id"] = parentID
	}
	req := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, body)
	req.Header.Set("X-Actor-Source", original.Header.Get("X-Actor-Source"))
	req.Header.Set("X-Agent-ID", original.Header.Get("X-Agent-ID"))
	req.Header.Set("X-Task-ID", original.Header.Get("X-Task-ID"))
	req.Header.Set("X-Platform-Approval-ID", approvalID)
	return req
}

func decideCreateIssueApproval(t *testing.T, approvalID, status string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		UPDATE cerebro_approval_request
		SET status = $2, decided_at = now(), expires_at = now() + interval '15 minutes', updated_at = now()
		WHERE id = $1`, approvalID, status); err != nil {
		t.Fatalf("decide create issue approval: %v", err)
	}
}

func TestCreateIssuePermission_RESTAskApproveCreatesExactlyOnce(t *testing.T) {
	setCreateIssueWorkspacePolicy(t, toolpolicy.SettingAsk)
	title := "REST approved once " + uuid.NewString()
	cleanupIssuesByTitle(t, title)
	original := agentCreateIssueRequest(t, title, "")

	pending := httptest.NewRecorder()
	testHandler.CreateIssue(pending, original)
	if pending.Code != http.StatusAccepted || issueCountByTitle(t, title) != 0 {
		t.Fatalf("initial Ask = status %d rows %d, want 202 and 0; body=%s", pending.Code, issueCountByTitle(t, title), pending.Body.String())
	}
	approvalID := approvalIDFromResponse(t, pending)
	decideCreateIssueApproval(t, approvalID, "approved")
	// A policy flip to Allow must not bypass consumption of an explicit resume
	// token; otherwise the stale approval could be replayed after Ask is restored.
	setCreateIssueWorkspacePolicy(t, toolpolicy.SettingAllow)

	approved := httptest.NewRecorder()
	testHandler.CreateIssue(approved, retryAgentCreateIssueRequest(t, original, title, "", approvalID))
	if approved.Code != http.StatusCreated || issueCountByTitle(t, title) != 1 {
		t.Fatalf("approved retry = status %d rows %d, want 201 and 1; body=%s", approved.Code, issueCountByTitle(t, title), approved.Body.String())
	}

	replay := httptest.NewRecorder()
	testHandler.CreateIssue(replay, retryAgentCreateIssueRequest(t, original, title, "", approvalID))
	if replay.Code != http.StatusForbidden {
		t.Fatalf("approval replay status=%d, want 403; body=%s", replay.Code, replay.Body.String())
	}
	if got := issueCountByTitle(t, title); got != 1 {
		t.Fatalf("approval replay mutated %d issue rows, want exactly 1", got)
	}
}

func TestCreateIssuePermission_RESTAskRejectCreatesNothing(t *testing.T) {
	setCreateIssueWorkspacePolicy(t, toolpolicy.SettingAsk)
	title := "REST rejected " + uuid.NewString()
	cleanupIssuesByTitle(t, title)
	original := agentCreateIssueRequest(t, title, "")
	pending := httptest.NewRecorder()
	testHandler.CreateIssue(pending, original)
	approvalID := approvalIDFromResponse(t, pending)
	decideCreateIssueApproval(t, approvalID, "rejected")

	rejected := httptest.NewRecorder()
	testHandler.CreateIssue(rejected, retryAgentCreateIssueRequest(t, original, title, "", approvalID))
	if rejected.Code != http.StatusForbidden {
		t.Fatalf("rejected retry status=%d, want 403; body=%s", rejected.Code, rejected.Body.String())
	}
	if got := issueCountByTitle(t, title); got != 0 {
		t.Fatalf("rejected retry mutated %d issue rows, want 0", got)
	}
}

func TestCreateIssuePermission_RESTAskExpiredCreatesNothing(t *testing.T) {
	setCreateIssueWorkspacePolicy(t, toolpolicy.SettingAsk)
	title := "REST expired " + uuid.NewString()
	cleanupIssuesByTitle(t, title)
	original := agentCreateIssueRequest(t, title, "")
	pending := httptest.NewRecorder()
	testHandler.CreateIssue(pending, original)
	approvalID := approvalIDFromResponse(t, pending)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE cerebro_approval_request
		SET status = 'approved', decided_at = now(), expires_at = now() - interval '1 minute', updated_at = now()
		WHERE id = $1`, approvalID); err != nil {
		t.Fatalf("expire create issue approval: %v", err)
	}

	expired := httptest.NewRecorder()
	testHandler.CreateIssue(expired, retryAgentCreateIssueRequest(t, original, title, "", approvalID))
	if expired.Code != http.StatusForbidden {
		t.Fatalf("expired retry status=%d, want 403; body=%s", expired.Code, expired.Body.String())
	}
	if got := issueCountByTitle(t, title); got != 0 {
		t.Fatalf("expired retry mutated %d issue rows, want 0", got)
	}
}

func TestCreateIssuePermission_RESTSubIssueAskApproveCreatesExactlyOnce(t *testing.T) {
	parentID := createPermissionTestParent(t)
	setCreateIssueWorkspacePolicy(t, toolpolicy.SettingAsk)
	title := "REST approved sub-issue once " + uuid.NewString()
	cleanupIssuesByTitle(t, title)
	original := agentCreateIssueRequest(t, title, parentID)
	pending := httptest.NewRecorder()
	testHandler.CreateIssue(pending, original)
	approvalID := approvalIDFromResponse(t, pending)
	decideCreateIssueApproval(t, approvalID, "approved")

	approved := httptest.NewRecorder()
	testHandler.CreateIssue(approved, retryAgentCreateIssueRequest(t, original, title, parentID, approvalID))
	if approved.Code != http.StatusCreated || issueCountByTitle(t, title) != 1 {
		t.Fatalf("approved sub-issue retry = status %d rows %d, want 201 and 1; body=%s", approved.Code, issueCountByTitle(t, title), approved.Body.String())
	}
	replay := httptest.NewRecorder()
	testHandler.CreateIssue(replay, retryAgentCreateIssueRequest(t, original, title, parentID, approvalID))
	if replay.Code != http.StatusForbidden || issueCountByTitle(t, title) != 1 {
		t.Fatalf("sub-issue replay = status %d rows %d, want 403 and 1; body=%s", replay.Code, issueCountByTitle(t, title), replay.Body.String())
	}
}

func TestCreateIssuePermissionApprovalUsesServerTaskOrigin(t *testing.T) {
	setCreateIssueWorkspacePolicy(t, toolpolicy.SettingAsk)
	title := "REST server origin " + uuid.NewString()
	sourceIssueID := createPermissionTestParent(t)
	agentID := createHandlerTestAgent(t, "create-issue-origin-"+uuid.NewString(), []byte(`{}`))
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, sourceIssueID)
	original := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": title, "allow_duplicate": true,
	})
	original.Header.Set("X-Actor-Source", "task_token")
	original.Header.Set("X-Agent-ID", agentID)
	original.Header.Set("X-Task-ID", taskID)
	pending := httptest.NewRecorder()
	testHandler.CreateIssue(pending, original)
	approvalID := approvalIDFromResponse(t, pending)

	var contextJSON []byte
	if err := testPool.QueryRow(context.Background(), `SELECT context FROM cerebro_approval_request WHERE id = $1`, approvalID).Scan(&contextJSON); err != nil {
		t.Fatalf("load approval context: %v", err)
	}
	var origin map[string]any
	if err := json.Unmarshal(contextJSON, &origin); err != nil {
		t.Fatalf("decode approval context: %v", err)
	}
	if origin["task_id"] != original.Header.Get("X-Task-ID") {
		t.Fatalf("approval task_id=%v, want server task %s; context=%s", origin["task_id"], original.Header.Get("X-Task-ID"), contextJSON)
	}
	if origin["issue_id"] != sourceIssueID || origin["surface"] != "issue" {
		t.Fatalf("approval origin=%v, want issue_id=%s surface=issue", origin, sourceIssueID)
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
