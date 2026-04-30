package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// enableChannels flips workspace.channels_enabled to TRUE for the shared
// test workspace and registers a t.Cleanup that flips it back and wipes any
// channels the test created. Tests that exercise the Channels feature must
// call this from setup, otherwise every endpoint correctly returns 404 (the
// default workspace state).
func enableChannels(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `UPDATE workspace SET channels_enabled = TRUE WHERE id = $1`, testWorkspaceID); err != nil {
		t.Fatalf("enable channels: %v", err)
	}
	t.Cleanup(func() {
		// Order matters: drop channels (cascades to memberships and messages)
		// before flipping the flag so any in-flight publishers don't see a
		// half-disabled state.
		testPool.Exec(context.Background(), `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = 'channel_mention'`, testWorkspaceID)
		testPool.Exec(context.Background(), `UPDATE workspace SET channels_enabled = FALSE WHERE id = $1`, testWorkspaceID)
	})
}

func decodeChannel(t *testing.T, w *httptest.ResponseRecorder) ChannelResponse {
	t.Helper()
	var resp ChannelResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode channel response: %v", err)
	}
	return resp
}

// TestChannels_Disabled404 — every Channels endpoint must 404 when the
// workspace flag is off. This is the spec's "invisible feature" guarantee.
func TestChannels_Disabled404(t *testing.T) {
	// Note: we deliberately do NOT call enableChannels here. The fixture
	// default has the flag off.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name": "general", "visibility": "public",
	})
	testHandler.CreateChannel(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("CreateChannel with flag off: want 404, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/channels", nil)
	testHandler.ListChannels(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("ListChannels with flag off: want 404, got %d", w.Code)
	}
}

func TestCreateChannel_Happy(t *testing.T) {
	enableChannels(t)

	// Subscribe to the bus before creating so we can verify the publish.
	// Bus subscriptions can't be unsubscribed — handlers stay registered for
	// the bus's lifetime, so we use atomic.Value rather than channels to
	// avoid leaking goroutines across tests.
	var publishedID atomic.Value
	testHandler.Bus.Subscribe(protocol.EventChannelCreated, func(e events.Event) {
		if payload, ok := e.Payload.(map[string]any); ok {
			if c, ok := payload["channel"].(ChannelResponse); ok {
				publishedID.Store(c.ID)
			}
		}
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name":         "general",
		"display_name": "General",
		"description":  "Workspace-wide chatter",
		"visibility":   "public",
	})
	testHandler.CreateChannel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateChannel: want 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeChannel(t, w)
	if resp.Name != "general" || resp.Visibility != "public" || resp.Kind != "channel" {
		t.Fatalf("unexpected channel response: %+v", resp)
	}

	// Wait briefly for the bus subscriber goroutine to fire.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if v := publishedID.Load(); v != nil && v.(string) == resp.ID {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("EventChannelCreated not published for channel %s", resp.ID)
}

func TestCreateChannel_RejectsInvalid(t *testing.T) {
	enableChannels(t)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"empty name", map[string]any{"name": "", "visibility": "public"}},
		{"uppercase", map[string]any{"name": "Bad", "visibility": "public"}},
		{"unknown visibility", map[string]any{"name": "ok", "visibility": "invisible"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/channels", tc.body)
			testHandler.CreateChannel(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestListAndGet_PrivateInvisibleToNonMembers(t *testing.T) {
	enableChannels(t)

	// Create a private channel as the test member.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name": "secret", "display_name": "Secret", "visibility": "private",
	})
	testHandler.CreateChannel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create private: %d %s", w.Code, w.Body.String())
	}
	priv := decodeChannel(t, w)

	// As an agent (header X-Agent-ID set), the channel should NOT appear in
	// list and direct GET should 404.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/channels", nil)
	req.Header.Set("X-Agent-ID", testAgentID())
	testHandler.ListChannels(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("agent ListChannels: want 200, got %d", w.Code)
	}
	var listed []ChannelResponse
	json.NewDecoder(w.Body).Decode(&listed)
	for _, c := range listed {
		if c.ID == priv.ID {
			t.Fatalf("private channel leaked to non-member agent: %+v", c)
		}
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/channels/"+priv.ID, nil)
	req.Header.Set("X-Agent-ID", testAgentID())
	req = withURLParam(req, "channelId", priv.ID)
	testHandler.GetChannel(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("agent GET private channel: want 404, got %d", w.Code)
	}
}

func TestArchiveChannel_HidesFromList(t *testing.T) {
	enableChannels(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name": "ephemeral", "display_name": "Ephemeral", "visibility": "public",
	})
	testHandler.CreateChannel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d", w.Code)
	}
	ch := decodeChannel(t, w)

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/channels/"+ch.ID, nil)
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.ArchiveChannel(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("archive: want 204, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/channels", nil)
	testHandler.ListChannels(w, req)
	var listed []ChannelResponse
	json.NewDecoder(w.Body).Decode(&listed)
	for _, c := range listed {
		if c.ID == ch.ID {
			t.Fatalf("archived channel still in list: %+v", c)
		}
	}
}

