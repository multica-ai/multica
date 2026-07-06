package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// childDoneFixture creates a parent + child pair so the parent-notification
// tests can drive the child's status changes independently. Cleanup is
// registered on the test so the rows are removed even on test failure.
type childDoneFixture struct {
	parent IssueResponse
	child  IssueResponse
}

func newChildDoneFixture(t *testing.T, parentStatus string) childDoneFixture {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "child-done parent " + time.Now().Format(time.RFC3339Nano),
		"status": parentStatus,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parent IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&parent); err != nil {
		t.Fatalf("decode parent: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "child-done child " + time.Now().Format(time.RFC3339Nano),
		"status":          "in_progress",
		"parent_issue_id": parent.ID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create child: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var child IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&child); err != nil {
		t.Fatalf("decode child: %v", err)
	}

	t.Cleanup(func() {
		ctx := context.Background()
		// Cascades through comment.
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, child.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, parent.ID)
	})

	return childDoneFixture{parent: parent, child: child}
}

// updateChildStatus drives an UpdateIssue HTTP call against the child issue.
func updateChildStatus(t *testing.T, childID, status string) {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+childID, map[string]any{"status": status})
	req = withURLParam(req, "id", childID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue child status=%q: expected 200, got %d: %s", status, w.Code, w.Body.String())
	}
}

