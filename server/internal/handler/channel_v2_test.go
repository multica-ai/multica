package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
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

func TestV2ChannelMessagesIncludeMentionAgentTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})
	agentID := createHandlerTestAgent(t, "Channel Message Task Agent", nil)
	channel := createChannelForMentionTest(t, "Message Task Channel", "open")

	msg := sendChannelMessageForMentionTest(t, channel.ID, testUserID,
		"please handle [@Agent](mention://agent/"+agentID+")")

	rr := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/", nil), "id", channel.ID)
	testHandler.ListChannelMessages(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ListChannelMessages: %d (%s)", rr.Code, rr.Body.String())
	}
	var listResp struct {
		Messages []ChannelMessageV2Response `json:"messages"`
		Total    int                        `json:"total"`
	}
	decodeJSON(t, rr, &listResp)
	var found *ChannelMessageV2Response
	for i := range listResp.Messages {
		if listResp.Messages[i].ID == msg.ID {
			found = &listResp.Messages[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("trigger message %s not found in response", msg.ID)
	}
	if len(found.AgentTasks) != 1 {
		t.Fatalf("message agent_tasks length = %d, want 1: %+v", len(found.AgentTasks), found.AgentTasks)
	}
	task := found.AgentTasks[0]
	if task.AgentID != agentID || task.ChannelMessageID != msg.ID || task.Kind != "channel_mention" {
		t.Fatalf("unexpected channel agent task: %+v", task)
	}
	if task.AgentName == "" {
		t.Fatalf("expected agent name in channel task response: %+v", task)
	}
}

// TestV2ChannelMessagesResolveAuthorNames verifies the message timeline
// surfaces the real Member/Agent display name (author_name) so the UI no longer
// has to fall back to the bare author_type ("member"/"agent"). A member-authored
// message resolves the user's name; an agent-authored message resolves the
// agent's name; a system message (no author) carries no name.
func TestV2ChannelMessagesResolveAuthorNames(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})

	agentID := createHandlerTestAgent(t, "Channel Author Agent", nil)
	channel := createChannelForMentionTest(t, "Author Name Channel", "open")
	chUUID := util.MustParseUUID(channel.ID)
	wsUUID := util.MustParseUUID(testWorkspaceID)

	// Member-authored message via the normal send path.
	memberMsg := sendChannelMessageForMentionTest(t, channel.ID, testUserID, "member says hi")

	// Agent-authored message inserted directly (agents post via the daemon CLI).
	agentMsg, err := testHandler.Queries.CreateChannelMessageTopLevel(ctx, db.CreateChannelMessageTopLevelParams{
		ChannelID:   chUUID,
		WorkspaceID: wsUUID,
		AuthorType:  "agent",
		AuthorID:    util.MustParseUUID(agentID),
		Content:     "agent says hi",
	})
	if err != nil {
		t.Fatalf("insert agent message: %v", err)
	}

	rr := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/", nil), "id", channel.ID)
	testHandler.ListChannelMessages(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ListChannelMessages: %d (%s)", rr.Code, rr.Body.String())
	}
	var listResp struct {
		Messages []ChannelMessageV2Response `json:"messages"`
	}
	decodeJSON(t, rr, &listResp)

	byID := make(map[string]ChannelMessageV2Response, len(listResp.Messages))
	for _, m := range listResp.Messages {
		byID[m.ID] = m
	}

	gotMember, ok := byID[memberMsg.ID]
	if !ok {
		t.Fatalf("member message %s missing from list", memberMsg.ID)
	}
	if gotMember.AuthorName == nil || *gotMember.AuthorName != handlerTestName {
		t.Fatalf("member author_name = %v, want %q", gotMember.AuthorName, handlerTestName)
	}

	gotAgent, ok := byID[uuidToString(agentMsg.ID)]
	if !ok {
		t.Fatalf("agent message missing from list")
	}
	if gotAgent.AuthorName == nil || *gotAgent.AuthorName != "Channel Author Agent" {
		t.Fatalf("agent author_name = %v, want %q", gotAgent.AuthorName, "Channel Author Agent")
	}

	// The send response itself should also carry the resolved name.
	if memberMsg.AuthorName == nil || *memberMsg.AuthorName != handlerTestName {
		t.Fatalf("send response author_name = %v, want %q", memberMsg.AuthorName, handlerTestName)
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

// TestV2ReplyMultibyteThreadTitle verifies that replying to a message whose
// content has a multi-byte UTF-8 rune straddling the 50-byte title-truncation
// boundary succeeds. A naive title[:50] slices the rune mid-sequence, producing
// invalid UTF-8 that PostgreSQL rejects on INSERT (SQLSTATE 22021) — which
// surfaced to users as a generic "failed to create reply thread" and made the
// message unreplyable. Any CJK/emoji content near the byte boundary hit this.
func TestV2ReplyMultibyteThreadTitle(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})

	rr := httptest.NewRecorder()
	testHandler.CreateChannel(rr, newRequest(http.MethodPost, "/api/channels", map[string]any{
		"name": "Multibyte Title Test",
	}))
	var channel ChannelResponse
	decodeJSON(t, rr, &channel)

	// 49 ASCII bytes + a 3-byte CJK rune ("中" = e4 b8 ad) + a suffix. The
	// rune's lead byte sits at byte index 49, so a [:50] truncation would
	// include it without its two continuation bytes → invalid UTF-8.
	content := strings.Repeat("x", 49) + "中" + "后缀内容"
	rr = httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/", map[string]any{
		"content": content,
	}), "id", channel.ID)
	testHandler.SendChannelMessage(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("SendChannelMessage: %d (%s)", rr.Code, rr.Body.String())
	}
	var origMsg ChannelMessageV2Response
	decodeJSON(t, rr, &origMsg)

	// Reply — auto-creates a thread titled from the (truncated) message content.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodPost, "/", map[string]any{
		"content": "a reply",
	}), "id", channel.ID)
	req = withURLParam(req, "msgId", origMsg.ID)
	testHandler.ReplyToMessage(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("ReplyToMessage on multibyte content: expected 201, got %d (%s)", rr.Code, rr.Body.String())
	}
	var replyResp struct {
		Message ChannelMessageV2Response `json:"message"`
		Thread  ChannelThreadResponse     `json:"thread"`
	}
	decodeJSON(t, rr, &replyResp)
	if replyResp.Thread.ID == "" {
		t.Fatal("expected thread to be created on reply")
	}
	// The auto-derived title must be valid UTF-8 (the regression) and a
	// prefix of the content truncated before — not inside — the CJK rune.
	if !utf8.ValidString(replyResp.Thread.Title) {
		t.Fatalf("thread title is not valid UTF-8: %q", replyResp.Thread.Title)
	}
	if want := strings.Repeat("x", 49); replyResp.Thread.Title != want {
		t.Fatalf("thread title = %q, want %q (truncated before the multi-byte rune)", replyResp.Thread.Title, want)
	}
}

