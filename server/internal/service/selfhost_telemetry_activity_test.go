package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// These cover the self-host telemetry "delegating member" ledger
// (member_delegation_day): only a member's own action marks a day, on the day
// the action happened, and a fold into a queued run keeps every human.

func delegationDays(t *testing.T, f principalFixture, userID string) []string {
	t.Helper()
	rows, err := f.Pool.Query(context.Background(),
		"SELECT activity_date::text FROM member_delegation_day WHERE user_id=$1 ORDER BY activity_date", userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var days []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		days = append(days, d)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return days
}

// activityFixture is conditionFixture with the ledger enabled, as the server
// does when telemetry is on.
func activityFixture(t *testing.T) (principalFixture, *IssueWakeupService, pgtype.UUID, string) {
	t.Helper()
	f, s, issue, agent := conditionFixture(t)
	s.Tasks.RecordMemberActivity = true
	return f, s, issue, agent
}

func cleanupDelegationDays(t *testing.T, f principalFixture, userIDs ...string) {
	t.Helper()
	for _, id := range userIDs {
		f.Cleanup(t, "DELETE FROM member_delegation_day WHERE user_id=$1", id)
	}
}

func utcDate(at time.Time) string { return at.UTC().Format(time.DateOnly) }

func assignIssueToAgent(t *testing.T, f principalFixture, issue pgtype.UUID, agent string) db.Issue {
	t.Helper()
	f.Exec(t, "UPDATE issue SET status='in_progress', assignee_type='agent', assignee_id=$2 WHERE id=$1", issue, agent)
	row, err := f.q.GetIssue(context.Background(), issue)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func TestSelfHostTelemetryActorAssignmentMarksToday(t *testing.T) {
	f, s, issueID, agent := activityFixture(t)
	cleanupDelegationDays(t, f, f.UserID)
	issue := assignIssueToAgent(t, f, issueID, agent)
	if _, err := s.Tasks.EnqueueTaskForIssueByActor(context.Background(), issue, parseTestUUID(t, f.UserID)); err != nil {
		t.Fatal(err)
	}
	if got := delegationDays(t, f, f.UserID); len(got) != 1 || got[0] != utcDate(time.Now()) {
		t.Fatalf("actor assignment days = %v, want [today]", got)
	}
}

// The parent issue's member creator is inherited by a child_done wakeup run as
// direct_human, but nobody acted.
func TestSelfHostTelemetryAutomaticChildDoneMustNotCountHumanActivity(t *testing.T) {
	f, s, issue, agent := activityFixture(t)
	ctx := context.Background()
	cleanupDelegationDays(t, f, f.UserID)
	f.Exec(t, "UPDATE issue SET status='in_progress', assignee_type='agent',assignee_id=$2,created_at=now()-interval '40 days' WHERE id=$1", issue, agent)
	child := f.Issue(t, "child", testutil.Cols{"parent_issue_id": issue, "status": "in_progress", "creator_type": "agent", "creator_id": agent})
	f.Cleanup(t, "DELETE FROM issue_child_event WHERE parent_id=$1", issue)
	if err := s.ProcessChildEvents(ctx, issue); err != nil {
		t.Fatal(err)
	}
	f.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", child)
	if err := s.ProcessChildEvents(ctx, issue); err != nil {
		t.Fatal(err)
	}
	var source string
	if err := f.Pool.QueryRow(ctx, "SELECT originator_source FROM agent_task_queue WHERE issue_id=$1 AND context->>'wakeup_system'='child_done'", issue).Scan(&source); err != nil {
		t.Fatalf("child_done run was not created: %v", err)
	}
	if got := delegationDays(t, f, f.UserID); len(got) != 0 {
		t.Fatalf("automatic child_done run (source=%s) marked delegation days %v", source, got)
	}
}

func TestSelfHostTelemetryAutomaticRetryMustNotCountHumanActivity(t *testing.T) {
	f, s, issueID, agent := activityFixture(t)
	ctx := context.Background()
	cleanupDelegationDays(t, f, f.UserID)
	issue := assignIssueToAgent(t, f, issueID, agent)
	parent, err := s.Tasks.EnqueueTaskForIssueByActor(ctx, issue, parseTestUUID(t, f.UserID))
	if err != nil {
		t.Fatal(err)
	}
	// Pretend the human acted yesterday, then fail the run and retry it today.
	yesterday := utcDate(time.Now().Add(-24 * time.Hour))
	f.Exec(t, "UPDATE member_delegation_day SET activity_date=$2::date WHERE user_id=$1", f.UserID, yesterday)
	f.Exec(t, "UPDATE agent_task_queue SET status='failed', completed_at=now() WHERE id=$1", parent.ID)
	retry, err := f.q.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !retry.RetryOfTaskID.Valid {
		t.Fatal("retry lineage missing")
	}
	if got := delegationDays(t, f, f.UserID); len(got) != 1 || got[0] != yesterday {
		t.Fatalf("retry changed delegation days to %v, want only [%s]", got, yesterday)
	}
}

// A replies to the agent yesterday; the run is still queued today when B's reply
// folds into it and re-attributes it to B. Both humans keep their own day.
func TestSelfHostTelemetryCoalescedHumanActionsMustRetainUsersAndDates(t *testing.T) {
	f, s, issueID, agent := activityFixture(t)
	ctx := context.Background()
	userB := f.member(t, "second human")
	cleanupDelegationDays(t, f, f.UserID, userB)
	issue := assignIssueToAgent(t, f, issueID, agent)
	yesterdayAt := time.Now().UTC().Add(-24 * time.Hour)

	commentA := f.Comment(t, issueID.String(), "first human delegates", testutil.Cols{"created_at": yesterdayAt})
	if _, err := s.Tasks.EnqueueTaskForIssue(ctx, issue, parseTestUUID(t, commentA)); err != nil {
		t.Fatal(err)
	}

	commentB := f.Comment(t, issueID.String(), "second human delegates", testutil.Cols{"author_id": userB, "parent_id": commentA})
	agentRow, err := f.q.GetAgent(ctx, parseTestUUID(t, agent))
	if err != nil {
		t.Fatal(err)
	}
	// The same steps handler.mergeCommentIntoPendingTask runs.
	attr, err := s.Tasks.AttributionForMergedComment(ctx, issue.WorkspaceID, parseTestUUID(t, commentB), false, agentRow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.q.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
		IssueID: issueID, AgentID: agentRow.ID, NewTriggerCommentID: parseTestUUID(t, commentB),
		NewOriginatorUserID: attr.UserID, NewAccountableUserID: attr.AccountableUserID,
		NewOriginatorSource:     pgtype.Text{String: attr.Source.String(), Valid: true},
		NewTriggerEvidenceKind:  pgtype.Text{String: string(attr.EvidenceKind), Valid: true},
		NewTriggerEvidenceRefID: attr.EvidenceRefID,
	}); err != nil {
		t.Fatal(err)
	}
	s.Tasks.RecordHumanDelegation(ctx, attr)

	if got := delegationDays(t, f, f.UserID); len(got) != 1 || got[0] != utcDate(yesterdayAt) {
		t.Errorf("A's days = %v, want [%s] (the comment's day, not the enqueue day)", got, utcDate(yesterdayAt))
	}
	if got := delegationDays(t, f, userB); len(got) != 1 || got[0] != utcDate(time.Now()) {
		t.Errorf("B's days = %v, want [today]", got)
	}
}

// Creating an issue already assigned to an agent is a member delegating work,
// on both the immediate and the deferred (channel media) path.
func TestSelfHostTelemetryCreateAssignedIssueMarksToday(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fireAt time.Time
	}{
		{"immediate", time.Time{}},
		{"deferred", time.Now().Add(time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, s, _, agent := activityFixture(t)
			cleanupDelegationDays(t, f, f.UserID)
			ctx := context.Background()
			f.Exec(t, "UPDATE workspace SET issue_counter=(SELECT COALESCE(MAX(number),0) FROM issue WHERE workspace_id=$1) WHERE id=$1", f.WorkspaceID)
			issues := NewIssueService(f.q, f.Pool, f.svc.Bus, nil, s.Tasks)
			result, err := issues.Create(ctx, IssueCreateParams{
				WorkspaceID: parseTestUUID(t, f.WorkspaceID), Title: "Human creates an assigned issue", Status: "todo", Priority: "medium",
				AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: parseTestUUID(t, agent),
				CreatorType: "member", CreatorID: parseTestUUID(t, f.UserID),
			}, IssueCreateOpts{AssignedAgentRunFireAt: tc.fireAt})
			if err != nil {
				t.Fatal(err)
			}
			f.Cleanup(t, "DELETE FROM issue WHERE id=$1", result.Issue.ID)
			f.Cleanup(t, "DELETE FROM activity_log WHERE issue_id=$1", result.Issue.ID)
			f.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id=$1", result.Issue.ID)
			if !result.AssignedTaskID.Valid {
				t.Fatal("no assigned run was created")
			}
			if got := delegationDays(t, f, f.UserID); len(got) != 1 || got[0] != utcDate(time.Now()) {
				t.Fatalf("create-with-assignee days = %v, want [today]", got)
			}
		})
	}
}

// With telemetry off (the server leaves RecordMemberActivity false under
// DO_NOT_TRACK) the ledger is never written.
func TestSelfHostTelemetryDisabledDoesNotRecordDelegation(t *testing.T) {
	f, s, issueID, agent := conditionFixture(t)
	cleanupDelegationDays(t, f, f.UserID)
	if s.Tasks.RecordMemberActivity {
		t.Fatal("ledger must be off unless the server enables telemetry")
	}
	issue := assignIssueToAgent(t, f, issueID, agent)
	if _, err := s.Tasks.EnqueueTaskForIssueByActor(context.Background(), issue, parseTestUUID(t, f.UserID)); err != nil {
		t.Fatal(err)
	}
	if got := delegationDays(t, f, f.UserID); len(got) != 0 {
		t.Fatalf("telemetry disabled but ledger recorded %v", got)
	}
}