func TestCreateOrFetchDM_Idempotent(t *testing.T) {
	enableChannels(t)

	body := map[string]any{
		"participants": []map[string]string{
			{"type": "agent", "id": testAgentID()},
		},
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/dms", body)
	testHandler.CreateOrFetchDM(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first DM call: want 200, got %d: %s", w.Code, w.Body.String())
	}
	first := decodeChannel(t, w)
	if first.Kind != "dm" || first.Visibility != "private" {
		t.Fatalf("DM has wrong kind/visibility: %+v", first)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/dms", body)
	testHandler.CreateOrFetchDM(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second DM call: want 200, got %d", w.Code)
	}
	second := decodeChannel(t, w)
	if first.ID != second.ID {
		t.Fatalf("expected idempotent DM, got %s vs %s", first.ID, second.ID)
	}
}

func TestCreateChannelMessage_PublishesAndCreatesInbox(t *testing.T) {
	enableChannels(t)

	// Create a channel.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name": "talk", "display_name": "Talk", "visibility": "public",
	})
	testHandler.CreateChannel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create channel: %d", w.Code)
	}
	ch := decodeChannel(t, w)

	// Make a second member to mention.
	ctx := context.Background()
	var otherUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "Channel Other", "channel-other-"+ch.ID+"@multica.local").Scan(&otherUserID); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, otherUserID)
	})
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, otherUserID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	// Subscribe to channel:message events. Bus subscriptions persist for the
	// process lifetime; the handler closes over a per-test atomic.Value so
	// it remains harmless after this test completes.
	var gotMessageID atomic.Value
	testHandler.Bus.Subscribe(protocol.EventChannelMessage, func(e events.Event) {
		if payload, ok := e.Payload.(map[string]any); ok {
			if msg, ok := payload["message"].(ChannelMessageResponse); ok {
				gotMessageID.Store(msg.ID)
			}
		}
	})

	// Post a message that mentions the other member via the markdown form.
	mention := "[@Other](mention://member/" + otherUserID + ")"
	body := map[string]any{
		"content": "hi " + mention + " welcome",
	}
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/messages", body)
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.CreateChannelMessage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateChannelMessage: want 201, got %d: %s", w.Code, w.Body.String())
	}
	var msg ChannelMessageResponse
	json.NewDecoder(w.Body).Decode(&msg)
	if msg.ChannelID != ch.ID {
		t.Fatalf("message ChannelID = %s, want %s", msg.ChannelID, ch.ID)
	}

	// WS event published.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if v := gotMessageID.Load(); v != nil && v.(string) == msg.ID {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if v := gotMessageID.Load(); v == nil || v.(string) != msg.ID {
		t.Fatalf("EventChannelMessage not published for %s", msg.ID)
	}

	// Inbox row written. Inbox writes are async (goroutine); poll briefly.
	deadline = time.Now().Add(2 * time.Second)
	var inboxCount int
	for time.Now().Before(deadline) {
		row := testPool.QueryRow(ctx, `
			SELECT count(*) FROM inbox_item
			WHERE workspace_id = $1 AND recipient_id = $2 AND type = 'channel_mention'
		`, testWorkspaceID, otherUserID)
		_ = row.Scan(&inboxCount)
		if inboxCount >= 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("expected at least 1 inbox_item for the mentioned user, got %d", inboxCount)
}

