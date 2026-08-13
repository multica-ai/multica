package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// previewIssueTriggerAs runs the preview endpoint as a specific member, so the
// private-agent gate is evaluated against that member rather than the fixture's
// workspace owner.
func previewIssueTriggerAs(t *testing.T, userID string, body map[string]any) IssueTriggerPreviewResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequestAs(userID, "POST", "/api/issues/preview-trigger?workspace_id="+testWorkspaceID, body)
	testHandler.PreviewIssueTrigger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PreviewIssueTrigger: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp IssueTriggerPreviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	return resp
}

// setIssueStatusAs drives the single-issue status-only write path as a specific
// member — the shape `multica issue status <id> <status>` sends.
func setIssueStatusAs(t *testing.T, userID, issueID, status string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequestAs(userID, "PUT", "/api/issues/"+issueID, map[string]any{"status": status}), "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue status=%s: expected 200, got %d: %s", status, w.Code, w.Body.String())
	}
}

// batchSetIssueStatusAs drives the batch status-only write path as a specific
// member.
func batchSetIssueStatusAs(t *testing.T, userID string, issueIDs []string, status string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequestAs(userID, "POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": issueIDs,
		"updates":   map[string]any{"status": status},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues status=%s: expected 200, got %d: %s", status, w.Code, w.Body.String())
	}
}

// TestStatusSourceDispatchHonoursPrivateAgentGate is the permission-boundary
// regression for the status source (both the backlog promotion and the reopen).
//
// UpdateIssue only runs validateAssigneePair when the write TOUCHES an assignee
// field, so a status-only write never passed through the private-agent gate at
// the HTTP boundary. Preview did evaluate canInvokeAgent, so a member with no
// invoke permission saw total_count=0 and yet the very same write enqueued a
// real task against the private agent — preview and the write path disagreed,
// and an unauthorised member could spend a private agent's runtime.
//
// The gate now runs on the direct-agent dispatch, mirroring the squad branch's
// canEnqueueSquadLeader. The status change itself still applies (existing
// product semantics); only the task must not be created.
func TestStatusSourceDispatchHonoursPrivateAgentGate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	cases := []struct {
		name       string
		prevStatus string
		baseNumber int
	}{
		{"reopen_from_in_review", "in_review", 92400},
		{"reopen_from_done", "done", 92410},
		{"promote_from_backlog", "backlog", 92420},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agentID, ownerID, memberID := privateAgentTestFixture(t)

			// --- unauthorised member: preview 0, write enqueues nothing ---
			denied := insertAssignedIssueWithStatus(t, "agent", agentID, tc.baseNumber, tc.name+"-denied", tc.prevStatus)

			pv := previewIssueTriggerAs(t, memberID, map[string]any{
				"issue_ids": []string{denied}, "status": "todo",
			})
			if pv.TotalCount != 0 {
				t.Fatalf("unauthorised member preview: expected 0 triggers, got %+v", pv)
			}

			setIssueStatusAs(t, memberID, denied, "todo")
			if got := taskCountForIssue(t, denied); got != 0 {
				t.Fatalf("unauthorised member must not enqueue against a private agent, got %d tasks", got)
			}
			if got := issueStatusOf(t, denied); got != "todo" {
				t.Fatalf("the status change itself must still apply, got %q", got)
			}

			// --- agent owner: preview 1, write enqueues exactly one ---
			allowed := insertAssignedIssueWithStatus(t, "agent", agentID, tc.baseNumber+1, tc.name+"-allowed", tc.prevStatus)

			pvOwner := previewIssueTriggerAs(t, ownerID, map[string]any{
				"issue_ids": []string{allowed}, "status": "todo",
			})
			if pvOwner.TotalCount != 1 {
				t.Fatalf("agent owner preview: expected 1 trigger, got %+v", pvOwner)
			}

			setIssueStatusAs(t, ownerID, allowed, "todo")
			if got := queuedTaskCountFor(t, allowed, agentID); got != 1 {
				t.Fatalf("agent owner must get exactly 1 queued task, got %d", got)
			}
		})
	}
}

