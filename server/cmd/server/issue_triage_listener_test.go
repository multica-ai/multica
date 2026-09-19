package main

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Triage entries neither subscribe nor notify (MUL-7189 §2.5).
//
// Every case runs the same event against the same two issues: one in Triage and
// one not, both `status = todo`, differing in that single column. The ordinary
// issue is the positive control — an event that produced nothing on BOTH would
// otherwise pass for the wrong reason.

// triageListenerFixture is a pair of issues and a member who would be notified
// about either of them.
type triageListenerFixture struct {
	ordinary string
	inTriage string
	watcher  string
}

const triageListenerWatcherEmail = "triage-listener-watcher@multica.ai"

func newTriageListenerFixture(t *testing.T) triageListenerFixture {
	t.Helper()
	watcher := createTestUser(t, triageListenerWatcherEmail)
	t.Cleanup(func() { cleanupTestUser(t, triageListenerWatcherEmail) })
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
		ON CONFLICT DO NOTHING
	`, testWorkspaceID, watcher); err != nil {
		t.Fatalf("add watcher to workspace: %v", err)
	}

	f := triageListenerFixture{
		ordinary: createTestIssue(t, testWorkspaceID, testUserID),
		inTriage: createTestIssue(t, testWorkspaceID, testUserID),
		watcher:  watcher,
	}
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET triage_state = 'pending' WHERE id = $1`, f.inTriage); err != nil {
		t.Fatalf("mark issue as a Triage entry: %v", err)
	}
	t.Cleanup(func() {
		for _, id := range []string{f.ordinary, f.inTriage} {
			cleanupInboxForIssue(t, id)
			testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, id)
			cleanupTestIssue(t, id)
		}
	})
	return f
}

func (f triageListenerFixture) issue(id string) handler.IssueResponse {
	description := "ping [watcher](mention://member/" + f.watcher + ")"
	assigneeType := "member"
	return handler.IssueResponse{
		ID:            id,
		WorkspaceID:   testWorkspaceID,
		Title:         "triage listener fixture",
		Status:        "todo",
		Priority:      "medium",
		CreatorType:   "member",
		CreatorID:     testUserID,
		Description:   &description,
		AssigneeType:  &assigneeType,
		AssigneeID:    &f.watcher,
		ParentIssueID: nil,
	}
}

