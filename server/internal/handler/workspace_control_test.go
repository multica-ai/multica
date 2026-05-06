package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestWorkspaceControlBindingWritableBySourceType(t *testing.T) {
	cases := []struct {
		sourceID string
		writable bool
	}{
		{sourceID: "device:task-1", writable: true},
		{sourceID: "md:notes/task-1.md", writable: true},
		{sourceID: "ledger:task-1", writable: false},
		{sourceID: "launchd:com.example.job", writable: false},
		{sourceID: "cron:daily-job", writable: false},
	}

	for _, tc := range cases {
		t.Run(tc.sourceID, func(t *testing.T) {
			binding, ok := parseWorkspaceControlBinding(pgtype.Text{
				String: "<!-- workspace-source-id: " + tc.sourceID + " -->",
				Valid:  true,
			})
			if !ok {
				t.Fatal("expected binding")
			}
			if binding.Writable != tc.writable {
				t.Fatalf("writable = %v, want %v", binding.Writable, tc.writable)
			}
		})
	}
}

func TestWorkspaceControlPolicyRejectsReadOnlySourceUpdate(t *testing.T) {
	issueID := createTestIssueWithDescription(t, "WC readonly", "<!-- workspace-source-id: ledger:task-1 -->")
	t.Cleanup(func() { deleteTestIssueDirect(t, issueID) })

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "in_progress"})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWorkspaceControlWritableSourceUpdateEnqueuesPendingMutation(t *testing.T) {
	issueID := createTestIssueWithDescription(t, "WC writable", "<!-- workspace-source-id: device:task-1 -->")
	t.Cleanup(func() { deleteTestIssueDirect(t, issueID) })

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{"priority": "high"})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.WorkspaceControl == nil || resp.WorkspaceControl.Status == nil || *resp.WorkspaceControl.Status != "pending" {
		t.Fatalf("expected pending workspace control state, got %#v", resp.WorkspaceControl)
	}

	var status string
	if err := testPool.QueryRow(context.Background(), `
		SELECT status FROM workspace_control_mutation WHERE issue_id = $1 ORDER BY created_at DESC LIMIT 1
	`, issueID).Scan(&status); err != nil {
		t.Fatalf("expected workspace control mutation row: %v", err)
	}
	if status != "pending" {
		t.Fatalf("expected pending mutation, got %q", status)
	}
}

func TestWorkspaceControlWebhookFailureMarksApplyFailed(t *testing.T) {
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "workspace source unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(webhook.Close)
	t.Setenv("MULTICA_WORKSPACE_CONTROL_WEBHOOK_URL", webhook.URL)

	issueID := createTestIssueWithDescription(t, "WC apply failed", "<!-- workspace-source-id: md:tasks/task-1.md -->")
	t.Cleanup(func() { deleteTestIssueDirect(t, issueID) })

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "in_progress"})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		var status string
		var errText string
		err := testPool.QueryRow(context.Background(), `
			SELECT status, COALESCE(error, '') FROM workspace_control_mutation WHERE issue_id = $1 ORDER BY created_at DESC LIMIT 1
		`, issueID).Scan(&status, &errText)
		if err != nil {
			t.Fatalf("expected workspace control mutation row: %v", err)
		}
		if status == "apply-failed" {
			if errText == "" {
				t.Fatal("expected apply-failed mutation to record error")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected apply-failed mutation, last status %q", status)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func createTestIssueWithDescription(t *testing.T, title string, description string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":       title,
		"description": description,
		"status":      "todo",
		"priority":    "low",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	return issue.ID
}

func deleteTestIssueDirect(t *testing.T, id string) {
	t.Helper()
	_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id)
	_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace_control_mutation WHERE issue_id = $1`, id)
}
