package handler

// chat_archive_cancel_test.go — archiving a chat session deletes its channel
// binding but used to leave queued tasks claimable. ClaimAgentTask does not
// read chat_session.status, so the daemon still ran the turn — and with the
// binding row gone the runtime brief described a private web chat while the
// channel adapter still held the chat id and posted the answer into the group.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/slack"
)

func TestArchivingAChatSessionCancelsItsQueuedTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "Archive Cancels Queued", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, chat_session_id, status, runtime_id)
		VALUES ($1, $2, 'queued', (SELECT runtime_id FROM agent WHERE id = $1))
		RETURNING id
	`, agentID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("queue a task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	req := httptest.NewRequest(http.MethodPatch, "/api/chat/sessions/"+sessionID+"/archived",
		bytes.NewReader([]byte(`{"archived":true}`)))
	req.Header.Set("X-User-ID", testUserID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	testHandler.SetChatSessionArchived(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("archive returned %d: %s", w.Code, w.Body.String())
	}

	var status string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("task status after archive = %q, want cancelled — the daemon will still run this turn, and with the channel binding already gone its answer goes to the room while the brief says private", status)
	}
}

// TestNoHistoryNoteNamesTheActualChannel: the history reader is Slack-only, so
// every other platform lands on the empty-read note. Telling a WeCom or Lark
// session that it "is not connected to a chat channel" is false, and the
// reader is an agent deciding who can see its answer.
func TestNoHistoryNoteNamesTheActualChannel(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "History Note Channel", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	// Web-only session: the original wording is correct here.
	if note := testHandler.noHistoryNote(ctx, parseUUID(sessionID)); !strings.Contains(note, "not connected to a chat channel") {
		t.Errorf("web-only session note = %q", note)
	}
	_ = ctx

	var instID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, status, installer_user_id)
		VALUES ($1, $2, 'wecom', '{"app_id":"bot-history-note"}'::jsonb, 'active', $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&instID); err != nil {
		t.Fatalf("create installation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, instID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO channel_chat_session_binding
			(chat_session_id, installation_id, channel_type, channel_chat_id, chat_type)
		VALUES ($1, $2, 'wecom', 'GROUP_1', 'group')
	`, sessionID, instID); err != nil {
		t.Fatalf("bind session: %v", err)
	}

	// Drive the real response path, not just the helper — otherwise the test
	// keeps passing when the handler stops calling it.
	req := httptest.NewRequest(http.MethodGet, "/api/chat/sessions/"+sessionID+"/channel-history", nil)
	req.Header.Set("X-User-ID", testUserID)
	w := httptest.NewRecorder()
	testHandler.respondChatHistory(w, req, parseUUID(sessionID), channel.HistoryPage{}, slack.ErrNoSlackSession)

	var body ChatChannelHistoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v (%s)", err, w.Body.String())
	}
	if strings.Contains(body.Note, "not connected to a chat channel") {
		t.Fatalf("a WeCom-bound session was told it is not on a channel: %q", body.Note)
	}
	if !strings.Contains(body.Note, "wecom") {
		t.Errorf("note does not name the platform: %q", body.Note)
	}
	if body.ChannelType != "wecom" {
		t.Errorf("channel_type = %q, want wecom", body.ChannelType)
	}
}
