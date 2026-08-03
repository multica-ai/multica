package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestLoadTaskDecisionEvidenceMakesSourceFailureExplicit(t *testing.T) {
	_, unavailable := loadTaskDecisionEvidence(
		context.Background(),
		util.MustParseUUID("00000000-0000-0000-0000-000000000001"),
		func(context.Context, pgtype.UUID) ([]cerebrodb.ListTaskAccessDecisionDiagnosticsRow, error) {
			return nil, errors.New("decision ledger unavailable")
		},
	)
	if !unavailable {
		t.Fatal("Decision Ledger query error was treated as complete diagnostics")
	}
}

func TestGetTaskMandateByUserReturnsExactStoredSnapshot(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "task-access-snapshot", []byte(`{}`))
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position)
		VALUES ($1, 'task access snapshot', 'todo', 'none', 'member', $2, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, issueID)
	})
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)
	if _, err := testPool.Exec(context.Background(), `DELETE FROM cerebro_task_mandate WHERE task_id = $1`, taskID); err != nil {
		t.Fatalf("clear seeded task mandate: %v", err)
	}
	expiresAt := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO cerebro_task_mandate (task_id, workspace_id, agent_id, allowed_tools, expires_at)
		VALUES ($1,$2,$3,'["tools:Read","firtal_registry"]'::jsonb,$4)
	`, taskID, testWorkspaceID, agentID, expiresAt); err != nil {
		t.Fatalf("insert task mandate: %v", err)
	}
	agent, err := testHandler.Queries.GetAgent(context.Background(), util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO cerebro_access_decision_ledger (
			workspace_id, agent_id, runtime_id, task_id, observed_tool_name,
			canonical_capability_id, legacy_decision, legacy_path, shadow_decision,
			policy_decision, evidence_level, differs, reason_code, reason, created_at
		) VALUES
			($1,$2,$3,$4,'mcp__company-brain__search','connection:company-brain/search','deny','policy_decision_service','deny','allow','declared',true,'runtime_capability_unavailable','display-only denial copy',now() - interval '1 second'),
			($1,$2,$3,$4,'mcp__company-brain__search','connection:company-brain/search','allow','policy_decision_service','allow','allow','discovered',false,'policy_allowed','display-only allow copy',now())
	`, testWorkspaceID, agentID, agent.RuntimeID, taskID); err != nil {
		t.Fatalf("insert access decision evidence: %v", err)
	}

	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID: util.MustParseUUID(testUserID), WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load member: %v", err)
	}
	req := newRequest(http.MethodGet, "/api/tasks/"+taskID+"/access", nil)
	req = withURLParam(req, "taskId", taskID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, member))
	w := httptest.NewRecorder()

	testHandler.GetTaskMandateByUser(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var response taskMandateResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.TaskID != taskID || response.AgentID != agentID {
		t.Fatalf("identity = task %q agent %q, want %q / %q", response.TaskID, response.AgentID, taskID, agentID)
	}
	if len(response.AllowedTools) != 2 || response.AllowedTools[0] != "tools:Read" {
		t.Fatalf("allowed tools = %#v, want exact snapshot", response.AllowedTools)
	}
	if response.Status != "active" {
		t.Fatalf("status = %q, want active", response.Status)
	}
	if response.LifecycleState != "legacy" || response.ClaimGeneration != 1 {
		t.Fatalf("legacy diagnostics = lifecycle %q generation %d", response.LifecycleState, response.ClaimGeneration)
	}
	if response.OfferedCount != 2 || response.AuthorizedCount != 2 || response.Verdict.Code != "allowed" {
		t.Fatalf("read model = offered %d authorized %d verdict %+v", response.OfferedCount, response.AuthorizedCount, response.Verdict)
	}
	if response.EnforcementEnabled {
		t.Fatal("default-off workspace reported Task Mandate enforcement as enabled")
	}
	if len(response.Diagnostics) == 0 || response.Diagnostics[0].Code != "task_observation_only" {
		t.Fatalf("diagnostics = %#v, want observation-only explanation", response.Diagnostics)
	}
	foundDenial := false
	for _, diagnostic := range response.Diagnostics {
		if diagnostic.Code == "observed_denial" && diagnostic.State == "denied" &&
			diagnostic.SourcePolicy == "Runtime availability" && diagnostic.AffectedCapability == "connection:company-brain/search" {
			foundDenial = true
		}
	}
	if !foundDenial {
		t.Fatalf("diagnostics = %#v, want the earlier enforced runtime-absence denial even after a later success", response.Diagnostics)
	}
}

