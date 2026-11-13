package main

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The Inbox files each notification by who the recipient is to the issue
// (MUL-9215). These tests drive the listeners and read what was stored.

type storedInboxRow struct {
	notifType string
	section   string
	archived  bool
	taskID    string
}

// inboxRowsOnIssue lists a member's rows on one issue, oldest first.
func inboxRowsOnIssue(t *testing.T, recipientID, issueID string) []storedInboxRow {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `
		SELECT type, coalesce(section, ''), archived, coalesce(details->>'task_id', '')
		FROM inbox_item
		WHERE recipient_type = 'member' AND recipient_id = $1 AND issue_id = $2
		ORDER BY created_at, id
	`, recipientID, issueID)
	if err != nil {
		t.Fatalf("inboxRowsOnIssue: %v", err)
	}
	defer rows.Close()
	var out []storedInboxRow
	for rows.Next() {
		var row storedInboxRow
		if err := rows.Scan(&row.notifType, &row.section, &row.archived, &row.taskID); err != nil {
			t.Fatalf("inboxRowsOnIssue scan: %v", err)
		}
		out = append(out, row)
	}
	return out
}

// activeInboxRows keeps the rows still in the Inbox.
func activeInboxRows(rows []storedInboxRow) []storedInboxRow {
	var out []storedInboxRow
	for _, row := range rows {
		if !row.archived {
			out = append(out, row)
		}
	}
	return out
}

// seedInboxTask records a run and who started it. source is the attribution
// waterfall level; retryOf, when set, makes it the retry of an earlier run.
func seedInboxTask(t *testing.T, issueID, originatorID, source, status, retryOf string) string {
	t.Helper()
	agentID := getAgentID(t)
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority,
		                              originator_user_id, accountable_user_id, originator_source, retry_of_task_id)
		SELECT id, runtime_id, $2, $5, 0, $3, $3, $4, NULLIF($6, '')::uuid FROM agent WHERE id = $1
		RETURNING id
	`, agentID, issueID, originatorID, source, status, retryOf).Scan(&taskID); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

func publishTaskEvent(bus *events.Bus, eventType, issueID, taskID string, extra map[string]any) {
	payload := map[string]any{
		"task_id":  taskID,
		"agent_id": "00000000-0000-0000-0000-aaaaaaaaaaaa",
		"issue_id": issueID,
	}
	for key, value := range extra {
		payload[key] = value
	}
	bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		Payload:     payload,
	})
}

func publishSectionsComment(bus *events.Bus, issueID, authorID, issueStatus string) {
	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     authorID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         "00000000-0000-0000-0000-000000000000",
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   authorID,
				Content:    "a reply",
				Type:       "comment",
			},
			"issue_title":  "sections test issue",
			"issue_status": issueStatus,
		},
	})
}

// inboxSectionsIssue is an issue the test user created, with a second member
// who answers for it by assignment and a third who only follows it.
type inboxSectionsIssue struct {
	issueID    string
	assigneeID string
	followerID string
	authorID   string
}

func newInboxSectionsIssue(t *testing.T, tag string) inboxSectionsIssue {
	t.Helper()
	member := func(role string) string {
		email := "inbox-sections-" + tag + "-" + role + "@multica.ai"
		id := createNotificationMember(t, email)
		t.Cleanup(func() { cleanupTestUser(t, email) })
		return id
	}
	f := inboxSectionsIssue{
		assigneeID: member("assignee"),
		followerID: member("follower"),
		authorID:   member("author"),
	}
	f.issueID = createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, f.issueID)
		cleanupTestIssue(t, f.issueID)
	})
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET assignee_type = 'member', assignee_id = $2 WHERE id = $1`, f.issueID, f.assigneeID); err != nil {
		t.Fatalf("assign issue: %v", err)
	}
	addTestSubscriber(t, f.issueID, "member", testUserID, "creator")
	addTestSubscriber(t, f.issueID, "member", f.assigneeID, "assignee")
	addTestSubscriber(t, f.issueID, "member", f.followerID, "commenter")
	return f
}

