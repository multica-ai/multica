package main

import (
	"context"
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// createTestKindIssueMain inserts an issue of the given kind into the shared
// notification test workspace and returns its UUID string.
func createTestKindIssueMain(t *testing.T, kind, creatorID string) string {
	t.Helper()
	ctx := context.Background()
	var issueID string
	err := testPool.QueryRow(ctx, `
		WITH bump AS (
			UPDATE workspace SET issue_counter = issue_counter + 1
			WHERE id = $1 RETURNING issue_counter
		)
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, kind)
		SELECT $1, 'channel guard test', 'todo', 'medium', 'member', $2, 0, issue_counter, $3 FROM bump
		RETURNING id
	`, testWorkspaceID, creatorID, kind).Scan(&issueID)
	if err != nil {
		t.Fatalf("createTestKindIssueMain(%s): %v", kind, err)
	}
	return issueID
}

// setChannelMentionGuardFlag flips the FIR-2680 workspace flag and, when
// enabled, binds the real guard to the notification hook. It restores the
// identity hook and clears the flag on cleanup so other tests are unaffected.
func setChannelMentionGuardFlag(t *testing.T, queries *db.Queries, enabled bool) {
	t.Helper()
	ctx := context.Background()
	cq := cerebrodb.New(testPool)
	if err := cq.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
		FlagKey:     "cerebro_channel_mention_members_only",
		Enabled:     enabled,
		Locked:      false,
	}); err != nil {
		t.Fatalf("set channel mention flag: %v", err)
	}
	setChannelMentionGuard(testPool, cq, queries)
	t.Cleanup(func() {
		filterChannelMentionRecipients = func(_ context.Context, _ events.Event, _ string, ids map[string]bool) map[string]bool {
			return ids
		}
		_ = cq.DeleteCerebroWorkspaceFeatureFlag(context.Background(), cerebrodb.DeleteCerebroWorkspaceFeatureFlagParams{
			WorkspaceID: util.MustParseUUID(testWorkspaceID),
			FlagKey:     "cerebro_channel_mention_members_only",
		})
	})
}

func publishChannelMention(t *testing.T, bus *events.Bus, issueID, actorID string, mentionedIDs ...string) {
	t.Helper()
	content := "hello"
	for _, id := range mentionedIDs {
		content += " [@user](mention://member/" + id + ")"
	}
	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     actorID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         "00000000-0000-0000-0000-000000000000",
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   actorID,
				Content:    stringPtr(content),
				Type:       "comment",
			},
			"issue_title":  "channel guard test",
			"issue_status": "todo",
		},
	})
}

// TestChannelMentionGuard_DropsNonParticipant is the end-to-end proof for
// FIR-2680: on a channel, with the flag ON, mentioning a non-participant
// creates NO "mentioned" inbox row, while a participant still gets one.
func TestChannelMentionGuard_DropsNonParticipant(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	participantEmail := "chan-guard-participant@multica.ai"
	participantID := createTestUser(t, participantEmail)
	t.Cleanup(func() { cleanupTestUser(t, participantEmail) })
	outsiderEmail := "chan-guard-outsider@multica.ai"
	outsiderID := createTestUser(t, outsiderEmail)
	t.Cleanup(func() { cleanupTestUser(t, outsiderEmail) })

	channelID := createTestKindIssueMain(t, "channel", testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, channelID)
		cleanupTestIssue(t, channelID)
	})
	addTestSubscriber(t, channelID, "member", participantID, "manual")

	setChannelMentionGuardFlag(t, queries, true)
	publishChannelMention(t, bus, channelID, testUserID, participantID, outsiderID)

	if active, _ := countInboxByTypeForRecipient(t, outsiderID, "mentioned"); active != 0 {
		t.Fatalf("non-participant must get NO mentioned row with guard ON, got %d", active)
	}
	if active, _ := countInboxByTypeForRecipient(t, participantID, "mentioned"); active == 0 {
		t.Fatalf("participant must still get a mentioned row, got 0")
	}
}

// TestChannelMentionGuard_KindIssueUnchanged verifies the guard is scoped to
// channels/groups: on a plain issue, a non-subscriber mention still notifies.
func TestChannelMentionGuard_KindIssueUnchanged(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	outsiderEmail := "issue-guard-outsider@multica.ai"
	outsiderID := createTestUser(t, outsiderEmail)
	t.Cleanup(func() { cleanupTestUser(t, outsiderEmail) })

	issueID := createTestKindIssueMain(t, "issue", testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	setChannelMentionGuardFlag(t, queries, true)
	publishChannelMention(t, bus, issueID, testUserID, outsiderID)

	if active, _ := countInboxByTypeForRecipient(t, outsiderID, "mentioned"); active == 0 {
		t.Fatalf("kind=issue mention must still notify a non-subscriber, got 0")
	}
}

// TestChannelMentionGuard_FlagOffUnchanged verifies back-compat: with the flag
// OFF, a channel mention of a non-participant still notifies them.
func TestChannelMentionGuard_FlagOffUnchanged(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	outsiderEmail := "chan-guard-flagoff@multica.ai"
	outsiderID := createTestUser(t, outsiderEmail)
	t.Cleanup(func() { cleanupTestUser(t, outsiderEmail) })

	channelID := createTestKindIssueMain(t, "channel", testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, channelID)
		cleanupTestIssue(t, channelID)
	})

	setChannelMentionGuardFlag(t, queries, false)
	publishChannelMention(t, bus, channelID, testUserID, outsiderID)

	if active, _ := countInboxByTypeForRecipient(t, outsiderID, "mentioned"); active == 0 {
		t.Fatalf("flag OFF: channel mention must still notify (back-compat), got 0")
	}
}
