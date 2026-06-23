package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func decodeJSON(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("failed to decode response: %v (body=%s)", err, rr.Body.String())
	}
}

func TestChannelLifecycle(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})

	// Create a channel.
	rr := httptest.NewRecorder()
	testHandler.CreateChannel(rr, newRequest(http.MethodPost, "/api/channels", map[string]any{
		"name":        "Product Sync",
		"description": "where humans and agents align",
	}))
	if rr.Code != http.StatusCreated {
		t.Fatalf("CreateChannel: expected 201, got %d (%s)", rr.Code, rr.Body.String())
	}
	var channel ChannelResponse
	decodeJSON(t, rr, &channel)
	if channel.Slug != "product-sync" {
		t.Fatalf("expected slug product-sync, got %s", channel.Slug)
	}
	if !channel.IsMember || channel.MemberRole == nil || *channel.MemberRole != "owner" {
		t.Fatalf("creator should be channel owner, got %+v", channel)
	}

	// List channels — creator sees it as a member.
	rr = httptest.NewRecorder()
	testHandler.ListChannels(rr, newRequest(http.MethodGet, "/api/channels", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("ListChannels: expected 200, got %d", rr.Code)
	}
	var listResp struct {
		Channels []ChannelResponse `json:"channels"`
		Total    int               `json:"total"`
	}
	decodeJSON(t, rr, &listResp)
	if listResp.Total != 1 || listResp.Channels[0].ID != channel.ID {
		t.Fatalf("expected 1 channel in list, got %+v", listResp)
	}

	// Create a thread with an opening message.
	rr = httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/", map[string]any{
		"title":   "Should we add Channels?",
		"content": "Kicking off the discussion.",
	}), "id", channel.ID)
	testHandler.CreateChannelThread(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("CreateChannelThread: expected 201, got %d (%s)", rr.Code, rr.Body.String())
	}
	var threadResp struct {
		Thread  ChannelThreadResponse  `json:"thread"`
		Message ChannelMessageResponse `json:"message"`
	}
	decodeJSON(t, rr, &threadResp)
	if threadResp.Thread.ID == "" || threadResp.Message.Content != "Kicking off the discussion." {
		t.Fatalf("unexpected thread/message: %+v", threadResp)
	}

	// Post a second message.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodPost, "/", map[string]any{"content": "I agree."}), "id", channel.ID)
	req = withURLParam(req, "threadId", threadResp.Thread.ID)
	testHandler.CreateChannelMessage(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("CreateChannelMessage: expected 201, got %d (%s)", rr.Code, rr.Body.String())
	}

	// List messages — should now have 2.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodGet, "/", nil), "id", channel.ID)
	req = withURLParam(req, "threadId", threadResp.Thread.ID)
	testHandler.ListThreadMessages(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ListThreadMessages: expected 200, got %d", rr.Code)
	}
	var msgsResp struct {
		Messages []ChannelMessageResponse `json:"messages"`
	}
	decodeJSON(t, rr, &msgsResp)
	if len(msgsResp.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgsResp.Messages))
	}
}

