package channels

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// createTestKindIssue creates an issue of the given kind with the supplied
// members as subscribers (participants) and returns it. Teardown cascades
// with the workspace / user rows created by the shared harness.
func createTestKindIssue(t *testing.T, kind string, creator pgtype.UUID, subscribers ...pgtype.UUID) db.Issue {
	t.Helper()
	ctx := context.Background()
	q := db.New(testPool)

	number, err := q.IncrementIssueCounter(ctx, testWorkspaceID)
	if err != nil {
		t.Fatalf("increment issue counter: %v", err)
	}
	issue, err := q.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: testWorkspaceID,
		Title:       "guard-" + kind,
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   creator,
		Position:    0,
		Number:      number,
		Kind:        pgtype.Text{String: kind, Valid: true},
	})
	if err != nil {
		t.Fatalf("create %s issue: %v", kind, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
	for _, m := range subscribers {
		if err := q.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
			IssueID: issue.ID, UserType: "member", UserID: m, Reason: "creator",
		}); err != nil {
			t.Fatalf("subscribe %v: %v", m, err)
		}
	}
	return issue
}

func setChannelMentionFlag(t *testing.T, cq *cerebrodb.Queries, enabled bool) {
	t.Helper()
	ctx := context.Background()
	if err := cq.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: testWorkspaceID,
		FlagKey:     channelMentionMembersOnlyFlagKey,
		Enabled:     enabled,
		Locked:      false,
	}); err != nil {
		t.Fatalf("set workspace flag: %v", err)
	}
	t.Cleanup(func() {
		_ = cq.DeleteCerebroWorkspaceFeatureFlag(context.Background(), cerebrodb.DeleteCerebroWorkspaceFeatureFlagParams{
			WorkspaceID: testWorkspaceID,
			FlagKey:     channelMentionMembersOnlyFlagKey,
		})
	})
}

// TestFilterChannelMentionRecipients_ChannelDropsNonParticipant verifies the
// core guard: with the flag ON, a channel mention keeps a participant and drops
// a non-participant. FIR-2680.
func TestFilterChannelMentionRecipients_ChannelDropsNonParticipant(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	cq := cerebrodb.New(testPool)

	member := createTestUserAndMember(t, "member-in-channel")
	outsider := createTestUserAndMember(t, "outsider-non-member")
	channel := createTestKindIssue(t, "channel", member, member)
	setChannelMentionFlag(t, cq, true)

	in := map[string]bool{uuidString(member): true, uuidString(outsider): true}
	out := FilterChannelMentionRecipients(ctx, q, cq, channel.ID, "member", uuidString(member), in)

	if !out[uuidString(member)] {
		t.Errorf("participant should be kept, got %v", out)
	}
	if out[uuidString(outsider)] {
		t.Errorf("non-participant should be dropped, got %v", out)
	}
}

// TestFilterChannelMentionRecipients_GroupDropsNonParticipant confirms kind=group
// is guarded exactly like kind=channel.
func TestFilterChannelMentionRecipients_GroupDropsNonParticipant(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	cq := cerebrodb.New(testPool)

	member := createTestUserAndMember(t, "member-in-group")
	outsider := createTestUserAndMember(t, "outsider-of-group")
	group := createTestKindIssue(t, "group", member, member)
	setChannelMentionFlag(t, cq, true)

	in := map[string]bool{uuidString(member): true, uuidString(outsider): true}
	out := FilterChannelMentionRecipients(ctx, q, cq, group.ID, "member", uuidString(member), in)

	if !out[uuidString(member)] || out[uuidString(outsider)] {
		t.Errorf("group guard failed: kept=%v", out)
	}
}

// TestFilterChannelMentionRecipients_NonChannelUnchanged verifies kind=issue is
// left untouched even with the flag ON — the guard is channel/group-only.
func TestFilterChannelMentionRecipients_NonChannelUnchanged(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	cq := cerebrodb.New(testPool)

	member := createTestUserAndMember(t, "member-issue")
	outsider := createTestUserAndMember(t, "outsider-issue")
	issue := createTestKindIssue(t, "issue", member, member)
	setChannelMentionFlag(t, cq, true)

	in := map[string]bool{uuidString(member): true, uuidString(outsider): true}
	out := FilterChannelMentionRecipients(ctx, q, cq, issue.ID, "member", uuidString(member), in)

	if !out[uuidString(member)] || !out[uuidString(outsider)] {
		t.Errorf("kind=issue must pass through unchanged, got %v", out)
	}
}

// TestFilterChannelMentionRecipients_DefaultOnDropsNonParticipant verifies the
// FIR-2680 go-live default: with NO override row for the flag, a channel mention
// of a non-participant is dropped (the flag defaults ON, matching the client
// registry default).
func TestFilterChannelMentionRecipients_DefaultOnDropsNonParticipant(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	cq := cerebrodb.New(testPool)

	member := createTestUserAndMember(t, "member-default-on")
	outsider := createTestUserAndMember(t, "outsider-default-on")
	channel := createTestKindIssue(t, "channel", member, member)
	// Deliberately no setChannelMentionFlag call — exercise the default.

	in := map[string]bool{uuidString(member): true, uuidString(outsider): true}
	out := FilterChannelMentionRecipients(ctx, q, cq, channel.ID, "member", uuidString(member), in)

	if !out[uuidString(member)] {
		t.Errorf("participant should be kept by default, got %v", out)
	}
	if out[uuidString(outsider)] {
		t.Errorf("non-participant should be dropped by default (flag defaults ON), got %v", out)
	}
}

// TestFilterChannelMentionRecipients_FlagOffUnchanged verifies an explicit OFF
// override wins over the default-ON: with the flag OFF, even a channel mention
// of a non-participant passes through.
func TestFilterChannelMentionRecipients_FlagOffUnchanged(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	cq := cerebrodb.New(testPool)

	member := createTestUserAndMember(t, "member-flagoff")
	outsider := createTestUserAndMember(t, "outsider-flagoff")
	channel := createTestKindIssue(t, "channel", member, member)
	setChannelMentionFlag(t, cq, false)

	in := map[string]bool{uuidString(member): true, uuidString(outsider): true}
	out := FilterChannelMentionRecipients(ctx, q, cq, channel.ID, "member", uuidString(member), in)

	if !out[uuidString(member)] || !out[uuidString(outsider)] {
		t.Errorf("flag OFF must pass through unchanged, got %v", out)
	}
}
