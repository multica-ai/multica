package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createSecondTestUser inserts an extra workspace member for tests that
// need two real members (e.g. DM tests). Returns the new user ID and a
// cleanup func.
func createSecondTestUser(t *testing.T, suffix string) (string, func()) {
	t.Helper()
	ctx := context.Background()
	email := fmt.Sprintf("handler-test-%s@multica.ai", suffix)
	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Test User "+suffix, email,
	).Scan(&userID); err != nil {
		t.Fatalf("create second user: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, userID,
	); err != nil {
		t.Fatalf("add second user to workspace: %v", err)
	}
	cleanup := func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	}
	return userID, cleanup
}

// TestCreateChannel exercises the happy path for a multi-party channel:
// POST /api/channels creates the underlying issue with kind='channel',
// subscribes the creator and named participants, and returns a populated
// ChannelResponse.
func TestCreateChannel(t *testing.T) {
	peerID, cleanup := createSecondTestUser(t, "channel-peer")
	defer cleanup()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"kind":        "channel",
		"name":        "growth-team",
		"description": "weekly growth discussions",
		"member_ids":  []string{peerID},
	})
	testHandler.CreateChannel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateChannel: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created ChannelResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if created.Kind != "channel" {
		t.Fatalf("expected kind 'channel', got %q", created.Kind)
	}
	if created.Title != "growth-team" {
		t.Fatalf("expected title 'growth-team', got %q", created.Title)
	}
	if len(created.Participants) != 2 {
		t.Fatalf("expected 2 participants (creator+peer), got %d", len(created.Participants))
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID)
	})
}

// TestCreateChannelDMIsIdempotent verifies that opening the same DM twice
// returns the existing channel rather than producing duplicates. This is
// the contract clients rely on when "Message Sara" is clicked twice.
func TestCreateChannelDMIsIdempotent(t *testing.T) {
	peerID, cleanup := createSecondTestUser(t, "dm-peer")
	defer cleanup()

	body := map[string]any{
		"kind":       "dm",
		"member_ids": []string{peerID},
	}

	w := httptest.NewRecorder()
	testHandler.CreateChannel(w, newRequest("POST", "/api/channels", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("first CreateChannel: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var first ChannelResponse
	json.NewDecoder(w.Body).Decode(&first)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, first.ID)
	})

	w = httptest.NewRecorder()
	testHandler.CreateChannel(w, newRequest("POST", "/api/channels", body))
	if w.Code != http.StatusOK {
		t.Fatalf("second CreateChannel: expected 200 (existing), got %d: %s", w.Code, w.Body.String())
	}
	var second ChannelResponse
	json.NewDecoder(w.Body).Decode(&second)
	if second.ID != first.ID {
		t.Fatalf("expected idempotent DM (same ID), got %q vs %q", first.ID, second.ID)
	}
}

// TestCreateChannelDMRejectsSelf prevents the "DM with myself" footgun —
// the server must refuse before creating an issue.
func TestCreateChannelDMRejectsSelf(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"kind":       "dm",
		"member_ids": []string{testUserID},
	})
	testHandler.CreateChannel(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateChannelRejectsBadKind ensures only 'channel' and 'dm' kinds
// are accepted — protects the issue table from invalid kinds bypassing
// the CHECK constraint. 'issue' is the default kind and not a valid
// channel kind, so it must be rejected here.
func TestCreateChannelRejectsBadKind(t *testing.T) {
	for _, badKind := range []string{"", "issue", "task", "thread", "anything"} {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/channels", map[string]any{
			"kind":       badKind,
			"name":       "x",
			"member_ids": []string{},
		})
		testHandler.CreateChannel(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("kind=%q: expected 400, got %d", badKind, w.Code)
		}
	}
}

// TestListChannelsExcludesNonParticipants verifies the participation
// gate: a member who isn't subscribed to a channel must not see it in
// their list.
func TestListChannelsExcludesNonParticipants(t *testing.T) {
	peerID, cleanup := createSecondTestUser(t, "list-peer")
	defer cleanup()

	// Create a channel where ONLY the peer is subscribed (not testUserID).
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"kind":       "channel",
		"name":       "private-thoughts",
		"member_ids": []string{peerID},
	})
	testHandler.CreateChannel(w, req)
	var created ChannelResponse
	json.NewDecoder(w.Body).Decode(&created)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	// Remove testUserID from the channel so they're no longer a participant.
	if _, err := testPool.Exec(context.Background(),
		`DELETE FROM issue_subscriber WHERE issue_id = $1 AND user_id = $2`,
		created.ID, testUserID,
	); err != nil {
		t.Fatalf("remove subscriber: %v", err)
	}

	w = httptest.NewRecorder()
	testHandler.ListChannels(w, newRequest("GET", "/api/channels", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListChannels: expected 200, got %d", w.Code)
	}
	var list []ChannelResponse
	json.NewDecoder(w.Body).Decode(&list)
	for _, c := range list {
		if c.ID == created.ID {
			t.Fatalf("listed channel %q despite being unsubscribed", created.ID)
		}
	}
}

// TestChannelsHiddenFromIssueList verifies the regression fix: kind='issue'
// filter on ListIssues prevents channels from leaking into the issue UI.
func TestChannelsHiddenFromIssueList(t *testing.T) {
	peerID, cleanup := createSecondTestUser(t, "issue-list")
	defer cleanup()

	// Create a channel.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"kind":       "channel",
		"name":       "should-not-appear",
		"member_ids": []string{peerID},
	})
	testHandler.CreateChannel(w, req)
	var created ChannelResponse
	json.NewDecoder(w.Body).Decode(&created)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	// Create a regular task too, so the list isn't trivially empty.
	w = httptest.NewRecorder()
	taskReq := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Real task " + created.ID,
	})
	testHandler.CreateIssue(w, taskReq)
	var task IssueResponse
	json.NewDecoder(w.Body).Decode(&task)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, task.ID)
	})

	w = httptest.NewRecorder()
	testHandler.ListIssues(w, newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues: expected 200, got %d", w.Code)
	}
	var listResp map[string]any
	json.NewDecoder(w.Body).Decode(&listResp)
	issues, _ := listResp["issues"].([]any)
	for _, raw := range issues {
		obj, _ := raw.(map[string]any)
		if id, _ := obj["id"].(string); id == created.ID {
			t.Fatalf("channel %q leaked into /api/issues — kind filter broken", created.ID)
		}
	}
}

// TestGetChannelRejectsNonParticipant verifies subscriber-based access
// control on GET /api/channels/{id}.
func TestGetChannelRejectsNonParticipant(t *testing.T) {
	peerID, cleanup := createSecondTestUser(t, "get-peer")
	defer cleanup()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"kind":       "channel",
		"name":       "no-trespass",
		"member_ids": []string{peerID},
	})
	testHandler.CreateChannel(w, req)
	var created ChannelResponse
	json.NewDecoder(w.Body).Decode(&created)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	// Drop testUserID so the requesting user is no longer a participant.
	testPool.Exec(context.Background(),
		`DELETE FROM issue_subscriber WHERE issue_id = $1 AND user_id = $2`,
		created.ID, testUserID,
	)

	w = httptest.NewRecorder()
	getReq := newRequest("GET", "/api/channels/"+created.ID, nil)
	getReq = withURLParam(getReq, "id", created.ID)
	testHandler.GetChannel(w, getReq)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}