func wantSingleRow(t *testing.T, who string, rows []storedInboxRow, notifType, section string) {
	t.Helper()
	active := activeInboxRows(rows)
	if len(active) != 1 {
		t.Fatalf("%s: %d active rows, want 1: %+v", who, len(active), rows)
	}
	if active[0].notifType != notifType || active[0].section != section {
		t.Fatalf("%s: row = %s in %q, want %s in %q", who, active[0].notifType, active[0].section, notifType, section)
	}
}

// The same comment needs the people who answer for the issue and is an update
// for someone who only follows it.
func TestInboxSection_CommentIsFiledByTheRecipientsRelation(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	f := newInboxSectionsIssue(t, "comment")

	var announced []string
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		payload, _ := e.Payload.(map[string]any)
		item, _ := payload["item"].(map[string]any)
		if item["issue_id"] == nil {
			return
		}
		if id, _ := item["issue_id"].(*string); id != nil && *id == f.issueID {
			section, _ := item["section"].(string)
			announced = append(announced, section)
		}
	})

	publishSectionsComment(bus, f.issueID, f.authorID, "todo")

	wantSingleRow(t, "creator", inboxRowsOnIssue(t, testUserID, f.issueID), "new_comment", "needs_you")
	wantSingleRow(t, "assignee", inboxRowsOnIssue(t, f.assigneeID, f.issueID), "new_comment", "needs_you")
	wantSingleRow(t, "follower", inboxRowsOnIssue(t, f.followerID, f.issueID), "new_comment", "updates")

	needsYou, updates := 0, 0
	for _, section := range announced {
		switch section {
		case "needs_you":
			needsYou++
		case "updates":
			updates++
		}
	}
	if needsYou != 2 || updates != 1 {
		t.Fatalf("inbox:new carried sections %v, want two needs_you and one updates", announced)
	}
}

// A mention is addressed to its target whatever their relation to the issue.
func TestInboxSection_MentionNeedsItsTarget(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	f := newInboxSectionsIssue(t, "mention")

	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     f.authorID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         "00000000-0000-0000-0000-000000000000",
				IssueID:    f.issueID,
				AuthorType: "member",
				AuthorID:   f.authorID,
				Content:    "[@Follower](mention://member/" + f.followerID + ") can you look?",
				Type:       "comment",
			},
			"issue_title":  "sections test issue",
			"issue_status": "todo",
		},
	})

	var mention *storedInboxRow
	for _, row := range inboxRowsOnIssue(t, f.followerID, f.issueID) {
		if row.notifType == "mentioned" {
			mention = &row
		}
	}
	if mention == nil || mention.section != "needs_you" {
		t.Fatalf("follower's mention = %+v, want a mentioned row in needs_you", mention)
	}
}

// A status change needs the people who answer for the issue only when the new
// status waits on a person.
func TestInboxSection_StatusChangeNeedsTheOwnerOnlyWhenItWaits(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	for _, tc := range []struct {
		status       string
		ownerSection string
	}{
		{"in_review", "needs_you"},
		{"blocked", "needs_you"},
		{"in_progress", "updates"},
		{"todo", "updates"},
	} {
		t.Run(tc.status, func(t *testing.T) {
			f := newInboxSectionsIssue(t, "status-"+tc.status)
			// publishStatusChange acts as the test user, so the assignee is the
			// owner who hears about it.
			publishStatusChange(bus, f.issueID, tc.status, "backlog")
			wantSingleRow(t, "assignee", inboxRowsOnIssue(t, f.assigneeID, f.issueID), "status_changed", tc.ownerSection)
			wantSingleRow(t, "follower", inboxRowsOnIssue(t, f.followerID, f.issueID), "status_changed", "updates")
		})
	}
}

