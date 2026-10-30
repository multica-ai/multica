package main

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Concurrency regressions for MUL-5483 review round 7.
//
// The earlier cut checked membership and ancestor opt-out in their own round
// trips and then inserted in another, so a competing request could invalidate
// either answer in between. These tests drive that window deliberately: a
// competing transaction takes the shared (workspace, user) lock and performs
// its write, and the delegated rule is fired while that transaction is still
// OPEN. Each test asserts two things — that the rule blocks rather than reading
// stale state, and that it reaches the correct decision once released.
//
// The two races are closed by different mechanisms, and the tests were checked
// against each one individually:
//
//   - the subtree opt-out race needs the ADVISORY LOCK. Its tombstone can land
//     on an issue the rule never reads, and the child being subscribed did not
//     exist when the opt-out was written, so no row lock reaches it. Removing
//     LockSubscriberWrites makes TestDelegatedSubscribe_BlocksOnConcurrentSubtreeOptOut
//     fail — the rule reads the pre-tombstone snapshot and subscribes.
//   - the member revoke race is closed by the FOR SHARE in AddDelegatedSubscriber,
//     which is why its test still passes with the advisory lock removed. The
//     lock is what additionally orders the rule against revoke's subscription
//     cleanup, so a row inserted just before a revoke cannot outlive it.

// blockedTx holds the serialization lock and whatever write the scenario needs,
// without committing. Call release() to commit and let the waiter through.
func blockedTx(t *testing.T, userID string, write func(*db.Queries) error) (release func()) {
	t.Helper()
	ctx := context.Background()

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin competing tx: %v", err)
	}
	committed := false
	t.Cleanup(func() {
		if !committed {
			tx.Rollback(context.Background())
		}
	})

	qtx := db.New(testPool).WithTx(tx)
	if err := qtx.LockSubscriberWrites(ctx, db.LockSubscriberWritesParams{
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
		UserID:      util.MustParseUUID(userID),
	}); err != nil {
		t.Fatalf("competing tx lock: %v", err)
	}
	if err := write(qtx); err != nil {
		t.Fatalf("competing tx write: %v", err)
	}

	return func() {
		if err := tx.Commit(context.Background()); err != nil {
			t.Fatalf("commit competing tx: %v", err)
		}
		committed = true
	}
}

// fireDelegatedRule runs the listener chain in the background and reports when
// it finished, so a test can assert on whether it is blocked.
func fireDelegatedRule(bus *events.Bus, issueID, agentID string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		publishAgentIssueCreated(bus, issueID, agentID)
	}()
	return done
}

func assertBlocked(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
		t.Fatalf("%s completed while a competing transaction still held the serialization lock: "+
			"the eligibility check read a snapshot that was about to become false", what)
	case <-time.After(400 * time.Millisecond):
	}
}

func assertFinishes(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never completed after the competing transaction committed", what)
	}
}

// TestDelegatedSubscribe_BlocksOnConcurrentSubtreeOptOut is the escape-hatch
// race. A user unsubscribes from a tree while an agent is still decomposing it;
// the child the agent files next did not exist when the tombstone was written,
// so there is no row a row-lock could have protected. If the rule reads the
// pre-tombstone snapshot, the user silently becomes a watcher again on exactly
// the tree they just left.
func TestDelegatedSubscribe_BlocksOnConcurrentSubtreeOptOut(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, testPool)

	agentID, runtimeID := firstFixtureAgent(t)
	parentID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, parentID) })

	task := createAgentTaskWithOriginator(t, agentID, runtimeID, util.MustParseUUID(testUserID))

	// The user leaves the whole tree. Still uncommitted.
	release := blockedTx(t, testUserID, func(qtx *db.Queries) error {
		_, err := qtx.UnsubscribeFromIssueSubtree(context.Background(), db.UnsubscribeFromIssueSubtreeParams{
			ID:       util.MustParseUUID(parentID),
			UserType: "member",
			UserID:   util.MustParseUUID(testUserID),
		})
		return err
	})

	// The agent files a NEW child under that tree, after the opt-out was
	// written but before it committed.
	childID := createAgentOriginIssue(t, agentID, "agent_create", task, util.MustParseUUID(parentID))

	done := fireDelegatedRule(bus, childID, agentID)
	assertBlocked(t, done, "delegated subscribe")

	release()
	assertFinishes(t, done, "delegated subscribe")

	if isSubscribed(t, queries, childID, "member", testUserID) {
		t.Fatal("a child filed during a concurrent subtree unsubscribe must not resurrect the subscription: " +
			"this is the escape hatch the feature depends on")
	}
}

// TestDelegatedSubscribe_BlocksOnConcurrentMemberRevoke is the membership race.
// Round 6 moved the membership check to write time, but the check and the
// insert were still two statements, so a revoke landing between them produced
// exactly the ghost subscriber the check existed to prevent.
func TestDelegatedSubscribe_BlocksOnConcurrentMemberRevoke(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, testPool)

	const email = "delegated-concurrent-revoke@multica.test"
	cleanupTestUser(t, email)
	userID := createTestUser(t, email)
	t.Cleanup(func() { cleanupTestUser(t, email) })
	addWorkspaceMember(t, userID)

	agentID, runtimeID := firstFixtureAgent(t)
	task := createAgentTaskWithOriginator(t, agentID, runtimeID, util.MustParseUUID(userID))

	// The member is being revoked. Still uncommitted.
	release := blockedTx(t, userID, func(qtx *db.Queries) error {
		return revokeMemberInTx(qtx, userID)
	})

	issueID := createAgentOriginIssue(t, agentID, "agent_create", task, util.MustParseUUID(createTestIssue(t, testWorkspaceID, testUserID)))

	done := fireDelegatedRule(bus, issueID, agentID)
	assertBlocked(t, done, "delegated subscribe")

	release()
	assertFinishes(t, done, "delegated subscribe")

	if isSubscribed(t, queries, issueID, "member", userID) {
		t.Fatal("a member revoked concurrently with the delegated rule must not end up subscribed")
	}
}

// revokeMemberInTx mirrors what revokeAndRemoveMember does to subscriber state,
// through the transaction the competing writer holds, so the member row the
// rule's FOR SHARE would observe is deleted but not yet committed.
func revokeMemberInTx(qtx *db.Queries, userID string) error {
	ctx := context.Background()
	if err := qtx.DeleteSubscriptionsByMember(ctx, db.DeleteSubscriptionsByMemberParams{
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
		UserID:      util.MustParseUUID(userID),
	}); err != nil {
		return err
	}
	member, err := qtx.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.MustParseUUID(userID),
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		return err
	}
	return qtx.DeleteMember(ctx, member.ID)
}
