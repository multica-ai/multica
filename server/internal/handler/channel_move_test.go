package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

	// Converge A into B: A should leave the top-level list and appear in B's
	// thread sorted by its original authored time (order_at = created_at).
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

	// Release A back to top-level: it should reappear in the timeline at its
	// original authored-time position (order_at = created_at).
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

// TestMoveChannelMessage_PreservesCreatedAt (H1): converging a message must
// leave created_at untouched and keep order_at equal to it so list order matches
// the displayed authored time — the unread model (has_unread / first_unread /
// last_activity in ListChannels) keys on created_at, so a mere re-location must
// not re-stamp it. Before the fix, created_at was set to now() and the moved
// message falsely read as unread for every member whose last_read_at < now().
func TestMoveChannelMessage_PreservesCreatedAt(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})

	ch := createV2Channel(t, "Move CreatedAt Channel")
	msgA := sendV2Message(t, ch.ID, "will be converged — created_at must not move")
	msgB := sendV2Message(t, ch.ID, "thread target")

	rr := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPatch, "/", map[string]any{
		"target_message_id": msgB.ID,
	}), "id", ch.ID)
	req = withURLParam(req, "msgId", msgA.ID)
	testHandler.MoveChannelMessage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("converge MoveChannelMessage: %d (%s)", rr.Code, rr.Body.String())
	}

	// created_at must equal its original send time; order_at must match it so
	// sort order aligns with the displayed timestamp.
	var createdAt, orderAt time.Time
	if err := testPool.QueryRow(ctx,
		`SELECT created_at, order_at FROM channel_message WHERE id = $1`, msgA.ID).
		Scan(&createdAt, &orderAt); err != nil {
		t.Fatalf("scan moved message: %v", err)
	}
	if !orderAt.Equal(createdAt) {
		t.Fatalf("expected order_at == created_at after reparent; got created_at=%v order_at=%v", createdAt, orderAt)
	}
}

// TestMoveChannelMessage_SortByAuthoredTime: a message converged into a thread
// that already has a newer reply must sort before that reply (by created_at),
// not jump to the end with order_at = now().
func TestMoveChannelMessage_SortByAuthoredTime(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})

	ch := createV2Channel(t, "Move Sort Channel")
	msgA := sendV2Message(t, ch.ID, "older top-level — will be converged")
	time.Sleep(10 * time.Millisecond)
	msgB := sendV2Message(t, ch.ID, "thread target")
	time.Sleep(10 * time.Millisecond)
	replyNewer := replyV2Message(t, ch.ID, msgB.ID, "newer reply already in thread")

	rr := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPatch, "/", map[string]any{
		"target_message_id": msgB.ID,
	}), "id", ch.ID)
	req = withURLParam(req, "msgId", msgA.ID)
	testHandler.MoveChannelMessage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("converge MoveChannelMessage: %d (%s)", rr.Code, rr.Body.String())
	}

	thread := getV2Thread(t, ch.ID, msgB.ID)
	if len(thread.Replies) != 2 {
		t.Fatalf("expected 2 replies after converge, got %d", len(thread.Replies))
	}
	if thread.Replies[0].ID != msgA.ID || thread.Replies[1].ID != replyNewer.ID {
		t.Fatalf("expected replies sorted by authored time [A, newer]; got [%s, %s]",
			thread.Replies[0].ID, thread.Replies[1].ID)
	}
}

// TestMoveChannelMessage_ReleaseCleansUpSourceThread (M4): releasing the last
// reply of a thread drops the now-empty, issue-free thread (its stale count /
// last_activity would otherwise linger and mis-order the thread list); a thread
// still linked to an issue is preserved (the issue's source_thread_id
// back-reference must stay valid).
func TestMoveChannelMessage_ReleaseCleansUpSourceThread(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})

	ch := createV2Channel(t, "Move Cleanup Channel")

	// Empty + issue-free → deleted after the last reply is released.
	msgB := sendV2Message(t, ch.ID, "target with one reply")
	reply := replyV2Message(t, ch.ID, msgB.ID, "the only reply")
	releaseMove := func(msgID string) {
		t.Helper()
		rr := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/", map[string]any{"target_message_id": nil}), "id", ch.ID)
		req = withURLParam(req, "msgId", msgID)
		testHandler.MoveChannelMessage(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("release MoveChannelMessage: %d (%s)", rr.Code, rr.Body.String())
		}
	}
	releaseMove(reply.ID)
	if th := getV2Thread(t, ch.ID, msgB.ID); th.Thread != nil {
		t.Fatalf("empty issue-free thread should be deleted after releasing its last reply")
	}

	// Issue-linked → preserved even when its last reply is released.
	msgC := sendV2Message(t, ch.ID, "target with reply + linked issue")
	reply2 := replyV2Message(t, ch.ID, msgC.ID, "only reply, but thread is linked to an issue")
	var threadID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM channel_thread WHERE root_message_id = $1`, msgC.ID).Scan(&threadID); err != nil {
		t.Fatalf("find thread for C: %v", err)
	}
	var issueNum int
	if err := testPool.QueryRow(ctx,
		`SELECT COALESCE(max(number), 0) + 1 FROM issue WHERE workspace_id = $1`, testWorkspaceID).
		Scan(&issueNum); err != nil {
		t.Fatalf("next issue number: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (workspace_id, number, title, status, priority, creator_type, creator_id, source_thread_id)
		VALUES ($1, $2, 'linked', 'todo', 'none', 'member', $3, $4)`,
		testWorkspaceID, issueNum, testUserID, threadID); err != nil {
		t.Fatalf("insert linked issue: %v", err)
	}
	releaseMove(reply2.ID)
	if th := getV2Thread(t, ch.ID, msgC.ID); th.Thread == nil {
		t.Fatalf("issue-linked thread must NOT be deleted when its last reply is released")
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