// countSystemCommentsOn returns the number of platform-generated comments on
// the given issue. The schema CHECK was widened in migration 107 to allow
// author_type='system'; this query is the canary that the migration applied
// and the helper inserts with the right author identity.
func countSystemCommentsOn(t *testing.T, issueID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM comment WHERE issue_id = $1 AND author_type = 'system'`,
		issueID,
	).Scan(&n); err != nil {
		t.Fatalf("count system comments: %v", err)
	}
	return n
}

func systemCommentOn(t *testing.T, issueID string) (content, authorIDStr string, parentNull bool, typeStr string) {
	t.Helper()
	row := testPool.QueryRow(context.Background(),
		`SELECT content, author_id::text, parent_id IS NULL, type
		   FROM comment
		   WHERE issue_id = $1 AND author_type = 'system'
		   ORDER BY created_at DESC
		   LIMIT 1`,
		issueID)
	if err := row.Scan(&content, &authorIDStr, &parentNull, &typeStr); err != nil {
		t.Fatalf("read system comment: %v", err)
	}
	return
}

// TestChildDoneNotifiesParent — the happy path for an unassigned parent. A
// child transitioning from a non-done status into `done` while its parent is
// open must produce exactly one top-level platform-generated comment on the
// parent. The comment must reference the child by its workspace-specific
// identifier (NOT a hardcoded `MUL-` prefix — that was the bug PR #2918
// review called out). When the parent has no assignee, the body must NOT
// carry any agent/member/squad mention either; the assignee-mention is the
// only mention we ever inject (see MUL-2538 Option C — covered separately
// in TestChildDoneMentionsParentAssignee_* below).
func TestChildDoneNotifiesParent(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("expected exactly 1 system comment on parent, got %d", got)
	}
	content, authorID, parentNull, typeStr := systemCommentOn(t, fx.parent.ID)

	if !parentNull {
		t.Errorf("system comment must be top-level (parent_id IS NULL)")
	}
	if typeStr != "system" {
		t.Errorf("system comment type should be 'system', got %q", typeStr)
	}
	if authorID != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("system comment author_id should be the zero UUID sentinel, got %q", authorID)
	}

	// Identifier substring must use the real workspace prefix (HAN-, seeded
	// in TestMain), never MUL-.
	if !strings.Contains(content, fx.child.Identifier) {
		t.Errorf("expected comment to contain child identifier %q, got: %s", fx.child.Identifier, content)
	}
	if strings.Contains(content, "MUL-") {
		t.Errorf("comment must not hardcode MUL- prefix, got: %s", content)
	}

	// The comment must contain the safe issue mention. With no parent
	// assignee, none of the routing mentions should appear either.
	if !strings.Contains(content, "mention://issue/"+fx.child.ID) {
		t.Errorf("expected mention://issue/<child-id> link in comment, got: %s", content)
	}
	for _, banned := range []string{"mention://agent/", "mention://member/", "mention://squad/"} {
		if strings.Contains(content, banned) {
			t.Errorf("parent has no assignee but comment included %q mention, got: %s", banned, content)
		}
	}
}

// TestChildDoneNotificationIsIdempotent — re-saving an already-done child
// must NOT fire a second notification. UpdateIssue is called with the same
// status='done' twice; only the first call is a transition and should
// produce a comment.
func TestChildDoneNotificationIsIdempotent(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "done")
	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("after first done: expected 1 comment, got %d", got)
	}

	// Second save of done — should be a no-op transition.
	updateChildStatus(t, fx.child.ID, "done")
	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("after second done: expected still 1 comment (idempotent), got %d", got)
	}
}

// TestChildReopenAndDoneFiresAgain — done → in_progress → done IS a real
// new completion event and should produce a second notification. This
// captures the "reopen + done counts as a new event" line from MUL-2538.
func TestChildReopenAndDoneFiresAgain(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "done")
	updateChildStatus(t, fx.child.ID, "in_progress")
	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 2 {
		t.Fatalf("expected 2 system comments after reopen+done cycle, got %d", got)
	}
}

// TestChildDoneSkippedWhenParentDone — when the parent is already at a
// terminal status, there is nothing for the parent assignee to advance to,
// so the notification must NOT fire.
func TestChildDoneSkippedWhenParentDone(t *testing.T) {
	fx := newChildDoneFixture(t, "done")

	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Errorf("parent at 'done' should not receive notification, got %d comments", got)
	}
}

// TestChildDoneSkippedWhenParentCancelled — same as above for cancelled.
func TestChildDoneSkippedWhenParentCancelled(t *testing.T) {
	fx := newChildDoneFixture(t, "cancelled")

	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Errorf("parent at 'cancelled' should not receive notification, got %d comments", got)
	}
}

// TestChildDoneSkippedWhenParentBacklog — a parent deliberately parked in
// `backlog` must not be woken when a child completes. Waking it would
// re-activate the parent assignee, which can then promote sibling backlog
// sub-issues into todo — the surprise auto-activation reported in #4320 /
// MUL-3497. No system comment, no trigger, until the user explicitly moves
// the parent out of backlog.
func TestChildDoneSkippedWhenParentBacklog(t *testing.T) {
	fx := newChildDoneFixture(t, "backlog")

	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Errorf("parent at 'backlog' should not receive notification, got %d comments", got)
	}
}

// TestChildDoneSkippedWhenNoParent — an issue with no parent_issue_id must
// not produce any system comment on anything.
func TestChildDoneSkippedWhenNoParent(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "orphan child-done " + time.Now().Format(time.RFC3339Nano),
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create orphan: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var orphan IssueResponse
	json.NewDecoder(w.Body).Decode(&orphan)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, orphan.ID)
	})

	// Sanity baseline — there should be zero system comments anywhere in
	// the workspace attributable to this orphan transition. We can only
	// check that the orphan didn't somehow get one itself, but combined
	// with the no-parent code path returning early, that is sufficient.
	updateChildStatus(t, orphan.ID, "done")

	if got := countSystemCommentsOn(t, orphan.ID); got != 0 {
		t.Errorf("orphan must not receive a self-notification, got %d system comments", got)
	}
}

// setIssueAssigneeDirect bypasses UpdateIssue (and its assignment trigger
// side effects) by writing to the assignee columns directly. The child-done
// notification helper reads the parent row through GetIssue at fire time,
// so a direct UPDATE is enough to drive the dispatch under each assignee
// type without queuing a parallel agent task at setup.
func setIssueAssigneeDirect(t *testing.T, issueID, assigneeType, assigneeID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET assignee_type = $2, assignee_id = $3 WHERE id = $1`,
		issueID, assigneeType, assigneeID,
	); err != nil {
		t.Fatalf("set parent assignee: %v", err)
	}
}