// TestV2AgentReplyAutoCreatesThreadWithNullCreator verifies that an agent
// replying to a fresh top-level message auto-creates a thread. The hazard:
// channel_thread.created_by REFERENCES "user"(id), but agents live in the
// agent table — stamping the agent UUID into created_by violates the FK and
// the reply fails with a generic "failed to create reply thread". This is the
// @-mention-then-agent-replies path (the daemon instructs the agent to reply
// so it "auto-creates a thread"). Agents must get a NULL created_by, mirroring
// the CreateThread handler.
func TestV2AgentReplyAutoCreatesThreadWithNullCreator(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})

	// Open channel + a top-level message authored by the test user.
	rr := httptest.NewRecorder()
	testHandler.CreateChannel(rr, newRequest(http.MethodPost, "/api/channels", map[string]any{
		"name": "Agent Reply Thread Test", "access_mode": "open",
	}))
	var channel ChannelResponse
	decodeJSON(t, rr, &channel)

	rr = httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/", map[string]any{"content": "Agent please reply"}), "id", channel.ID)
	testHandler.SendChannelMessage(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("SendChannelMessage: %d (%s)", rr.Code, rr.Body.String())
	}
	var origMsg ChannelMessageV2Response
	decodeJSON(t, rr, &origMsg)

	// An agent (in the test workspace) + a running task that authorises the
	// X-Agent-ID / X-Task-ID header pair resolveActor checks.
	agentID := createHandlerTestAgent(t, "agent-reply-thread-agent", nil)
	taskID := createHandlerTestTaskForAgent(t, agentID)

	// Agent replies → auto-creates a thread.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodPost, "/", map[string]any{"content": "agent reply"}), "id", channel.ID)
	req = withURLParam(req, "msgId", origMsg.ID)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	testHandler.ReplyToMessage(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("agent ReplyToMessage: expected 201, got %d (%s)", rr.Code, rr.Body.String())
	}
	var replyResp struct {
		Message ChannelMessageV2Response `json:"message"`
		Thread  ChannelThreadResponse    `json:"thread"`
	}
	decodeJSON(t, rr, &replyResp)
	if replyResp.Thread.ID == "" {
		t.Fatal("expected thread to be created on agent reply")
	}

	// The agent-created thread's created_by must be NULL — an agent UUID would
	// violate the channel_thread_created_by_fkey. Assert both the response
	// (CreatedBy is omitted when NULL) and the persisted row.
	if replyResp.Thread.CreatedBy != nil && *replyResp.Thread.CreatedBy != "" {
		t.Fatalf("agent-created thread created_by = %q, want NULL", *replyResp.Thread.CreatedBy)
	}
	var dbCreatedBy *string
	if err := testPool.QueryRow(ctx,
		`SELECT created_by::text FROM channel_thread WHERE id = $1`, replyResp.Thread.ID,
	).Scan(&dbCreatedBy); err != nil {
		t.Fatalf("query thread created_by: %v", err)
	}
	if dbCreatedBy != nil && *dbCreatedBy != "" {
		t.Fatalf("persisted thread created_by = %q, want NULL (FK to user forbids agent UUID)", *dbCreatedBy)
	}
}

