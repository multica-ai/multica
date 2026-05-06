package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// pinTestDeleteRequest drives /api/pins through chi.URLParam by setting both
// {itemType}/{itemId} on a single chi route context — withURLParam clobbers
// any previous value because each call creates a fresh context, so we must
// install both params atomically here.
func pinTestDeleteRequest(itemType, itemID string) *http.Request {
	req := newRequest("DELETE", "/api/pins/"+itemType+"/"+itemID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("itemType", itemType)
	rctx.URLParams.Add("itemId", itemID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestPinChannelLifecycle covers the full pin/list/unpin round-trip for a
// channel: create channel, pin it, listing returns it with title=channel name,
// unpin removes it. Single test on purpose — listing is meaningless without
// pinning, and pin/unpin races if split across goroutines.
func TestPinChannelLifecycle(t *testing.T) {
	peerID, cleanup := createSecondTestUser(t, "pin-channel")
	defer cleanup()

	// 1. Create a channel.
	w := httptest.NewRecorder()
	testHandler.CreateChannel(w, newRequest("POST", "/api/channels", map[string]any{
		"kind":       "channel",
		"name":       "growth",
		"member_ids": []string{peerID},
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateChannel: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var channel ChannelResponse
	json.NewDecoder(w.Body).Decode(&channel)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, channel.ID)
		testPool.Exec(context.Background(), `DELETE FROM pinned_item WHERE item_id = $1`, channel.ID)
	})

	// 2. Pin the channel.
	w = httptest.NewRecorder()
	testHandler.CreatePin(w, newRequest("POST", "/api/pins", map[string]any{
		"item_type": "channel",
		"item_id":   channel.ID,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreatePin (channel): expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// 3. ListPins returns it with the channel name as title and a "#" icon.
	w = httptest.NewRecorder()
	testHandler.ListPins(w, newRequest("GET", "/api/pins", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListPins: expected 200, got %d", w.Code)
	}
	var pins []PinnedItemResponse
	json.NewDecoder(w.Body).Decode(&pins)

	var got *PinnedItemResponse
	for i, p := range pins {
		if p.ItemType == "channel" && p.ItemID == channel.ID {
			got = &pins[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("ListPins: pinned channel %q not in response: %+v", channel.ID, pins)
	}
	if got.Title != "growth" {
		t.Fatalf("ListPins: expected channel title 'growth', got %q", got.Title)
	}
	if got.Icon == nil || *got.Icon != "#" {
		t.Fatalf("ListPins: expected channel icon '#', got %v", got.Icon)
	}

	// 4. Unpin removes it.
	w = httptest.NewRecorder()
	testHandler.DeletePin(w, pinTestDeleteRequest("channel", channel.ID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeletePin: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.ListPins(w, newRequest("GET", "/api/pins", nil))
	json.NewDecoder(w.Body).Decode(&pins)
	for _, p := range pins {
		if p.ItemType == "channel" && p.ItemID == channel.ID {
			t.Fatalf("ListPins after unpin: channel still present: %+v", p)
		}
	}
}

// TestPinDMLifecycle is the DM mirror of TestPinChannelLifecycle. The key
// extra check: the pin's title must be the *peer's* name (not the empty
// issue title) so the sidebar can render "Sara" rather than blank.
func TestPinDMLifecycle(t *testing.T) {
	peerID, cleanup := createSecondTestUser(t, "pin-dm")
	defer cleanup()
	// Update the peer's name so we can assert on it.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE "user" SET name = $1 WHERE id = $2`, "Sara Peer", peerID,
	); err != nil {
		t.Fatalf("rename peer: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.CreateChannel(w, newRequest("POST", "/api/channels", map[string]any{
		"kind":       "dm",
		"member_ids": []string{peerID},
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateChannel (dm): expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var dm ChannelResponse
	json.NewDecoder(w.Body).Decode(&dm)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, dm.ID)
		testPool.Exec(context.Background(), `DELETE FROM pinned_item WHERE item_id = $1`, dm.ID)
	})

	w = httptest.NewRecorder()
	testHandler.CreatePin(w, newRequest("POST", "/api/pins", map[string]any{
		"item_type": "dm",
		"item_id":   dm.ID,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreatePin (dm): expected 201, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.ListPins(w, newRequest("GET", "/api/pins", nil))
	var pins []PinnedItemResponse
	json.NewDecoder(w.Body).Decode(&pins)

	var got *PinnedItemResponse
	for i, p := range pins {
		if p.ItemType == "dm" && p.ItemID == dm.ID {
			got = &pins[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("ListPins: pinned DM %q not in response: %+v", dm.ID, pins)
	}
	if got.Title != "Sara Peer" {
		t.Fatalf("ListPins: expected DM title 'Sara Peer' (peer name), got %q", got.Title)
	}

	w = httptest.NewRecorder()
	testHandler.DeletePin(w, pinTestDeleteRequest("dm", dm.ID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeletePin (dm): expected 204, got %d", w.Code)
	}
}

// TestPinChannelRequiresParticipant verifies the access gate: a user who
// is not subscribed to a channel must not be able to pin it.
func TestPinChannelRequiresParticipant(t *testing.T) {
	peerID, cleanup := createSecondTestUser(t, "pin-stranger")
	defer cleanup()

	// Create a channel where ONLY the peer is subscribed.
	w := httptest.NewRecorder()
	testHandler.CreateChannel(w, newRequest("POST", "/api/channels", map[string]any{
		"kind":       "channel",
		"name":       "private",
		"member_ids": []string{peerID},
	}))
	var ch ChannelResponse
	json.NewDecoder(w.Body).Decode(&ch)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, ch.ID)
	})

	// Drop testUserID from the participant list.
	if _, err := testPool.Exec(context.Background(),
		`DELETE FROM issue_subscriber WHERE issue_id = $1 AND user_id = $2`,
		ch.ID, testUserID,
	); err != nil {
		t.Fatalf("remove subscriber: %v", err)
	}

	w = httptest.NewRecorder()
	testHandler.CreatePin(w, newRequest("POST", "/api/pins", map[string]any{
		"item_type": "channel",
		"item_id":   ch.ID,
	}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("CreatePin without participation: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestPinRejectsBadItemType keeps the API surface tight: only the four
// approved item_types can reach the database constraint.
func TestPinRejectsBadItemType(t *testing.T) {
	for _, bad := range []string{"", "chat_session", "task", "user", "anything"} {
		w := httptest.NewRecorder()
		testHandler.CreatePin(w, newRequest("POST", "/api/pins", map[string]any{
			"item_type": bad,
			"item_id":   testWorkspaceID, // non-empty UUID-shaped string
		}))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("item_type=%q: expected 400, got %d: %s", bad, w.Code, w.Body.String())
		}
	}
}

// TestPinChannelDMConstraintAcceptsAllFour is the migration round-trip:
// after the constraint extension, the database must accept all four
// item_types and reject anything else.
func TestPinChannelDMConstraintAcceptsAllFour(t *testing.T) {
	ctx := context.Background()
	stub := "00000000-0000-0000-0000-000000000001"

	for _, kind := range []string{"issue", "project", "channel", "dm"} {
		var pinID string
		err := testPool.QueryRow(ctx, `
			INSERT INTO pinned_item (workspace_id, user_id, item_type, item_id)
			VALUES ($1, $2, $3, $4)
			RETURNING id
		`, testWorkspaceID, testUserID, kind, stub).Scan(&pinID)
		if err != nil {
			t.Fatalf("constraint should accept %q, got error: %v", kind, err)
		}
		testPool.Exec(ctx, `DELETE FROM pinned_item WHERE id = $1`, pinID)
	}

	// Anything outside the allowed set must still be rejected by the constraint.
	_, err := testPool.Exec(ctx, `
		INSERT INTO pinned_item (workspace_id, user_id, item_type, item_id)
		VALUES ($1, $2, 'bogus', $3)
	`, testWorkspaceID, testUserID, stub)
	if err == nil {
		testPool.Exec(ctx, `DELETE FROM pinned_item WHERE workspace_id = $1 AND item_type = 'bogus'`, testWorkspaceID)
		t.Fatal("constraint should reject 'bogus' but the insert succeeded")
	}
}
