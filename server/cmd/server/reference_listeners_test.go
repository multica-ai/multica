package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestReferenceChannelMessageMentionRecordsActivity pins the activity details
// contract that issue-detail.tsx renders against: a channel message that
// @-mentions an issue must create a "referenced_by" activity whose details
// carry the channel name + id and the originating message id, so the Issue
// timeline can name the channel and deep-link to the message.
func TestReferenceChannelMessageMentionRecordsActivity(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerReferenceListeners(bus, queries)

	targetIssueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, targetIssueID)
		cleanupTestIssue(t, targetIssueID)
	})

	ctx := context.Background()
	ch, err := queries.CreateChannel(ctx, db.CreateChannelParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Name:        "Ref Mention Channel",
		Slug:        "ref-mention-" + targetIssueID[:8],
		Description: "",
		AccessMode:  "open",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel WHERE id = $1`, ch.ID)
	})

	messageID := uuid.NewString()
	bus.Publish(events.Event{
		Type:        protocol.EventChannelMessageCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"channel_id": ch.ID.String(),
			"message": map[string]any{
				"id":      messageID,
				"content": fmt.Sprintf("[Issue](mention://issue/%s)", targetIssueID),
			},
		},
	})

	activities := listActivitiesForIssue(t, queries, targetIssueID)
	if len(activities) != 1 {
		t.Fatalf("expected 1 referenced_by activity, got %d", len(activities))
	}
	a := activities[0]
	if a.Action != "referenced_by" {
		t.Fatalf("expected action 'referenced_by', got %q", a.Action)
	}
	var details map[string]string
	if err := json.Unmarshal(a.Details, &details); err != nil {
		t.Fatalf("unmarshal details: %v", err)
	}
	if got := details["source_type"]; got != "channel_message" {
		t.Errorf("details.source_type = %q, want %q", got, "channel_message")
	}
	if got := details["source_channel_id"]; got != ch.ID.String() {
		t.Errorf("details.source_channel_id = %q, want %q", got, ch.ID.String())
	}
	if got := details["source_channel_name"]; got != ch.Name {
		t.Errorf("details.source_channel_name = %q, want %q", got, ch.Name)
	}
	if got := details["source_id"]; got != messageID {
		t.Errorf("details.source_id = %q, want %q", got, messageID)
	}
}