func TestMarkChannelRead(t *testing.T) {
	enableChannels(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name": "tracked", "display_name": "Tracked", "visibility": "public",
	})
	testHandler.CreateChannel(w, req)
	ch := decodeChannel(t, w)

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/messages", map[string]any{"content": "first"})
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.CreateChannelMessage(w, req)
	var msg ChannelMessageResponse
	json.NewDecoder(w.Body).Decode(&msg)

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/read", map[string]any{"message_id": msg.ID})
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.MarkChannelRead(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("MarkChannelRead: want 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify membership row updated.
	var lastReadID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT last_read_message_id::text FROM channel_membership
		WHERE channel_id = $1 AND member_type = 'member' AND member_id = $2
	`, ch.ID, testUserID).Scan(&lastReadID); err != nil {
		t.Fatalf("query membership: %v", err)
	}
	if !strings.EqualFold(lastReadID, msg.ID) {
		t.Fatalf("last_read_message_id = %q, want %q", lastReadID, msg.ID)
	}
}

func TestAddRemoveChannelMember(t *testing.T) {
	enableChannels(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name": "joinable", "display_name": "Joinable", "visibility": "private",
	})
	testHandler.CreateChannel(w, req)
	ch := decodeChannel(t, w)

	// Add the agent as a member.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/members", map[string]any{
		"member_type": "agent", "member_id": testAgentID(), "role": "member",
	})
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.AddChannelMember(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("AddChannelMember: want 201, got %d: %s", w.Code, w.Body.String())
	}

	// As that agent, GetChannel should now succeed.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/channels/"+ch.ID, nil)
	req.Header.Set("X-Agent-ID", testAgentID())
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.GetChannel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("agent GET after join: want 200, got %d", w.Code)
	}

	// Remove the member. withURLParams takes flat key/value pairs and
	// preserves any params previously injected on the request.
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/channels/"+ch.ID+"/members/agent/"+testAgentID(), nil)
	req = withURLParams(req, "channelId", ch.ID, "memberType", "agent", "memberId", testAgentID())
	testHandler.RemoveChannelMember(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("RemoveChannelMember: want 204, got %d", w.Code)
	}

	// As that agent, GetChannel should now 404 again.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/channels/"+ch.ID, nil)
	req.Header.Set("X-Agent-ID", testAgentID())
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.GetChannel(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("agent GET after removal: want 404, got %d", w.Code)
	}
}

// TestCreateChannelMessage_TriggersAgentMention exercises the Phase 3a
// fan-out: posting a message that @-mentions an agent who's a channel
// member should enqueue a task with context.type="channel_mention". The
// goroutine fan-out is async, so we poll briefly.
func TestCreateChannelMessage_TriggersAgentMention(t *testing.T) {
	enableChannels(t)
	ctx := context.Background()

	// Create a channel and add the test agent as a member.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name": "trig-handler", "display_name": "TrigHandler", "visibility": "public",
	})
	testHandler.CreateChannel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create channel: %d", w.Code)
	}
	ch := decodeChannel(t, w)

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/members", map[string]any{
		"member_type": "agent", "member_id": testAgentID(), "role": "member",
	})
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.AddChannelMember(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("add agent: %d %s", w.Code, w.Body.String())
	}

	// Drop any tasks left from prior tests so the dedup query starts clean.
	testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, testAgentID())
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, testAgentID())
	})

	// Post a message mentioning the agent.
	mention := "[@bot](mention://agent/" + testAgentID() + ")"
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/messages", map[string]any{
		"content": "hey " + mention + " can you take a look",
	})
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.CreateChannelMessage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create message: %d %s", w.Code, w.Body.String())
	}

	// Poll for the task. Fan-out is async; 2s is generous.
	deadline := time.Now().Add(2 * time.Second)
	var taskCount int
	for time.Now().Before(deadline) {
		row := testPool.QueryRow(ctx, `
			SELECT count(*) FROM agent_task_queue
			WHERE agent_id = $1
			  AND context->>'type' = 'channel_mention'
			  AND context->>'channel_id' = $2
		`, testAgentID(), ch.ID)
		_ = row.Scan(&taskCount)
		if taskCount >= 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("expected at least 1 channel-mention task, got %d", taskCount)
}

// TestCompleteChannelMentionTask_PostsReply exercises Phase 3b end to end:
// post a channel message with @agent mention → task gets enqueued (3a) →
// simulate the daemon completing the task with output → verify a new
// channel_message lands with author_type='agent' and author_id matching.
func TestCompleteChannelMentionTask_PostsReply(t *testing.T) {
	enableChannels(t)
	ctx := context.Background()

	// Create the channel and add the test agent as a member.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name": "complete-test", "display_name": "CompleteTest", "visibility": "public",
	})
	testHandler.CreateChannel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create channel: %d %s", w.Code, w.Body.String())
	}
	ch := decodeChannel(t, w)

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/members", map[string]any{
		"member_type": "agent", "member_id": testAgentID(), "role": "member",
	})
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.AddChannelMember(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("add agent: %d", w.Code)
	}

	// Clean any prior tasks to avoid dedup interference.
	testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, testAgentID())
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, testAgentID())
	})

	// Post a message that mentions the agent → enqueues a task.
	mention := "[@bot](mention://agent/" + testAgentID() + ")"
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/messages", map[string]any{
		"content": "hey " + mention + " please respond",
	})
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.CreateChannelMessage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create message: %d", w.Code)
	}

	// Wait for the async fan-out to enqueue the task.
	var taskID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		row := testPool.QueryRow(ctx, `
			SELECT id::text FROM agent_task_queue
			WHERE agent_id = $1
			  AND context->>'type' = 'channel_mention'
			  AND context->>'channel_id' = $2
			LIMIT 1
		`, testAgentID(), ch.ID)
		_ = row.Scan(&taskID)
		if taskID != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if taskID == "" {
		t.Fatalf("no channel-mention task was enqueued")
	}

	// Mark the task as 'running' so CompleteTask's state-machine
	// guard accepts it (the prod path always goes queued → dispatched →
	// running → completed).
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'running',
		    started_at = now(),
		    dispatched_at = now()
		WHERE id = $1
	`, taskID); err != nil {
		t.Fatalf("set running: %v", err)
	}

	// Simulate the daemon's CompleteTask call. The result payload mirrors
	// the daemon's protocol.TaskCompletedPayload.
	taskUUID := parseUUID(taskID)
	resultJSON, _ := json.Marshal(map[string]any{
		"output": "Hi! I'm the agent — thanks for tagging me.",
	})
	if _, err := testHandler.TaskService.CompleteTask(ctx, taskUUID, resultJSON, "" /*sessionID*/, "" /*workDir*/); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	// Verify the agent's reply landed as a channel_message.
	var (
		count       int
		authorType  string
		authorID    string
		content     string
	)
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM channel_message
		WHERE channel_id = $1 AND author_type = 'agent'
	`, ch.ID).Scan(&count); err != nil {
		t.Fatalf("count agent messages: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 agent reply, got %d", count)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT author_type, author_id::text, content FROM channel_message
		WHERE channel_id = $1 AND author_type = 'agent'
		ORDER BY created_at DESC LIMIT 1
	`, ch.ID).Scan(&authorType, &authorID, &content); err != nil {
		t.Fatalf("fetch agent reply: %v", err)
	}
	if authorID != testAgentID() {
		t.Fatalf("author_id = %q, want agent id %q", authorID, testAgentID())
	}
	if !strings.Contains(content, "Hi! I'm the agent") {
		t.Fatalf("reply content unexpected: %q", content)
	}
}