// TestBatchStatusSourceDispatchHonoursPrivateAgentGate is the batch-path mirror:
// the batch write shares the same predicate and dispatch, so the gate must hold
// there too.
func TestBatchStatusSourceDispatchHonoursPrivateAgentGate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, ownerID, memberID := privateAgentTestFixture(t)

	deniedReopen := insertAssignedIssueWithStatus(t, "agent", agentID, 92430, "batch-gate-denied-reopen", "in_review")
	deniedPromote := insertAssignedIssueWithStatus(t, "agent", agentID, 92431, "batch-gate-denied-promote", "backlog")

	pv := previewIssueTriggerAs(t, memberID, map[string]any{
		"issue_ids": []string{deniedReopen, deniedPromote}, "status": "todo",
	})
	if pv.TotalCount != 0 {
		t.Fatalf("unauthorised member batch preview: expected 0 triggers, got %+v", pv)
	}

	batchSetIssueStatusAs(t, memberID, []string{deniedReopen, deniedPromote}, "todo")
	for _, id := range []string{deniedReopen, deniedPromote} {
		if got := taskCountForIssue(t, id); got != 0 {
			t.Fatalf("unauthorised member batch must not enqueue on %s, got %d tasks", id, got)
		}
	}

	allowedReopen := insertAssignedIssueWithStatus(t, "agent", agentID, 92432, "batch-gate-allowed-reopen", "done")
	allowedPromote := insertAssignedIssueWithStatus(t, "agent", agentID, 92433, "batch-gate-allowed-promote", "backlog")

	pvOwner := previewIssueTriggerAs(t, ownerID, map[string]any{
		"issue_ids": []string{allowedReopen, allowedPromote}, "status": "todo",
	})
	if pvOwner.TotalCount != 2 {
		t.Fatalf("agent owner batch preview: expected 2 triggers, got %+v", pvOwner)
	}

	batchSetIssueStatusAs(t, ownerID, []string{allowedReopen, allowedPromote}, "todo")
	for _, id := range []string{allowedReopen, allowedPromote} {
		if got := queuedTaskCountFor(t, id, agentID); got != 1 {
			t.Fatalf("agent owner batch: expected exactly 1 queued task on %s, got %d", id, got)
		}
	}
}

// TestAssignSourceDispatchStillEnqueuesForAuthorisedMember guards the other
// direction: adding the dispatch gate must not silently swallow the run on the
// assign source, which is already gated at the HTTP boundary. The agent owner
// assigning their own private agent still gets a task; an unauthorised member
// is rejected by validateAssigneePair with 403 before any of this.
func TestAssignSourceDispatchStillEnqueuesForAuthorisedMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, ownerID, memberID := privateAgentTestFixture(t)

	assigned := insertAssignedIssueWithStatus(t, "", "", 92440, "assign-gate-owner", "todo")
	w := httptest.NewRecorder()
	req := withURLParam(newRequestAs(ownerID, "PUT", "/api/issues/"+assigned, map[string]any{
		"assignee_type": "agent", "assignee_id": agentID,
	}), "id", assigned)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("owner assign: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := queuedTaskCountFor(t, assigned, agentID); got != 1 {
		t.Fatalf("owner assign must still enqueue exactly 1 task, got %d", got)
	}

	rejected := insertAssignedIssueWithStatus(t, "", "", 92441, "assign-gate-member", "todo")
	w2 := httptest.NewRecorder()
	req2 := withURLParam(newRequestAs(memberID, "PUT", "/api/issues/"+rejected, map[string]any{
		"assignee_type": "agent", "assignee_id": agentID,
	}), "id", rejected)
	testHandler.UpdateIssue(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("unauthorised assign: expected 403 from validateAssigneePair, got %d: %s", w2.Code, w2.Body.String())
	}
	if got := taskCountForIssue(t, rejected); got != 0 {
		t.Fatalf("unauthorised assign must enqueue nothing, got %d tasks", got)
	}
}

// issueStatusOf reads an issue's persisted status.
func issueStatusOf(t *testing.T, issueID string) string {
	t.Helper()
	var status string
	if err := testPool.QueryRow(t.Context(),
		`SELECT status FROM issue WHERE id = $1`, issueID,
	).Scan(&status); err != nil {
		t.Fatalf("read issue status: %v", err)
	}
	return status
}
