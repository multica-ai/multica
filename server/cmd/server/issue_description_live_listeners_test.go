package main

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func publishDescriptionChange(bus *events.Bus, issueID, actorID, prev, next string, extra map[string]any) {
	payload := map[string]any{
		"issue": handler.IssueResponse{
			ID:          issueID,
			WorkspaceID: testWorkspaceID,
			Title:       "live description",
			Status:      "todo",
			Priority:    "none",
			CreatorType: "member",
			CreatorID:   testUserID,
			Description: &next,
		},
		"description_changed": true,
		"prev_description":    &prev,
	}
	for k, v := range extra {
		payload[k] = v
	}
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     actorID,
		Payload:     payload,
	})
}

func TestActivityLiveDescriptionSessionIsOneEntryPerEditor(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, issueID)
		cleanupTestIssue(t, issueID)
	})
	bobEmail := "live-desc-bob@multica.ai"
	bob := createNotificationMember(t, bobEmail)
	t.Cleanup(func() { cleanupTestUser(t, bobEmail) })

	me := []handler.DocContributor{{Type: "member", ID: testUserID}}
	both := []handler.DocContributor{{Type: "member", ID: testUserID}, {Type: "member", ID: bob}}
	for i, editors := range [][]handler.DocContributor{me, me, both, me} {
		publishDescriptionChange(bus, issueID, testUserID, "v", "v"+string(rune('a'+i)), map[string]any{
			"description_live_editors": editors,
		})
	}
	activities := listActivitiesForIssue(t, queries, issueID)
	if len(activities) != 2 {
		t.Fatalf("a live session must record one entry per editor, got %d", len(activities))
	}
	actors := map[string]bool{}
	for _, a := range activities {
		var details map[string]string
		_ = json.Unmarshal(a.Details, &details)
		if a.Action != "description_updated" || details["live"] != "true" {
			t.Fatalf("unexpected activity %+v (%s)", a, a.Details)
		}
		actors[util.UUIDToString(a.ActorID)] = true
	}
	if len(actors) != 2 {
		t.Fatalf("entries must credit each editor: %v", actors)
	}

	// A write outside a live session is recorded as it always was.
	publishDescriptionChange(bus, issueID, testUserID, "v", "agent", nil)
	if got := len(listActivitiesForIssue(t, queries, issueID)); got != 3 {
		t.Fatalf("non-live description change must add an entry, got %d", got)
	}
}

func TestNotificationLiveDescriptionMentionNotifiesOnce(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	mentionedEmail := "live-desc-mentioned@multica.ai"
	mentioned := createNotificationMember(t, mentionedEmail)
	t.Cleanup(func() { cleanupTestUser(t, mentionedEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	key := "member:" + mentioned
	withMention := "Hi [@M](mention://member/" + mentioned + ")"
	live := []handler.DocContributor{{Type: "member", ID: testUserID}}
	// Typed: the first save carrying the mention notifies.
	publishDescriptionChange(bus, issueID, testUserID, "Hi", withMention, map[string]any{
		"description_live_editors": live, "description_seen_mentions": []string{},
	})
	// Deleted and typed again in the same session: already seen, so quiet.
	publishDescriptionChange(bus, issueID, testUserID, withMention, "Hi", map[string]any{
		"description_live_editors": live, "description_seen_mentions": []string{key},
	})
	publishDescriptionChange(bus, issueID, testUserID, "Hi", withMention, map[string]any{
		"description_live_editors": live, "description_seen_mentions": []string{key},
	})

	items := inboxItemsForRecipient(t, queries, mentioned)
	mentions := 0
	for _, item := range items {
		if item.Type == "mentioned" {
			mentions++
		}
	}
	if mentions != 1 {
		t.Fatalf("a mention typed in one live session must notify once, got %d (%d items)", mentions, len(items))
	}
}
