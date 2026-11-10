package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func blockerRule(t *testing.T, f principalFixture, issue pgtype.UUID) db.IssueWakeup {
	t.Helper()
	w, err := f.q.GetSystemWakeup(context.Background(), db.GetSystemWakeupParams{IssueID: issue, SystemRule: systemRuleText(SystemRuleBlockersDone)})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestIssueBlockerGateAndWake(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	ctx := context.Background()
	f.Exec(t, "UPDATE issue SET assignee_type='agent',assignee_id=$2,status='todo' WHERE id=$1", issue, agent)
	a, b := f.Issue(t, "first blocker"), f.Issue(t, "second blocker")
	for _, id := range []string{a, b} {
		if err := AddIssueBlocker(ctx, f.q, parseTestUUID(t, f.WorkspaceID), issue, parseTestUUID(t, id), "member", parseTestUUID(t, f.UserID)); err != nil {
			t.Fatal(err)
		}
	}
	f.Cleanup(t, "DELETE FROM issue_blocker WHERE issue_id=$1", issue)
	if err := guardIssueNotInTriage(ctx, f.q, issue, OriginDerived); !errors.Is(err, ErrIssueHasBlockers) {
		t.Fatalf("derived enqueue = %v", err)
	}
	if err := guardIssueNotInTriage(ctx, f.q, issue, OriginNamed); err != nil {
		t.Fatalf("named enqueue = %v", err)
	}
	dispatch := func() {
		t.Helper()
		if err := s.TickWorkspaces(ctx, parseTestUUID(t, f.WorkspaceID)); err != nil {
			t.Fatal(err)
		}
	}
	w := blockerRule(t, f, issue)
	f.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", a)
	dispatch()
	if n := wakeRuns(t, f, w.ID); n != 0 {
		t.Fatalf("first blocker queued %d runs", n)
	}
	f.Exec(t, "UPDATE issue SET status='cancelled' WHERE id=$1", b)
	dispatch()
	dispatch()
	if n := wakeRuns(t, f, w.ID); n != 1 {
		t.Fatalf("all blockers queued %d runs", n)
	}
	got := blockerRule(t, f, issue)
	task, err := f.q.GetAgentTask(ctx, got.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(task.HandoffNote.String, "cancelled") || !strings.Contains(task.HandoffNote.String, "first blocker") {
		t.Fatalf("missing blocker facts: %s", task.HandoffNote.String)
	}
	current, err := f.q.GetIssue(ctx, issue)
	if err != nil || current.Status != "todo" {
		t.Fatalf("platform changed status: %+v %v", current, err)
	}
	f.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE issue_id=$1", issue)
	// A reopen/close between scheduler passes must rearm the rule.
	f.Exec(t, "UPDATE issue SET status='todo' WHERE id=$1", a)
	f.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", a)
	dispatch()
	if n := wakeRuns(t, f, w.ID); n != 2 {
		t.Fatalf("reopened blocker left %d runs", n)
	}
}

func TestIssueBlockerBacklogAndMember(t *testing.T) {
	f, s, issue, _ := wakeFixture(t)
	ctx := context.Background()
	blocker := f.Issue(t, "member prerequisite")
	f.Exec(t, "UPDATE issue SET status='backlog',assignee_type='member',assignee_id=$2 WHERE id=$1", issue, f.UserID)
	if err := AddIssueBlocker(ctx, f.q, parseTestUUID(t, f.WorkspaceID), issue, parseTestUUID(t, blocker), "member", parseTestUUID(t, f.UserID)); err != nil {
		t.Fatal(err)
	}
	f.Cleanup(t, "DELETE FROM issue_blocker WHERE issue_id=$1", issue)
	f.Cleanup(t, "DELETE FROM inbox_item WHERE issue_id=$1", issue)
	f.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", blocker)
	dispatch := func() {
		t.Helper()
		if err := s.TickWorkspaces(ctx, parseTestUUID(t, f.WorkspaceID)); err != nil {
			t.Fatal(err)
		}
	}
	dispatch()
	if n := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1 AND type='blockers_done'", issue); n != 0 {
		t.Fatalf("backlog notified %d times", n)
	}
	f.Exec(t, "UPDATE issue SET status='blocked' WHERE id=$1", issue)
	dispatch()
	dispatch()
	if n := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1 AND type='blockers_done'", issue); n != 1 {
		t.Fatalf("member notified %d times", n)
	}
}