// TestV2ConvertMessageToIssueNoThread was removed alongside the dead
// ConvertMessageToIssue handler (OPE-3457): manual message→issue conversion now
// flows through CreateIssue with source_channel_id/source_message_id, which
// reuses ensureThreadForMessage + linkIssueToExistingThread — the same
// helpers the agent quick-create completion path uses. That live path is
// covered by the CreateIssue / quick-create tests.

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

func createChannelForMentionTest(t *testing.T, name string, accessMode string) ChannelResponse {
	t.Helper()
	rr := httptest.NewRecorder()
	body := map[string]any{"name": name}
	if accessMode != "" {
		body["access_mode"] = accessMode
	}
	testHandler.CreateChannel(rr, newRequest(http.MethodPost, "/api/channels", body))
	if rr.Code != http.StatusCreated {
		t.Fatalf("CreateChannel: %d (%s)", rr.Code, rr.Body.String())
	}
	var channel ChannelResponse
	decodeJSON(t, rr, &channel)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel WHERE id = $1`, channel.ID)
	})
	return channel
}

func addChannelMemberForMentionTest(t *testing.T, channelID, userID, role string) {
	t.Helper()
	if role == "" {
		role = "member"
	}
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO channel_member (channel_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (channel_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		channelID, userID, role); err != nil {
		t.Fatalf("add channel member: %v", err)
	}
}

func sendChannelMessageForMentionTest(t *testing.T, channelID, authorID, content string) ChannelMessageV2Response {
	t.Helper()
	req := withURLParam(newRequestAs(authorID, http.MethodPost, "/", map[string]any{
		"content": content,
	}), "id", channelID)
	rr := httptest.NewRecorder()
	testHandler.SendChannelMessage(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("SendChannelMessage: %d (%s)", rr.Code, rr.Body.String())
	}
	var msg ChannelMessageV2Response
	decodeJSON(t, rr, &msg)
	return msg
}

func countChannelInboxItemsForUser(t *testing.T, recipientID, channelID string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM inbox_item
		WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND type = 'mentioned'
		  AND details->>'source_type' = 'channel_message'
		  AND details->>'channel_id' = $3
	`, testWorkspaceID, recipientID, channelID).Scan(&count); err != nil {
		t.Fatalf("count inbox items: %v", err)
	}
	return count
}