func inboxCountForIssue(t *testing.T, issueID string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM inbox_item WHERE issue_id = $1`, issueID).Scan(&count); err != nil {
		t.Fatalf("count inbox items: %v", err)
	}
	return count
}

func inboxTypesFor(t *testing.T, queries *db.Queries, recipientID string) map[string]int {
	t.Helper()
	types := map[string]int{}
	for _, item := range inboxItemsForRecipient(t, queries, recipientID) {
		types[item.Type]++
	}
	return types
}

// TestTriageEntryCreatesNoSubscribersOrInbox covers the two listeners at once:
// the subscriber rows and the inbox rows an ordinary issue would produce.
func TestTriageEntryCreatesNoSubscribersOrInbox(t *testing.T) {
	queries := db.New(testPool)
	f := newTriageListenerFixture(t)
	bus := newNotificationBus(t, queries)

	publish := func(issueID string) {
		bus.Publish(events.Event{
			Type:        protocol.EventIssueCreated,
			WorkspaceID: testWorkspaceID,
			ActorType:   "member",
			ActorID:     testUserID,
			Payload:     map[string]any{"issue": f.issue(issueID)},
		})
	}

	publish(f.ordinary)
	if subscriberCount(t, queries, f.ordinary) == 0 {
		t.Fatal("issue:created on an ordinary issue subscribed nobody — the fixture cannot prove anything")
	}
	if inboxCountForIssue(t, f.ordinary) == 0 {
		t.Fatal("issue:created on an ordinary issue notified nobody — the fixture cannot prove anything")
	}

	publish(f.inTriage)
	if count := subscriberCount(t, queries, f.inTriage); count != 0 {
		t.Fatalf("issue:created on a Triage entry added %d subscribers, want 0", count)
	}
	if count := inboxCountForIssue(t, f.inTriage); count != 0 {
		t.Fatalf("issue:created on a Triage entry wrote %d inbox rows, want 0", count)
	}
}

func TestTriageEntryUpdatesAndCommentsStaySilent(t *testing.T) {
	queries := db.New(testPool)
	f := newTriageListenerFixture(t)
	bus := newNotificationBus(t, queries)

	// Someone must be subscribed for an update to have anyone to notify; a
	// Triage entry could have picked one up before this rule existed.
	addTestSubscriber(t, f.inTriage, "member", f.watcher, "creator")

	prevAssignee := "member"
	prevID := testUserID
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue":              f.issue(f.inTriage),
			"assignee_changed":   true,
			"status_changed":     true,
			"prev_assignee_type": &prevAssignee,
			"prev_assignee_id":   &prevID,
			"prev_status":        "backlog",
		},
	})

	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         util.UUIDToString(util.MustParseUUID(f.ordinary)),
				IssueID:    f.inTriage,
				AuthorType: "member",
				AuthorID:   testUserID,
				Content:    "hello [watcher](mention://member/" + f.watcher + ")",
				Type:       "comment",
			},
			"issue_title":  "triage listener fixture",
			"issue_status": "todo",
		},
	})

	if count := inboxCountForIssue(t, f.inTriage); count != 0 {
		t.Fatalf("a Triage entry's update and comment wrote %d inbox rows, want 0", count)
	}
	if isSubscribed(t, queries, f.inTriage, "member", testUserID) {
		t.Fatal("commenting on a Triage entry subscribed the commenter")
	}
}

// accept is the seam: the entry enters the workspace for the first time, so the
// creation rules run even though no field the update listener looks at changed.
func TestAcceptFromTriageSubscribesAndNotifiesTheAssignee(t *testing.T) {
	queries := db.New(testPool)
	f := newTriageListenerFixture(t)
	bus := newNotificationBus(t, queries)

	// accept has already cleared the column by the time the event is published.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET triage_state = NULL WHERE id = $1`, f.inTriage); err != nil {
		t.Fatalf("clear triage_state: %v", err)
	}

	// The assignee is unchanged — the triager pre-filled it — and neither the
	// status nor the description moved. Every flag the update listener keys on
	// is absent on purpose.
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue":                f.issue(f.inTriage),
			"accepted_from_triage": true,
		},
	})

	if !isSubscribed(t, queries, f.inTriage, "member", testUserID) {
		t.Fatal("accept did not subscribe the creator")
	}
	if !isSubscribed(t, queries, f.inTriage, "member", f.watcher) {
		t.Fatal("accept did not subscribe the assignee")
	}

	types := inboxTypesFor(t, queries, f.watcher)
	if types["issue_assigned"] == 0 {
		t.Fatalf("accept sent the assignee no issue_assigned notification; got %v", types)
	}
	if types["unassigned"] > 0 {
		t.Fatalf("accept read the unchanged assignee as an unassignment; got %v", types)
	}
	if types["status_changed"] > 0 {
		t.Fatalf("accept reported a status transition that did not happen; got %v", types)
	}
}

// A triage decision names people to explain itself. It must reach none of them.
func TestTriageDecisionCommentNotifiesAndSubscribesNobody(t *testing.T) {
	queries := db.New(testPool)
	f := newTriageListenerFixture(t)
	bus := newNotificationBus(t, queries)

	// On an ORDINARY issue, so the only thing that can suppress it is its type.
	addTestSubscriber(t, f.ordinary, "member", f.watcher, "creator")

	publish := func(commentType string) {
		bus.Publish(events.Event{
			Type:        protocol.EventCommentCreated,
			WorkspaceID: testWorkspaceID,
			ActorType:   "member",
			ActorID:     testUserID,
			Payload: map[string]any{
				"comment": handler.CommentResponse{
					IssueID:    f.ordinary,
					AuthorType: "member",
					AuthorID:   testUserID,
					Content:    "duplicate of what [watcher](mention://member/" + f.watcher + ") filed",
					Type:       commentType,
				},
				"issue_title":  "triage listener fixture",
				"issue_status": "todo",
			},
		})
	}

	publish(handler.CommentTypeTriageDecision)
	if count := inboxCountForIssue(t, f.ordinary); count != 0 {
		t.Fatalf("a triage decision wrote %d inbox rows, want 0", count)
	}
	if isSubscribed(t, queries, f.ordinary, "member", testUserID) {
		t.Fatal("a triage decision subscribed its author")
	}

	// The control: the same body as an ordinary comment does all of it.
	publish("comment")
	if count := inboxCountForIssue(t, f.ordinary); count == 0 {
		t.Fatal("the same comment posted as an ordinary comment notified nobody — the suppression above proves nothing")
	}
	if !isSubscribed(t, queries, f.ordinary, "member", testUserID) {
		t.Fatal("an ordinary comment did not subscribe its author — the suppression above proves nothing")
	}
}
