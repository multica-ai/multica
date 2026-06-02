package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestV2FlatMessageSendAndList verifies the V2 flat message flow:
// send a top-level message and list it in the channel timeline.
func TestV2FlatMessageSendAndList(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})

	// Create channel + join (creator is auto-member).
	rr := httptest.NewRecorder()
	testHandler.CreateChannel(rr, newRequest(http.MethodPost, "/api/channels", map[string]any{
		"name": "V2 Test Channel",
	}))
	if rr.Code != http.StatusCreated {
		t.Fatalf("CreateChannel: %d (%s)", rr.Code, rr.Body.String())
	}
	var channel ChannelResponse
	decodeJSON(t, rr, &channel)

	// Send a top-level message.
	rr = httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/", map[string]any{
		"content": "Hello V2 flat world!",
	}), "id", channel.ID)
	testHandler.SendChannelMessage(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("SendChannelMessage: %d (%s)", rr.Code, rr.Body.String())
	}
	var msg ChannelMessageV2Response
	decodeJSON(t, rr, &msg)
	if msg.Content != "Hello V2 flat world!" {
		t.Fatalf("unexpected message content: %s", msg.Content)
	}
	if msg.ReplyToID != nil {
		t.Fatalf("top-level message should have nil reply_to_id")
	}

	// List messages — should have the one we sent.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodGet, "/", nil), "id", channel.ID)
	testHandler.ListChannelMessages(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ListChannelMessages: %d (%s)", rr.Code, rr.Body.String())
	}
	var listResp struct {
		Messages []ChannelMessageV2Response `json:"messages"`
		Total    int                        `json:"total"`
	}
	decodeJSON(t, rr, &listResp)
	if listResp.Total != 1 {
		t.Fatalf("expected 1 message, got %d", listResp.Total)
	}
	if listResp.Messages[0].ID != msg.ID {
		t.Fatalf("listed message ID mismatch")
	}
}

// TestV2ReplyAutoCreateThread verifies that replying to a message
// implicitly creates a thread.
func TestV2ReplyAutoCreateThread(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})

	// Channel + top-level message.
	rr := httptest.NewRecorder()
	testHandler.CreateChannel(rr, newRequest(http.MethodPost, "/api/channels", map[string]any{
		"name": "Reply Thread Test",
	}))
	var channel ChannelResponse
	decodeJSON(t, rr, &channel)

	rr = httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/", map[string]any{
		"content": "Original message",
	}), "id", channel.ID)
	testHandler.SendChannelMessage(rr, req)
	var origMsg ChannelMessageV2Response
	decodeJSON(t, rr, &origMsg)

	// Reply to it — should auto-create thread.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodPost, "/", map[string]any{
		"content": "This is a reply",
	}), "id", channel.ID)
	req = withURLParam(req, "msgId", origMsg.ID)
	testHandler.ReplyToMessage(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("ReplyToMessage: %d (%s)", rr.Code, rr.Body.String())
	}
	var replyResp struct {
		Message ChannelMessageV2Response `json:"message"`
		Thread  ChannelThreadResponse    `json:"thread"`
	}
	decodeJSON(t, rr, &replyResp)
	if replyResp.Thread.ID == "" {
		t.Fatal("expected thread to be created on reply")
	}
	if replyResp.Message.Content != "This is a reply" {
		t.Fatalf("reply content mismatch: %s", replyResp.Message.Content)
	}

	// Get message thread — should have the reply.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodGet, "/", nil), "id", channel.ID)
	req = withURLParam(req, "msgId", origMsg.ID)
	testHandler.GetMessageThread(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GetMessageThread: %d (%s)", rr.Code, rr.Body.String())
	}
	var threadResp struct {
		RootMessage ChannelMessageV2Response `json:"root_message"`
		Replies     []ChannelMessageResponse `json:"replies"`
		Thread      *ChannelThreadResponse   `json:"thread"`
	}
	decodeJSON(t, rr, &threadResp)
	if threadResp.Thread == nil {
		t.Fatal("expected thread in response")
	}
	if len(threadResp.Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(threadResp.Replies))
	}
}

// TestV2ConvertMessageToIssueNoThread verifies that ConvertMessageToIssue
// creates a thread when none exists, fulfilling "Issue from thread" invariant.
func TestV2ConvertMessageToIssueNoThread(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()

	var projectID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO project (workspace_id, title) VALUES ($1, 'Convert test') RETURNING id`,
		testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1 AND project_id = $2`, testWorkspaceID, projectID)
		testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID)
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})

	// Channel + message (no thread yet).
	rr := httptest.NewRecorder()
	testHandler.CreateChannel(rr, newRequest(http.MethodPost, "/api/channels", map[string]any{
		"name": "Convert Test Channel",
	}))
	var channel ChannelResponse
	decodeJSON(t, rr, &channel)

	rr = httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/", map[string]any{
		"content": "We should implement feature X",
	}), "id", channel.ID)
	testHandler.SendChannelMessage(rr, req)
	var msg ChannelMessageV2Response
	decodeJSON(t, rr, &msg)

	// Convert message to issue.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodPost, "/", map[string]any{
		"project_id": projectID,
	}), "id", channel.ID)
	req = withURLParam(req, "msgId", msg.ID)
	testHandler.ConvertMessageToIssue(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("ConvertMessageToIssue: %d (%s)", rr.Code, rr.Body.String())
	}
	var issueResp struct {
		IssueID     string `json:"issue_id"`
		IssueNumber int32  `json:"issue_number"`
		Title       string `json:"title"`
		ThreadID    string `json:"thread_id"`
	}
	decodeJSON(t, rr, &issueResp)
	if issueResp.ThreadID == "" {
		t.Fatal("expected thread_id in response — thread should be created implicitly")
	}
	if issueResp.IssueID == "" {
		t.Fatal("expected issue_id in response")
	}

	// Verify thread has system "created from thread" message.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodGet, "/", nil), "id", channel.ID)
	req = withURLParam(req, "msgId", msg.ID)
	testHandler.GetMessageThread(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GetMessageThread after convert: %d (%s)", rr.Code, rr.Body.String())
	}
	var threadResp struct {
		Replies []ChannelMessageResponse `json:"replies"`
		Thread  *ChannelThreadResponse   `json:"thread"`
	}
	decodeJSON(t, rr, &threadResp)
	if threadResp.Thread == nil {
		t.Fatal("expected thread to exist after convert")
	}
	foundSystem := false
	for _, reply := range threadResp.Replies {
		if reply.AuthorType == "system" {
			foundSystem = true
		}
	}
	if !foundSystem {
		t.Fatal("expected system 'created from thread' message in thread after convert")
	}
}

