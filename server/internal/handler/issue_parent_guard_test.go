package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Incident replays + positives for the UMC-288 squad-parent ownership and
// active-dependency guards. These lock the server-side enforcement so a
// role-owned child cannot be used as a handoff bypass and a parent cannot
// record an active dependency on a ghost child.

type guardIssue struct {
	ID            string  `json:"id"`
	Identifier    string  `json:"identifier"`
	Status        string  `json:"status"`
	AssigneeType  *string `json:"assignee_type"`
	AssigneeID    *string `json:"assignee_id"`
	ParentIssueID *string `json:"parent_issue_id"`
}

func createIssueExpect(t *testing.T, body map[string]any, wantCode int) (guardIssue, string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues", body)
	testHandler.CreateIssue(w, req)
	if w.Code != wantCode {
		t.Fatalf("CreateIssue %v: want %d, got %d: %s", body, wantCode, w.Code, w.Body.String())
	}
	var out guardIssue
	if w.Code == http.StatusCreated {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode create response: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, out.ID) })
	}
	return out, w.Body.String()
}

func updateIssueExpect(t *testing.T, issueID string, body map[string]any, wantCode int) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/issues/"+issueID, body)
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != wantCode {
		t.Fatalf("UpdateIssue %v: want %d, got %d: %s", body, wantCode, w.Code, w.Body.String())
	}
	return w.Body.String()
}

func setMetaExpect(t *testing.T, issueID, key, rawValue string, wantCode int) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID+"/metadata/"+key, json.RawMessage(`{"value":`+rawValue+`}`))
	req = withURLParams(req, "id", issueID, "key", key)
	testHandler.SetIssueMetadataKey(w, req)
	if w.Code != wantCode {
		t.Fatalf("SetIssueMetadataKey %s=%s: want %d, got %d: %s", key, rawValue, wantCode, w.Code, w.Body.String())
	}
	return w.Body.String()
}