func parentSystemCommentContent(t *testing.T, issueID string) string {
	t.Helper()
	if got := countSystemCommentsOn(t, issueID); got != 1 {
		t.Fatalf("expected exactly 1 system comment on parent, got %d", got)
	}
	content, _, _, _ := systemCommentOn(t, issueID)
	return content
}

func countPendingTasksForAgent(t *testing.T, issueID, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_task_queue
		   WHERE issue_id = $1 AND agent_id = $2
		     AND status IN ('queued', 'dispatched', 'running')`,
		issueID, agentID,
	).Scan(&n); err != nil {
		t.Fatalf("count pending tasks: %v", err)
	}
	return n
}

func countInboxItems(t *testing.T, recipientUserID, issueID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM inbox_item
		   WHERE recipient_id = $1 AND issue_id = $2`,
		recipientUserID, issueID,
	).Scan(&n); err != nil {
		t.Fatalf("count inbox items: %v", err)
	}
	return n
}

// TestChildDoneMentionsParentAssignee_Agent verifies the MUL-2538 Option C
// happy path for an agent parent assignee: the system comment carries a
// `mention://agent/<id>` link AND a real mention-style task is enqueued on
// the parent. The trigger fires through TaskService.EnqueueTaskForMention,
// so the dedupe + readiness checks match the @-mention path users already
// rely on.
func TestChildDoneMentionsParentAssignee_Agent(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	var agentID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("locate test agent: %v", err)
	}
	setIssueAssigneeDirect(t, fx.parent.ID, "agent", agentID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	wantMention := "mention://agent/" + agentID
	if !strings.Contains(content, wantMention) {
		t.Errorf("expected %q in system comment, got: %s", wantMention, content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, agentID); got != 1 {
		t.Errorf("expected 1 pending task for parent agent, got %d", got)
	}
}

// TestChildDoneSkippedWhenParentMember verifies the MUL-2538 follow-up: a
// human parent assignee should NOT receive the platform-generated system
// comment at all. Humans read their own timeline manually; the automated
// notification is pure noise and skipping it also removes the question of
// whether to mention/inbox-row the member.
//
// The assignee row uses `user_id` (NOT `member.id`) — that is the
// production invariant validated by validateAssigneePair for member
// assignees (see server/internal/handler/issue.go), so the fixture must
// match or it would be exercising a state that cannot occur for real.
func TestChildDoneSkippedWhenParentMember(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	var userID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT user_id FROM member WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&userID); err != nil {
		t.Fatalf("locate workspace member: %v", err)
	}
	setIssueAssigneeDirect(t, fx.parent.ID, "member", userID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM inbox_item WHERE issue_id = $1`, fx.parent.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Errorf("parent with member assignee should not receive a system comment, got %d", got)
	}
	if got := countInboxItems(t, userID, fx.parent.ID); got != 0 {
		t.Errorf("parent with member assignee should not receive an inbox row, got %d", got)
	}
}

// TestChildDoneMentionsParentAssignee_Squad verifies the squad branch: the
// system comment carries a `mention://squad/<id>` link and the squad
// leader receives a leader-role task. Reuses the squad fixture helper from
// squad_comment_trigger_test.go.
func TestChildDoneMentionsParentAssignee_Squad(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")
	sq := newSquadCommentTriggerFixture(t)

	setIssueAssigneeDirect(t, fx.parent.ID, "squad", sq.SquadID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	wantMention := "mention://squad/" + sq.SquadID
	if !strings.Contains(content, wantMention) {
		t.Errorf("expected %q in system comment, got: %s", wantMention, content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, sq.LeaderID); got != 1 {
		t.Errorf("expected 1 pending leader task for parent squad, got %d", got)
	}
}

// TestChildDoneTriggersParentAgentWhenSameAgentOwnsChild — when the parent
// agent assignee is the SAME agent that owns the just-finished child, the
// parent agent must still be triggered (MUL-2808). A child finishing and
// waking its parent is a serial sub-task handoff between two different
// issues, not a self-loop — and the lone-agent decomposition pattern (one
// agent owns both the parent and the sub-issues it created) has no other
// wake path. The comment is created AND exactly one task is enqueued on the
// parent; runaway re-triggering is bounded by the HasPendingTaskForIssueAndAgent
// dedup, not by suppressing the trigger.
func TestChildDoneTriggersParentAgentWhenSameAgentOwnsChild(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	var agentID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("locate test agent: %v", err)
	}
	// Both child and parent assigned to the same agent. Setting the child
	// assignee via direct SQL avoids the assignment-trigger side effect
	// that would otherwise queue an unrelated task on the child.
	setIssueAssigneeDirect(t, fx.parent.ID, "agent", agentID)
	setIssueAssigneeDirect(t, fx.child.ID, "agent", agentID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE issue_id IN ($1, $2)`,
			fx.parent.ID, fx.child.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	if !strings.Contains(content, "mention://agent/"+agentID) {
		t.Errorf("expected parent-assignee mention in system comment, got: %s", content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, agentID); got != 1 {
		t.Errorf("expected 1 pending task on parent (serial sub-task handoff), got %d", got)
	}
}

// TestChildDoneTriggersParentAgentWhenChildSquadSharesLeader — parent is
// assigned to agent A directly; the finished child is assigned to a squad
// whose leader is also agent A. Because the parent is an AGENT, dispatch
// routes through the agent path, which (post-MUL-2808) has no self-trigger
// guard: A coordinates the parent and must be woken to advance it when the
// child completes, regardless of who executed the child. The genuinely
// loop-prone case — BOTH sides squads sharing a leader — is still guarded on
// the squad path (see TestChildDoneSelfTriggerGuard_SquadParentDifferentSquadSameLeader).
func TestChildDoneTriggersParentAgentWhenChildSquadSharesLeader(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")
	sq := newSquadCommentTriggerFixture(t)

	// Parent agent == squad leader, child assigned to the squad.
	setIssueAssigneeDirect(t, fx.parent.ID, "agent", sq.LeaderID)
	setIssueAssigneeDirect(t, fx.child.ID, "squad", sq.SquadID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE issue_id IN ($1, $2)`,
			fx.parent.ID, fx.child.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	if !strings.Contains(content, "mention://agent/"+sq.LeaderID) {
		t.Errorf("expected parent-agent mention in system comment, got: %s", content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, sq.LeaderID); got != 1 {
		t.Errorf("expected 1 pending task on parent (serial sub-task handoff), got %d", got)
	}
}

// TestChildDoneSelfTriggerGuard_SquadParentDifferentSquadSameLeader — the
// cross-squad shared-leader loop. Parent is squad A, child is squad B,
// both squads have the same leader agent. The previous guard only blocked
// `parent.squad == child.squad`, so two distinct squads sharing a leader
// would still wake the same agent. effectiveChildAgentOwner reduces both
// sides to "leader agent" and blocks the enqueue.
func TestChildDoneSelfTriggerGuard_SquadParentDifferentSquadSameLeader(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")
	parentSquad := newSquadCommentTriggerFixture(t)

	// Spin up a SECOND squad that reuses the same leader as parentSquad.
	ctx := context.Background()
	var childSquadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Child Done Shared Leader Squad", parentSquad.LeaderID, testUserID).
		Scan(&childSquadID); err != nil {
		t.Fatalf("create second squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, childSquadID)
	})

	setIssueAssigneeDirect(t, fx.parent.ID, "squad", parentSquad.SquadID)
	setIssueAssigneeDirect(t, fx.child.ID, "squad", childSquadID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE issue_id IN ($1, $2)`,
			fx.parent.ID, fx.child.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	if !strings.Contains(content, "mention://squad/"+parentSquad.SquadID) {
		t.Errorf("expected parent-squad mention in system comment, got: %s", content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, parentSquad.LeaderID); got != 0 {
		t.Errorf("expected 0 pending leader tasks on parent (cross-squad shared-leader guard), got %d", got)
	}
}