// Phase 5b — attachments -----------------------------------------------------

// seedTestAttachment writes a row directly via the queries layer. We
// don't actually upload bytes — that would require S3 + the multipart
// form path. The handler's create-message branch only validates the
// attachment row exists, lives in the workspace, and was uploaded by
// the calling actor; the bytes themselves are irrelevant to the
// metadata-stamping behavior we're testing here.
func seedTestAttachment(t *testing.T, uploaderType string, uploaderID string) string {
	t.Helper()
	ctx := context.Background()
	id := uuid.Must(uuid.NewV7())
	_, err := testHandler.Queries.CreateAttachment(ctx, db.CreateAttachmentParams{
		ID:           pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID:  parseUUID(testWorkspaceID),
		UploaderType: uploaderType,
		UploaderID:   parseUUID(uploaderID),
		Filename:     "test.png",
		Url:          "https://example.invalid/" + id.String() + ".png",
		ContentType:  "image/png",
		SizeBytes:    1024,
	})
	if err != nil {
		t.Fatalf("seed attachment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM attachment WHERE id = $1`, id)
	})
	return id.String()
}

func TestChannelMessage_AttachmentIDsStoredAndHydrated(t *testing.T) {
	enableChannels(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name": "att-test", "display_name": "AttTest", "visibility": "public",
	})
	testHandler.CreateChannel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create channel: %d", w.Code)
	}
	ch := decodeChannel(t, w)

	att1 := seedTestAttachment(t, "member", testUserID)
	att2 := seedTestAttachment(t, "member", testUserID)

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/messages", map[string]any{
		"content":        "see image",
		"attachment_ids": []string{att1, att2},
	})
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.CreateChannelMessage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create message: %d %s", w.Code, w.Body.String())
	}
	var created ChannelMessageResponse
	json.NewDecoder(w.Body).Decode(&created)
	if len(created.Attachments) != 2 {
		t.Fatalf("create response attachments len = %d, want 2", len(created.Attachments))
	}
	// Order from the request must be preserved on the response.
	if created.Attachments[0].ID != att1 || created.Attachments[1].ID != att2 {
		t.Fatalf("attachment order not preserved: got %v, want [%s,%s]",
			[]string{created.Attachments[0].ID, created.Attachments[1].ID}, att1, att2)
	}

	// Round-trip via the list endpoint — attachments must reappear.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/channels/"+ch.ID+"/messages", nil)
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.ListChannelMessages(w, req)
	var listed []ChannelMessageResponse
	json.NewDecoder(w.Body).Decode(&listed)
	var hit *ChannelMessageResponse
	for i := range listed {
		if listed[i].ID == created.ID {
			hit = &listed[i]
		}
	}
	if hit == nil {
		t.Fatalf("message not in list")
	}
	if len(hit.Attachments) != 2 || hit.Attachments[0].ID != att1 {
		t.Fatalf("list hydration drift: %+v", hit.Attachments)
	}
}