func TestChannelIssueReflow(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()

	var projectID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO project (workspace_id, title) VALUES ($1, 'Channel reflow test') RETURNING id`,
		testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1 AND project_id = $2`, testWorkspaceID, projectID)
		testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID)
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})

	// Channel + thread.
	rr := httptest.NewRecorder()
	testHandler.CreateChannel(rr, newRequest(http.MethodPost, "/api/channels", map[string]any{"name": "Reflow Channel"}))
	var channel ChannelResponse
	decodeJSON(t, rr, &channel)

	rr = httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/", map[string]any{
		"title": "Reflow discussion", "content": "let's ship",
	}), "id", channel.ID)
	testHandler.CreateChannelThread(rr, req)
	var threadResp struct {
		Thread ChannelThreadResponse `json:"thread"`
	}
	decodeJSON(t, rr, &threadResp)
	threadID := threadResp.Thread.ID

	// Create an issue FROM the thread.
	rr = httptest.NewRecorder()
	testHandler.CreateIssue(rr, newRequest(http.MethodPost, "/api/issues", map[string]any{
		"title":            "Implement reflow",
		"project_id":       projectID,
		"source_thread_id": threadID,
	}))
	if rr.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d (%s)", rr.Code, rr.Body.String())
	}
	var issue IssueResponse
	decodeJSON(t, rr, &issue)
	if issue.SourceThreadID == nil || *issue.SourceThreadID != threadID {
		t.Fatalf("issue should reference source thread, got %+v", issue.SourceThreadID)
	}

	// The thread should now have a system "created from thread" message.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodGet, "/", nil), "id", channel.ID)
	req = withURLParam(req, "threadId", threadID)
	testHandler.ListThreadMessages(rr, req)
	var msgsResp struct {
		Messages []ChannelMessageResponse `json:"messages"`
		Issues   []threadIssueResponse    `json:"issues"`
	}
	decodeJSON(t, rr, &msgsResp)
	foundSystem := false
	for _, m := range msgsResp.Messages {
		if m.AuthorType == "system" {
			foundSystem = true
		}
	}
	if !foundSystem {
		t.Fatalf("expected a system 'created from thread' message, messages=%+v", msgsResp.Messages)
	}
	if len(msgsResp.Issues) != 1 {
		t.Fatalf("expected 1 linked issue on thread, got %d", len(msgsResp.Issues))
	}

	// Change the issue status -> reflow a status message into the thread.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodPatch, "/", map[string]any{"status": "done"}), "id", issue.ID)
	testHandler.UpdateIssue(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("UpdateIssue: expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodGet, "/", nil), "id", channel.ID)
	req = withURLParam(req, "threadId", threadID)
	testHandler.ListThreadMessages(rr, req)
	decodeJSON(t, rr, &msgsResp)
	statusReflow := 0
	for _, m := range msgsResp.Messages {
		if m.AuthorType == "system" {
			statusReflow++
		}
	}
	if statusReflow < 2 {
		t.Fatalf("expected a status reflow system message after status change, system msgs=%d", statusReflow)
	}
}

func TestChannelInviteOnlyAccess(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = 'channel-outsider@multica.ai'`)
	})

	// Owner creates an invite-only channel and posts a message into it.
	rr := httptest.NewRecorder()
	testHandler.CreateChannel(rr, newRequest(http.MethodPost, "/api/channels", map[string]any{
		"name": "Private Channel", "access_mode": "invite",
	}))
	if rr.Code != http.StatusCreated {
		t.Fatalf("CreateChannel: %d (%s)", rr.Code, rr.Body.String())
	}
	var channel ChannelResponse
	decodeJSON(t, rr, &channel)
	if channel.AccessMode != "invite" {
		t.Fatalf("expected invite mode, got %s", channel.AccessMode)
	}

	rr = httptest.NewRecorder()
	testHandler.SendChannelMessage(rr, withURLParam(newRequest(http.MethodPost, "/", map[string]any{"content": "secret"}), "id", channel.ID))
	if rr.Code != http.StatusCreated {
		t.Fatalf("owner SendChannelMessage: %d (%s)", rr.Code, rr.Body.String())
	}

	// An outsider (non-member of the channel, plain workspace member).
	var outsiderID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ('Outsider', 'channel-outsider@multica.ai') RETURNING id`).Scan(&outsiderID); err != nil {
		t.Fatalf("create outsider: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, outsiderID); err != nil {
		t.Fatalf("add outsider member: %v", err)
	}

	// Outsider CAN see the invite-only channel in the list (not as a member).
	rr = httptest.NewRecorder()
	listReq := newRequest(http.MethodGet, "/api/channels", nil)
	listReq.Header.Set("X-User-ID", outsiderID)
	testHandler.ListChannels(rr, listReq)
	if rr.Code != http.StatusOK {
		t.Fatalf("outsider ListChannels: %d (%s)", rr.Code, rr.Body.String())
	}
	var listResp struct {
		Channels []ChannelResponse `json:"channels"`
	}
	decodeJSON(t, rr, &listResp)
	seen := false
	for _, c := range listResp.Channels {
		if c.ID == channel.ID {
			seen = true
			if c.IsMember {
				t.Fatalf("outsider should not be marked as member of invite channel")
			}
		}
	}
	if !seen {
		t.Fatalf("outsider ListChannels: invite-only channel not visible")
	}

	// Outsider CAN read the channel detail.
	rr = httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/", nil), "id", channel.ID)
	req.Header.Set("X-User-ID", outsiderID)
	testHandler.GetChannel(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("outsider GetChannel: expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}

	// Outsider CAN read the messages.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodGet, "/", nil), "id", channel.ID)
	req.Header.Set("X-User-ID", outsiderID)
	testHandler.ListChannelMessages(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("outsider ListChannelMessages: expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var msgs struct {
		Total int `json:"total"`
	}
	decodeJSON(t, rr, &msgs)
	if msgs.Total != 1 {
		t.Fatalf("outsider expected 1 message, got %d", msgs.Total)
	}

	// Outsider CANNOT post — canPost() gate rejects non-members.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodPost, "/", map[string]any{"content": "intrusion"}), "id", channel.ID)
	req.Header.Set("X-User-ID", outsiderID)
	testHandler.SendChannelMessage(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("outsider SendChannelMessage: expected 403, got %d (%s)", rr.Code, rr.Body.String())
	}
}

// TestChannelOpenNonMemberCanPost verifies the open-channel posting rule: a
// workspace member who has NOT joined the channel (no channel_member row) may
// still post and reply in an open channel. The frontend canPost gate treats
// open channels as workspace-wide, so the backend must agree — otherwise a
// non-member gets a 403 on send/reply despite the UI letting them type.
func TestChannelOpenNonMemberCanPost(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = 'open-outsider@multica.ai'`)
	})

	// Owner creates an OPEN channel and posts a top-level message.
	rr := httptest.NewRecorder()
	testHandler.CreateChannel(rr, newRequest(http.MethodPost, "/api/channels", map[string]any{
		"name": "Open Channel", "access_mode": "open",
	}))
	if rr.Code != http.StatusCreated {
		t.Fatalf("CreateChannel: %d (%s)", rr.Code, rr.Body.String())
	}
	var channel ChannelResponse
	decodeJSON(t, rr, &channel)
	if channel.AccessMode != "open" {
		t.Fatalf("expected open mode, got %s", channel.AccessMode)
	}

	rr = httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/", map[string]any{"content": "Original"}), "id", channel.ID)
	testHandler.SendChannelMessage(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("owner SendChannelMessage: %d (%s)", rr.Code, rr.Body.String())
	}
	var origMsg ChannelMessageV2Response
	decodeJSON(t, rr, &origMsg)

	// A workspace member who is NOT a channel member.
	var outsiderID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ('Open Outsider', 'open-outsider@multica.ai') RETURNING id`).Scan(&outsiderID); err != nil {
		t.Fatalf("create outsider: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, outsiderID); err != nil {
		t.Fatalf("add outsider member: %v", err)
	}

	// Non-member CAN post in the open channel.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodPost, "/", map[string]any{"content": "non-member post"}), "id", channel.ID)
	req.Header.Set("X-User-ID", outsiderID)
	testHandler.SendChannelMessage(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("non-member SendChannelMessage in open channel: expected 201, got %d (%s)", rr.Code, rr.Body.String())
	}

	// Non-member CAN reply to an existing message in the open channel.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodPost, "/", map[string]any{"content": "non-member reply"}), "id", channel.ID)
	req = withURLParam(req, "msgId", origMsg.ID)
	req.Header.Set("X-User-ID", outsiderID)
	testHandler.ReplyToMessage(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("non-member ReplyToMessage in open channel: expected 201, got %d (%s)", rr.Code, rr.Body.String())
	}
}
