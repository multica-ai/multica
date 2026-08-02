package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// firstFixtureAgent returns an agent in the test workspace along with its
// runtime; the delegated rule only cares that the issue creator IS an agent,
// but agent_task_queue.runtime_id is NOT NULL so the task insert needs both.
func firstFixtureAgent(t *testing.T) (agentID, runtimeID string) {
	t.Helper()
	if err := testPool.QueryRow(context.Background(),
		`SELECT id::text, runtime_id::text FROM agent
		 WHERE workspace_id = $1 AND runtime_id IS NOT NULL
		 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}
	return agentID, runtimeID
}

// createAgentTaskWithOriginator inserts a queued task attributed to the given
// human, standing in for the run an agent is executing when it files an issue.
// An invalid originator models an unattributed / autopilot-rooted chain.
func createAgentTaskWithOriginator(t *testing.T, agentID, runtimeID string, originator pgtype.UUID) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	var taskID pgtype.UUID
	err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, originator_user_id, accountable_user_id)
		VALUES ($1, $2, 'queued', 0, $3, $3)
		RETURNING id
	`, agentID, runtimeID, originator).Scan(&taskID)
	if err != nil {
		t.Fatalf("create agent task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

// createAgentOriginIssue inserts an issue whose creator is an agent and whose
// provenance points at originTask — the exact row shape the ordinary agent
// `issue create` path produces (MUL-4305).
func createAgentOriginIssue(t *testing.T, agentID, originType string, originTask pgtype.UUID, parent pgtype.UUID) string {
	t.Helper()
	ctx := context.Background()
	var issueID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id,
		                   position, number, origin_type, origin_id, parent_issue_id)
		VALUES ($1, 'delegated subscriber test issue', 'todo', 'medium', 'agent', $2, 0,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1),
		        $3, $4, $5)
		RETURNING id
	`, testWorkspaceID, agentID, originType, originTask, parent).Scan(&issueID)
	if err != nil {
		t.Fatalf("create agent-origin issue: %v", err)
	}
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })
	return issueID
}

// publishAgentIssueCreated fires the event the create path publishes, so the
// test exercises the real listener chain rather than calling the rule directly.
func publishAgentIssueCreated(bus *events.Bus, issueID, agentID string) {
	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "agent",
		ActorID:     agentID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "delegated subscriber test issue",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "agent",
				CreatorID:   agentID,
			},
		},
	})
}

func subscriberReason(t *testing.T, queries *db.Queries, issueID, userType, userID string) string {
	t.Helper()
	subs, err := queries.ListIssueSubscribers(context.Background(), util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("ListIssueSubscribers: %v", err)
	}
	for _, s := range subs {
		if s.UserType == userType && util.UUIDToString(s.UserID) == userID {
			return s.Reason
		}
	}
	return ""
}

// TestDelegatedSubscribe_AgentCreatedSubIssue is MUL-5483's headline case. Every
// pre-existing auto-subscribe rule keys on ACTOR identity, so an agent-created,
// agent-assigned sub-issue ends up with a full subscriber list and zero members
// to notify. The human the run is attributed to must be subscribed instead.
func TestDelegatedSubscribe_AgentCreatedSubIssue(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, testPool)

	agentID, runtimeID := firstFixtureAgent(t)
	parentID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, parentID) })

	task := createAgentTaskWithOriginator(t, agentID, runtimeID, util.MustParseUUID(testUserID))
	issueID := createAgentOriginIssue(t, agentID, "agent_create", task, util.MustParseUUID(parentID))

	publishAgentIssueCreated(bus, issueID, agentID)

	if !isSubscribed(t, queries, issueID, "member", testUserID) {
		t.Fatal("expected the human the run is attributed to to be subscribed to the agent-created sub-issue")
	}
	if got := subscriberReason(t, queries, issueID, "member", testUserID); got != "delegated" {
		t.Fatalf("reason = %q, want delegated", got)
	}
}

// TestDelegatedSubscribe_QuickCreateKeepsDirectReason: quick create is direct
// human intent and must keep the full-notification 'creator' tier. This also
// covers the path that used to be hand-written at task completion and now
// resolves through the same shared rule.
func TestDelegatedSubscribe_QuickCreateKeepsDirectReason(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, testPool)

	agentID, runtimeID := firstFixtureAgent(t)
	task := createAgentTaskWithOriginator(t, agentID, runtimeID, util.MustParseUUID(testUserID))
	issueID := createAgentOriginIssue(t, agentID, "quick_create", task, pgtype.UUID{})

	publishAgentIssueCreated(bus, issueID, agentID)

	if got := subscriberReason(t, queries, issueID, "member", testUserID); got != "creator" {
		t.Fatalf("reason = %q, want creator — quick create must not be downgraded to the delegated tier", got)
	}
}

// TestDelegatedSubscribe_UnattributedOriginSubscribesNobody: when the origin run
// carries no human (degraded attribution), we must not invent one to notify.
func TestDelegatedSubscribe_UnattributedOriginSubscribesNobody(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, testPool)

	agentID, runtimeID := firstFixtureAgent(t)
	task := createAgentTaskWithOriginator(t, agentID, runtimeID, pgtype.UUID{})
	issueID := createAgentOriginIssue(t, agentID, "agent_create", task, pgtype.UUID{})

	publishAgentIssueCreated(bus, issueID, agentID)

	if isSubscribed(t, queries, issueID, "member", testUserID) {
		t.Fatal("an unattributed origin task must not subscribe any member")
	}
}

// TestDelegatedSubscribe_RespectsSubtreeOptOut is the reason the tombstone
// exists. Without it "stop watching this tree" is undone by the very next child
// the agent files under it: the rule fires per issue, and an agent-built tree
// keeps growing after the user has already said no.
func TestDelegatedSubscribe_RespectsSubtreeOptOut(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, testPool)

	agentID, runtimeID := firstFixtureAgent(t)
	parentID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, parentID) })

	// The user leaves the parent tree.
	if _, err := queries.UnsubscribeFromIssueSubtree(ctx, db.UnsubscribeFromIssueSubtreeParams{
		ID:       util.MustParseUUID(parentID),
		UserType: "member",
		UserID:   util.MustParseUUID(testUserID),
	}); err != nil {
		t.Fatalf("UnsubscribeFromIssueSubtree: %v", err)
	}

	// The agent then files a NEW child under that same parent.
	task := createAgentTaskWithOriginator(t, agentID, runtimeID, util.MustParseUUID(testUserID))
	childID := createAgentOriginIssue(t, agentID, "agent_create", task, util.MustParseUUID(parentID))
	publishAgentIssueCreated(bus, childID, agentID)

	if isSubscribed(t, queries, childID, "member", testUserID) {
		t.Fatal("a child created after a sub-tree opt-out must not re-subscribe the user")
	}
}

// TestUnsubscribeIsDurableAgainstAutoRules: unsubscribing used to be row
// ABSENCE, which any later auto-subscribe rule silently overwrote. It must now
// survive a rule that would otherwise re-add the same person.
func TestUnsubscribeIsDurableAgainstAutoRules(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	if _, err := queries.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
		IssueID:  util.MustParseUUID(issueID),
		UserType: "member",
		UserID:   util.MustParseUUID(testUserID),
		Reason:   "creator",
	}); err != nil {
		t.Fatalf("AddIssueSubscriber: %v", err)
	}
	if err := queries.RemoveIssueSubscriber(ctx, db.RemoveIssueSubscriberParams{
		IssueID:  util.MustParseUUID(issueID),
		UserType: "member",
		UserID:   util.MustParseUUID(testUserID),
	}); err != nil {
		t.Fatalf("RemoveIssueSubscriber: %v", err)
	}
	if isSubscribed(t, queries, issueID, "member", testUserID) {
		t.Fatal("expected the user to be unsubscribed")
	}

	// A later rule pass (commented, mentioned, delegated, …) tries to re-add.
	if _, err := queries.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
		IssueID:  util.MustParseUUID(issueID),
		UserType: "member",
		UserID:   util.MustParseUUID(testUserID),
		Reason:   "commenter",
	}); err != nil {
		t.Fatalf("AddIssueSubscriber (second pass): %v", err)
	}
	if isSubscribed(t, queries, issueID, "member", testUserID) {
		t.Fatal("an auto-subscribe rule must not resurrect an explicit unsubscribe")
	}

	// An explicit subscribe IS allowed to override the user's own opt-out.
	if err := queries.SubscribeToIssueExplicitly(ctx, db.SubscribeToIssueExplicitlyParams{
		IssueID:  util.MustParseUUID(issueID),
		UserType: "member",
		UserID:   util.MustParseUUID(testUserID),
		Reason:   "manual",
	}); err != nil {
		t.Fatalf("SubscribeToIssueExplicitly: %v", err)
	}
	if !isSubscribed(t, queries, issueID, "member", testUserID) {
		t.Fatal("an explicit subscribe must clear the opt-out tombstone")
	}
}

// TestDeliverToSubscriber_DelegatedTier pins the noise contract. The tier drops
// churn and nothing else: a child FINISHING is real signal, one per piece of work
// a reviewer has to act on. An earlier cut suppressed those in favour of a
// synthesized "whole batch finished" roll-up; that machinery is gone (see the
// MUL-5483 thread), and the tree-level signal comes from the parent's own status
// transition, which this same rule delivers.
func TestDeliverToSubscriber_DelegatedTier(t *testing.T) {
	cases := []struct {
		name        string
		reason      string
		notifType   string
		issueStatus string
		want        bool
	}{
		{"direct subscriber keeps every event", "creator", "status_changed", "in_progress", true},
		{"direct subscriber keeps comments", "assignee", "new_comment", "todo", true},

		{"delegated skips routine progress", "delegated", "status_changed", "in_progress", false},
		{"delegated skips backlog parking", "delegated", "status_changed", "backlog", false},
		{"delegated skips todo", "delegated", "status_changed", "todo", false},
		{"delegated skips comment churn", "delegated", "new_comment", "in_progress", false},
		{"delegated skips assignee churn", "delegated", "assignee_changed", "in_progress", false},
		{"delegated skips date churn", "delegated", "due_date_changed", "in_progress", false},

		{"delegated gets the review handoff", "delegated", "status_changed", "in_review", true},
		{"delegated gets completion", "delegated", "status_changed", "done", true},
		{"delegated gets cancellation", "delegated", "status_changed", "cancelled", true},
		{"delegated gets blocked", "delegated", "status_changed", "blocked", true},

		{"delegated always gets direct mentions", "delegated", "mentioned", "in_progress", true},
		{"delegated always gets failures", "delegated", "task_failed", "in_progress", true},
		{"delegated always gets agent_blocked", "delegated", "agent_blocked", "in_progress", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deliverToSubscriber(tc.reason, tc.notifType, tc.issueStatus); got != tc.want {
				t.Fatalf("deliverToSubscriber(%q, %q, %q) = %v, want %v",
					tc.reason, tc.notifType, tc.issueStatus, got, tc.want)
			}
		})
	}
}

// TestDelegatedTier_ChildCompletionDeliversExactlyOnce is the case a unit test of
// deliverToSubscriber alone cannot catch. status_changed bubbles from a child to
// the parent's subscribers, and the common shape is a human who is BOTH a direct
// subscriber of the parent and a delegate on the child. They must be told the
// child finished, and told once — not twice via the bubble, and not zero times.
func TestDelegatedTier_ChildCompletionDeliversExactlyOnce(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()

	agentID, runtimeID := firstFixtureAgent(t)
	parentID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, parentID) })

	// Direct on the parent — the human filed the epic themselves.
	if _, err := queries.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
		IssueID:  util.MustParseUUID(parentID),
		UserType: "member",
		UserID:   util.MustParseUUID(testUserID),
		Reason:   "creator",
	}); err != nil {
		t.Fatalf("subscribe parent: %v", err)
	}

	// Agent files a child; the delegated rule subscribes the human to it.
	registerSubscriberListeners(bus, testPool)
	task := createAgentTaskWithOriginator(t, agentID, runtimeID, util.MustParseUUID(testUserID))
	childID := createAgentOriginIssue(t, agentID, "agent_create", task, util.MustParseUUID(parentID))
	publishAgentIssueCreated(bus, childID, agentID)
	if got := subscriberReason(t, queries, childID, "member", testUserID); got != "delegated" {
		t.Fatalf("child subscription reason = %q, want delegated", got)
	}

	countInbox := func() int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(ctx,
			`SELECT COUNT(*) FROM inbox_item WHERE recipient_type='member' AND recipient_id=$1 AND issue_id=$2`,
			testUserID, childID,
		).Scan(&n); err != nil {
			t.Fatalf("count inbox: %v", err)
		}
		return n
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, childID)
	})

	// The new status travels as notifySubscribers' issueStatus argument, not on
	// the event. The agent actor matters: notifyIssueSubscribers skips the actor,
	// and a human-actored event would skip the recipient under test.
	agentEvent := events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "agent",
		ActorID:     agentID,
	}

	// Routine progress on the child stays suppressed for the delegate, and the
	// parent bubble must not smuggle it back in.
	notifySubscribers(ctx, queries, bus, childID, "in_progress", testWorkspaceID,
		agentEvent, nil, "status_changed", "info", "child", "", emptyDetails)
	if n := countInbox(); n != 0 {
		t.Fatalf("routine child progress reached the inbox %d time(s) — the parent bubble bypassed the tier", n)
	}

	// Completion delivers, exactly once despite two paths to the same person.
	notifySubscribers(ctx, queries, bus, childID, "in_review", testWorkspaceID,
		agentEvent, nil, "status_changed", "info", "child", "", emptyDetails)
	if n := countInbox(); n != 1 {
		t.Fatalf("expected exactly 1 inbox row for the child completion, got %d", n)
	}
}

// TestDeliverToSubscriber_UnknownReasonIsDirect: 'reason' is an open vocabulary
// (a new rule can add one without a migration touching this file). An
// unrecognized reason must fall back to FULL delivery — silently dropping a
// user's notifications is the worse failure.
func TestDeliverToSubscriber_UnknownReasonIsDirect(t *testing.T) {
	if !deliverToSubscriber("some_future_reason", "new_comment", "in_progress") {
		t.Fatal("an unrecognized subscription reason must default to full delivery")
	}
}
