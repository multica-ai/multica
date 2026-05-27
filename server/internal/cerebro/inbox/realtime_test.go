package inbox

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
)

// FIR-2394 — inbox metadata mutations (mute/unmute/mark-unread/unarchive) must
// fan out a WS event so other sessions refresh their unread count in realtime
// instead of going stale until a manual refresh.

func uuidVal(t *testing.T) pgtype.UUID {
	t.Helper()
	return pgtype.UUID{Bytes: uuid.New(), Valid: true}
}

func TestPublishInboxEvent_EmitsWorkspaceScopedEvent(t *testing.T) {
	bus := events.New()
	var got events.Event
	hits := 0
	bus.Subscribe(eventInboxUnread, func(e events.Event) {
		got = e
		hits++
	})

	h := New(nil, nil, bus)
	item := cerebrodb.InboxItem{
		ID:          uuidVal(t),
		WorkspaceID: uuidVal(t),
		RecipientID: uuidVal(t),
	}
	h.publishInboxEvent(eventInboxUnread, item)

	if hits != 1 {
		t.Fatalf("expected 1 published event, got %d", hits)
	}
	if got.WorkspaceID == "" {
		t.Errorf("event missing workspace id (would not route to any room)")
	}
	payload, ok := got.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload not a map: %T", got.Payload)
	}
	if payload["item_id"] == "" || payload["recipient_id"] == "" {
		t.Errorf("payload missing item_id/recipient_id: %+v", payload)
	}
}

func TestPublishInboxEvent_NilBusIsSafe(t *testing.T) {
	h := New(nil, nil, nil)
	// Must not panic when no bus is wired (older tests / non-realtime paths).
	h.publishInboxEvent(eventInboxMuted, cerebrodb.InboxItem{})
}

// The client realtime refreshMap keys on the event-type prefix and invalidates
// the inbox for any `inbox:*` event except `inbox:new`. If any of these lost
// the prefix, the corresponding mutation would silently stop refreshing other
// sessions — exactly the FIR-2394 regression. Guard the contract.
func TestInboxEventTypesCarryInboxPrefix(t *testing.T) {
	for _, et := range []string{eventInboxMuted, eventInboxUnmuted, eventInboxUnread, eventInboxUnarchived} {
		if !strings.HasPrefix(et, "inbox:") {
			t.Errorf("event %q must carry the inbox: prefix so the client refreshMap catches it", et)
		}
		if et == "inbox:new" {
			t.Errorf("event %q collides with the specific-handler inbox:new and would skip the refresh fallback", et)
		}
	}
}