func channelMentionTasksForAgent(t *testing.T, agentID, channelID string) []db.AgentTaskQueue {
	t.Helper()
	taskRows, err := testPool.Query(context.Background(), `
		SELECT id, agent_id, issue_id, status, priority, dispatched_at, started_at, completed_at,
		       result, error, created_at, context, runtime_id, session_id, work_dir,
		       trigger_comment_id, chat_session_id, autopilot_run_id, attempt, max_attempts,
		       parent_task_id, failure_reason, trigger_summary, force_fresh_session,
		       is_leader_task, wait_reason, channel_id, channel_message_id, channel_thread_id, channel_reply_to_id
		FROM agent_task_queue
		WHERE agent_id = $1 AND channel_id = $2
		ORDER BY created_at ASC
	`, agentID, channelID)
	if err != nil {
		t.Fatalf("query tasks: %v", err)
	}
	defer taskRows.Close()
	var rows []db.AgentTaskQueue
	for taskRows.Next() {
		var task db.AgentTaskQueue
		if err := taskRows.Scan(
			&task.ID, &task.AgentID, &task.IssueID, &task.Status, &task.Priority, &task.DispatchedAt,
			&task.StartedAt, &task.CompletedAt, &task.Result, &task.Error, &task.CreatedAt,
			&task.Context, &task.RuntimeID, &task.SessionID, &task.WorkDir, &task.TriggerCommentID,
			&task.ChatSessionID, &task.AutopilotRunID, &task.Attempt, &task.MaxAttempts,
			&task.ParentTaskID, &task.FailureReason, &task.TriggerSummary, &task.ForceFreshSession,
			&task.IsLeaderTask, &task.WaitReason, &task.ChannelID, &task.ChannelMessageID,
			&task.ChannelThreadID, &task.ChannelReplyToID,
		); err != nil {
			t.Fatalf("scan task: %v", err)
		}
		rows = append(rows, task)
	}
	if err := taskRows.Err(); err != nil {
		t.Fatalf("iterate tasks: %v", err)
	}
	return rows
}

func TestV2ChannelMemberMentionCreatesInboxWithNullIssueAndChannelDetails(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	recipientID := createWorkspaceMemberUser(t, "Channel Mention Recipient", "channel-mention-recipient@multica.test")
	channel := createChannelForMentionTest(t, "Mention Inbox Channel", "open")

	sendChannelMessageForMentionTest(t, channel.ID, testUserID,
		"please review [@Recipient](mention://member/"+recipientID+")")

	rows, err := testHandler.Queries.ListInboxItems(ctx, db.ListInboxItemsParams{
		WorkspaceID:   util.MustParseUUID(testWorkspaceID),
		RecipientType: "member",
		RecipientID:   util.MustParseUUID(recipientID),
	})
	if err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 inbox item for explicit member mention, got %d", len(rows))
	}
	item := rows[0]
	if item.IssueID.Valid {
		t.Fatalf("channel inbox item must not reference an issue, got %s", uuidToString(item.IssueID))
	}
	var details map[string]any
	if err := json.Unmarshal(item.Details, &details); err != nil {
		t.Fatalf("decode details: %v", err)
	}
	if details["source_type"] != "channel_message" {
		t.Fatalf("details.source_type = %v, want channel_message", details["source_type"])
	}
	if details["channel_id"] != channel.ID {
		t.Fatalf("details.channel_id = %v, want %s", details["channel_id"], channel.ID)
	}
	if details["message_id"] == "" || details["link"] == "" {
		t.Fatalf("details must include message_id and link, got %+v", details)
	}

	var eventIssueID *string
	if err := testPool.QueryRow(ctx, `
		SELECT issue_id FROM notification_event
		WHERE workspace_id = $1 AND recipient_user_id = $2 AND type = 'mentioned'
		ORDER BY created_at DESC LIMIT 1
	`, testWorkspaceID, recipientID).Scan(&eventIssueID); err != nil {
		t.Fatalf("load notification event: %v", err)
	}
	if eventIssueID != nil {
		t.Fatalf("channel notification event must have NULL issue_id, got %s", *eventIssueID)
	}
}