func TestIssueStageBlockersAreLiveAndRejectCycles(t *testing.T) {
	f, s, parent, agent := wakeFixture(t)
	ctx := context.Background()
	a := f.Issue(t, "stage one", testutil.Cols{"parent_issue_id": parent, "stage": 1})
	b := f.Issue(t, "stage three", testutil.Cols{"parent_issue_id": parent, "stage": 3, "assignee_type": "agent", "assignee_id": agent})
	issue := parseTestUUID(t, b)
	f.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN(SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
	f.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id=$1", issue)
	f.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	if err := guardIssueNotInTriage(ctx, f.q, issue, OriginDerived); !errors.Is(err, ErrIssueHasBlockers) {
		t.Fatalf("stage gate = %v", err)
	}
	err := AddIssueBlocker(ctx, f.q, parseTestUUID(t, f.WorkspaceID), parseTestUUID(t, a), issue, "member", parseTestUUID(t, f.UserID))
	if err == nil {
		t.Fatal("manual edge formed a cycle through a stage")
	}
	// Moving the sibling away removes its implied edge without any sync writes.
	f.Exec(t, "UPDATE issue SET parent_issue_id=NULL,stage=NULL WHERE id=$1", a)
	if err := s.TickWorkspaces(ctx, parseTestUUID(t, f.WorkspaceID)); err != nil {
		t.Fatal(err)
	}
	if n := wakeRuns(t, f, blockerRule(t, f, issue).ID); n != 1 {
		t.Fatalf("detaching predecessor queued %d runs", n)
	}
	if err := guardIssueNotInTriage(ctx, f.q, issue, OriginDerived); err != nil {
		t.Fatal(err)
	}
}

func TestStageProgressAutomaticStagesSkipParentCheckpoint(t *testing.T) {
	children := []subIssue{{Stage: pgtype.Int4{Int32: 1, Valid: true}, Closed: true}, {Stage: pgtype.Int4{Int32: 2, Valid: true}}}
	if met, _, _ := stageProgress(children); met {
		t.Fatal("todo stage woke the parent")
	}
	children[1].Backlog = true
	if met, _, _ := stageProgress(children); !met {
		t.Fatal("backlog stage lost the parent checkpoint")
	}
}

func TestIssueBlockerDeletionAndWorkspaceIsolation(t *testing.T) {
	f, s, parent, agent := wakeFixture(t)
	ctx := context.Background()
	earlier := f.Issue(t, "deleted earlier stage", testutil.Cols{"parent_issue_id": parent, "stage": 1})
	later := f.Issue(t, "waiting later stage", testutil.Cols{"parent_issue_id": parent, "stage": 3, "assignee_type": "agent", "assignee_id": agent})
	issue := parseTestUUID(t, later)
	f.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	f.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id=$1", issue)
	f.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN(SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
	foreignWorkspace := f.Workspace(t, "foreign blockers", "foreign-blockers-"+later[:8])
	foreignIssue := f.Issue(t, "foreign issue", testutil.Cols{"workspace_id": foreignWorkspace})
	if err := AddIssueBlocker(ctx, f.q, parseTestUUID(t, f.WorkspaceID), issue, parseTestUUID(t, foreignIssue), "member", parseTestUUID(t, f.UserID)); !errors.Is(err, ErrIssueBlockerNotFound) {
		t.Fatalf("cross-workspace reference = %v", err)
	}
	// The database change is observed without a materialized stage edge.
	f.Exec(t, "DELETE FROM issue WHERE id=$1", earlier)
	if n := f.Count(t, "SELECT count(*) FROM issue_child_event WHERE parent_id=$1 AND child_id=$2 AND kind='detached'", parent, earlier); n != 1 {
		t.Fatalf("delete recorded %d detached events", n)
	}
	if err := s.TickWorkspaces(ctx, parseTestUUID(t, f.WorkspaceID)); err != nil {
		t.Fatal(err)
	}
	if n := wakeRuns(t, f, blockerRule(t, f, issue).ID); n != 1 {
		t.Fatalf("deleting last predecessor queued %d runs", n)
	}
}
