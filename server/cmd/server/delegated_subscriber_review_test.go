package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Regressions for the MUL-5483 code review. Each test pins one behavior that
// the first cut got wrong.

// countSubscriberAddedEvents subscribes to the bus and counts subscriber:added
// broadcasts for a given issue, so a test can assert on what CLIENTS were told
// rather than only on what the DB holds.
func countSubscriberAddedEvents(bus *events.Bus, issueID string) *int {
	n := 0
	bus.Subscribe(protocol.EventSubscriberAdded, func(e events.Event) {
		p, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		if id, _ := p["issue_id"].(string); id == issueID {
			n++
		}
	})
	return &n
}

// TestAddSubscriber_NoBroadcastWhenTombstoneRefusesWrite covers the review's
// finding 3. AddIssueSubscriber correctly refuses to resurrect an opt-out, but
// addSubscriber used to publish subscriber:added regardless. Clients insert the
// user into their cache on that event, so the UI claimed "watching" while the
// DB said otherwise — and the user kept receiving nothing.
func TestAddSubscriber_NoBroadcastWhenTombstoneRefusesWrite(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	if _, err := queries.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
		IssueID: util.MustParseUUID(issueID), UserType: "member",
		UserID: util.MustParseUUID(testUserID), Reason: "creator",
	}); err != nil {
		t.Fatalf("seed subscriber: %v", err)
	}
	if err := queries.RemoveIssueSubscriber(ctx, db.RemoveIssueSubscriberParams{
		IssueID: util.MustParseUUID(issueID), UserType: "member",
		UserID: util.MustParseUUID(testUserID),
	}); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}

	events := countSubscriberAddedEvents(bus, issueID)
	addSubscriber(bus, queries, testWorkspaceID, issueID, "member", testUserID, "commenter")

	if isSubscribed(t, queries, issueID, "member", testUserID) {
		t.Fatal("tombstone was resurrected")
	}
	if *events != 0 {
		t.Fatalf("published %d subscriber:added event(s) for a write the DB refused — clients would show a subscription that does not exist", *events)
	}
}

// TestAddSubscriber_BroadcastsOnRealInsert is the other half: the suppression
// above must not silence legitimate subscriptions.
func TestAddSubscriber_BroadcastsOnRealInsert(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	events := countSubscriberAddedEvents(bus, issueID)
	addSubscriber(bus, queries, testWorkspaceID, issueID, "member", testUserID, "creator")

	if !isSubscribed(t, queries, issueID, "member", testUserID) {
		t.Fatal("expected the subscriber to be written")
	}
	if *events != 1 {
		t.Fatalf("expected exactly 1 subscriber:added event, got %d", *events)
	}
}

// TestDelegatedUpgradesToDirectReason covers finding 4. Once the user is
// actually participating — assigned, mentioned, or commenting — they are no
// longer a passive delegate, so the reduced delivery tier must stop applying.
// ON CONFLICT DO NOTHING pinned reason at 'delegated' forever.
func TestDelegatedUpgradesToDirectReason(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	add := func(reason string) {
		t.Helper()
		if _, err := queries.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
			IssueID: util.MustParseUUID(issueID), UserType: "member",
			UserID: util.MustParseUUID(testUserID), Reason: reason,
		}); err != nil {
			t.Fatalf("AddIssueSubscriber(%s): %v", reason, err)
		}
	}

	add("delegated")
	if got := subscriberReason(t, queries, issueID, "member", testUserID); got != "delegated" {
		t.Fatalf("reason = %q, want delegated", got)
	}

	add("commenter")
	if got := subscriberReason(t, queries, issueID, "member", testUserID); got != "commenter" {
		t.Fatalf("reason = %q after commenting, want commenter — a delegate who joins in must leave the reduced tier", got)
	}

	// A direct reason must never be downgraded back to delegated by a later
	// agent-created sibling pass.
	add("delegated")
	if got := subscriberReason(t, queries, issueID, "member", testUserID); got != "commenter" {
		t.Fatalf("reason = %q, want commenter — a direct subscription must not be downgraded", got)
	}
}

