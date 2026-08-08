package handler

// chat_archive_cancel_test.go — archiving a chat session deletes its channel
// binding but used to leave queued tasks claimable. ClaimAgentTask does not
// read chat_session.status, so the daemon still ran the turn — and with the
// binding row gone the runtime brief described a private web chat while the
// channel adapter still held the chat id and posted the answer into the group.
//
// The cancel is therefore scoped to sessions that HAD a binding, which is what
// the two tests below are: the same fixture archived twice, once bound and once
// not. A web-only chat has no room and no late answer, and cancelling it would
// destroy the user's typed prompt — CancelAgentTasksByChatSession is a bare
// UPDATE with no restore, unlike the Stop button's CancelTaskByUser — so
// archiving must leave it alone.
//
// For the bound case, cancelling the rows in the database is only half of it:
// the cancellation has to reach the same post-commit lifecycle
// DeleteChatSession runs, or the rows read 'cancelled' while agents stay
// 'working', other clients keep showing the task, the tasks' API tokens stay
// live, and the runtime waits for its next poll before claiming anything else.

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

func TestArchivingAChannelBoundChatSessionCancelsItsQueuedTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newArchiveCancelFixture(t, "Archive Cancels Queued")

	// The binding is the reason this cancel exists at all: the archive drops it
	// a few lines after deciding, and the adapter on the other side still knows
	// the room. Bind before archiving, or the test is a web chat wearing the
	// bug's failure message.
	f.bindToChannel(t, "GROUP_ARCHIVE_CANCEL")

	f.archive(t)

	for _, tc := range []struct{ label, id string }{
		{"queued", f.queuedTaskID},
		{"running", f.runningTaskID},
	} {
		if status := f.taskStatus(t, tc.id); status != "cancelled" {
			t.Fatalf("%s task status after archiving a channel-bound session = %q, want cancelled — the daemon will still run this turn, and with the channel binding already gone its answer goes to the room while the brief says private", tc.label, status)
		}
	}

	// The binding read that gates the cancel happens before the delete; the
	// delete still has to run, or the channel keeps resolving to this session.
	if f.bindingExists(t) {
		t.Errorf("archived session kept its channel_chat_session_binding — inbound traffic would revive a read-only conversation")
	}

	// Post-commit lifecycle. Each of these is something BroadcastCancelledTasks
	// does and nothing else on this path does.

	if agentStatus := f.agentStatus(t); agentStatus != "idle" {
		t.Errorf("agent status after archiving its only conversation = %q, want idle — the agent keeps showing as working in the UI with no task left to finish it", agentStatus)
	}

	if seen := f.drainCancelledEvents(); len(seen) != 2 {
		t.Errorf("task:cancelled emitted for %d of 2 tasks — every other client keeps the cancelled turn on screen until something else forces a refresh", len(seen))
	}

	if live := f.liveTaskTokens(t); live != 0 {
		t.Errorf("cancelled task still has %d live task token(s) — the agent process can keep calling the API back for a turn that was cancelled", live)
	}
}

