// CEREBRO-PATCH(child-status-notify): FIR-2601 — tests for the in_review/blocked
// parent notification + parent wake added in issue_child_status_cerebro.go.
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

// These tests cover the FIR-2601 extension (issue_child_status_cerebro.go):
// the same platform-owned parent notification + parent wake that fires on a
// child reaching `done` must ALSO fire when a child transitions into
// `in_review` or `blocked`. The fixtures and assertion helpers are shared with
// issue_child_done_test.go (same package).

// TestChildInReviewNotifiesParent — a child moving into `in_review` posts
// exactly one top-level system comment on an open, unassigned parent. The body
// references the child by its workspace identifier (never a hardcoded MUL-
// prefix), carries the safe issue mention, and — with no parent assignee —
// carries no routing mention.
func TestChildInReviewNotifiesParent(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "in_review")

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
	if !strings.Contains(content, fx.child.Identifier) {
		t.Errorf("expected comment to contain child identifier %q, got: %s", fx.child.Identifier, content)
	}
	if strings.Contains(content, "MUL-") {
		t.Errorf("comment must not hardcode MUL- prefix, got: %s", content)
	}
	if !strings.Contains(content, "in_review") {
		t.Errorf("expected in_review wording in comment, got: %s", content)
	}
	if !strings.Contains(content, "mention://issue/"+fx.child.ID) {
		t.Errorf("expected mention://issue/<child-id> link in comment, got: %s", content)
	}
	for _, banned := range []string{"mention://agent/", "mention://member/", "mention://squad/"} {
		if strings.Contains(content, banned) {
			t.Errorf("parent has no assignee but comment included %q mention, got: %s", banned, content)
		}
	}
}

func TestChildStatusNotificationSkippedWhenWorkspaceFlagOff(t *testing.T) {
	wireChildNotifyFlagQueries(t)
	clearChildNotifyFlag(t, testUserID, "cerebro_child_status_notify_parent")
	setChildNotifyFlag(t, "00000000-0000-0000-0000-000000000000", "cerebro_child_status_notify_parent", false, false)
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "in_review")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Fatalf("workspace flag off should suppress in_review parent notification, got %d comments", got)
	}
}

func TestChildStatusNotificationPersonalOverrideBeatsUnlockedWorkspaceOff(t *testing.T) {
	wireChildNotifyFlagQueries(t)
	setChildNotifyFlag(t, "00000000-0000-0000-0000-000000000000", "cerebro_child_status_notify_parent", false, false)
	setChildNotifyFlag(t, testUserID, "cerebro_child_status_notify_parent", true, false)
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "blocked")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("personal override on should allow blocked parent notification, got %d comments", got)
	}
}

// TestChildBlockedNotifiesParent — a child moving into `blocked` posts exactly
// one system comment on the parent whose body signals the blocked state so the
// parent driver knows to unblock or re-route.
func TestChildBlockedNotifiesParent(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "blocked")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("expected exactly 1 system comment on parent, got %d", got)
	}
	content, _, _, _ := systemCommentOn(t, fx.parent.ID)
	if !strings.Contains(content, "blocked") {
		t.Errorf("expected blocked wording in comment, got: %s", content)
	}
	if !strings.Contains(content, "mention://issue/"+fx.child.ID) {
		t.Errorf("expected mention://issue/<child-id> link in comment, got: %s", content)
	}
}

// TestChildInReviewWakesParentAgent — an agent-assigned parent gets the
// `mention://agent/<id>` link in the body AND exactly one mention-style task
// enqueued, identical to the done path.
func TestChildInReviewWakesParentAgent(t *testing.T) {
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

	updateChildStatus(t, fx.child.ID, "in_review")

	content := parentSystemCommentContent(t, fx.parent.ID)
	if !strings.Contains(content, "mention://agent/"+agentID) {
		t.Errorf("expected parent-agent mention in system comment, got: %s", content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, agentID); got != 1 {
		t.Errorf("expected 1 pending task for parent agent, got %d", got)
	}
}

// TestChildBlockedWakesParentSquadLeader — a squad-assigned parent gets the
// `mention://squad/<id>` link and the squad LEADER (not all members) receives
// one leader-role task.
func TestChildBlockedWakesParentSquadLeader(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")
	sq := newSquadCommentTriggerFixture(t)

	setIssueAssigneeDirect(t, fx.parent.ID, "squad", sq.SquadID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
	})

	updateChildStatus(t, fx.child.ID, "blocked")

	content := parentSystemCommentContent(t, fx.parent.ID)
	if !strings.Contains(content, "mention://squad/"+sq.SquadID) {
		t.Errorf("expected parent-squad mention in system comment, got: %s", content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, sq.LeaderID); got != 1 {
		t.Errorf("expected 1 pending leader task for parent squad, got %d", got)
	}
}

// TestChildStatusNotificationIsIdempotent — re-saving the same in_review status
// is not a transition and must not fire a second comment.
func TestChildStatusNotificationIsIdempotent(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "in_review")
	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("after first in_review: expected 1 comment, got %d", got)
	}
	updateChildStatus(t, fx.child.ID, "in_review")
	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("after second in_review: expected still 1 comment (idempotent), got %d", got)
	}
}

// TestChildStatusSkippedWhenParentTerminal — a parent already at done or
// cancelled has no follow-up to drive, so neither in_review nor blocked fires.
func TestChildStatusSkippedWhenParentTerminal(t *testing.T) {
	for _, parentStatus := range []string{"done", "cancelled"} {
		fx := newChildDoneFixture(t, parentStatus)
		updateChildStatus(t, fx.child.ID, "in_review")
		if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
			t.Errorf("parent at %q should not receive notification, got %d comments", parentStatus, got)
		}
	}
}

// TestChildStatusSkippedWhenParentMember — a human parent assignee reads its
// own timeline; no system comment and no inbox row, same product call as done.
func TestChildStatusSkippedWhenParentMember(t *testing.T) {
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

	updateChildStatus(t, fx.child.ID, "blocked")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Errorf("parent with member assignee should not receive a system comment, got %d", got)
	}
}

// TestChildInReviewThenDoneFiresBothPaths — in_review and done are two distinct
// handoff signals; a child moving in_progress → in_review → done produces one
// comment from the status helper (in_review) and one from the upstream done
// helper, never a double-fire of either.
func TestChildInReviewThenDoneFiresBothPaths(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "in_review")
	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 2 {
		t.Fatalf("expected 2 system comments (in_review + done), got %d", got)
	}
}

// TestChildStatusSkippedWhenNoParent — an orphan issue produces no system
// comment on a blocked transition (no parent to notify).
func TestChildStatusSkippedWhenNoParent(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "orphan child-status " + time.Now().Format(time.RFC3339Nano),
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

	updateChildStatus(t, orphan.ID, "blocked")

	if got := countSystemCommentsOn(t, orphan.ID); got != 0 {
		t.Errorf("orphan must not receive a self-notification, got %d system comments", got)
	}
}