func TestChannelMessage_AttachmentIDFromAnotherUploaderRejected(t *testing.T) {
	enableChannels(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name": "att-leak", "display_name": "AttLeak", "visibility": "public",
	})
	testHandler.CreateChannel(w, req)
	ch := decodeChannel(t, w)

	// Seed an attachment uploaded by an AGENT (not the calling member).
	stranger := seedTestAttachment(t, "agent", testAgentID())

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/messages", map[string]any{
		"content":        "trying to attach someone else's file",
		"attachment_ids": []string{stranger},
	})
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.CreateChannelMessage(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("foreign-attachment attach: want 400, got %d %s", w.Code, w.Body.String())
	}
}

// Phase 5 — edits + deletes -------------------------------------------------

func TestEditChannelMessage_AuthorOnly(t *testing.T) {
	enableChannels(t)
	ctx := context.Background()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name": "edit-test", "display_name": "EditTest", "visibility": "public",
	})
	testHandler.CreateChannel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create channel: %d", w.Code)
	}
	ch := decodeChannel(t, w)

	// Post a message as the test member.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/messages", map[string]any{
		"content": "original",
	})
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.CreateChannelMessage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create message: %d", w.Code)
	}
	var msg ChannelMessageResponse
	json.NewDecoder(w.Body).Decode(&msg)

	// Author edits — 200.
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/channels/"+ch.ID+"/messages/"+msg.ID, map[string]any{
		"content": "edited body",
	})
	req = withURLParams(req, "channelId", ch.ID, "messageId", msg.ID)
	testHandler.UpdateChannelMessage(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("author edit: want 200, got %d %s", w.Code, w.Body.String())
	}
	var updated ChannelMessageResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Content != "edited body" {
		t.Fatalf("content not updated: %q", updated.Content)
	}
	if updated.EditedAt == nil {
		t.Fatalf("edited_at not stamped")
	}

	// As the test agent (different actor), edit attempt → 403.
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/channels/"+ch.ID+"/messages/"+msg.ID, map[string]any{
		"content": "agent tries",
	})
	req.Header.Set("X-Agent-ID", testAgentID())
	req = withURLParams(req, "channelId", ch.ID, "messageId", msg.ID)
	testHandler.UpdateChannelMessage(w, req)
	// Note: agent isn't a member, so requireChannelAccess fires first
	// and returns 404 (don't leak that the message exists). Either 403
	// or 404 is acceptable as "you can't edit this"; we accept both.
	if w.Code != http.StatusForbidden && w.Code != http.StatusNotFound {
		t.Fatalf("non-author edit: want 403 or 404, got %d", w.Code)
	}

	// DB row is unchanged.
	var content string
	if err := testPool.QueryRow(ctx, `SELECT content FROM channel_message WHERE id = $1`, msg.ID).Scan(&content); err != nil {
		t.Fatalf("query content: %v", err)
	}
	if content != "edited body" {
		t.Fatalf("DB content drifted: %q", content)
	}
}

