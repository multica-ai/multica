package service

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// wakeWaitingRun queues an ordinary run of the agent on the issue that runs
// as runAs, like an assignment or a comment would.
func wakeWaitingRun(t *testing.T, f principalFixture, issue pgtype.UUID, agent, runAs string) string {
	t.Helper()
	return f.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')"),
		"originator_user_id": runAs, "accountable_user_id": runAs})
}

// wakeClaim claims the run as a daemon that renders joined wakeups does and
// returns what its prompt gets.
func wakeClaim(t *testing.T, f principalFixture, s *IssueWakeupService, taskID string) string {
	t.Helper()
	f.Exec(t, "UPDATE agent_task_queue SET status='dispatched',dispatched_at=clock_timestamp() WHERE id=$1", taskID)
	task, err := f.q.GetAgentTask(context.Background(), parseTestUUID(t, taskID))
	if err != nil {
		t.Fatal(err)
	}
	joined, err := s.JoinWaitingWakeups(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	return JoinedWakeupNotes(joined)
}

func invocableByWorkspace(t *testing.T, f principalFixture, agent string) {
	t.Helper()
	f.Exec(t, "UPDATE agent SET permission_mode='public_to',visibility='workspace' WHERE id=$1", agent)
	f.Insert(t, "agent_invocation_target", testutil.Cols{"agent_id": agent, "target_type": "workspace", "target_id": f.WorkspaceID})
}

// Joining a run counts toward the rule's cap like starting one.
func TestJoinedWakeupCountsTowardTheCap(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", MaxFires: 1, EventTypes: []string{"comment.created"}, Instruction: "Review"})
	waiting := wakeWaitingRun(t, f, issue, agent, f.UserID)
	f.Comment(t, util.UUIDToString(issue), "trigger")
	wakeTick(t, f, s, w.ID)
	if notes := wakeClaim(t, f, s, waiting); !strings.Contains(notes, "Review") {
		t.Fatalf("the claimed run lacks the rule: %q", notes)
	}
	got, err := f.q.LocklessWakeup(context.Background(), w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.PausedReason.String != wakeupPausedMaxFires || got.FireCount != 1 {
		t.Fatalf("after joining once with max_fires=1: enabled=%t paused=%q fire_count=%d", got.Enabled, got.PausedReason.String, got.FireCount)
	}
}

// A rule turned off before the run is claimed hands it nothing, and one
// turned off after a claim that did not go through is dropped from the next.
func TestTurnedOffWakeupHandsNothingOver(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name       string
		afterClaim bool
		off        func(*IssueWakeupService, pgtype.UUID, db.IssueWakeup, pgtype.UUID) error
	}{
		{"deleted before the claim", false, func(s *IssueWakeupService, issue pgtype.UUID, w db.IssueWakeup, member pgtype.UUID) error {
			return s.Delete(ctx, issue, w.ID, member)
		}},
		{"turned off before the claim", false, func(s *IssueWakeupService, issue pgtype.UUID, w db.IssueWakeup, member pgtype.UUID) error {
			_, err := s.Disable(ctx, issue, w.ID, member)
			return err
		}},
		{"turned off before a repeated claim", true, func(s *IssueWakeupService, issue pgtype.UUID, w db.IssueWakeup, member pgtype.UUID) error {
			_, err := s.Disable(ctx, issue, w.ID, member)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, s, issue, agent := conditionFixture(t)
			w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created"}, Instruction: "Do not run after it is off"})
			waiting := wakeWaitingRun(t, f, issue, agent, f.UserID)
			f.Comment(t, util.UUIDToString(issue), "trigger")
			wakeTick(t, f, s, w.ID)
			if tc.afterClaim {
				if notes := wakeClaim(t, f, s, waiting); notes == "" {
					t.Fatal("the first claim got nothing")
				}
				// The claim did not go through; the run waits again.
				f.Exec(t, "UPDATE agent_task_queue SET status='queued',dispatched_at=NULL WHERE id=$1", waiting)
			}
			if err := tc.off(s, issue, w, parseTestUUID(t, f.UserID)); err != nil {
				t.Fatal(err)
			}
			if notes := wakeClaim(t, f, s, waiting); notes != "" {
				t.Fatalf("the run still gets the rule: %q", notes)
			}
		})
	}
}

// A run that runs as someone else is never joined: the rule starts its own
// run as its creator.
func TestWakeupJoinsOnlyRunsOfTheSamePerson(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	invocableByWorkspace(t, f, agent)
	other := f.member(t, "wake-other")
	w, err := s.Create(context.Background(), issue, parseTestUUID(t, other), pgtype.UUID{}, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "Other member's instruction"})
	if err != nil {
		t.Fatal(err)
	}
	waiting := wakeWaitingRun(t, f, issue, agent, f.UserID)
	f.Comment(t, util.UUIDToString(issue), "trigger")
	wakeTick(t, f, s, w.ID)
	if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1 AND originator_user_id=$2", util.UUIDToString(w.ID), other); n != 1 {
		t.Fatalf("own runs as the rule's creator = %d, want 1", n)
	}
	if notes := wakeClaim(t, f, s, waiting); notes != "" {
		t.Fatalf("another person's run got the rule: %q", notes)
	}
}