func TestV2ChannelMemberMentionOpenChannelSkipsNonWorkspaceUser(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	var outsiderID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Channel Mention Outsider', 'channel-mention-outsider@multica.test')
		RETURNING id
	`).Scan(&outsiderID); err != nil {
		t.Fatalf("create outsider user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, outsiderID)
	})
	channel := createChannelForMentionTest(t, "Open Mention Visibility Channel", "open")

	sendChannelMessageForMentionTest(t, channel.ID, testUserID,
		"do not notify outsider [@Outsider](mention://member/"+outsiderID+")")

	if got := countChannelInboxItemsForUser(t, outsiderID, channel.ID); got != 0 {
		t.Fatalf("outsider inbox count = %d, want 0", got)
	}
}

func TestV2ChannelAllMentionInviteOnlyNotifiesOnlyChannelMembers(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	channelMemberID := createWorkspaceMemberUser(t, "Channel All Member", "channel-all-member@multica.test")
	workspaceOnlyID := createWorkspaceMemberUser(t, "Channel All Workspace Only", "channel-all-workspace-only@multica.test")
	channel := createChannelForMentionTest(t, "Invite All Mention Channel", "invite")
	addChannelMemberForMentionTest(t, channel.ID, channelMemberID, "member")

	sendChannelMessageForMentionTest(t, channel.ID, testUserID,
		"heads up [@All](mention://all/all)")

	if got := countChannelInboxItemsForUser(t, channelMemberID, channel.ID); got != 1 {
		t.Fatalf("channel member inbox count = %d, want 1", got)
	}
	if got := countChannelInboxItemsForUser(t, workspaceOnlyID, channel.ID); got != 0 {
		t.Fatalf("workspace-only member inbox count = %d, want 0", got)
	}
	if got := countChannelInboxItemsForUser(t, testUserID, channel.ID); got != 0 {
		t.Fatalf("@all should exclude sender by default, sender inbox count = %d", got)
	}
}

func TestV2ChannelPrivateAgentMentionRequiresTriggerPermission(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	plainMemberID := createWorkspaceMemberUser(t, "Channel Private Agent Caller", "channel-private-agent-caller@multica.test")
	privateAgentID := createHandlerTestAgent(t, "Channel Private Mention Agent", nil)
	channel := createChannelForMentionTest(t, "Private Agent Mention Channel", "open")
	addChannelMemberForMentionTest(t, channel.ID, plainMemberID, "member")

	sendChannelMessageForMentionTest(t, channel.ID, plainMemberID,
		"please run [@PrivateAgent](mention://agent/"+privateAgentID+")")

	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue
		WHERE agent_id = $1 AND channel_id = $2
	`, privateAgentID, channel.ID).Scan(&count); err != nil {
		t.Fatalf("count channel agent tasks: %v", err)
	}
	if count != 0 {
		t.Fatalf("private agent should not be enqueued by unallowed member, got %d tasks", count)
	}
}