func TestDeleteChannelMessage_ByAuthor(t *testing.T) {
	enableChannels(t)
	ctx := context.Background()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name": "delete-author", "display_name": "DA", "visibility": "public",
	})
	testHandler.CreateChannel(w, req)
	ch := decodeChannel(t, w)
	msg := postChannelMessage(t, ch.ID, "delete me")

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/channels/"+ch.ID+"/messages/"+msg.ID, nil)
	req = withURLParams(req, "channelId", ch.ID, "messageId", msg.ID)
	testHandler.DeleteChannelMessage(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d %s", w.Code, w.Body.String())
	}

	// Soft-deleted row: deleted_at stamped, deletion_reason='user'.
	var (
		deletedAt sql.NullTime
		reason    sql.NullString
	)
	if err := testPool.QueryRow(ctx, `SELECT deleted_at, deletion_reason FROM channel_message WHERE id = $1`, msg.ID).Scan(&deletedAt, &reason); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !deletedAt.Valid {
		t.Fatalf("deleted_at not set")
	}
	if reason.String != "user" {
		t.Fatalf("deletion_reason = %q, want 'user'", reason.String)
	}

	// List excludes the soft-deleted row from the timeline view.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/channels/"+ch.ID+"/messages", nil)
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.ListChannelMessages(w, req)
	var listed []ChannelMessageResponse
	json.NewDecoder(w.Body).Decode(&listed)
	for _, m := range listed {
		if m.ID == msg.ID {
			t.Fatalf("soft-deleted message leaked into top-level list: %+v", m)
		}
	}
}

func TestDeleteChannelMessage_ByChannelAdmin(t *testing.T) {
	enableChannels(t)
	ctx := context.Background()

	// Create the channel as the test member (becomes admin via creator role).
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name": "admin-delete", "display_name": "AD", "visibility": "public",
	})
	testHandler.CreateChannel(w, req)
	ch := decodeChannel(t, w)

	// Add the agent as a regular member.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/members", map[string]any{
		"member_type": "agent", "member_id": testAgentID(), "role": "member",
	})
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.AddChannelMember(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("add agent: %d", w.Code)
	}

	// The agent posts a message via the API (X-Agent-ID header).
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/messages", map[string]any{
		"content": "agent message",
	})
	req.Header.Set("X-Agent-ID", testAgentID())
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.CreateChannelMessage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("agent post: %d %s", w.Code, w.Body.String())
	}
	var msg ChannelMessageResponse
	json.NewDecoder(w.Body).Decode(&msg)

	// The test member (admin) deletes the agent's message.
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/channels/"+ch.ID+"/messages/"+msg.ID, nil)
	req = withURLParams(req, "channelId", ch.ID, "messageId", msg.ID)
	testHandler.DeleteChannelMessage(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("admin delete: want 204, got %d %s", w.Code, w.Body.String())
	}

	// deletion_reason='admin'.
	var reason sql.NullString
	if err := testPool.QueryRow(ctx, `SELECT deletion_reason FROM channel_message WHERE id = $1`, msg.ID).Scan(&reason); err != nil {
		t.Fatalf("query reason: %v", err)
	}
	if reason.String != "admin" {
		t.Fatalf("deletion_reason = %q, want 'admin' (was admin-deletion)", reason.String)
	}
}