// TestUnsubscribeIssueScopeLeavesFutureChildrenAlone covers finding 2. Both menu
// items wrote an indistinguishable tombstone, and HasAncestorOptOut treated any
// ancestor tombstone as a subtree opt-out — so "unsubscribe from this issue"
// silently suppressed every child the agent would ever file underneath it. The
// two choices then differed only for descendants that already existed.
func TestUnsubscribeIssueScopeLeavesFutureChildrenAlone(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, testPool)

	agentID, runtimeID := firstFixtureAgent(t)
	parentID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, parentID) })

	if _, err := queries.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
		IssueID: util.MustParseUUID(parentID), UserType: "member",
		UserID: util.MustParseUUID(testUserID), Reason: "creator",
	}); err != nil {
		t.Fatalf("seed parent subscriber: %v", err)
	}
	// Narrow opt-out: this issue only.
	if err := queries.RemoveIssueSubscriber(ctx, db.RemoveIssueSubscriberParams{
		IssueID: util.MustParseUUID(parentID), UserType: "member",
		UserID: util.MustParseUUID(testUserID),
	}); err != nil {
		t.Fatalf("RemoveIssueSubscriber: %v", err)
	}

	task := createAgentTaskWithOriginator(t, agentID, runtimeID, util.MustParseUUID(testUserID))
	childID := createAgentOriginIssue(t, agentID, "agent_create", task, util.MustParseUUID(parentID))
	publishAgentIssueCreated(bus, childID, agentID)

	if !isSubscribed(t, queries, childID, "member", testUserID) {
		t.Fatal("leaving one issue must not suppress future children — that is what the subtree option is for")
	}
	if isSubscribed(t, queries, parentID, "member", testUserID) {
		t.Fatal("the narrow opt-out must still hold on the issue it targeted")
	}
}

// TestUnsubscribeSubtreeReportsEveryAffectedIssue covers finding 5. The SQL
// retires the whole subtree, but the handler published one subscriber:removed
// for the root only, so a client with a CHILD open kept rendering a
// subscription the server had already dropped. The query has to report every
// issue it touched for the handler to broadcast per issue.
func TestUnsubscribeSubtreeReportsEveryAffectedIssue(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	parentID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, parentID) })
	childID := createChildIssue(t, testWorkspaceID, testUserID, util.MustParseUUID(parentID))
	grandchildID := createChildIssue(t, testWorkspaceID, testUserID, util.MustParseUUID(childID))

	for _, id := range []string{parentID, childID, grandchildID} {
		if _, err := queries.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
			IssueID: util.MustParseUUID(id), UserType: "member",
			UserID: util.MustParseUUID(testUserID), Reason: "delegated",
		}); err != nil {
			t.Fatalf("seed subscriber on %s: %v", id, err)
		}
	}

	affected, err := queries.UnsubscribeFromIssueSubtree(ctx, db.UnsubscribeFromIssueSubtreeParams{
		ID: util.MustParseUUID(parentID), UserType: "member",
		UserID: util.MustParseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("UnsubscribeFromIssueSubtree: %v", err)
	}

	got := map[string]bool{}
	for _, id := range affected {
		got[util.UUIDToString(id)] = true
	}
	for _, want := range []string{parentID, childID, grandchildID} {
		if !got[want] {
			t.Fatalf("subtree unsubscribe did not report issue %s; clients with it open keep a stale subscription", want)
		}
		if isSubscribed(t, queries, want, "member", testUserID) {
			t.Fatalf("issue %s is still subscribed after a subtree unsubscribe", want)
		}
	}
}

// createChildIssue inserts an issue under the given parent.
func createChildIssue(t *testing.T, workspaceID, creatorID string, parent pgtype.UUID) string {
	t.Helper()
	var issueID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id,
		                   position, number, parent_issue_id)
		VALUES ($1, 'child issue', 'todo', 'medium', 'member', $2, 0,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1), $3)
		RETURNING id
	`, workspaceID, creatorID, parent).Scan(&issueID)
	if err != nil {
		t.Fatalf("createChildIssue: %v", err)
	}
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })
	return issueID
}