// TestArchivingAWebOnlyChatSessionLeavesItsTasksRunning is the other direction:
// the same fixture with no channel binding. Archive is the reversible
// organizing action the UI presents — the row offers Stop OR Archive, never
// both; Stop is `danger` behind a confirmation and Archive fires immediately
// with none; unarchive undoes it and hard delete is offered only afterwards.
// Cancelling here would make that immediate, unconfirmed click destructive, and
// irreversibly so: the Stop button cancels through CancelTaskByUser, which
// returns the prompt via cancelled_chat_message.restore_to_input or writes a
// durable chat_draft_restore row, while CancelAgentTasksByChatSession does
// neither and unarchive cannot undo a cancel.
func TestArchivingAWebOnlyChatSessionLeavesItsTasksRunning(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := newArchiveCancelFixture(t, "Archive Leaves Web Chat")

	// Deliberately no binding: this is a plain web chat.

	f.archive(t)

	for _, tc := range []struct{ label, id, want string }{
		{"queued", f.queuedTaskID, "queued"},
		{"running", f.runningTaskID, "running"},
	} {
		if status := f.taskStatus(t, tc.id); status != tc.want {
			t.Fatalf("%s task status after archiving a web-only chat = %q, want %q — there is no room for a late answer to reach here, and cancelling destroys the user's typed prompt: this path writes no chat_draft_restore row and returns no restore_to_input, so the text is gone and unarchiving cannot bring it back", tc.label, status, tc.want)
		}
	}

	if agentStatus := f.agentStatus(t); agentStatus != "working" {
		t.Errorf("agent status after archiving a web-only chat = %q, want working — its turn is still running", agentStatus)
	}

	if seen := f.drainCancelledEvents(); len(seen) != 0 {
		t.Errorf("archiving a web-only chat emitted task:cancelled for %d task(s) — every client drops a turn that is still running", len(seen))
	}

	if live := f.liveTaskTokens(t); live != 1 {
		t.Errorf("running task has %d live task token(s) after archiving a web-only chat, want 1 — revoking it strands the agent process mid-turn", live)
	}

	// The archive itself must still happen; narrowing the cancel must not turn
	// into skipping the status flip.
	var status string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM chat_session WHERE id = $1`, f.sessionID).Scan(&status); err != nil {
		t.Fatalf("reload chat session: %v", err)
	}
	if status != "archived" {
		t.Fatalf("chat session status = %q, want archived", status)
	}
}

// archiveCancelFixture is one agent marked 'working' with one chat session
// carrying two in-flight turns — one queued, one already running — plus a live
// task token for the running one and a filtered task:cancelled subscription.
// CancelAgentTasksByChatSession matches both statuses, and the running one is
// where dropping the post-commit broadcast shows up worst: the agent stays
// 'working' with no task left.
type archiveCancelFixture struct {
	agentID       string
	sessionID     string
	queuedTaskID  string
	runningTaskID string
	cancelled     chan string
}

func newArchiveCancelFixture(t *testing.T, agentName string) *archiveCancelFixture {
	t.Helper()
	ctx := context.Background()

	f := &archiveCancelFixture{}
	f.agentID = createHandlerTestAgent(t, agentName, []byte("[]"))
	f.sessionID = createHandlerTestChatSession(t, f.agentID)
	f.queuedTaskID = queueArchiveTestTask(t, f.agentID, f.sessionID, "queued")
	f.runningTaskID = queueArchiveTestTask(t, f.agentID, f.sessionID, "running")

	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET status = 'working' WHERE id = $1`, f.agentID); err != nil {
		t.Fatalf("mark agent working: %v", err)
	}

	// A live task token for the running task. captureTaskCancelled revokes it;
	// on the bound leg, without the broadcast the token outlives the task it
	// authenticates, and on the web-only leg it must survive untouched.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, now() + interval '24 hours')
	`, "archive-cancel-test-"+f.runningTaskID, f.runningTaskID, f.agentID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("mint task token: %v", err)
	}

	// Bus handlers are never unsubscribed in this suite, so filter to this
	// fixture's own task ids and never block a later publisher.
	f.cancelled = make(chan string, 4)
	testHandler.Bus.Subscribe(protocol.EventTaskCancelled, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		id, _ := payload["task_id"].(string)
		if id != f.queuedTaskID && id != f.runningTaskID {
			return
		}
		select {
		case f.cancelled <- id:
		default:
		}
	})

	return f
}

// bindToChannel gives the session a channel binding, the way an inbound message
// from a group room would.
func (f *archiveCancelFixture) bindToChannel(t *testing.T, channelChatID string) {
	t.Helper()
	ctx := context.Background()

	// channel_* rows have no FK to chat_session, so they outlive the session
	// helper's cleanup; clear by deterministic key.
	cleanup := func() {
		testPool.Exec(context.Background(),
			`DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, f.sessionID)
		testPool.Exec(context.Background(),
			`DELETE FROM channel_installation WHERE agent_id = $1`, f.agentID)
	}
	cleanup()
	t.Cleanup(cleanup)

	var instID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, status, installer_user_id)
		VALUES ($1, $2, 'wecom', $3::jsonb, 'active', $4)
		RETURNING id
	`, testWorkspaceID, f.agentID, `{"app_id":"bot-archive-cancel"}`, testUserID).Scan(&instID); err != nil {
		t.Fatalf("create installation: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO channel_chat_session_binding
			(chat_session_id, installation_id, channel_type, channel_chat_id, chat_type)
		VALUES ($1, $2, 'wecom', $3, 'group')
	`, f.sessionID, instID, channelChatID); err != nil {
		t.Fatalf("bind session: %v", err)
	}
}

func (f *archiveCancelFixture) archive(t *testing.T) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/api/chat/sessions/"+f.sessionID+"/archived",
		bytes.NewReader([]byte(`{"archived":true}`)))
	req.Header.Set("X-User-ID", testUserID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionId", f.sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	testHandler.SetChatSessionArchived(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("archive returned %d: %s", w.Code, w.Body.String())
	}
}

func (f *archiveCancelFixture) taskStatus(t *testing.T, taskID string) string {
	t.Helper()
	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("reload task %s: %v", taskID, err)
	}
	return status
}

func (f *archiveCancelFixture) agentStatus(t *testing.T) string {
	t.Helper()
	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM agent WHERE id = $1`, f.agentID).Scan(&status); err != nil {
		t.Fatalf("reload agent: %v", err)
	}
	return status
}

func (f *archiveCancelFixture) liveTaskTokens(t *testing.T) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM task_token WHERE task_id = $1`, f.runningTaskID).Scan(&n); err != nil {
		t.Fatalf("count task tokens: %v", err)
	}
	return n
}

func (f *archiveCancelFixture) bindingExists(t *testing.T) bool {
	t.Helper()
	var exists bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM channel_chat_session_binding WHERE chat_session_id = $1)`,
		f.sessionID).Scan(&exists); err != nil {
		t.Fatalf("query chat session binding: %v", err)
	}
	return exists
}

// drainCancelledEvents reads what the bus already delivered. events.Bus.Publish
// is synchronous, so by the time the handler has returned every event this
// archive was going to emit is already in the channel — an empty drain means
// none were emitted, not that they are still in flight.
func (f *archiveCancelFixture) drainCancelledEvents() map[string]bool {
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case id := <-f.cancelled:
			seen[id] = true
		default:
			return seen
		}
	}
	return seen
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
		testPool.Exec(context.Background(), `DELETE FROM task_token WHERE task_id = $1`, taskID)
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