// A creator who lost access by the time the run is claimed hands it nothing.
func TestJoinRechecksTheCreatorsAccess(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	invocableByWorkspace(t, f, agent)
	other := f.member(t, "wake-revoked")
	w, err := s.Create(context.Background(), issue, parseTestUUID(t, other), pgtype.UUID{}, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "Check access"})
	if err != nil {
		t.Fatal(err)
	}
	waiting := wakeWaitingRun(t, f, issue, agent, other)
	f.Comment(t, util.UUIDToString(issue), "trigger")
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 0 {
		t.Fatalf("the rule queued %d runs while one of the same person waited", n)
	}
	f.Exec(t, "DELETE FROM member WHERE user_id=$1 AND workspace_id=$2", other, f.WorkspaceID)
	if notes := wakeClaim(t, f, s, waiting); notes != "" {
		t.Fatalf("the rule joined after its creator lost access: %q", notes)
	}
}

// A rule whose waiting run never takes its input, because the run was
// cancelled or went to a daemon that cannot render it, still has the input
// and starts its own run.
func TestWakeupKeepsItsInputWhenTheWaitingRunDoesNotTakeIt(t *testing.T) {
	ctx := context.Background()
	t.Run("run cancelled with the rule that queued it", func(t *testing.T) {
		f, s, issue, agent := conditionFixture(t)
		member := parseTestUUID(t, f.UserID)
		host := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "every", IntervalSeconds: 3600, Instruction: "Scheduled check"})
		if err := s.Trigger(ctx, issue, host.ID, member); err != nil {
			t.Fatal(err)
		}
		w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "Independent once-only work"})
		f.Comment(t, util.UUIDToString(issue), "trigger")
		wakeTick(t, f, s, w.ID)
		if n := wakeRuns(t, f, w.ID); n != 0 {
			t.Fatalf("the rule queued %d runs while the scheduled run waited", n)
		}
		if _, err := s.Disable(ctx, issue, host.ID, member); err != nil {
			t.Fatal(err)
		}
		wakeTick(t, f, s, w.ID)
		if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1 AND status='queued'", util.UUIDToString(w.ID)); n != 1 {
			t.Fatalf("queued runs of the rule = %d, want 1", n)
		}
	})
	t.Run("run claimed by an older daemon", func(t *testing.T) {
		f, s, issue, agent := conditionFixture(t)
		w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "Review"})
		waiting := wakeWaitingRun(t, f, issue, agent, f.UserID)
		f.Comment(t, util.UUIDToString(issue), "trigger")
		wakeTick(t, f, s, w.ID)
		f.Exec(t, "UPDATE agent_task_queue SET status='dispatched',dispatched_at=clock_timestamp() WHERE id=$1", waiting)
		wakeTick(t, f, s, w.ID)
		if n := wakeRuns(t, f, w.ID); n != 1 {
			t.Fatalf("runs of the rule = %d, want 1", n)
		}
	})
}

// The one-time backfill can create a parent's rule while a sub-issue's
// closing is recorded but not processed; that closing still wakes the
// assignee.
func TestChildDoneBackfillKeepsAPendingClose(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	ctx := context.Background()
	f.Exec(t, "UPDATE issue SET status='in_progress',assignee_type='agent',assignee_id=$2 WHERE id=$1", issue, agent)
	child := f.Issue(t, "child", testutil.Cols{"parent_issue_id": issue, "status": "in_progress"})
	f.Cleanup(t, "DELETE FROM issue_child_event WHERE parent_id=$1", issue)
	// A sub-issue from before the rule existed: only its closing is recorded.
	f.Exec(t, "DELETE FROM issue_child_event WHERE parent_id=$1", issue)
	f.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", child)
	parent, err := f.q.GetIssue(ctx, issue)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ensureRule(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if err = s.ProcessChildEvents(ctx, issue); err != nil {
		t.Fatal(err)
	}
	w, err := f.q.GetSystemWakeup(ctx, db.GetSystemWakeupParams{IssueID: issue, SystemRule: systemRuleText(SystemRuleChildDone)})
	if err != nil {
		t.Fatal(err)
	}
	if n := wakeRuns(t, f, w.ID); n != 1 {
		t.Fatalf("pending closing lost after the backfill: runs=%d condition_state=%q", n, w.ConditionState)
	}
}