func TestV2ChannelAgentAndSquadMentionEnqueuesLeaderOnceWithContext(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	leaderID := createHandlerTestAgent(t, "Channel Squad Leader", nil)
	squad := seedSquadForBriefing(t, leaderID, "Channel Mention Squad", "Act as the channel lead.")
	channel := createChannelForMentionTest(t, "Agent Squad Mention Channel", "open")

	sendChannelMessageForMentionTest(t, channel.ID, testUserID,
		"same leader twice [@Leader](mention://agent/"+leaderID+") [@Squad](mention://squad/"+uuidToString(squad.ID)+")")

	rows := channelMentionTasksForAgent(t, leaderID, channel.ID)
	if len(rows) != 1 {
		t.Fatalf("same message must enqueue resolved leader once, got %d tasks", len(rows))
	}
	task := rows[0]
	if task.IssueID.Valid || task.TriggerCommentID.Valid {
		t.Fatal("channel mention task must not reference issue/comment")
	}
	if !task.ChannelID.Valid || uuidToString(task.ChannelID) != channel.ID {
		t.Fatalf("task channel_id = %s, want %s", uuidToString(task.ChannelID), channel.ID)
	}
	if task.IsLeaderTask {
		t.Fatal("explicit agent mention should win first and not mark the deduped task as squad leader task")
	}
	var payload map[string]any
	if err := json.Unmarshal(task.Context, &payload); err != nil {
		t.Fatalf("decode task context: %v", err)
	}
	if payload["type"] != "channel_mention" {
		t.Fatalf("context.type = %v, want channel_mention", payload["type"])
	}
	if payload["mention_type"] != "agent" {
		t.Fatalf("context.mention_type = %v, want agent", payload["mention_type"])
	}

	squadOnlyAgentID := createHandlerTestAgent(t, "Channel Squad Only Leader", nil)
	squadOnly := seedSquadForBriefing(t, squadOnlyAgentID, "Channel Squad Only", "")
	sendChannelMessageForMentionTest(t, channel.ID, testUserID,
		"squad only [@SquadOnly](mention://squad/"+uuidToString(squadOnly.ID)+")")
	squadRows := channelMentionTasksForAgent(t, squadOnlyAgentID, channel.ID)
	if len(squadRows) != 1 {
		t.Fatalf("expected 1 squad-only task, got %d", len(squadRows))
	}
	task = squadRows[0]
	if !task.IsLeaderTask {
		t.Fatal("squad mention should enqueue leader task with is_leader_task=true")
	}
	if err := json.Unmarshal(task.Context, &payload); err != nil {
		t.Fatalf("decode squad task context: %v", err)
	}
	if payload["mention_type"] != "squad" || payload["squad_id"] != uuidToString(squadOnly.ID) {
		t.Fatalf("squad task context missing squad identity: %+v", payload)
	}
}

