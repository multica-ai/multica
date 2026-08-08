package main

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Inbox events are personal state. Every one of them must reach only its
// recipient — never the workspace room, which every member of the workspace is
// subscribed to.
//
// inbox:unread was the one that leaked: it was absent from both personalEvents
// and the targeted-subscriber list, so SubscribeAll fanned it out to the whole
// workspace. Marking a single notification unread therefore pushed that
// person's item_id and recipient_id to every connected colleague and made all
// of their clients invalidate an inbox that had not changed.
func TestRegisterListeners_InboxEventsNeverReachTheWorkspaceRoom(t *testing.T) {
	const (
		workspaceID = "11111111-1111-4111-8111-111111111111"
		recipientID = "22222222-2222-4222-8222-222222222222"
		itemID      = "33333333-3333-4333-8333-333333333333"
	)

	cases := []struct {
		name    string
		event   string
		payload map[string]any
	}{
		{"inbox:new", protocol.EventInboxNew, map[string]any{
			"item": map[string]any{"id": itemID, "recipient_id": recipientID},
		}},
		{"inbox:read", protocol.EventInboxRead, map[string]any{
			"item_id": itemID, "recipient_id": recipientID,
		}},
		{"inbox:unread", protocol.EventInboxUnread, map[string]any{
			"item_id": itemID, "recipient_id": recipientID,
		}},
		{"inbox:archived", protocol.EventInboxArchived, map[string]any{
			"item_id": itemID, "recipient_id": recipientID,
		}},
		{"inbox:unarchived", protocol.EventInboxUnarchived, map[string]any{
			"item_id": itemID, "recipient_id": recipientID,
		}},
		{"inbox:batch-read", protocol.EventInboxBatchRead, map[string]any{
			"recipient_id": recipientID, "count": 3,
		}},
		{"inbox:batch-archived", protocol.EventInboxBatchArchived, map[string]any{
			"recipient_id": recipientID, "count": 3,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus := events.New()
			fake := &fakeBroadcaster{}
			registerListeners(bus, fake)

			bus.Publish(events.Event{
				Type:        tc.event,
				WorkspaceID: workspaceID,
				ActorType:   "member",
				ActorID:     recipientID,
				Payload:     tc.payload,
			})

			if len(fake.workspaceCalls) != 0 {
				t.Fatalf("%s was broadcast to the workspace room — it is personal state", tc.event)
			}
			if len(fake.scopeCalls) != 0 {
				t.Fatalf("%s was broadcast to a scope room", tc.event)
			}
			if len(fake.userCalls) != 1 {
				t.Fatalf("%s reached %d users, want exactly the recipient", tc.event, len(fake.userCalls))
			}
			if got := fake.userCalls[0].userID; got != recipientID {
				t.Fatalf("%s was sent to %q, want the recipient %q", tc.event, got, recipientID)
			}

			// The recipient must still receive a well-formed frame — silencing
			// the leak by dropping the event entirely would be its own bug.
			var frame map[string]any
			if err := json.Unmarshal(fake.userCalls[0].msg, &frame); err != nil {
				t.Fatalf("%s frame is not valid JSON: %v", tc.event, err)
			}
			if frame["type"] != tc.event {
				t.Fatalf("frame type = %v, want %s", frame["type"], tc.event)
			}
		})
	}
}

// A missing recipient must drop the event rather than fall through to the
// workspace fanout: an inbox event with nobody to deliver it to is a producer
// bug, and broadcasting it would be the same leak by another route.
func TestRegisterListeners_InboxEventWithoutRecipientIsDropped(t *testing.T) {
	bus := events.New()
	fake := &fakeBroadcaster{}
	registerListeners(bus, fake)

	bus.Publish(events.Event{
		Type:        protocol.EventInboxUnread,
		WorkspaceID: "11111111-1111-4111-8111-111111111111",
		ActorType:   "member",
		Payload:     map[string]any{"item_id": "33333333-3333-4333-8333-333333333333"},
	})

	if len(fake.userCalls) != 0 || len(fake.workspaceCalls) != 0 || len(fake.scopeCalls) != 0 {
		t.Fatalf("an inbox event with no recipient must be dropped, got user=%d workspace=%d scope=%d",
			len(fake.userCalls), len(fake.workspaceCalls), len(fake.scopeCalls))
	}
}