// Phase 4 — threads + reactions ---------------------------------------------

func postChannelMessage(t *testing.T, channelID, content string) ChannelMessageResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels/"+channelID+"/messages", map[string]any{
		"content": content,
	})
	req = withURLParam(req, "channelId", channelID)
	testHandler.CreateChannelMessage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create message: %d %s", w.Code, w.Body.String())
	}
	var msg ChannelMessageResponse
	if err := json.NewDecoder(w.Body).Decode(&msg); err != nil {
		t.Fatalf("decode message: %v", err)
	}
	return msg
}

func TestThreads_RepliesNestUnderParent(t *testing.T) {
	enableChannels(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name": "thread-test", "display_name": "ThreadTest", "visibility": "public",
	})
	testHandler.CreateChannel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create channel: %d", w.Code)
	}
	ch := decodeChannel(t, w)

	// Post a parent and three replies.
	parent := postChannelMessage(t, ch.ID, "parent message")
	for i := 0; i < 3; i++ {
		w = httptest.NewRecorder()
		req = newRequest("POST", "/api/channels/"+ch.ID+"/messages", map[string]any{
			"content":           "reply " + string(rune('a'+i)),
			"parent_message_id": parent.ID,
		})
		req = withURLParam(req, "channelId", ch.ID)
		testHandler.CreateChannelMessage(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("reply %d: %d %s", i, w.Code, w.Body.String())
		}
	}

	// Top-level list should include parent only — replies are nested.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/channels/"+ch.ID+"/messages", nil)
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.ListChannelMessages(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list messages: %d", w.Code)
	}
	var listed []ChannelMessageResponse
	json.NewDecoder(w.Body).Decode(&listed)
	var found *ChannelMessageResponse
	for i := range listed {
		if listed[i].ID == parent.ID {
			found = &listed[i]
		}
		if listed[i].ParentMessageID != nil {
			t.Fatalf("top-level list contains a reply (%s)", listed[i].ID)
		}
	}
	if found == nil {
		t.Fatalf("parent not found in top-level list")
	}
	if found.ThreadReplyCount != 3 {
		t.Fatalf("ThreadReplyCount = %d, want 3", found.ThreadReplyCount)
	}

	// Thread endpoint returns parent + 3 replies.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/channels/"+ch.ID+"/messages/"+parent.ID+"/thread", nil)
	req = withURLParams(req, "channelId", ch.ID, "messageId", parent.ID)
	testHandler.ListChannelMessageThread(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("thread: %d %s", w.Code, w.Body.String())
	}
	var threadResp struct {
		Parent  ChannelMessageResponse   `json:"parent"`
		Replies []ChannelMessageResponse `json:"replies"`
	}
	json.NewDecoder(w.Body).Decode(&threadResp)
	if threadResp.Parent.ID != parent.ID {
		t.Fatalf("thread parent = %q, want %q", threadResp.Parent.ID, parent.ID)
	}
	if len(threadResp.Replies) != 3 {
		t.Fatalf("expected 3 replies, got %d", len(threadResp.Replies))
	}
}