// A done or cancelled issue leaves every Inbox. What remains is the one update
// saying it closed.
func TestInbox_ClosedIssueLeavesEveryInbox(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	for _, status := range []string{"done", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			f := newInboxSectionsIssue(t, "closed-"+status)
			publishSectionsComment(bus, f.issueID, f.authorID, "in_review")

			var archived []map[string]any
			bus.Subscribe(protocol.EventInboxBatchArchived, func(e events.Event) {
				payload, _ := e.Payload.(map[string]any)
				if payload["issue_id"] == f.issueID {
					archived = append(archived, payload)
				}
			})

			publishStatusChange(bus, f.issueID, status, "in_review")

			for who, id := range map[string]string{"assignee": f.assigneeID, "follower": f.followerID} {
				rows := inboxRowsOnIssue(t, id, f.issueID)
				if len(rows) != 2 || !rows[0].archived {
					t.Fatalf("%s: rows = %+v, want the comment archived", who, rows)
				}
				wantSingleRow(t, who, rows, "status_changed", "updates")
			}
			// The test user closed it, so they get no update, and nothing waits.
			if active := activeInboxRows(inboxRowsOnIssue(t, testUserID, f.issueID)); len(active) != 0 {
				t.Fatalf("closer: active rows = %+v, want none", active)
			}
			if len(archived) != 3 {
				t.Fatalf("inbox:batch-archived events = %d, want one per recipient", len(archived))
			}
			for _, payload := range archived {
				if payload["reason"] != "issue_closed" {
					t.Fatalf("batch-archived reason = %v, want issue_closed", payload["reason"])
				}
			}
		})
	}
}

// A member's own action starting a run hands the issue to the agent: their
// rows leave "Needs you", and nobody else's do.
func TestInbox_RunStartedByMemberArchivesTheirRows(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	f := newInboxSectionsIssue(t, "run-start")
	publishSectionsComment(bus, f.issueID, f.authorID, "todo")

	var archived []map[string]any
	bus.Subscribe(protocol.EventInboxBatchArchived, func(e events.Event) {
		payload, _ := e.Payload.(map[string]any)
		if payload["issue_id"] == f.issueID {
			archived = append(archived, payload)
		}
	})

	// A run an agent started on the member's behalf, and the retry of an
	// earlier attempt, are not the member handing anything over. The listener
	// reads who started the run, not its status, so the rows are seeded
	// finished: one agent may have a single pending run per issue.
	delegated := seedInboxTask(t, f.issueID, testUserID, "delegation", "completed", "")
	publishTaskEvent(bus, protocol.EventTaskQueued, f.issueID, delegated, nil)
	first := seedInboxTask(t, f.issueID, testUserID, "direct_human", "failed", "")
	retry := seedInboxTask(t, f.issueID, testUserID, "direct_human", "completed", first)
	publishTaskEvent(bus, protocol.EventTaskQueued, f.issueID, retry, nil)
	if active := activeInboxRows(inboxRowsOnIssue(t, testUserID, f.issueID)); len(active) != 1 {
		t.Fatalf("after a delegated run and a retry: active rows = %+v, want the comment kept", active)
	}

	direct := seedInboxTask(t, f.issueID, testUserID, "direct_human", "completed", "")
	publishTaskEvent(bus, protocol.EventTaskQueued, f.issueID, direct, nil)

	if active := activeInboxRows(inboxRowsOnIssue(t, testUserID, f.issueID)); len(active) != 0 {
		t.Fatalf("starter: active rows = %+v, want none", active)
	}
	if active := activeInboxRows(inboxRowsOnIssue(t, f.assigneeID, f.issueID)); len(active) != 1 {
		t.Fatalf("assignee: active rows = %+v, want theirs untouched", active)
	}
	if len(archived) != 1 || archived[0]["recipient_id"] != testUserID || archived[0]["reason"] != "handed_to_agent" {
		t.Fatalf("inbox:batch-archived = %+v, want one handed_to_agent event for the starter", archived)
	}
}

