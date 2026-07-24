package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/featureflags"
)

// enableApprovalForTest turns the platform flag on and enables the flow for the
// test workspace with the given operations. The config is reset on cleanup.
func enableApprovalForTest(t *testing.T, operations []string) {
	t.Helper()
	withFeatureFlag(t, testHandler, featureflags.ApprovalFlow, true)
	body := map[string]any{"enabled": true, "operations": operations}
	w := httptest.NewRecorder()
	testHandler.UpdateApprovalConfig(w, newRequest("PUT", "/api/approvals/config", body))
	if w.Code != http.StatusOK {
		t.Fatalf("enable approval config: %d %s", w.Code, w.Body.String())
	}
	t.Cleanup(func() {
		w2 := httptest.NewRecorder()
		testHandler.UpdateApprovalConfig(w2, newRequest("PUT", "/api/approvals/config", map[string]any{"enabled": false, "operations": []string{}}))
	})
}

func requireTestDB(t *testing.T) {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
}

// createSecondMember inserts a second user + workspace member with the given
// role and returns the user id. Cleaned up by the workspace-slug cascade plus
// an explicit user delete.
func createSecondMember(t *testing.T, role string) string {
	t.Helper()
	ctx := context.Background()
	email := fmt.Sprintf("approval-%s-%s@multica.ai", role, t.Name())
	var userID string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`, "Second "+role, email).Scan(&userID); err != nil {
		t.Fatalf("create second user: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, testWorkspaceID, userID, role); err != nil {
		t.Fatalf("create second member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

func decodeApproval(t *testing.T, body []byte) ApprovalRequestResponse {
	t.Helper()
	var ar ApprovalRequestResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		t.Fatalf("decode approval: %v (body=%s)", err, body)
	}
	return ar
}

func TestApproval_DisabledWhenFlagOff(t *testing.T) {
	requireTestDB(t)
	// Flag off: config endpoint 404s and create 409s.
	w := httptest.NewRecorder()
	testHandler.GetApprovalConfig(w, newRequest("GET", "/api/approvals/config", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("config with flag off: got %d, want 404", w.Code)
	}

	w = httptest.NewRecorder()
	testHandler.CreateApproval(w, newRequest("POST", "/api/approvals/", map[string]any{
		"operation": "project.delete",
	}))
	if w.Code != http.StatusConflict {
		t.Fatalf("create with flag off: got %d, want 409", w.Code)
	}
}

func TestApproval_Lifecycle_Approve_Execute(t *testing.T) {
	requireTestDB(t)
	enableApprovalForTest(t, []string{"project.delete"})

	// Seed a project to delete.
	var projectID string
	if err := testPool.QueryRow(context.Background(), `INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id`, testWorkspaceID, "Approval Test Project").Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	// Create the approval request.
	w := httptest.NewRecorder()
	testHandler.CreateApproval(w, newRequest("POST", "/api/approvals/", map[string]any{
		"operation":  "project.delete",
		"target_id":  projectID,
		"reason":     "cleanup",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create approval: %d %s", w.Code, w.Body.String())
	}
	ar := decodeApproval(t, w.Body.Bytes())
	if ar.Status != "pending" || ar.Operation != "project.delete" || ar.InitiatedByType != "member" {
		t.Fatalf("unexpected approval: %+v", ar)
	}
	approvalID := ar.ID

	// Get it back.
	w = httptest.NewRecorder()
	testHandler.GetApproval(w, withURLParam(newRequest("GET", "/api/approvals/"+approvalID, nil), "id", approvalID))
	if w.Code != http.StatusOK {
		t.Fatalf("get approval: %d %s", w.Code, w.Body.String())
	}

	// A non-admin member cannot approve.
	secondMember := createSecondMember(t, "member")
	w = httptest.NewRecorder()
	testHandler.ApproveApproval(w, withURLParam(newRequestAsUser(secondMember, "POST", "/api/approvals/"+approvalID+"/approve", nil), "id", approvalID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin approve: got %d, want 403", w.Code)
	}

	// Owner approves.
	w = httptest.NewRecorder()
	testHandler.ApproveApproval(w, withURLParam(newRequest("POST", "/api/approvals/"+approvalID+"/approve", map[string]any{"comment": "ok"}), "id", approvalID))
	if w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}
	ar = decodeApproval(t, w.Body.Bytes())
	if ar.Status != "approved" || ar.DecidedByType == nil || *ar.DecidedByType != "member" {
		t.Fatalf("unexpected approved: %+v", ar)
	}

	// Double-decide is rejected (state machine: no longer pending).
	w = httptest.NewRecorder()
	testHandler.RejectApproval(w, withURLParam(newRequest("POST", "/api/approvals/"+approvalID+"/reject", nil), "id", approvalID))
	if w.Code != http.StatusConflict {
		t.Fatalf("double-decide: got %d, want 409", w.Code)
	}

	// Execute the approved action.
	w = httptest.NewRecorder()
	testHandler.ExecuteApproval(w, withURLParam(newRequest("POST", "/api/approvals/"+approvalID+"/execute", nil), "id", approvalID))
	if w.Code != http.StatusOK {
		t.Fatalf("execute: %d %s", w.Code, w.Body.String())
	}

	// The project is gone.
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM project WHERE id = $1`, projectID).Scan(&count); err != nil {
		t.Fatalf("count project: %v", err)
	}
	if count != 0 {
		t.Fatalf("project still exists after execute")
	}

	// Re-executing is rejected.
	w = httptest.NewRecorder()
	testHandler.ExecuteApproval(w, withURLParam(newRequest("POST", "/api/approvals/"+approvalID+"/execute", nil), "id", approvalID))
	if w.Code != http.StatusConflict {
		t.Fatalf("re-execute: got %d, want 409", w.Code)
	}

	// History has created + approved + executed.
	w = httptest.NewRecorder()
	testHandler.ListApprovalEvents(w, withURLParam(newRequest("GET", "/api/approvals/"+approvalID+"/events", nil), "id", approvalID))
	if w.Code != http.StatusOK {
		t.Fatalf("list events: %d %s", w.Code, w.Body.String())
	}
	var ev struct {
		Events []ApprovalEventResponse `json:"events"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &ev); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(ev.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(ev.Events))
	}
	wantTypes := []string{"created", "approved", "executed"}
	for i, et := range wantTypes {
		if ev.Events[i].EventType != et {
			t.Fatalf("event %d: got %s, want %s", i, ev.Events[i].EventType, et)
		}
	}
}

func TestApproval_Reject_BlocksExecution(t *testing.T) {
	requireTestDB(t)
	enableApprovalForTest(t, []string{"project.delete"})

	w := httptest.NewRecorder()
	testHandler.CreateApproval(w, newRequest("POST", "/api/approvals/", map[string]any{"operation": "project.delete", "target_id": seedProjectID(t)}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	ar := decodeApproval(t, w.Body.Bytes())

	w = httptest.NewRecorder()
	testHandler.RejectApproval(w, withURLParam(newRequest("POST", "/api/approvals/"+ar.ID+"/reject", map[string]any{"comment": "no"}), "id", ar.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("reject: %d %s", w.Code, w.Body.String())
	}

	// A rejected request cannot be executed (strong blocking: reject = cancel).
	w = httptest.NewRecorder()
	testHandler.ExecuteApproval(w, withURLParam(newRequest("POST", "/api/approvals/"+ar.ID+"/execute", nil), "id", ar.ID))
	if w.Code != http.StatusConflict {
		t.Fatalf("execute rejected: got %d, want 409", w.Code)
	}
}

func TestApproval_CancelByInitiator(t *testing.T) {
	requireTestDB(t)
	enableApprovalForTest(t, []string{"project.delete"})

	w := httptest.NewRecorder()
	testHandler.CreateApproval(w, newRequest("POST", "/api/approvals/", map[string]any{"operation": "project.delete", "target_id": seedProjectID(t)}))
	ar := decodeApproval(t, w.Body.Bytes())

	// A non-initiator cannot cancel.
	other := createSecondMember(t, "member")
	w = httptest.NewRecorder()
	testHandler.CancelApproval(w, withURLParam(newRequestAsUser(other, "POST", "/api/approvals/"+ar.ID+"/cancel", nil), "id", ar.ID))
	if w.Code != http.StatusConflict {
		t.Fatalf("non-initiator cancel: got %d, want 409", w.Code)
	}

	// Initiator cancels.
	w = httptest.NewRecorder()
	testHandler.CancelApproval(w, withURLParam(newRequest("POST", "/api/approvals/"+ar.ID+"/cancel", nil), "id", ar.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
	}
	if decodeApproval(t, w.Body.Bytes()).Status != "cancelled" {
		t.Fatal("expected cancelled status")
	}
}

func TestApproval_ListAndPending(t *testing.T) {
	requireTestDB(t)
	enableApprovalForTest(t, []string{"project.delete"})

	testHandler.CreateApproval(httptest.NewRecorder(), newRequest("POST", "/api/approvals/", map[string]any{"operation": "project.delete", "target_id": seedProjectID(t)}))
	testHandler.CreateApproval(httptest.NewRecorder(), newRequest("POST", "/api/approvals/", map[string]any{"operation": "project.delete", "target_id": seedProjectID(t)}))

	w := httptest.NewRecorder()
	testHandler.ListApprovals(w, newRequest("GET", "/api/approvals/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var listed struct {
		Approvals []ApprovalRequestResponse `json:"approvals"`
	}
	json.Unmarshal(w.Body.Bytes(), &listed)
	if len(listed.Approvals) < 2 {
		t.Fatalf("expected >=2 approvals, got %d", len(listed.Approvals))
	}

	w = httptest.NewRecorder()
	testHandler.ListPendingApprovals(w, newRequest("GET", "/api/approvals/pending", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list pending: %d %s", w.Code, w.Body.String())
	}
	var pending struct {
		Approvals []ApprovalRequestResponse `json:"approvals"`
	}
	json.Unmarshal(w.Body.Bytes(), &pending)
	for _, a := range pending.Approvals {
		if a.Status != "pending" {
			t.Fatalf("pending list returned non-pending: %s", a.Status)
		}
	}
}

func TestApproval_ConfigGetAndUpdate(t *testing.T) {
	requireTestDB(t)
	enableApprovalForTest(t, []string{"project.delete", "agent.delete"})

	w := httptest.NewRecorder()
	testHandler.GetApprovalConfig(w, newRequest("GET", "/api/approvals/config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("get config: %d %s", w.Code, w.Body.String())
	}
	var cfg ApprovalConfigResponse
	json.Unmarshal(w.Body.Bytes(), &cfg)
	if !cfg.Enabled || len(cfg.Operations) != 2 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if len(cfg.Available) == 0 {
		t.Fatal("expected available operations list")
	}

	// Non-admin cannot update config.
	other := createSecondMember(t, "member")
	w = httptest.NewRecorder()
	testHandler.UpdateApprovalConfig(w, newRequestAsUser(other, "PUT", "/api/approvals/config", map[string]any{"enabled": true, "operations": []string{"project.delete"}}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin config update: got %d, want 403", w.Code)
	}

	// Unknown operation rejected.
	w = httptest.NewRecorder()
	testHandler.UpdateApprovalConfig(w, newRequest("PUT", "/api/approvals/config", map[string]any{"enabled": true, "operations": []string{"bogus.op"}}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown op: got %d, want 400", w.Code)
	}
}

// TestApproval_BatchDeleteIssueGate exercises the Stage 3 integration: a
// workspace with approval required for issue.batch_delete gets a 202 + pending
// request, and after approval the same call (with ?approval_id=) deletes.
func TestApproval_BatchDeleteIssueGate(t *testing.T) {
	requireTestDB(t)
	enableApprovalForTest(t, []string{"issue.batch_delete"})

	// Seed two issues.
	issueIDs := []string{seedIssueID(t), seedIssueID(t)}

	// First call: gated -> 202, nothing deleted.
	w := httptest.NewRecorder()
	testHandler.BatchDeleteIssues(w, newRequest("POST", "/api/issues/batch-delete", map[string]any{"issue_ids": issueIDs}))
	if w.Code != http.StatusAccepted {
		t.Fatalf("gated batch delete: got %d, want 202: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Approval ApprovalRequestResponse `json:"approval"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode gated resp: %v", err)
	}
	if resp.Approval.Status != "pending" {
		t.Fatalf("expected pending, got %s", resp.Approval.Status)
	}
	// Issues still exist.
	if countIssues(t, issueIDs) != 2 {
		t.Fatal("issues should still exist while approval pending")
	}

	// Approve it.
	approvalID := resp.Approval.ID
	w = httptest.NewRecorder()
	testHandler.ApproveApproval(w, withURLParam(newRequest("POST", "/api/approvals/"+approvalID+"/approve", nil), "id", approvalID))
	if w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}

	// Second call with ?approval_id= proceeds and deletes.
	w = httptest.NewRecorder()
	testHandler.BatchDeleteIssues(w, newRequest("POST", "/api/issues/batch-delete?approval_id="+approvalID, map[string]any{"issue_ids": issueIDs}))
	if w.Code != http.StatusOK {
		t.Fatalf("approved batch delete: got %d, want 200: %s", w.Code, w.Body.String())
	}
	if countIssues(t, issueIDs) != 0 {
		t.Fatalf("issues should be deleted after approved batch delete")
	}
}

// seedProjectID inserts a throwaway project and returns its id.
func seedProjectID(t *testing.T) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id`, testWorkspaceID, "Seed Project").Scan(&id); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, id)
	})
	return id
}

func seedIssueID(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	// Assign a real per-workspace number via the workspace counter (matches the
	// insertIssueTo helper). The issue.number column defaults to 0, so a bare
	// insert would collide on uq_issue_workspace_number as soon as a second
	// issue is seeded or any later test inserts its own number=0 issue.
	var number int32
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
		WHERE id = $1
		RETURNING issue_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next issue number: %v", err)
	}
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, number)
		VALUES ($1, $2, 'member', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Seed Issue", testUserID, number).Scan(&id); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id)
	})
	return id
}

func countIssues(t *testing.T, ids []string) int {
	t.Helper()
	if len(ids) == 0 {
		return 0
	}
	var count int
	query := `SELECT count(*) FROM issue WHERE id = ANY($1::uuid[])`
	if err := testPool.QueryRow(context.Background(), query, ids).Scan(&count); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	return count
}