// TestV2IssueReflowToChannelTimeline verifies that issue status changes
// produce a top-level system message in the channel main timeline.
func TestV2IssueReflowToChannelTimeline(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()

	var projectID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO project (workspace_id, title) VALUES ($1, 'Reflow V2') RETURNING id`,
		testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1 AND project_id = $2`, testWorkspaceID, projectID)
		testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID)
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})

	// Channel + thread + issue from thread.
	rr := httptest.NewRecorder()
	testHandler.CreateChannel(rr, newRequest(http.MethodPost, "/api/channels", map[string]any{
		"name": "Reflow V2 Channel",
	}))
	var channel ChannelResponse
	decodeJSON(t, rr, &channel)

	rr = httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/", map[string]any{
		"title": "Reflow discussion", "content": "discuss this",
	}), "id", channel.ID)
	testHandler.CreateChannelThread(rr, req)
	var threadCreateResp struct {
		Thread ChannelThreadResponse `json:"thread"`
	}
	decodeJSON(t, rr, &threadCreateResp)
	threadID := threadCreateResp.Thread.ID

	// Create issue FROM the thread (via issue create API).
	rr = httptest.NewRecorder()
	testHandler.CreateIssue(rr, newRequest(http.MethodPost, "/api/issues", map[string]any{
		"title":            "Reflow test issue",
		"project_id":       projectID,
		"source_thread_id": threadID,
	}))
	if rr.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d (%s)", rr.Code, rr.Body.String())
	}
	var issue IssueResponse
	decodeJSON(t, rr, &issue)

	// Change issue status to "done".
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodPatch, "/", map[string]any{"status": "done"}), "id", issue.ID)
	testHandler.UpdateIssue(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("UpdateIssue: %d (%s)", rr.Code, rr.Body.String())
	}

	// Check channel main timeline for system activity message.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodGet, "/", nil), "id", channel.ID)
	testHandler.ListChannelMessages(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ListChannelMessages: %d (%s)", rr.Code, rr.Body.String())
	}
	var listResp struct {
		Messages []ChannelMessageV2Response `json:"messages"`
		Total    int                        `json:"total"`
	}
	decodeJSON(t, rr, &listResp)
	foundReflow := false
	for _, m := range listResp.Messages {
		if m.AuthorType == "system" {
			foundReflow = true
		}
	}
	if !foundReflow {
		t.Fatal("expected a system reflow message in channel main timeline after issue status change")
	}
}

// TestV2LockedChannelPermission verifies that a locked channel restricts
// posting to managers only.
func TestV2LockedChannelPermission(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = 'locked-member@multica.ai'`)
	})

	// Create a channel and lock it.
	rr := httptest.NewRecorder()
	testHandler.CreateChannel(rr, newRequest(http.MethodPost, "/api/channels", map[string]any{
		"name": "Locked Channel",
	}))
	var channel ChannelResponse
	decodeJSON(t, rr, &channel)

	// Lock the channel (admin action).
	rr = httptest.NewRecorder()
	isLocked := true
	req := withURLParam(newRequest(http.MethodPatch, "/", map[string]any{
		"is_locked": isLocked,
	}), "id", channel.ID)
	testHandler.UpdateChannel(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("UpdateChannel (lock): %d (%s)", rr.Code, rr.Body.String())
	}

	// Create a non-owner member and add them to the channel.
	var memberID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ('Locked Member', 'locked-member@multica.ai') RETURNING id`).Scan(&memberID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, memberID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO channel_member (channel_id, user_id, role) VALUES ($1, $2, 'member')`, channel.ID, memberID); err != nil {
		t.Fatalf("add channel member: %v", err)
	}

	// The non-owner member should NOT be able to post.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodPost, "/", map[string]any{
		"content": "I should not be able to post",
	}), "id", channel.ID)
	req.Header.Set("X-User-ID", memberID)
	testHandler.SendChannelMessage(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("locked channel: non-manager should get 403, got %d (%s)", rr.Code, rr.Body.String())
	}

	// The channel owner/creator (admin) SHOULD be able to post.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodPost, "/", map[string]any{
		"content": "Admin can post in locked channel",
	}), "id", channel.ID)
	testHandler.SendChannelMessage(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("locked channel: manager should be able to post, got %d (%s)", rr.Code, rr.Body.String())
	}
}