// When the run a member started completes, the issue comes back to them in
// "Needs you", whether or not the agent mentioned them.
func TestInbox_RunCompletedHandsBackToTheMemberWhoStartedIt(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	f := newInboxSectionsIssue(t, "run-done")

	direct := seedInboxTask(t, f.issueID, f.followerID, "direct_human", "completed", "")
	publishTaskEvent(bus, protocol.EventTaskCompleted, f.issueID, direct, nil)
	// A run an agent started on someone's behalf hands nothing back.
	delegated := seedInboxTask(t, f.issueID, f.authorID, "delegation", "completed", "")
	publishTaskEvent(bus, protocol.EventTaskCompleted, f.issueID, delegated, nil)

	rows := inboxRowsOnIssue(t, f.followerID, f.issueID)
	wantSingleRow(t, "starter", rows, "agent_completed", "needs_you")
	if rows[0].taskID != direct {
		t.Fatalf("starter: row names run %q, want %q", rows[0].taskID, direct)
	}
	for who, id := range map[string]string{"delegating member": f.authorID, "creator": testUserID, "assignee": f.assigneeID} {
		if rows := inboxRowsOnIssue(t, id, f.issueID); len(rows) != 0 {
			t.Fatalf("%s: rows = %+v, want none", who, rows)
		}
	}
}

// A run that closed the issue has nothing to hand back.
func TestInbox_RunCompletedOnClosedIssueHandsNothingBack(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	f := newInboxSectionsIssue(t, "run-done-closed")
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET status = 'done' WHERE id = $1`, f.issueID); err != nil {
		t.Fatalf("close issue: %v", err)
	}

	task := seedInboxTask(t, f.issueID, f.followerID, "direct_human", "completed", "")
	publishTaskEvent(bus, protocol.EventTaskCompleted, f.issueID, task, nil)

	if rows := inboxRowsOnIssue(t, f.followerID, f.issueID); len(rows) != 0 {
		t.Fatalf("starter: rows = %+v, want none on a closed issue", rows)
	}
}

// A failed run comes back to whoever started it and to the people who answer
// for the issue; a follower gets an update. An attempt with a retry pending
// has not ended the run and reaches no one.
func TestInbox_RunFailedIsFiledByRelationOnceFinal(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	f := newInboxSectionsIssue(t, "run-failed")
	starterEmail := "inbox-sections-run-failed-starter@multica.ai"
	starterID := createNotificationMember(t, starterEmail)
	t.Cleanup(func() { cleanupTestUser(t, starterEmail) })

	task := seedInboxTask(t, f.issueID, starterID, "direct_human", "failed", "")

	publishTaskEvent(bus, protocol.EventTaskFailed, f.issueID, task, map[string]any{"retry_pending": true})
	for who, id := range map[string]string{"starter": starterID, "creator": testUserID, "follower": f.followerID} {
		if rows := inboxRowsOnIssue(t, id, f.issueID); len(rows) != 0 {
			t.Fatalf("%s: rows = %+v after an attempt that will be retried, want none", who, rows)
		}
	}

	publishTaskEvent(bus, protocol.EventTaskFailed, f.issueID, task, map[string]any{"retry_pending": false})

	// The starter does not subscribe to the issue and still hears.
	starterRows := inboxRowsOnIssue(t, starterID, f.issueID)
	wantSingleRow(t, "starter", starterRows, "task_failed", "needs_you")
	if starterRows[0].taskID != task {
		t.Fatalf("starter: row names run %q, want %q", starterRows[0].taskID, task)
	}
	wantSingleRow(t, "creator", inboxRowsOnIssue(t, testUserID, f.issueID), "task_failed", "needs_you")
	wantSingleRow(t, "assignee", inboxRowsOnIssue(t, f.assigneeID, f.issueID), "task_failed", "needs_you")
	wantSingleRow(t, "follower", inboxRowsOnIssue(t, f.followerID, f.issueID), "task_failed", "updates")
}

// A member an agent filed the issue for answers for it like its creator.
func TestInboxSection_DelegatedSubscriberAnswersForTheIssue(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	f := newInboxSectionsIssue(t, "delegated")
	delegatedEmail := "inbox-sections-delegated-owner@multica.ai"
	delegatedID := createNotificationMember(t, delegatedEmail)
	t.Cleanup(func() { cleanupTestUser(t, delegatedEmail) })
	addTestSubscriber(t, f.issueID, "member", delegatedID, "delegated")

	publishStatusChange(bus, f.issueID, "in_review", "in_progress")

	wantSingleRow(t, "delegated owner", inboxRowsOnIssue(t, delegatedID, f.issueID), "status_changed", "needs_you")
}