func TestV2ChannelMentionTasksCanResumeByAgentAndChannel(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "Channel Resume Agent", nil)
	channel := createChannelForMentionTest(t, "Resume Mention Channel", "open")

	first := sendChannelMessageForMentionTest(t, channel.ID, testUserID,
		"first [@Resume](mention://agent/"+agentID+")")
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'completed', session_id = 'resume-session-1', work_dir = '/tmp/channel-resume'
		WHERE agent_id = $1 AND channel_message_id = $2
	`, agentID, first.ID); err != nil {
		t.Fatalf("mark first task completed: %v", err)
	}
	sendChannelMessageForMentionTest(t, channel.ID, testUserID,
		"second [@Resume](mention://agent/"+agentID+")")

	row, err := testHandler.Queries.GetLastChannelTaskSession(ctx, db.GetLastChannelTaskSessionParams{
		AgentID:   util.MustParseUUID(agentID),
		ChannelID: util.MustParseUUID(channel.ID),
	})
	if err != nil {
		t.Fatalf("GetLastChannelTaskSession: %v", err)
	}
	if row.SessionID.String != "resume-session-1" || row.WorkDir.String != "/tmp/channel-resume" {
		t.Fatalf("last channel session = (%q, %q), want resume-session-1 and workdir",
			row.SessionID.String, row.WorkDir.String)
	}
}

func TestV2ChannelContextIncludesTriggerAndReplies(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	channel := createChannelForMentionTest(t, "Context Mention Channel", "open")
	root := sendChannelMessageForMentionTest(t, channel.ID, testUserID, "root trigger message")
	req := withURLParam(newRequest(http.MethodPost, "/", map[string]any{
		"content": "reply context",
	}), "id", channel.ID)
	req = withURLParam(req, "msgId", root.ID)
	rr := httptest.NewRecorder()
	testHandler.ReplyToMessage(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("ReplyToMessage: %d (%s)", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodGet, "/?message="+root.ID+"&include-replies=true&recent=10", nil), "id", channel.ID)
	testHandler.GetChannelContext(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GetChannelContext: %d (%s)", rr.Code, rr.Body.String())
	}
	var resp ChannelContextResponse
	decodeJSON(t, rr, &resp)
	if resp.TriggerMessage == nil || resp.TriggerMessage.ID != root.ID {
		t.Fatalf("expected trigger_message %s, got %+v", root.ID, resp.TriggerMessage)
	}
	if len(resp.Messages) == 0 {
		t.Fatal("expected recent top-level messages")
	}
	if len(resp.Replies) != 1 || resp.Replies[0].Content != "reply context" {
		t.Fatalf("expected reply context, got %+v", resp.Replies)
	}
}

func TestV2ChannelMentionTaskCompletionFallsBackToTopLevelChannelMessage(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "Channel Completion Agent", nil)
	channel := createChannelForMentionTest(t, "Completion Channel", "open")
	trigger := sendChannelMessageForMentionTest(t, channel.ID, testUserID,
		"please report here [@Completion](mention://agent/"+agentID+")")
	tasks := channelMentionTasksForAgent(t, agentID, channel.ID)
	if len(tasks) != 1 {
		t.Fatalf("expected one channel task, got %d", len(tasks))
	}
	task := tasks[0]
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue SET status = 'running', started_at = now()
		WHERE id = $1
	`, task.ID); err != nil {
		t.Fatalf("mark task running: %v", err)
	}
	payload, err := json.Marshal(protocol.TaskCompletedPayload{
		TaskID: uuidToString(task.ID),
		Output: "完成：已处理频道请求",
	})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	completed, err := testHandler.TaskService.CompleteTask(ctx, task.ID, payload, "channel-session", "/tmp/channel-task")
	if err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if completed == nil || completed.Status != "completed" {
		t.Fatalf("task not completed: %+v", completed)
	}

	var count int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM channel_message
		WHERE channel_id = $1
		  AND thread_id IS NULL
		  AND author_type = 'agent'
		  AND author_id = $2
		  AND content = '完成：已处理频道请求'
	`, channel.ID, agentID).Scan(&count); err != nil {
		t.Fatalf("count fallback channel messages: %v", err)
	}
	if count != 1 {
		t.Fatalf("fallback channel message count = %d, want 1", count)
	}
	if trigger.ID == "" {
		t.Fatal("trigger sanity check failed")
	}
}

// TestV2ListChannelMessagesAround verifies the ?around=<id> deep-link: the
// target message plus a window of older and newer top-level messages are
// returned in ASC order, and has_more reflects older history beyond the
// window. This is what makes a copied message link land on the right message
// even when it falls outside the latest page.
func TestV2ListChannelMessagesAround(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
	})

	rr := httptest.NewRecorder()
	testHandler.CreateChannel(rr, newRequest(http.MethodPost, "/api/channels", map[string]any{
		"name": "Around Test Channel",
	}))
	if rr.Code != http.StatusCreated {
		t.Fatalf("CreateChannel: %d (%s)", rr.Code, rr.Body.String())
	}
	var channel ChannelResponse
	decodeJSON(t, rr, &channel)

	// Send 5 top-level messages; record their IDs in send order (ASC by
	// created_at, which monotonic per insert).
	send := func(content string) string {
		t.Helper()
		rr := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPost, "/", map[string]any{
			"content": content,
		}), "id", channel.ID)
		testHandler.SendChannelMessage(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("SendChannelMessage: %d (%s)", rr.Code, rr.Body.String())
		}
		var m ChannelMessageV2Response
		decodeJSON(t, rr, &m)
		return m.ID
	}
	ids := []string{
		send("msg-1"),
		send("msg-2"),
		send("msg-3"),
		send("msg-4"),
		send("msg-5"),
	}
	targetID := ids[2] // msg-3, the middle message.

	// List with ?around=target&limit=4 → half=2: up to 2 older, target, up to 2 newer.
	rr = httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/?around="+targetID+"&limit=4", nil), "id", channel.ID)
	testHandler.ListChannelMessages(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ListChannelMessages around: %d (%s)", rr.Code, rr.Body.String())
	}
	var aroundResp struct {
		Messages []ChannelMessageV2Response `json:"messages"`
		HasMore  bool                       `json:"has_more"`
	}
	decodeJSON(t, rr, &aroundResp)

	// Expect: msg-1, msg-2, msg-3(target), msg-4, msg-5 — but limit=4 caps the
	// window at half=2 older + target + half=2 newer = 5 rows; since only 2
	// older exist and 2 newer exist, all 5 fit and has_more must be false.
	if len(aroundResp.Messages) != 5 {
		t.Fatalf("expected 5 messages in around window, got %d", len(aroundResp.Messages))
	}
	gotIDs := make([]string, 5)
	for i, m := range aroundResp.Messages {
		gotIDs[i] = m.ID
	}
	wantASC := ids // already ASC
	for i := range wantASC {
		if gotIDs[i] != wantASC[i] {
			t.Fatalf("around order mismatch at %d: got %v, want %v", i, gotIDs, wantASC)
		}
	}
	if aroundResp.HasMore {
		t.Fatalf("expected has_more=false (no older history beyond window), got true")
	}

	// Now target the oldest message (msg-1): no older side, target, 2 newer.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodGet, "/?around="+ids[0]+"&limit=4", nil), "id", channel.ID)
	testHandler.ListChannelMessages(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ListChannelMessages around oldest: %d (%s)", rr.Code, rr.Body.String())
	}
	decodeJSON(t, rr, &aroundResp)
	if len(aroundResp.Messages) != 3 {
		t.Fatalf("expected 3 messages (target + 2 newer), got %d", len(aroundResp.Messages))
	}
	if aroundResp.Messages[0].ID != ids[0] {
		t.Fatalf("expected oldest target first, got %s", aroundResp.Messages[0].ID)
	}
	if aroundResp.HasMore {
		t.Fatalf("expected has_more=false for oldest target, got true")
	}

	// has_more=true case: add 3 more older messages by targeting msg-1 with a
	// tiny window so older history exists beyond it. Re-target msg-3 with
	// limit=2 (half=1): 1 older + target + 1 newer; but 2 older exist beyond
	// the 1 returned → has_more=true.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodGet, "/?around="+targetID+"&limit=2", nil), "id", channel.ID)
	testHandler.ListChannelMessages(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ListChannelMessages around small window: %d (%s)", rr.Code, rr.Body.String())
	}
	decodeJSON(t, rr, &aroundResp)
	// half=1 → 1 older (msg-2) + target (msg-3) + 1 newer (msg-4) = 3 rows.
	if len(aroundResp.Messages) != 3 {
		t.Fatalf("expected 3 messages in small around window, got %d", len(aroundResp.Messages))
	}
	if aroundResp.Messages[1].ID != targetID {
		t.Fatalf("expected target in middle position, got %v", aroundResp.Messages)
	}
	if !aroundResp.HasMore {
		t.Fatalf("expected has_more=true (older history msg-1 beyond window), got false")
	}

	// Invalid around id (not a UUID) → 400.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodGet, "/?around=not-a-uuid", nil), "id", channel.ID)
	testHandler.ListChannelMessages(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid around id, got %d", rr.Code)
	}

	// Stale link: a well-formed UUID that doesn't exist falls back to the
	// latest page (200) instead of erroring — a shared link must still open
	// the channel even if the message was deleted.
	rr = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodGet, "/?around="+util.MustParseUUID("00000000-0000-0000-0000-000000000000").String(), nil), "id", channel.ID)
	testHandler.ListChannelMessages(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for stale around link (fallback to latest), got %d (%s)", rr.Code, rr.Body.String())
	}
	decodeJSON(t, rr, &aroundResp)
	if len(aroundResp.Messages) == 0 {
		t.Fatalf("fallback to latest returned no messages")
	}
}
