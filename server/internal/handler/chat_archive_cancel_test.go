package handler

// chat_archive_cancel_test.go — archiving a chat session deletes its channel
// binding but used to leave queued tasks claimable. ClaimAgentTask does not
// read chat_session.status, so the daemon still ran the turn — and with the
// binding row gone the runtime brief described a private web chat while the
// channel adapter still held the chat id and posted the answer into the group.
//
// Cancelling the rows in the database is only half of it: the cancellation has
// to reach the same post-commit lifecycle DeleteChatSession runs, or the rows
// read 'cancelled' while agents stay 'working', other clients keep showing the
// task, the tasks' API tokens stay live, and the runtime waits for its next
// poll before claiming anything else.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/slack"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestArchivingAChatSessionCancelsItsQueuedTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "Archive Cancels Queued", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	// One queued turn and one already running: CancelAgentTasksByChatSession
	// matches both, and the running one is where dropping the post-commit
	// broadcast shows up worst — the agent stays 'working' with no task left.
	queuedTaskID := queueArchiveTestTask(t, agentID, sessionID, "queued")
	runningTaskID := queueArchiveTestTask(t, agentID, sessionID, "running")

	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET status = 'working' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("mark agent working: %v", err)
	}

	// A live task token for the running task. captureTaskCancelled revokes it;
	// without the broadcast the token outlives the task it authenticates.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, now() + interval '24 hours')
	`, "archive-cancel-test-hash", runningTaskID, agentID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("mint task token: %v", err)
	}

	// Bus handlers are never unsubscribed in this suite, so filter to this
	// test's own task ids and never block a later publisher.
	cancelledEvents := make(chan string, 4)
	testHandler.Bus.Subscribe(protocol.EventTaskCancelled, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		id, _ := payload["task_id"].(string)
		if id != queuedTaskID && id != runningTaskID {
			return
		}
		select {
		case cancelledEvents <- id:
		default:
		}
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

	for _, tc := range []struct{ label, id string }{
		{"queued", queuedTaskID},
		{"running", runningTaskID},
	} {
		var status string
		if err := testPool.QueryRow(ctx,
			`SELECT status FROM agent_task_queue WHERE id = $1`, tc.id).Scan(&status); err != nil {
			t.Fatalf("reload %s task: %v", tc.label, err)
		}
		if status != "cancelled" {
			t.Fatalf("%s task status after archive = %q, want cancelled — the daemon will still run this turn, and with the channel binding already gone its answer goes to the room while the brief says private", tc.label, status)
		}
	}

	// Post-commit lifecycle. Each of these is something BroadcastCancelledTasks
	// does and nothing else on this path does.

	var agentStatus string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM agent WHERE id = $1`, agentID).Scan(&agentStatus); err != nil {
		t.Fatalf("reload agent: %v", err)
	}
	if agentStatus != "idle" {
		t.Errorf("agent status after archiving its only conversation = %q, want idle — the agent keeps showing as working in the UI with no task left to finish it", agentStatus)
	}

	seen := map[string]bool{}
drain:
	for len(seen) < 2 {
		select {
		case id := <-cancelledEvents:
			seen[id] = true
		default:
			break drain
		}
	}
	if len(seen) != 2 {
		t.Errorf("task:cancelled emitted for %d of 2 tasks — every other client keeps the cancelled turn on screen until something else forces a refresh", len(seen))
	}

	var liveTokens int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM task_token WHERE task_id = $1`, runningTaskID).Scan(&liveTokens); err != nil {
		t.Fatalf("count task tokens: %v", err)
	}
	if liveTokens != 0 {
		t.Errorf("cancelled task still has %d live task token(s) — the agent process can keep calling the API back for a turn that was cancelled", liveTokens)
	}
}

func queueArchiveTestTask(t *testing.T, agentID, sessionID, status string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, chat_session_id, status, runtime_id)
		VALUES ($1, $2, $3, (SELECT runtime_id FROM agent WHERE id = $1))
		RETURNING id
	`, agentID, sessionID, status).Scan(&taskID); err != nil {
		t.Fatalf("queue a %s task: %v", status, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

// TestNoHistoryNoteNamesTheActualChannel: the history reader is Slack-only, so
// every other platform lands on the empty-read note. Telling a WeCom or Lark
// session that it "is not connected to a chat channel" is false, and the
// reader is an agent deciding who can see its answer. Both legs go through the
// real response path and assert channel_type and note agree — they are derived
// from one binding read, so they cannot contradict each other.
func TestNoHistoryNoteNamesTheActualChannel(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "History Note Channel", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	// Web-only session: the original wording is correct here.
	body := readNoHistoryResponse(t, sessionID)
	if body.ChannelType != "" {
		t.Errorf("web-only session channel_type = %q, want empty", body.ChannelType)
	}
	if !strings.Contains(body.Note, "not connected to a chat channel") {
		t.Errorf("web-only session note = %q", body.Note)
	}

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

	body = readNoHistoryResponse(t, sessionID)
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

// readNoHistoryResponse drives the real response path rather than the note
// helper — otherwise the test keeps passing when the handler stops calling it.
func readNoHistoryResponse(t *testing.T, sessionID string) ChatChannelHistoryResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/chat/sessions/"+sessionID+"/channel-history", nil)
	req.Header.Set("X-User-ID", testUserID)
	w := httptest.NewRecorder()
	testHandler.respondChatHistory(w, req, parseUUID(sessionID), channel.HistoryPage{}, slack.ErrNoSlackSession)

	var body ChatChannelHistoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v (%s)", err, w.Body.String())
	}
	return body
}