func TestGetTaskMandateByUserReportsExpiredSnapshotAsAllowedWhenEnforcementOff(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "task-access-expired-observation", []byte(`{}`))
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position)
		VALUES ($1, 'expired task access observation', 'todo', 'none', 'member', $2, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, issueID)
	})
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)
	if _, err := testPool.Exec(context.Background(), `DELETE FROM cerebro_task_mandate WHERE task_id = $1`, taskID); err != nil {
		t.Fatalf("clear seeded task mandate: %v", err)
	}
	issuedAt := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Microsecond)
	expiresAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO cerebro_task_mandate (task_id, workspace_id, agent_id, allowed_tools, issued_at, expires_at)
		VALUES ($1,$2,$3,'["tools:Read"]'::jsonb,$4,$5)
	`, taskID, testWorkspaceID, agentID, issuedAt, expiresAt); err != nil {
		t.Fatalf("insert expired task mandate: %v", err)
	}

	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID: util.MustParseUUID(testUserID), WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load member: %v", err)
	}
	req := newRequest(http.MethodGet, "/api/tasks/"+taskID+"/access", nil)
	req = withURLParam(req, "taskId", taskID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, member))
	w := httptest.NewRecorder()

	testHandler.GetTaskMandateByUser(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var response taskMandateResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != "expired" || response.EnforcementEnabled {
		t.Fatalf("snapshot state = status %q enforcement %v, want expired observation with enforcement off", response.Status, response.EnforcementEnabled)
	}
	if !response.Verdict.Allowed || response.Verdict.Code != "allowed" || response.Verdict.RecoveryAction != "none" {
		t.Fatalf("diagnostic verdict = %+v, want allowed/none to match call-time behavior while enforcement is off", response.Verdict)
	}
	foundExpired := false
	for _, diagnostic := range response.Diagnostics {
		if diagnostic.Code == "task_expired" && diagnostic.State == "stale" && diagnostic.RecoveryAction != "" {
			foundExpired = true
		}
	}
	if !foundExpired {
		t.Fatalf("diagnostics = %#v, want stale expired snapshot with recovery", response.Diagnostics)
	}
}

func TestGetTaskMandateByUserRejectsInvalidTaskID(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/not-a-task/access", nil)
	req = withURLParam(req, "taskId", "not-a-task")
	(&Handler{}).GetTaskMandateByUser(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestGetTaskMandateByUserReturnsSharedDiagnosticWhenSnapshotIsMissing(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "task-access-missing", []byte(`{}`))
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position)
		VALUES ($1, 'missing task access snapshot', 'todo', 'none', 'member', $2, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, issueID)
	})
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)
	if _, err := testPool.Exec(context.Background(), `DELETE FROM cerebro_task_mandate WHERE task_id = $1`, taskID); err != nil {
		t.Fatalf("clear seeded task mandate: %v", err)
	}
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID: util.MustParseUUID(testUserID), WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load member: %v", err)
	}
	req := newRequest(http.MethodGet, "/api/tasks/"+taskID+"/access", nil)
	req = withURLParam(req, "taskId", taskID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, member))
	w := httptest.NewRecorder()

	testHandler.GetTaskMandateByUser(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
	var response struct {
		Diagnostics []struct {
			Code               string `json:"code"`
			State              string `json:"state"`
			AffectedCapability string `json:"affected_capability"`
			SourcePolicy       string `json:"source_policy"`
			RecoveryAction     string `json:"recovery_action"`
		} `json:"diagnostics"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Diagnostics) != 1 || response.Diagnostics[0].Code != "task_mandate_missing" ||
		response.Diagnostics[0].State != "unavailable" || response.Diagnostics[0].AffectedCapability != "task:"+taskID ||
		response.Diagnostics[0].SourcePolicy != "Task Mandate" || response.Diagnostics[0].RecoveryAction == "" {
		t.Fatalf("diagnostics = %#v, want shared missing-snapshot recovery", response.Diagnostics)
	}
}
