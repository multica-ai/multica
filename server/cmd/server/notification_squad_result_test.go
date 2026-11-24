package main

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestNotification_SquadResult posts a handoff on a squad's issue, completes
// the leader run it woke, and checks that a member who muted squad handoffs
// gets the handoff only when that run passed nothing on (MUL-10054), while a
// member on the default keeps exactly the handoff's own row.
func TestNotification_SquadResult(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	fx := newSquadHandoffFixture(t)

	mutedEmail := "notif-squad-result-muted@multica.ai"
	mutedID := createNotificationMember(t, mutedEmail)
	t.Cleanup(func() { cleanupTestUser(t, mutedEmail) })
	setNotificationPref(t, mutedID, inbox.SquadHandoffsPref, "muted")

	defaultEmail := "notif-squad-result-default@multica.ai"
	defaultID := createNotificationMember(t, defaultEmail)
	t.Cleanup(func() { cleanupTestUser(t, defaultEmail) })

	cases := []struct {
		name string
		// trigger is the comment that woke the leader run; nil posts the
		// worker's handoff.
		trigger func(t *testing.T, issueID string) string
		// followUp records what the leader run did after it was woken.
		followUp func(t *testing.T, issueID, leaderTask string)
		leader   bool
		result   bool
	}{
		{name: "leader ends the chain", leader: true, result: true},
		{name: "leader dispatches again", leader: true, result: false,
			followUp: func(t *testing.T, issueID, leaderTask string) {
				fx.f.Comment(t, issueID, agentMention(fx.workerID)+" one more pass", testutil.Cols{
					"author_type": "agent", "author_id": fx.leaderID, "source_task_id": leaderTask,
				})
			}},
		{name: "leader writes to people", leader: true, result: false,
			followUp: func(t *testing.T, issueID, leaderTask string) {
				fx.f.Comment(t, issueID, "Merged, all done.", testutil.Cols{
					"author_type": "agent", "author_id": fx.leaderID, "source_task_id": leaderTask,
				})
			}},
		{name: "leader starts another run without a comment", leader: true, result: false,
			followUp: func(t *testing.T, issueID, leaderTask string) {
				fx.f.Task(t, fx.outsideID, testutil.Cols{
					"runtime_id": fx.runtimeID, "issue_id": issueID, "delegated_from_task_id": leaderTask,
				})
			}},
		{name: "leader waits on a wakeup", leader: true, result: false,
			followUp: func(t *testing.T, issueID, leaderTask string) {
				leaderWakeup(t, fx, issueID, leaderTask, true)
			}},
		{name: "leader's wakeup already disabled", leader: true, result: true,
			followUp: func(t *testing.T, issueID, leaderTask string) {
				leaderWakeup(t, fx, issueID, leaderTask, false)
			}},
		{name: "leader woken by a person", leader: true, result: false,
			trigger: func(t *testing.T, issueID string) string {
				return fx.f.Comment(t, issueID, "status?")
			}},
		{name: "a worker run ends", leader: false, result: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issueID := fx.f.Issue(t, "squad result: "+tc.name, testutil.Cols{
				"assignee_type": "squad", "assignee_id": fx.squadID,
			})
			t.Cleanup(func() { cleanupInboxForIssue(t, issueID) })
			addTestSubscriber(t, issueID, "member", mutedID, "manual")
			addTestSubscriber(t, issueID, "member", defaultID, "manual")

			var triggerID string
			if tc.trigger != nil {
				triggerID = tc.trigger(t, issueID)
			} else {
				const report = "Merged PR #1, branch deleted."
				triggerID = fx.f.Comment(t, issueID, report, testutil.Cols{
					"author_type": "agent", "author_id": fx.workerID,
				})
				publishAgentComment(bus, issueID, triggerID, fx.workerID, report)
			}

			runCols := testutil.Cols{
				"runtime_id": fx.runtimeID, "issue_id": issueID, "trigger_comment_id": triggerID,
			}
			runAgent := fx.workerID
			if tc.leader {
				runCols["is_leader_task"] = true
				runCols["squad_id"] = fx.squadID
				runAgent = fx.leaderID
			}
			run := fx.f.Task(t, runAgent, runCols)
			if tc.followUp != nil {
				tc.followUp(t, issueID, run)
			}
			bus.Publish(events.Event{
				Type:        protocol.EventTaskCompleted,
				WorkspaceID: testWorkspaceID,
				ActorType:   "system",
				Payload:     map[string]any{"task_id": run, "agent_id": runAgent, "issue_id": issueID},
			})

			if got := squadResultRows(t, mutedID, issueID); got != boolCount(tc.result) {
				t.Fatalf("member who muted squad handoffs got %d result rows, want %d", got, boolCount(tc.result))
			}
			if got := squadResultRows(t, defaultID, issueID); got != 0 {
				t.Fatalf("member on the default got %d result rows, want 0", got)
			}
			if tc.trigger == nil && newCommentRow(t, defaultID, issueID) == nil {
				t.Fatalf("member on the default got no row for the handoff itself")
			}
		})
	}
}

// leaderWakeup records a one-time wakeup the leader run left on issueID, the
// way `multica issue wakeup create` does from inside that run.
func leaderWakeup(t *testing.T, fx squadHandoffFixture, issueID, leaderTask string, enabled bool) {
	t.Helper()
	fx.f.Insert(t, "issue_wakeup", testutil.Cols{
		"id": util.UUIDToString(dbid.NewV7()), "workspace_id": testWorkspaceID, "issue_id": issueID,
		"agent_id": fx.leaderID, "created_by": testUserID, "source_task_id": leaderTask,
		"instruction": "check CI", "kind": "at", "mode": "once",
		"next_fire_at": time.Now().Add(time.Hour), "enabled": enabled,
	})
}

func publishAgentComment(bus *events.Bus, issueID, commentID, authorID, content string) {
	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "agent",
		ActorID:     authorID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID: commentID, IssueID: issueID, AuthorType: "agent",
				AuthorID: authorID, Content: content, Type: "comment",
			},
			"issue_title":  "squad result",
			"issue_status": "in_progress",
		},
	})
}

// squadResultRows counts the recipient's new_comment rows on issueID marked
// as a squad result.
func squadResultRows(t *testing.T, recipientID, issueID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM inbox_item
		WHERE recipient_type = 'member' AND recipient_id = $1 AND issue_id = $2
		  AND type = 'new_comment' AND details->>'squad_result' = 'true'
	`, recipientID, issueID).Scan(&n); err != nil {
		t.Fatalf("count squad result rows: %v", err)
	}
	return n
}

func boolCount(b bool) int {
	if b {
		return 1
	}
	return 0
}
