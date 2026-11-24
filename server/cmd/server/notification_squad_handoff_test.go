package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type squadHandoffFixture struct {
	f         *testutil.Fixture
	runtimeID string
	squadID   string
	leaderID  string
	workerID  string // on the squad's roster
	outsideID string // not on the roster
}

func newSquadHandoffFixture(t *testing.T) squadHandoffFixture {
	t.Helper()
	f := workspaceFixture(t)
	runtimeID := f.Runtime(t, "squad-handoff-runtime")
	leaderID := f.Agent(t, "squad-handoff-leader", runtimeID)
	workerID := f.Agent(t, "squad-handoff-worker", runtimeID)
	outsideID := f.Agent(t, "squad-handoff-outside", runtimeID)
	squadID := f.Squad(t, "squad-handoff", leaderID)
	f.SquadMember(t, squadID, "agent", leaderID)
	f.SquadMember(t, squadID, "agent", workerID)
	return squadHandoffFixture{f: f, runtimeID: runtimeID, squadID: squadID,
		leaderID: leaderID, workerID: workerID, outsideID: outsideID}
}

func agentMention(id string) string  { return fmt.Sprintf("[@A](mention://agent/%s)", id) }
func memberMention(id string) string { return fmt.Sprintf("[@M](mention://member/%s)", id) }

