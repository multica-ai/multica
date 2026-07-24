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

func createChildDoneSibling(t *testing.T, parentID string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "child-done sibling " + time.Now().Format(time.RFC3339Nano),
		"status":          "in_progress",
		"parent_issue_id": parentID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create sibling: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var child IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&child); err != nil {
		t.Fatalf("decode sibling: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, child.ID)
	})
	return child
}

func createChildDoneLeaderOriginTask(t *testing.T, parentID, leaderID, squadID, originatorID string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, is_leader_task, squad_id,
			originator_user_id, accountable_user_id, originator_source
		)
		SELECT a.id, a.runtime_id, $2, 'completed', 0, true, $3, $4, $4, 'direct_human'
		FROM agent a
		WHERE a.id = $1
		RETURNING id
	`, leaderID, parentID, squadID, originatorID).Scan(&taskID); err != nil {
		t.Fatalf("create child-done leader origin task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

func createChildDoneOriginatorMember(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	email := "child-done-originator-" + time.Now().Format("20060102150405.000000000") + "@multica.test"
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('Child Done Originator', $1) RETURNING id
	`, email).Scan(&userID); err != nil {
		t.Fatalf("create child-done originator: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("add child-done originator to workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
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

// TestChildDoneWakesSquadThatCreatedChildForUnassignedParent covers GH #5706:
// an explicit @squad mention can start a leader run without assigning the
// parent issue. When that run creates staged work, the task row is the durable
// proof of which squad owns the orchestration handoff. Closing the child must
// route the parent-level stage instruction back to that same squad leader while
// leaving the parent itself unassigned.
func TestChildDoneWakesSquadThatCreatedChildForUnassignedParent(t *testing.T) {
	ctx := context.Background()
	fx := newChildDoneFixture(t, "in_progress")
	sq := newSquadCommentTriggerFixture(t)

	// origin_id is the authoritative link to the task that created the child.
	// Deliberately put the task outside the child's timestamp window: exact
	// provenance must keep working despite clock skew or timestamp correction.
	var originTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, is_leader_task, squad_id,
			created_at, started_at, completed_at
		)
		SELECT
			a.id, a.runtime_id, $2, 'completed', 0, true, $3,
			now() - interval '4 minutes',
			now() - interval '4 minutes',
			now() - interval '3 minutes'
		FROM agent a
		WHERE a.id = $1
		RETURNING id
	`, sq.LeaderID, fx.parent.ID, sq.SquadID).Scan(&originTaskID); err != nil {
		t.Fatalf("create originating squad leader task: %v", err)
	}

	// The same leader may lead multiple squads. A newer task for another squad
	// on the same parent must not override the child's exact origin task.
	var otherSquadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Other Child Origin Squad", sq.LeaderID, testUserID).Scan(&otherSquadID); err != nil {
		t.Fatalf("create other squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, otherSquadID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, is_leader_task, squad_id,
			created_at, started_at, completed_at
		)
		SELECT
			a.id, a.runtime_id, $2, 'completed', 0, true, $3,
			now() - interval '90 seconds',
			now() - interval '90 seconds',
			now() - interval '30 seconds'
		FROM agent a
		WHERE a.id = $1
	`, sq.LeaderID, fx.parent.ID, otherSquadID); err != nil {
		t.Fatalf("create unrelated newer squad leader task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE issue
		SET creator_type = 'agent', creator_id = $2,
		    created_at = now() - interval '1 minute', stage = 1,
		    origin_type = 'agent_create', origin_id = $3
		WHERE id = $1
	`, fx.child.ID, sq.LeaderID, originTaskID); err != nil {
		t.Fatalf("stamp child provenance: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	if !strings.Contains(content, "mention://squad/"+sq.SquadID) {
		t.Errorf("expected originating squad mention in system comment, got: %s", content)
	}
	if strings.Contains(content, "mention://squad/"+otherSquadID) {
		t.Errorf("newer unrelated squad must not be mentioned, got: %s", content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, sq.LeaderID); got != 1 {
		t.Errorf("expected 1 pending leader task for unassigned parent, got %d", got)
	}

	var assigneeType, assigneeID *string
	if err := testPool.QueryRow(ctx,
		`SELECT assignee_type, assignee_id::text FROM issue WHERE id = $1`,
		fx.parent.ID,
	).Scan(&assigneeType, &assigneeID); err != nil {
		t.Fatalf("load parent assignee: %v", err)
	}
	if assigneeType != nil || assigneeID != nil {
		t.Fatalf("parent should remain unassigned, got type=%v id=%v", assigneeType, assigneeID)
	}
}

// TestChildDoneSquadContinuationInheritsOriginTaskAttribution proves that the
// mention-started coordinator's human authority follows the stage handoff. The
// parent creator is deliberately a different member: falling through the
// system comment to parent provenance would attach the wrong user's connected
// apps and A2A authority to the continuation task.
func TestChildDoneSquadContinuationInheritsOriginTaskAttribution(t *testing.T) {
	ctx := context.Background()
	fx := newChildDoneFixture(t, "in_progress")
	sq := newSquadCommentTriggerFixture(t)
	mentionerID := createChildDoneOriginatorMember(t)

	originTaskID := createChildDoneLeaderOriginTask(t, fx.parent.ID, sq.LeaderID, sq.SquadID, mentionerID)
	if _, err := testPool.Exec(ctx, `
		UPDATE issue
		SET creator_type = 'agent', creator_id = $2, stage = 1,
		    origin_type = 'agent_create', origin_id = $3
		WHERE id = $1
	`, fx.child.ID, sq.LeaderID, originTaskID); err != nil {
		t.Fatalf("stamp child provenance: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	var originatorID, accountableID, source, delegatedFrom string
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(originator_user_id::text, ''), COALESCE(accountable_user_id::text, ''),
		       COALESCE(originator_source, ''), COALESCE(delegated_from_task_id::text, '')
		FROM agent_task_queue
		WHERE issue_id = $1 AND id <> $2
		  AND status IN ('queued', 'dispatched', 'running')
		ORDER BY created_at DESC
		LIMIT 1
	`, fx.parent.ID, originTaskID).Scan(&originatorID, &accountableID, &source, &delegatedFrom); err != nil {
		t.Fatalf("load continuation task attribution: %v", err)
	}
	if originatorID != mentionerID || accountableID != mentionerID {
		t.Errorf("continuation human attribution = originator %q accountable %q, want mentioner %q", originatorID, accountableID, mentionerID)
	}
	if source != "delegation" || delegatedFrom != originTaskID {
		t.Errorf("continuation lineage = source %q delegated_from %q, want delegation from %q", source, delegatedFrom, originTaskID)
	}
}

// TestChildDoneDoesNotContinueAfterOriginatorPermissionRevoked proves that a
// durable origin task preserves provenance, not permanent authority. Permission
// changes made while the stage is running must take effect before the next
// leader task is created.
func TestChildDoneDoesNotContinueAfterOriginatorPermissionRevoked(t *testing.T) {
	ctx := context.Background()
	fx := newChildDoneFixture(t, "in_progress")
	sq := newSquadCommentTriggerFixture(t)
	originatorID := createChildDoneOriginatorMember(t)
	originTaskID := createChildDoneLeaderOriginTask(t, fx.parent.ID, sq.LeaderID, sq.SquadID, originatorID)

	if _, err := testPool.Exec(ctx, `
		UPDATE issue
		SET creator_type = 'agent', creator_id = $2, stage = 1,
		    origin_type = 'agent_create', origin_id = $3
		WHERE id = $1
	`, fx.child.ID, sq.LeaderID, originTaskID); err != nil {
		t.Fatalf("stamp child provenance: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent SET permission_mode = 'private' WHERE id = $1`, sq.LeaderID); err != nil {
		t.Fatalf("revoke leader invocation permission: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE agent SET permission_mode = 'public_to' WHERE id = $1`, sq.LeaderID)
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Errorf("closed stage emitted %d comments, want 1", got)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, sq.LeaderID); got != 0 {
		t.Errorf("revoked originator queued %d continuation tasks, want 0", got)
	}
}

// TestChildDoneDoesNotContinueToUnauthorizedReplacementLeader covers leader
// rotation during a running stage. The original human may continue through a
// replacement only when they can currently invoke that replacement agent.
func TestChildDoneDoesNotContinueToUnauthorizedReplacementLeader(t *testing.T) {
	ctx := context.Background()
	fx := newChildDoneFixture(t, "in_progress")
	sq := newSquadCommentTriggerFixture(t)
	originatorID := createChildDoneOriginatorMember(t)
	originTaskID := createChildDoneLeaderOriginTask(t, fx.parent.ID, sq.LeaderID, sq.SquadID, originatorID)
	replacementLeaderID := createHandlerTestAgent(t, "Child Done Private Replacement", nil)

	if _, err := testPool.Exec(ctx, `UPDATE agent SET permission_mode = 'private' WHERE id = $1`, replacementLeaderID); err != nil {
		t.Fatalf("make replacement leader private: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE squad SET leader_id = $2 WHERE id = $1`, sq.SquadID, replacementLeaderID); err != nil {
		t.Fatalf("rotate squad leader: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE issue
		SET creator_type = 'agent', creator_id = $2, stage = 1,
		    origin_type = 'agent_create', origin_id = $3
		WHERE id = $1
	`, fx.child.ID, sq.LeaderID, originTaskID); err != nil {
		t.Fatalf("stamp child provenance: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
		testPool.Exec(context.Background(), `UPDATE squad SET leader_id = $2 WHERE id = $1`, sq.SquadID, sq.LeaderID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Errorf("closed stage emitted %d comments, want 1", got)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, replacementLeaderID); got != 0 {
		t.Errorf("unauthorized replacement leader received %d continuation tasks, want 0", got)
	}
}

// TestChildDoneContinuesToAuthorizedReplacementLeader keeps leader rotation a
// supported operation. Rotation alone does not invalidate the origin task when
// the original human can invoke the replacement leader.
func TestChildDoneContinuesToAuthorizedReplacementLeader(t *testing.T) {
	ctx := context.Background()
	fx := newChildDoneFixture(t, "in_progress")
	sq := newSquadCommentTriggerFixture(t)
	originatorID := createChildDoneOriginatorMember(t)
	originTaskID := createChildDoneLeaderOriginTask(t, fx.parent.ID, sq.LeaderID, sq.SquadID, originatorID)
	replacementLeaderID := createHandlerTestAgent(t, "Child Done Public Replacement", nil)

	if _, err := testPool.Exec(ctx, `UPDATE squad SET leader_id = $2 WHERE id = $1`, sq.SquadID, replacementLeaderID); err != nil {
		t.Fatalf("rotate squad leader: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE issue
		SET creator_type = 'agent', creator_id = $2, stage = 1,
		    origin_type = 'agent_create', origin_id = $3
		WHERE id = $1
	`, fx.child.ID, sq.LeaderID, originTaskID); err != nil {
		t.Fatalf("stamp child provenance: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
		testPool.Exec(context.Background(), `UPDATE squad SET leader_id = $2 WHERE id = $1`, sq.SquadID, sq.LeaderID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	if got := countPendingTasksForAgent(t, fx.parent.ID, replacementLeaderID); got != 1 {
		t.Errorf("authorized replacement leader received %d continuation tasks, want 1", got)
	}
}

// TestBatchChildDoneDoesNotChooseAmongDifferentOriginTasks covers the batch
// ordering boundary. Two children in the same closing stage came from distinct
// squad leader tasks. Choosing the request's first representative would make
// issue_ids order decide which squad runs; ambiguous provenance must fail
// closed while still recording the stage-complete system comment.
func TestBatchChildDoneDoesNotChooseAmongDifferentOriginTasks(t *testing.T) {
	ctx := context.Background()
	fx := newChildDoneFixture(t, "in_progress")
	otherChild := createChildDoneSibling(t, fx.parent.ID)
	sqA := newSquadCommentTriggerFixture(t)
	leaderB := createHandlerTestAgent(t, "Child Done Batch Leader B", nil)

	var squadBID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Child Done Batch Squad B', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, leaderB, testUserID).Scan(&squadBID); err != nil {
		t.Fatalf("create second squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadBID)
	})

	originA := createChildDoneLeaderOriginTask(t, fx.parent.ID, sqA.LeaderID, sqA.SquadID, testUserID)
	originB := createChildDoneLeaderOriginTask(t, fx.parent.ID, leaderB, squadBID, testUserID)
	if _, err := testPool.Exec(ctx, `
		UPDATE issue
		SET creator_type = 'agent', creator_id = CASE id WHEN $1 THEN $3::uuid ELSE $4::uuid END,
		    stage = 1, origin_type = 'agent_create',
		    origin_id = CASE id WHEN $1 THEN $5::uuid ELSE $6::uuid END
		WHERE id IN ($1, $2)
	`, fx.child.ID, otherChild.ID, sqA.LeaderID, leaderB, originA, originB); err != nil {
		t.Fatalf("stamp ambiguous child provenance: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
	})

	batchSetStatus(t, []string{fx.child.ID, otherChild.ID}, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	for _, squadID := range []string{sqA.SquadID, squadBID} {
		if strings.Contains(content, "mention://squad/"+squadID) {
			t.Errorf("ambiguous batch must not mention squad %s, got: %s", squadID, content)
		}
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, sqA.LeaderID); got != 0 {
		t.Errorf("ambiguous batch queued %d tasks for first squad leader, want 0", got)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, leaderB); got != 0 {
		t.Errorf("ambiguous batch queued %d tasks for second squad leader, want 0", got)
	}
}

// TestBatchChildDoneWakesSquadForOneSharedOriginTask keeps the positive batch
// boundary explicit: a leader task commonly creates several same-stage
// children, and closing them together must still produce one coordinator wake.
func TestBatchChildDoneWakesSquadForOneSharedOriginTask(t *testing.T) {
	ctx := context.Background()
	fx := newChildDoneFixture(t, "in_progress")
	otherChild := createChildDoneSibling(t, fx.parent.ID)
	sq := newSquadCommentTriggerFixture(t)
	originTaskID := createChildDoneLeaderOriginTask(t, fx.parent.ID, sq.LeaderID, sq.SquadID, testUserID)

	if _, err := testPool.Exec(ctx, `
		UPDATE issue
		SET creator_type = 'agent', creator_id = $3, stage = 1,
		    origin_type = 'agent_create', origin_id = $4
		WHERE id IN ($1, $2)
	`, fx.child.ID, otherChild.ID, sq.LeaderID, originTaskID); err != nil {
		t.Fatalf("stamp shared child provenance: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
	})

	// Reverse creation order to prove routing is not coupled to the presentation
	// representative selected from issue_ids.
	batchSetStatus(t, []string{otherChild.ID, fx.child.ID}, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	if !strings.Contains(content, "mention://squad/"+sq.SquadID) {
		t.Errorf("expected shared originating squad mention, got: %s", content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, sq.LeaderID); got != 1 {
		t.Errorf("expected one shared-origin leader task, got %d", got)
	}
}

// TestChildDoneDoesNotRouteMixedOriginStageSequentially covers the single-
// update boundary: the last child to finish closes the stage, but routing must
// consider every child in that stage, including siblings that were already
// terminal. Otherwise completion order decides which squad receives the wake.
func TestChildDoneDoesNotRouteMixedOriginStageSequentially(t *testing.T) {
	ctx := context.Background()
	fx := newChildDoneFixture(t, "in_progress")
	otherChild := createChildDoneSibling(t, fx.parent.ID)
	sqA := newSquadCommentTriggerFixture(t)
	leaderB := createHandlerTestAgent(t, "Child Done Sequential Leader B", nil)

	var squadBID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Child Done Sequential Squad B', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, leaderB, testUserID).Scan(&squadBID); err != nil {
		t.Fatalf("create second squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadBID)
	})

	originA := createChildDoneLeaderOriginTask(t, fx.parent.ID, sqA.LeaderID, sqA.SquadID, testUserID)
	originB := createChildDoneLeaderOriginTask(t, fx.parent.ID, leaderB, squadBID, testUserID)
	if _, err := testPool.Exec(ctx, `
		UPDATE issue
		SET creator_type = 'agent', creator_id = CASE id WHEN $1 THEN $3::uuid ELSE $4::uuid END,
		    stage = 1, origin_type = 'agent_create',
		    origin_id = CASE id WHEN $1 THEN $5::uuid ELSE $6::uuid END
		WHERE id IN ($1, $2)
	`, fx.child.ID, otherChild.ID, sqA.LeaderID, leaderB, originA, originB); err != nil {
		t.Fatalf("stamp sequential mixed provenance: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")
	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Fatalf("open stage emitted %d comments, want 0", got)
	}
	updateChildStatus(t, otherChild.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	for _, squadID := range []string{sqA.SquadID, squadBID} {
		if strings.Contains(content, "mention://squad/"+squadID) {
			t.Errorf("mixed-origin stage must not mention squad %s, got: %s", squadID, content)
		}
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, sqA.LeaderID); got != 0 {
		t.Errorf("mixed-origin stage queued %d tasks for first squad leader, want 0", got)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, leaderB); got != 0 {
		t.Errorf("mixed-origin stage queued %d tasks for last-finishing squad leader, want 0", got)
	}
}

// TestBatchChildDoneIncludesAlreadyTerminalSiblingInOriginValidation covers a
// batch that closes a stage after one sibling finished earlier. The final
// barrier snapshot is mixed-origin even though every child in this batch shares
// one origin, so the handoff must fail closed.
func TestBatchChildDoneIncludesAlreadyTerminalSiblingInOriginValidation(t *testing.T) {
	ctx := context.Background()
	fx := newChildDoneFixture(t, "in_progress")
	batchChildA := createChildDoneSibling(t, fx.parent.ID)
	batchChildB := createChildDoneSibling(t, fx.parent.ID)
	sqA := newSquadCommentTriggerFixture(t)
	leaderB := createHandlerTestAgent(t, "Child Done Existing Terminal Leader B", nil)

	var squadBID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Child Done Existing Terminal Squad B', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, leaderB, testUserID).Scan(&squadBID); err != nil {
		t.Fatalf("create second squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadBID)
	})

	originA := createChildDoneLeaderOriginTask(t, fx.parent.ID, sqA.LeaderID, sqA.SquadID, testUserID)
	originB := createChildDoneLeaderOriginTask(t, fx.parent.ID, leaderB, squadBID, testUserID)
	if _, err := testPool.Exec(ctx, `
		UPDATE issue
		SET creator_type = 'agent', creator_id = $2, stage = 1,
		    origin_type = 'agent_create', origin_id = $3
		WHERE id = $1;
	`, fx.child.ID, sqA.LeaderID, originA); err != nil {
		t.Fatalf("stamp existing terminal provenance: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE issue
		SET creator_type = 'agent', creator_id = $3, stage = 1,
		    origin_type = 'agent_create', origin_id = $4
		WHERE id IN ($1, $2)
	`, batchChildA.ID, batchChildB.ID, leaderB, originB); err != nil {
		t.Fatalf("stamp batch provenance: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")
	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Fatalf("open stage emitted %d comments, want 0", got)
	}
	batchSetStatus(t, []string{batchChildA.ID, batchChildB.ID}, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	for _, squadID := range []string{sqA.SquadID, squadBID} {
		if strings.Contains(content, "mention://squad/"+squadID) {
			t.Errorf("mixed-origin stage must not mention squad %s, got: %s", squadID, content)
		}
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, sqA.LeaderID); got != 0 {
		t.Errorf("mixed-origin stage queued %d tasks for existing sibling squad leader, want 0", got)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, leaderB); got != 0 {
		t.Errorf("mixed-origin stage queued %d tasks for batch squad leader, want 0", got)
	}
}

// TestChildDoneDoesNotWakeArchivedOriginSquad covers a squad archived after it
// created the staged child. The durable origin remains useful for attribution,
// but an archived squad is no longer a valid routing target.
func TestChildDoneDoesNotWakeArchivedOriginSquad(t *testing.T) {
	ctx := context.Background()
	fx := newChildDoneFixture(t, "in_progress")
	sq := newSquadCommentTriggerFixture(t)
	originTaskID := createChildDoneLeaderOriginTask(t, fx.parent.ID, sq.LeaderID, sq.SquadID, testUserID)

	if _, err := testPool.Exec(ctx, `
		UPDATE issue
		SET creator_type = 'agent', creator_id = $2, stage = 1,
		    origin_type = 'agent_create', origin_id = $3
		WHERE id = $1
	`, fx.child.ID, sq.LeaderID, originTaskID); err != nil {
		t.Fatalf("stamp child provenance: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE squad SET archived_at = now() WHERE id = $1`, sq.SquadID); err != nil {
		t.Fatalf("archive origin squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Errorf("closed stage emitted %d comments, want 1", got)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, sq.LeaderID); got != 0 {
		t.Errorf("archived origin squad queued %d continuation tasks, want 0", got)
	}
}

// TestChildDoneDoesNotInferSquadWhenOriginTaskIsNotLeader proves that an
// unrelated leader run cannot become orchestration authority merely because
// its timestamp window contains child creation. The exact origin task is a
// generic agent task, so the unassigned parent must remain unwoken.
func TestChildDoneDoesNotInferSquadWhenOriginTaskIsNotLeader(t *testing.T) {
	ctx := context.Background()
	fx := newChildDoneFixture(t, "in_progress")
	sq := newSquadCommentTriggerFixture(t)

	var originTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			created_at, started_at, completed_at
		)
		SELECT
			a.id, a.runtime_id, $2, 'completed', 0,
			now() - interval '3 minutes',
			now() - interval '3 minutes',
			now() - interval '2 minutes'
		FROM agent a
		WHERE a.id = $1
		RETURNING id
	`, sq.LeaderID, fx.parent.ID).Scan(&originTaskID); err != nil {
		t.Fatalf("create generic origin task: %v", err)
	}

	// This leader task is deliberately a tempting but unrelated candidate for
	// the old timestamp inference: it brackets the child's creation time.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, is_leader_task, squad_id,
			created_at, started_at, completed_at
		)
		SELECT
			a.id, a.runtime_id, $2, 'completed', 0, true, $3,
			now() - interval '90 seconds',
			now() - interval '90 seconds',
			now() - interval '30 seconds'
		FROM agent a
		WHERE a.id = $1
	`, sq.LeaderID, fx.parent.ID, sq.SquadID); err != nil {
		t.Fatalf("create unrelated squad leader task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE issue
		SET creator_type = 'agent', creator_id = $2,
		    created_at = now() - interval '1 minute', stage = 1,
		    origin_type = 'agent_create', origin_id = $3
		WHERE id = $1
	`, fx.child.ID, sq.LeaderID, originTaskID); err != nil {
		t.Fatalf("stamp child provenance: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	if strings.Contains(content, "mention://squad/"+sq.SquadID) {
		t.Errorf("unrelated squad must not be mentioned, got: %s", content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, sq.LeaderID); got != 0 {
		t.Errorf("unrelated squad leader received %d pending tasks, want 0", got)
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
// child completes, regardless of who executed the child. The squad path now
// behaves identically: MUL-3969 removed its old same-squad / shared-leader
// guards, so BOTH sides being squads that share a leader also wakes the leader
// (see TestChildDoneWakesLeaderWhenParentAndChildSquadsShareLeader).
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

// TestChildDoneWakesLeaderWhenParentAndChildSquadsShareLeader — cross-squad
// shared-leader case. Parent is squad A, child is squad B, both squads have
// the same leader agent. The squad path used to suppress the leader wake here
// (effectiveChildAgentOwner reduced both sides to the shared leader), but that
// guard was removed in MUL-3969: waking the leader on the PARENT is a serial
// sub-task handoff across two DIFFERENT issues, not a self-loop, and it is the
// only signal that carries the parent-level stage-barrier instruction. The
// leader must now be woken exactly once; runaway re-triggering is bounded by
// the HasPendingTaskForIssueAndAgent idempotency check.
func TestChildDoneWakesLeaderWhenParentAndChildSquadsShareLeader(t *testing.T) {
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
	if got := countPendingTasksForAgent(t, fx.parent.ID, parentSquad.LeaderID); got != 1 {
		t.Errorf("expected 1 pending leader task on parent (shared-leader guard removed, MUL-3969), got %d", got)
	}
}

// TestChildDoneWakesLeaderWhenChildIsSameSquad — the MUL-3969 repro. Parent
// and the just-finished child are BOTH assigned to the same squad (the common
// "a squad decomposes its parent into sub-issues it works itself" pattern).
// The old same-squad guard suppressed the leader wake, so the stage-barrier
// system comment landed on the parent but the "wrap up / advance" instruction
// was never delivered to the leader and the parent silently stalled in
// in_progress. The leader must now be woken exactly once.
func TestChildDoneWakesLeaderWhenChildIsSameSquad(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")
	sq := newSquadCommentTriggerFixture(t)

	setIssueAssigneeDirect(t, fx.parent.ID, "squad", sq.SquadID)
	setIssueAssigneeDirect(t, fx.child.ID, "squad", sq.SquadID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE issue_id IN ($1, $2)`,
			fx.parent.ID, fx.child.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	if !strings.Contains(content, "mention://squad/"+sq.SquadID) {
		t.Errorf("expected parent-squad mention in system comment, got: %s", content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, sq.LeaderID); got != 1 {
		t.Errorf("expected 1 pending leader task for same-squad child (MUL-3969), got %d", got)
	}
}

// TestStageLeaderPrepareTimeoutRetryCanAdvanceNextStage covers the full server
// half of MUL-4923's recovery chain: a stage barrier wakes the squad leader,
// the pre-start attempt fails with the daemon's timeout reason, the atomic
// retry preserves leader/squad/trigger provenance, and that retry can promote
// the parked next stage as the leader actor.
func TestStageLeaderPrepareTimeoutRetryCanAdvanceNextStage(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newChildDoneFixture(t, "in_progress")
	sq := newSquadCommentTriggerFixture(t)
	setIssueAssigneeDirect(t, fx.parent.ID, "squad", sq.SquadID)
	setIssueAssigneeDirect(t, fx.child.ID, "squad", sq.SquadID)
	if _, err := testPool.Exec(ctx, `UPDATE issue SET stage = 1 WHERE id = $1`, fx.child.ID); err != nil {
		t.Fatalf("set stage 1: %v", err)
	}

	// Stage 2 exists but is deliberately parked. The server wakes the leader at
	// the Stage 1 barrier; only the leader decides to promote this child.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "stage 2 after prepare timeout",
		"status":          "backlog",
		"parent_issue_id": fx.parent.ID,
		"stage":           2,
		"assignee_type":   "squad",
		"assignee_id":     sq.SquadID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create stage 2: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var stage2 IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&stage2); err != nil {
		t.Fatalf("decode stage 2: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id IN ($1, $2)`, fx.parent.ID, stage2.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, stage2.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")
	content := parentSystemCommentContent(t, fx.parent.ID)
	if !strings.Contains(content, "Stage 2 is next") {
		t.Fatalf("stage barrier comment does not identify Stage 2: %s", content)
	}

	var originalID, originalSquadID, originalTriggerID string
	var originalLeader bool
	if err := testPool.QueryRow(ctx, `
		SELECT id::text, is_leader_task, squad_id::text, trigger_comment_id::text
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
		ORDER BY created_at DESC
		LIMIT 1
	`, fx.parent.ID, sq.LeaderID).Scan(&originalID, &originalLeader, &originalSquadID, &originalTriggerID); err != nil {
		t.Fatalf("load Stage 1 leader wake: %v", err)
	}
	if !originalLeader || originalSquadID != sq.SquadID || originalTriggerID == "" {
		t.Fatalf("leader wake provenance = leader:%v squad:%q trigger:%q", originalLeader, originalSquadID, originalTriggerID)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'dispatched', dispatched_at = now()
		WHERE id = $1
	`, originalID); err != nil {
		t.Fatalf("dispatch original leader task: %v", err)
	}

	if _, err := testHandler.TaskService.FailTask(ctx, parseUUID(originalID), "task preparation timed out after 5m0s", "", "", "timeout"); err != nil {
		t.Fatalf("fail original leader task: %v", err)
	}

	var retryID, retryStatus, retrySquadID, retryTriggerID string
	var retryLeader bool
	var retryAttempt int32
	if err := testPool.QueryRow(ctx, `
		SELECT id::text, status, is_leader_task, squad_id::text,
		       trigger_comment_id::text, attempt
		FROM agent_task_queue
		WHERE parent_task_id = $1
	`, originalID).Scan(&retryID, &retryStatus, &retryLeader, &retrySquadID, &retryTriggerID, &retryAttempt); err != nil {
		t.Fatalf("load automatic retry: %v", err)
	}
	if retryStatus != "queued" || retryAttempt != 2 || !retryLeader || retrySquadID != originalSquadID || retryTriggerID != originalTriggerID {
		t.Fatalf("retry provenance = status:%q attempt:%d leader:%v squad:%q trigger:%q; want queued attempt 2 with original leader context",
			retryStatus, retryAttempt, retryLeader, retrySquadID, retryTriggerID)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'running', dispatched_at = now(), started_at = now()
		WHERE id = $1
	`, retryID); err != nil {
		t.Fatalf("start retry leader task: %v", err)
	}

	// Act through the normal issue handler with the retry task as the agent
	// identity. This is the operation the Stage handoff prompt asks the leader
	// to perform, and proves the retry was not demoted to a generic worker.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+stage2.ID, map[string]any{"status": "todo"})
	req.Header.Set("X-Agent-ID", sq.LeaderID)
	req.Header.Set("X-Task-ID", retryID)
	req = withURLParam(req, "id", stage2.ID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("retry leader promote Stage 2: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var stage2Status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, stage2.ID).Scan(&stage2Status); err != nil {
		t.Fatalf("load promoted Stage 2: %v", err)
	}
	if stage2Status != "todo" {
		t.Fatalf("Stage 2 status = %q, want todo", stage2Status)
	}
	if got := countPendingTasksForAgent(t, stage2.ID, sq.LeaderID); got != 1 {
		t.Fatalf("promoted Stage 2 queued %d leader tasks, want 1", got)
	}
}
