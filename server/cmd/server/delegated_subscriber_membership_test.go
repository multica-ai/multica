package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// addWorkspaceMember joins a user to the test workspace and returns a function
// that revokes that membership, modelling the workspace_revoke path without
// dragging its runtime teardown into a subscriber test.
func addWorkspaceMember(t *testing.T, userID string) (revoke func()) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, userID,
	); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}
	revoked := false
	revoke = func() {
		if revoked {
			return
		}
		revoked = true
		if _, err := testPool.Exec(context.Background(),
			`DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`,
			testWorkspaceID, userID,
		); err != nil {
			t.Fatalf("revoke workspace member: %v", err)
		}
	}
	t.Cleanup(revoke)
	return revoke
}

// TestDelegatedSubscribe_SkipsOriginatorRemovedFromWorkspace covers review
// round 6 finding 2.
//
// originator_user_id is stamped when the task is QUEUED and never revisited.
// GetAgentTaskInWorkspace proves the task's agent still belongs to the
// workspace, which is a different claim from "the human does". A member can be
// revoked while their tasks keep running on a shared runtime, and every child
// those tasks file afterwards would write a subscriber row for a non-member:
// a ghost watcher accumulating inbox rows, and restored history if they ever
// rejoin. Membership has to be re-checked at write time.
func TestDelegatedSubscribe_SkipsOriginatorRemovedFromWorkspace(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, testPool)

	const email = "delegated-revoked-originator@multica.test"
	cleanupTestUser(t, email)
	userID := createTestUser(t, email)
	t.Cleanup(func() { cleanupTestUser(t, email) })

	// Member at the moment the run is queued — this is what stamps the task.
	revoke := addWorkspaceMember(t, userID)

	agentID, runtimeID := firstFixtureAgent(t)
	task := createAgentTaskWithOriginator(t, agentID, runtimeID, util.MustParseUUID(userID))

	// Revoked mid-run. The task survives and keeps decomposing.
	revoke()

	issueID := createAgentOriginIssue(t, agentID, "agent_create", task, pgtype.UUID{})
	publishAgentIssueCreated(bus, issueID, agentID)

	if isSubscribed(t, queries, issueID, "member", userID) {
		t.Fatal("a revoked member must not be auto-subscribed by the delegated rule: " +
			"the subscription is a ghost that keeps accruing inbox rows outside the workspace's membership boundary")
	}
}

// TestDelegatedSubscribe_SubscribesOriginatorStillInWorkspace is the positive
// control for the test above. Without it, a membership check that rejected
// EVERYONE would still pass the negative case.
func TestDelegatedSubscribe_SubscribesOriginatorStillInWorkspace(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, testPool)

	const email = "delegated-active-originator@multica.test"
	cleanupTestUser(t, email)
	userID := createTestUser(t, email)
	t.Cleanup(func() { cleanupTestUser(t, email) })

	addWorkspaceMember(t, userID)

	agentID, runtimeID := firstFixtureAgent(t)
	task := createAgentTaskWithOriginator(t, agentID, runtimeID, util.MustParseUUID(userID))
	issueID := createAgentOriginIssue(t, agentID, "agent_create", task, pgtype.UUID{})

	publishAgentIssueCreated(bus, issueID, agentID)

	if !isSubscribed(t, queries, issueID, "member", userID) {
		t.Fatal("an originator who is still a workspace member must be subscribed")
	}
	if got := subscriberReason(t, queries, issueID, "member", userID); got != "delegated" {
		t.Fatalf("reason = %q, want delegated", got)
	}
}