// TestNotification_SquadHandoffs drives the comment:created listener with one
// comment per case, each on its own issue, and checks who gets the
// new_comment row: a member who muted squad_handoffs and one on the default
// (MUL-10008).
func TestNotification_SquadHandoffs(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	fx := newSquadHandoffFixture(t)

	mutedEmail := "notif-squad-handoff-muted@multica.ai"
	mutedID := createNotificationMember(t, mutedEmail)
	t.Cleanup(func() { cleanupTestUser(t, mutedEmail) })
	setNotificationPref(t, mutedID, inbox.SquadHandoffsPref, "muted")

	defaultEmail := "notif-squad-handoff-default@multica.ai"
	defaultID := createNotificationMember(t, defaultEmail)
	t.Cleanup(func() { cleanupTestUser(t, defaultEmail) })

	leaderTask := fx.f.Task(t, fx.leaderID, testutil.Cols{
		"runtime_id": fx.runtimeID, "is_leader_task": true, "squad_id": fx.squadID,
	})
	// leaderTaskOn is the leader run an @squad mention starts on issueID.
	leaderTaskOn := func(t *testing.T, issueID string) string {
		return fx.f.Task(t, fx.leaderID, testutil.Cols{
			"runtime_id": fx.runtimeID, "issue_id": issueID, "is_leader_task": true, "squad_id": fx.squadID,
		})
	}

	cases := []struct {
		name     string
		assignee string // "squad" or "agent"
		author   string
		content  string
		// task returns the authoring task of the comment, or "" for none.
		task    func(t *testing.T, issueID string) string
		handoff bool
	}{
		{name: "leader dispatching to a worker", author: fx.leaderID,
			content: agentMention(fx.workerID) + " take phase 1", handoff: true},
		{name: "leader without a mention", author: fx.leaderID,
			content: "PR merged.", handoff: false},
		{name: "leader asking a person while dispatching", author: fx.leaderID,
			content: memberMention(testUserID) + " " + agentMention(fx.workerID) + " please decide", handoff: false},
		{name: "roster worker reporting", author: fx.workerID,
			content: "Done, PR is up.", handoff: true},
		{name: "outside agent the leader dispatched", author: fx.outsideID,
			content: "Deployed.", handoff: true,
			task: func(t *testing.T, issueID string) string {
				return fx.f.Task(t, fx.outsideID, testutil.Cols{
					"runtime_id": fx.runtimeID, "issue_id": issueID, "delegated_from_task_id": leaderTask,
				})
			}},
		{name: "outside agent nobody dispatched", author: fx.outsideID,
			content: "Drive-by note.", handoff: false},
		{name: "roster worker answering a person", author: fx.workerID,
			content: "Here is the answer.", handoff: false,
			task: func(t *testing.T, issueID string) string {
				ask := fx.f.Comment(t, issueID, "can you check this?")
				return fx.f.Task(t, fx.workerID, testutil.Cols{
					"runtime_id": fx.runtimeID, "issue_id": issueID, "trigger_comment_id": ask,
				})
			}},
		{name: "roster worker on an issue not assigned to the squad", assignee: "agent", author: fx.workerID,
			content: "Done.", handoff: false},
		{name: "leader called in with @squad dispatching", assignee: "agent", author: fx.leaderID,
			content: agentMention(fx.workerID) + " review the PR", handoff: true,
			task: func(t *testing.T, issueID string) string { return leaderTaskOn(t, issueID) }},
		{name: "leader called in with @squad without a mention", assignee: "agent", author: fx.leaderID,
			content: "Review is done, ready to merge.", handoff: false,
			task: func(t *testing.T, issueID string) string { return leaderTaskOn(t, issueID) }},
		{name: "outside agent the called-in leader dispatched", assignee: "agent", author: fx.outsideID,
			content: "Review: request changes.", handoff: true,
			task: func(t *testing.T, issueID string) string {
				return fx.f.Task(t, fx.outsideID, testutil.Cols{
					"runtime_id": fx.runtimeID, "issue_id": issueID, "delegated_from_task_id": leaderTaskOn(t, issueID),
				})
			}},
		{name: "roster worker a dispatched worker handed to", assignee: "agent", author: fx.workerID,
			content: "Fixed.", handoff: true,
			task: func(t *testing.T, issueID string) string {
				first := fx.f.Task(t, fx.outsideID, testutil.Cols{
					"runtime_id": fx.runtimeID, "issue_id": issueID, "delegated_from_task_id": leaderTaskOn(t, issueID),
				})
				return fx.f.Task(t, fx.workerID, testutil.Cols{
					"runtime_id": fx.runtimeID, "issue_id": issueID, "delegated_from_task_id": first,
				})
			}},
		{name: "agent the leader dispatched from another issue", assignee: "agent", author: fx.outsideID,
			content: "Done.", handoff: false,
			task: func(t *testing.T, issueID string) string {
				return fx.f.Task(t, fx.outsideID, testutil.Cols{
					"runtime_id": fx.runtimeID, "issue_id": issueID, "delegated_from_task_id": leaderTask,
				})
			}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assigneeType, assigneeID := "squad", fx.squadID
			if tc.assignee == "agent" {
				assigneeType, assigneeID = "agent", fx.leaderID
			}
			issueID := fx.f.Issue(t, "squad handoff: "+tc.name, testutil.Cols{
				"assignee_type": assigneeType, "assignee_id": assigneeID,
			})
			t.Cleanup(func() { cleanupInboxForIssue(t, issueID) })
			addTestSubscriber(t, issueID, "member", mutedID, "manual")
			addTestSubscriber(t, issueID, "member", defaultID, "manual")

			commentCols := testutil.Cols{"author_type": "agent", "author_id": tc.author}
			if tc.task != nil {
				commentCols["source_task_id"] = tc.task(t, issueID)
			}
			commentID := fx.f.Comment(t, issueID, tc.content, commentCols)

			bus.Publish(events.Event{
				Type:        protocol.EventCommentCreated,
				WorkspaceID: testWorkspaceID,
				ActorType:   "agent",
				ActorID:     tc.author,
				Payload: map[string]any{
					"comment": handler.CommentResponse{
						ID: commentID, IssueID: issueID, AuthorType: "agent",
						AuthorID: tc.author, Content: tc.content, Type: "comment",
					},
					"issue_title":  "squad handoff",
					"issue_status": "in_progress",
				},
			})

			defaultRow := newCommentRow(t, defaultID, issueID)
			if defaultRow == nil {
				t.Fatalf("member on the default got no new_comment row")
			}
			var details map[string]string
			if err := json.Unmarshal(defaultRow.Details, &details); err != nil {
				t.Fatalf("details must stay a string map for clients: %v (%s)", err, defaultRow.Details)
			}
			if marked := details[inbox.SquadHandoffDetail] == "true"; marked != tc.handoff {
				t.Fatalf("squad_handoff marked %v, want %v (details %s)", marked, tc.handoff, defaultRow.Details)
			}
			if got := newCommentRow(t, mutedID, issueID) != nil; got == tc.handoff {
				t.Fatalf("member who muted squad handoffs got a row: %v, want %v", got, !tc.handoff)
			}
		})
	}
}

// newCommentRow is the recipient's new_comment row on issueID, or nil.
func newCommentRow(t *testing.T, recipientID, issueID string) *db.InboxItem {
	t.Helper()
	var item db.InboxItem
	err := testPool.QueryRow(context.Background(), `
		SELECT id, details FROM inbox_item
		WHERE recipient_type = 'member' AND recipient_id = $1 AND issue_id = $2 AND type = 'new_comment'
	`, recipientID, issueID).Scan(&item.ID, &item.Details)
	if err != nil {
		return nil
	}
	return &item
}