func getMeta(t *testing.T, issueID string) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/metadata", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListIssueMetadata(w, req)
	var resp struct {
		Metadata map[string]any `json:"metadata"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp.Metadata
}

// seedGuardSquad creates a squad with a fresh leader agent and returns the
// squad id (string) and leader agent id.
func seedGuardSquad(t *testing.T, name string) (string, string) {
	t.Helper()
	leaderID := createHandlerTestAgent(t, name+"-leader", []byte("{}"))
	squad := seedSquadForBriefing(t, leaderID, name, "")
	return uuidToString(squad.ID), leaderID
}

func createSquadParent(t *testing.T, squadID, title string) guardIssue {
	t.Helper()
	p, _ := createIssueExpect(t, map[string]any{
		"title":         title,
		"status":        "backlog",
		"assignee_type": "squad",
		"assignee_id":   squadID,
	}, http.StatusCreated)
	return p
}

func TestSquadParentChildOwnershipGuard_Create(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, leaderID := seedGuardSquad(t, "umc288-create")
	squadB, _ := seedGuardSquad(t, "umc288-create-b")
	parent := createSquadParent(t, squadID, "umc288 create parent")

	// UMC-307: role-owned (agent) child under a squad parent must fail.
	_, body := createIssueExpect(t, map[string]any{
		"title": "role-owned agent child", "status": "backlog",
		"parent_issue_id": parent.ID, "assignee_type": "agent", "assignee_id": leaderID,
	}, http.StatusBadRequest)
	if !strings.Contains(body, "same squad") {
		t.Errorf("agent child: expected same-squad error, got %s", body)
	}

	// Member-owned child under a squad parent must fail.
	createIssueExpect(t, map[string]any{
		"title": "role-owned member child", "status": "backlog",
		"parent_issue_id": parent.ID, "assignee_type": "member", "assignee_id": testUserID,
	}, http.StatusBadRequest)

	// A different squad under the squad parent must fail.
	createIssueExpect(t, map[string]any{
		"title": "different squad child", "status": "backlog",
		"parent_issue_id": parent.ID, "assignee_type": "squad", "assignee_id": squadB,
	}, http.StatusBadRequest)

	// Omitted assignee inherits the parent squad (stays squad-owned).
	child, _ := createIssueExpect(t, map[string]any{
		"title": "inherits squad", "status": "backlog", "parent_issue_id": parent.ID,
	}, http.StatusCreated)
	if child.AssigneeType == nil || *child.AssigneeType != "squad" || child.AssigneeID == nil || *child.AssigneeID != squadID {
		t.Fatalf("omitted assignee should inherit parent squad, got type=%v id=%v", child.AssigneeType, child.AssigneeID)
	}

	// Explicit same-squad child is accepted.
	createIssueExpect(t, map[string]any{
		"title": "same squad child", "status": "backlog", "parent_issue_id": parent.ID,
		"assignee_type": "squad", "assignee_id": squadID,
	}, http.StatusCreated)
}

func TestSquadParentChildOwnershipGuard_Update(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, leaderID := seedGuardSquad(t, "umc288-update")
	parent := createSquadParent(t, squadID, "umc288 update parent")
	child, _ := createIssueExpect(t, map[string]any{
		"title": "squad child", "status": "backlog", "parent_issue_id": parent.ID,
	}, http.StatusCreated)

	// Re-owning a squad child to an agent under a squad parent must fail.
	body := updateIssueExpect(t, child.ID, map[string]any{
		"assignee_type": "agent", "assignee_id": leaderID,
	}, http.StatusBadRequest)
	if !strings.Contains(body, "same squad") {
		t.Errorf("update to agent: expected same-squad error, got %s", body)
	}

	// Re-parenting an agent-owned top-level issue under a squad parent must fail.
	orphan, _ := createIssueExpect(t, map[string]any{
		"title": "agent orphan", "status": "backlog",
		"assignee_type": "agent", "assignee_id": leaderID,
	}, http.StatusCreated)
	updateIssueExpect(t, orphan.ID, map[string]any{"parent_issue_id": parent.ID}, http.StatusBadRequest)

	// An unrelated edit on a coherent squad child is not retroactively rejected.
	updateIssueExpect(t, child.ID, map[string]any{"title": "renamed"}, http.StatusOK)

	// Review gap: reparenting an UNASSIGNED orphan under a squad parent must not
	// leave it unassigned — fail closed (no ghost surface).
	unassignedOrphan, _ := createIssueExpect(t, map[string]any{
		"title": "unassigned orphan", "status": "backlog",
	}, http.StatusCreated)
	ub := updateIssueExpect(t, unassignedOrphan.ID, map[string]any{"parent_issue_id": parent.ID}, http.StatusBadRequest)
	if !strings.Contains(ub, "unassigned") {
		t.Errorf("reparent unassigned orphan: expected unassigned error, got %s", ub)
	}

	// Review gap: an explicit unassign of a same-squad child under a squad parent
	// must fail (do not silently drop ownership into a ghost).
	eub := updateIssueExpect(t, child.ID, map[string]any{"assignee_type": nil, "assignee_id": nil}, http.StatusBadRequest)
	if !strings.Contains(eub, "unassigned") {
		t.Errorf("explicit unassign under squad parent: expected unassigned error, got %s", eub)
	}
}

func TestSquadParentChildOwnershipGuard_Batch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, leaderID := seedGuardSquad(t, "umc288-batch")
	parent := createSquadParent(t, squadID, "umc288 batch parent")
	child, _ := createIssueExpect(t, map[string]any{
		"title": "squad child", "status": "backlog", "parent_issue_id": parent.ID,
	}, http.StatusCreated)

	// Batch re-owning the child to an agent must fail explicitly (not a silent
	// skip that lowers `updated`).
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{child.ID},
		"updates":   map[string]any{"assignee_type": "agent", "assignee_id": leaderID},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("batch ownership violation: want 400, got %d: %s", w.Code, w.Body.String())
	}
	// The child must be untouched (still squad-owned).
	var atype string
	testPool.QueryRow(context.Background(), `SELECT assignee_type FROM issue WHERE id = $1`, child.ID).Scan(&atype)
	if atype != "squad" {
		t.Fatalf("child assignee_type after rejected batch = %q, want squad", atype)
	}

	// A batch that does not touch ownership still applies.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{child.ID},
		"updates":   map[string]any{"priority": "high"},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("non-ownership batch: want 200, got %d: %s", w.Code, w.Body.String())
	}

	// Review gap: batch reparent of an UNASSIGNED orphan under a squad parent
	// must fail the whole batch explicitly (no ghost surface).
	orphan, _ := createIssueExpect(t, map[string]any{
		"title": "batch unassigned orphan", "status": "backlog",
	}, http.StatusCreated)
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{orphan.ID},
		"updates":   map[string]any{"parent_issue_id": parent.ID},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("batch reparent unassigned orphan: want 400, got %d: %s", w.Code, w.Body.String())
	}

	// Review gap: batch explicit unassign of a same-squad child must fail.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{child.ID},
		"updates":   map[string]any{"assignee_type": nil, "assignee_id": nil},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("batch explicit unassign: want 400, got %d: %s", w.Code, w.Body.String())
	}
	// The child stays squad-owned after the rejected batch.
	var stillType string
	testPool.QueryRow(context.Background(), `SELECT assignee_type FROM issue WHERE id = $1`, child.ID).Scan(&stillType)
	if stillType != "squad" {
		t.Fatalf("child assignee_type after rejected unassign batch = %q, want squad", stillType)
	}
}

func TestActiveDependencyMetadataGuard(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	squadID, leaderID := seedGuardSquad(t, "umc288-meta")
	parent := createSquadParent(t, squadID, "umc288 meta parent")

	// UMC-305: linked_child pointing at a ghost child (squad-owned but inert —
	// no run, no comment) must fail and write nothing.
	ghost, _ := createIssueExpect(t, map[string]any{
		"title": "ghost child", "status": "backlog", "parent_issue_id": parent.ID,
	}, http.StatusCreated)
	// Pre-existing unrelated metadata must survive the rejected write.
	setMetaExpect(t, parent.ID, "pipeline_status", `"waiting"`, http.StatusOK)
	body := setMetaExpect(t, parent.ID, "linked_child", `"`+ghost.ID+`"`, http.StatusBadRequest)
	if !strings.Contains(body, "inert") {
		t.Errorf("ghost linked_child: expected inert error, got %s", body)
	}
	md := getMeta(t, parent.ID)
	if _, present := md["linked_child"]; present {
		t.Errorf("rejected linked_child must not be written; metadata=%v", md)
	}
	if md["pipeline_status"] != "waiting" {
		t.Errorf("rejected write must preserve prior metadata; metadata=%v", md)
	}

	// Ownership: linked_child to an issue owned by a role agent (not the squad)
	// under a squad-owned parent must fail.
	foreign, _ := createIssueExpect(t, map[string]any{
		"title": "agent-owned foreign", "status": "backlog",
		"assignee_type": "agent", "assignee_id": leaderID,
	}, http.StatusCreated)
	ownBody := setMetaExpect(t, parent.ID, "linked_child", `"`+foreign.ID+`"`, http.StatusBadRequest)
	if !strings.Contains(ownBody, "same squad") {
		t.Errorf("foreign linked_child: expected same-squad error, got %s", ownBody)
	}

	// Non-existent reference fails.
	setMetaExpect(t, parent.ID, "linked_child", `"UMC-99999999"`, http.StatusBadRequest)
	// Non-string reference fails.
	setMetaExpect(t, parent.ID, "linked_child", `123`, http.StatusBadRequest)

	// Positive LAUNCHED: same-squad child with a comment readback is accepted.
	// (Backlog keeps the fixture free of a squad-leader enqueue side effect; the
	// comment is the non-inert readback signal the guard keys on.)
	launched, _ := createIssueExpect(t, map[string]any{
		"title": "launched child", "status": "backlog", "parent_issue_id": parent.ID,
	}, http.StatusCreated)
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'agent', $3, 'kickoff', 'comment') RETURNING id
	`, launched.ID, testWorkspaceID, leaderID).Scan(&commentID); err != nil {
		t.Fatalf("seed launched comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID) })
	setMetaExpect(t, parent.ID, "linked_child", `"`+launched.Identifier+`"`, http.StatusOK)

	// Positive DEFERRED: same-squad backlog child with an explicit deferred
	// reason is accepted.
	deferred, _ := createIssueExpect(t, map[string]any{
		"title": "deferred child", "status": "backlog", "parent_issue_id": parent.ID,
	}, http.StatusCreated)
	setMetaExpect(t, deferred.ID, "deferred_reason", `"waiting upstream contract"`, http.StatusOK)
	setMetaExpect(t, parent.ID, "linked_child", `"`+deferred.ID+`"`, http.StatusOK)

	// Review gap: a non-string (or empty) deferred value must NOT count as
	// DEFERRED — no metadata type-sniffing bypass. waiting_on=false /
	// deferred_reason=0 / deferred_reason="" leave the child inert, so linking
	// it is rejected.
	for i, neg := range []struct{ key, val string }{
		{"waiting_on", `false`},
		{"deferred_reason", `0`},
		{"deferred_reason", `""`},
	} {
		bad, _ := createIssueExpect(t, map[string]any{
			"title": fmt.Sprintf("bad-deferred-%d", i), "status": "backlog", "parent_issue_id": parent.ID,
		}, http.StatusCreated)
		setMetaExpect(t, bad.ID, neg.key, neg.val, http.StatusOK)
		nb := setMetaExpect(t, parent.ID, "linked_child", `"`+bad.ID+`"`, http.StatusBadRequest)
		if !strings.Contains(nb, "inert") {
			t.Errorf("deferred %s=%s should be inert (not a valid deferral), got %s", neg.key, neg.val, nb)
		}
	}
}

// TestCompleteTaskCapturesBranchName locks the readback fix: the branch_name
// the daemon reports on completion is captured into the stored result (it was
// previously dropped), and the visible-comment fallback still fires.
func TestCompleteTaskCapturesBranchName(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'umc288 readback fixture', 'in_progress', 'none', $2, 'member', 88288, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at)
		VALUES ($1, $2, $3, 'running', 0, now()) RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{"output": "done; opened PR", "branch_name": "feat/umc-288-guard", "pr_url": "https://example.test/pr/1"},
		testWorkspaceID, "legit-daemon")
	req = withURLParams(req, "taskId", taskID)
	testHandler.CompleteTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: want 200, got %d: %s", w.Code, w.Body.String())
	}

	// branch_name (and pr_url) survive into the stored result.
	var result []byte
	if err := testPool.QueryRow(ctx, `SELECT result FROM agent_task_queue WHERE id = $1`, taskID).Scan(&result); err != nil {
		t.Fatalf("read result: %v", err)
	}
	var stored map[string]any
	if err := json.Unmarshal(result, &stored); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if stored["branch_name"] != "feat/umc-288-guard" {
		t.Errorf("branch_name not captured in result: %v", stored)
	}
	if stored["pr_url"] != "https://example.test/pr/1" {
		t.Errorf("pr_url not captured in result: %v", stored)
	}

	// Visible-comment fallback remains: a silent agent run still surfaces output.
	var comments int
	testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1 AND author_type = 'agent'`, issueID).Scan(&comments)
	if comments < 1 {
		t.Errorf("expected the visible-comment fallback to synthesize a comment, got %d", comments)
	}
}