func TestReactions_AddRemoveAndCount(t *testing.T) {
	enableChannels(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name": "react-test", "display_name": "ReactTest", "visibility": "public",
	})
	testHandler.CreateChannel(w, req)
	ch := decodeChannel(t, w)
	msg := postChannelMessage(t, ch.ID, "react to me")

	// Add 👍
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/messages/"+msg.ID+"/reactions", map[string]any{
		"emoji": "👍",
	})
	req = withURLParams(req, "channelId", ch.ID, "messageId", msg.ID)
	testHandler.AddChannelReaction(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("add reaction: %d %s", w.Code, w.Body.String())
	}

	// Idempotent: same emoji twice still 201.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/messages/"+msg.ID+"/reactions", map[string]any{
		"emoji": "👍",
	})
	req = withURLParams(req, "channelId", ch.ID, "messageId", msg.ID)
	testHandler.AddChannelReaction(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("idempotent add: %d", w.Code)
	}

	// List reflects exactly one reaction.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/channels/"+ch.ID+"/messages", nil)
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.ListChannelMessages(w, req)
	var listed []ChannelMessageResponse
	json.NewDecoder(w.Body).Decode(&listed)
	var target *ChannelMessageResponse
	for i := range listed {
		if listed[i].ID == msg.ID {
			target = &listed[i]
		}
	}
	if target == nil {
		t.Fatalf("message not in list")
	}
	if len(target.Reactions) != 1 || target.Reactions[0].Emoji != "👍" {
		t.Fatalf("expected one 👍 reaction, got %+v", target.Reactions)
	}

	// Remove it.
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/channels/"+ch.ID+"/messages/"+msg.ID+"/reactions", map[string]any{
		"emoji": "👍",
	})
	req = withURLParams(req, "channelId", ch.ID, "messageId", msg.ID)
	testHandler.RemoveChannelReaction(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("remove reaction: %d %s", w.Code, w.Body.String())
	}

	// Re-list: zero reactions.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/channels/"+ch.ID+"/messages", nil)
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.ListChannelMessages(w, req)
	json.NewDecoder(w.Body).Decode(&listed)
	for _, m := range listed {
		if m.ID == msg.ID && len(m.Reactions) != 0 {
			t.Fatalf("expected 0 reactions after remove, got %d", len(m.Reactions))
		}
	}
}

func TestReactions_HiddenFromNonMembersOfPrivateChannel(t *testing.T) {
	enableChannels(t)

	// Create a private channel as the test member; agent is NOT added.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/channels", map[string]any{
		"name": "react-priv", "display_name": "ReactPriv", "visibility": "private",
	})
	testHandler.CreateChannel(w, req)
	ch := decodeChannel(t, w)
	msg := postChannelMessage(t, ch.ID, "secret post")

	// Owner adds 🔒.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/messages/"+msg.ID+"/reactions", map[string]any{
		"emoji": "🔒",
	})
	req = withURLParams(req, "channelId", ch.ID, "messageId", msg.ID)
	testHandler.AddChannelReaction(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("owner add reaction: %d", w.Code)
	}

	// As an agent (non-member), even the message itself shouldn't be
	// retrievable — the channel-access guard fires before the reaction
	// query runs. We assert the guard rather than the reaction list,
	// because the proper invariant is "no leak via the parent surface".
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/channels/"+ch.ID+"/messages", nil)
	req.Header.Set("X-Agent-ID", testAgentID())
	req = withURLParam(req, "channelId", ch.ID)
	testHandler.ListChannelMessages(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("agent list of private channel: want 404, got %d", w.Code)
	}

	// Direct reaction-add by a non-member also 404s (the channel is
	// invisible). Adding to a private message you can't see is exactly
	// the leak-vector we're guarding.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/channels/"+ch.ID+"/messages/"+msg.ID+"/reactions", map[string]any{
		"emoji": "👀",
	})
	req.Header.Set("X-Agent-ID", testAgentID())
	req = withURLParams(req, "channelId", ch.ID, "messageId", msg.ID)
	testHandler.AddChannelReaction(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-member reaction add: want 404, got %d", w.Code)
	}
}

// testAgentID returns the workspace's first agent id, lazily seeding one if
// the shared fixture didn't include it. We don't add a workspace-test-agent
// to setupHandlerTestFixture because most tests don't need one.
func testAgentID() string {
	var id string
	row := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID)
	_ = row.Scan(&id)
	return id
}
