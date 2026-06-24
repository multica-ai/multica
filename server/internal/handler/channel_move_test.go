package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestMoveChannelMessage_ConvergeAndRelease exercises the converge/release
// drag: a top-level message collapsed into another message's thread (becoming
// its latest reply), then released back to the top-level timeline.
func TestMoveChannelMessage_ConvergeAndRelease(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})

	ch := createV2Channel(t, "Move Converge Channel")
	// A and B are both top-level messages; A is the one we converge into B.
	msgA := sendV2Message(t, ch.ID, "top-level A — will be converged")
	msgB := sendV2Message(t, ch.ID, "top-level B — thread target")

	// Converge A into B: A should leave the top-level list and appear as B's
	// latest reply (created_at re-stamped, so it sorts last).
	rr := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPatch, "/", map[string]any{
		"target_message_id": msgB.ID,
	}), "id", ch.ID)
	req = withURLParam(req, "msgId", msgA.ID)
	testHandler.MoveChannelMessage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("converge MoveChannelMessage: %d (%s)", rr.Code, rr.Body.String())
	}

	topLevel := listV2Messages(t, ch.ID)
	for _, m := range topLevel {
		if m.ID == msgA.ID {
			t.Fatalf("converged message A still present in top-level list")
		}
	}

	thread := getV2Thread(t, ch.ID, msgB.ID)
	if thread.Thread == nil {
		t.Fatalf("target thread should have been created on converge")
	}
	if len(thread.Replies) != 1 || thread.Replies[0].ID != msgA.ID {
		t.Fatalf("expected A as the single reply of B, got %+v", thread.Replies)
	}
	if thread.Replies[0].ReplyToID == nil || *thread.Replies[0].ReplyToID != msgB.ID {
		t.Fatalf("converged A should reply_to B")
	}

	// Release A back to top-level: it should reappear in the timeline. A's
	// thread_id/reply_to_id are cleared and created_at re-stamped so it lands
	// as the latest top-level message.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodPatch, "/", map[string]any{
		"target_message_id": nil,
	}), "id", ch.ID)
	req = withURLParam(req, "msgId", msgA.ID)
	testHandler.MoveChannelMessage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("release MoveChannelMessage: %d (%s)", rr.Code, rr.Body.String())
	}

	topLevel = listV2Messages(t, ch.ID)
	var found bool
	for _, m := range topLevel {
		if m.ID == msgA.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("released message A missing from top-level list")
	}
	// B's thread should no longer list A as a reply.
	thread = getV2Thread(t, ch.ID, msgB.ID)
	for _, r := range thread.Replies {
		if r.ID == msgA.ID {
			t.Fatalf("released A still listed as a reply of B")
		}
	}
}

// TestMoveChannelMessage_Permission gates the move on author-or-canManage: a
// non-author workspace member (not a channel manager) may not move someone
// else's message.
func TestMoveChannelMessage_Permission(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})

	ch := createV2Channel(t, "Move Permission Channel")
	msgA := sendV2Message(t, ch.ID, "authored by test user")
	msgB := sendV2Message(t, ch.ID, "target")

	other := createWorkspaceMemberUser(t, "Move Other", "move-other@multica.test")
	// Open channel: other can post, but is neither author of A nor a manager.
	rr := httptest.NewRecorder()
	req := withURLParam(newRequestAs(other, http.MethodPatch, "/", map[string]any{
		"target_message_id": msgB.ID,
	}), "id", ch.ID)
	req = withURLParam(req, "msgId", msgA.ID)
	testHandler.MoveChannelMessage(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("non-author non-manager move: expected 403, got %d (%s)", rr.Code, rr.Body.String())
	}
}

// TestMoveChannelMessage_RefusesPopulatedThread ensures converge is refused
// (409) when the moved message already has a thread — collapsing a populated
// thread would orphan its replies/issues.
func TestMoveChannelMessage_RefusesPopulatedThread(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})

	ch := createV2Channel(t, "Move Refuse Channel")
	msgA := sendV2Message(t, ch.ID, "has a reply, cannot collapse")
	msgB := sendV2Message(t, ch.ID, "target")
	replyV2Message(t, ch.ID, msgA.ID, "a reply that anchors A's thread")

	rr := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPatch, "/", map[string]any{
		"target_message_id": msgB.ID,
	}), "id", ch.ID)
	req = withURLParam(req, "msgId", msgA.ID)
	testHandler.MoveChannelMessage(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("converge populated thread: expected 409, got %d (%s)", rr.Code, rr.Body.String())
	}

	// Converge-into-self is a 400.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodPatch, "/", map[string]any{
		"target_message_id": msgA.ID,
	}), "id", ch.ID)
	req = withURLParam(req, "msgId", msgA.ID)
	testHandler.MoveChannelMessage(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("converge into self: expected 400, got %d (%s)", rr.Code, rr.Body.String())
	}
}

// --- helpers ---

func createV2Channel(t *testing.T, name string) ChannelResponse {
	t.Helper()
	rr := httptest.NewRecorder()
	testHandler.CreateChannel(rr, newRequest(http.MethodPost, "/api/channels", map[string]any{"name": name}))
	if rr.Code != http.StatusCreated {
		t.Fatalf("CreateChannel %s: %d (%s)", name, rr.Code, rr.Body.String())
	}
	var ch ChannelResponse
	decodeJSON(t, rr, &ch)
	return ch
}

func sendV2Message(t *testing.T, channelID, content string) ChannelMessageV2Response {
	t.Helper()
	rr := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/", map[string]any{"content": content}), "id", channelID)
	testHandler.SendChannelMessage(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("SendChannelMessage: %d (%s)", rr.Code, rr.Body.String())
	}
	var msg ChannelMessageV2Response
	decodeJSON(t, rr, &msg)
	return msg
}

func replyV2Message(t *testing.T, channelID, rootID, content string) ChannelMessageV2Response {
	t.Helper()
	rr := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/", map[string]any{"content": content}), "id", channelID)
	req = withURLParam(req, "msgId", rootID)
	testHandler.ReplyToMessage(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("ReplyToMessage: %d (%s)", rr.Code, rr.Body.String())
	}
	var resp struct {
		Message ChannelMessageV2Response `json:"message"`
	}
	decodeJSON(t, rr, &resp)
	return resp.Message
}

func listV2Messages(t *testing.T, channelID string) []ChannelMessageV2Response {
	t.Helper()
	rr := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/", nil), "id", channelID)
	testHandler.ListChannelMessages(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ListChannelMessages: %d (%s)", rr.Code, rr.Body.String())
	}
	var resp struct {
		Messages []ChannelMessageV2Response `json:"messages"`
	}
	decodeJSON(t, rr, &resp)
	return resp.Messages
}

func getV2Thread(t *testing.T, channelID, rootID string) struct {
	RootMessage ChannelMessageV2Response   `json:"root_message"`
	Replies     []ChannelMessageV2Response `json:"replies"`
	Thread      any                        `json:"thread"`
} {
	t.Helper()
	rr := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/", nil), "id", channelID)
	req = withURLParam(req, "msgId", rootID)
	testHandler.GetMessageThread(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GetMessageThread: %d (%s)", rr.Code, rr.Body.String())
	}
	var resp struct {
		RootMessage ChannelMessageV2Response   `json:"root_message"`
		Replies     []ChannelMessageV2Response `json:"replies"`
		Thread      any                        `json:"thread"`
	}
	decodeJSON(t, rr, &resp)
	return resp
}
