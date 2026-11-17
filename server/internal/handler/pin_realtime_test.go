package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Pins belong to one member, so every pin event is published private to them.
func TestPinEventsArePrivateToTheirMember(t *testing.T) {
	h := *testHandler
	h.Bus = events.New()
	var got []events.Event
	for _, eventType := range []string{protocol.EventPinCreated, protocol.EventPinReordered, protocol.EventPinDeleted} {
		h.Bus.Subscribe(eventType, func(e events.Event) { got = append(got, e) })
	}
	issueID := dbfx.Issue(t, "Pinned for one member")
	dbfx.Cleanup(t, `DELETE FROM pinned_item WHERE item_id = $1`, issueID)

	var pin PinnedItemResponse
	testutil.Call(t, h.CreatePin, newRequest(http.MethodPost, "/api/pins", map[string]any{"item_type": "issue", "item_id": issueID})).
		Want(http.StatusCreated).JSON(&pin)
	testutil.Call(t, h.ReorderPins, newRequest(http.MethodPut, "/api/pins/reorder", map[string]any{
		"items": []map[string]any{{"id": pin.ID, "position": 1}},
	})).Want(http.StatusNoContent)
	testutil.Call(t, h.DeletePin, withURLParams(newRequest(http.MethodDelete, "/api/pins/issue/"+issueID, nil), "itemType", "issue", "itemId", issueID)).
		Want(http.StatusNoContent)

	want := []string{protocol.EventPinCreated, protocol.EventPinReordered, protocol.EventPinDeleted}
	if len(got) != len(want) {
		t.Fatalf("published %d events, want %d: %+v", len(got), len(want), got)
	}
	for i, e := range got {
		if e.Type != want[i] || e.WorkspaceID != testWorkspaceID || e.ActorID != testUserID || e.MemberPrivateUserID != testUserID {
			t.Fatalf("event %d = type %q workspace %q actor %q private to %q, want %q private to %q",
				i, e.Type, e.WorkspaceID, e.ActorID, e.MemberPrivateUserID, want[i], testUserID)
		}
	}
}
