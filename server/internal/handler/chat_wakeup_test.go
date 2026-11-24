package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// seedChatWakeup stores an active reminder in a conversation, asked for by
// requesterID.
func seedChatWakeup(t *testing.T, sessionID, agentID, requesterID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_wakeup (id, workspace_id, chat_session_id, agent_id, requester_id, title, instruction, kind, fire_at, next_eval_at, expires_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, 'Release shipped', 'Report back', 'at', now() + interval '1 hour', now() + interval '1 hour', now() + interval '1 hour')
		RETURNING id`, testWorkspaceID, sessionID, agentID, requesterID).Scan(&id); err != nil {
		t.Fatalf("seed chat wakeup: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_wakeup WHERE id = $1`, id)
	})
	return id
}

func cancelChatWakeupAs(t *testing.T, userID, sessionID, wakeupID string) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequestAs(userID, http.MethodPost, "/api/chat/sessions/"+sessionID+"/wakeups/"+wakeupID+"/cancel", nil)
	req = withURLParams(req, "sessionId", sessionID, "wakeupId", wakeupID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.CancelChatSessionWakeup(w, req)
	return w
}

// In a group chat the person who asked for a wakeup is not the session's
// owner; both of them can cancel it, and nobody else can.
func TestCancelChatSessionWakeup_RequesterOrSessionOwner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "ChatWakeupCancelAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	suffix := time.Now().UnixNano()
	sender := createWorkspaceMemberUser(t, "Wakeup sender", fmt.Sprintf("wakeup-sender-%d@multica.test", suffix))
	bystander := createWorkspaceMemberUser(t, "Wakeup bystander", fmt.Sprintf("wakeup-bystander-%d@multica.test", suffix))

	senders := seedChatWakeup(t, sessionID, agentID, sender)
	if w := cancelChatWakeupAs(t, bystander, sessionID, senders); w.Code != http.StatusForbidden {
		t.Fatalf("bystander cancel: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if w := cancelChatWakeupAs(t, sender, sessionID, senders); w.Code != http.StatusOK {
		t.Fatalf("requester cancel: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	another := seedChatWakeup(t, sessionID, agentID, sender)
	if w := cancelChatWakeupAs(t, testUserID, sessionID, another); w.Code != http.StatusOK {
		t.Fatalf("session owner cancel: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// chat_wakeup has no chat_session FK, so DeleteChatSession prunes it.
func TestDeleteChatSession_PrunesChatWakeups(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "ChatWakeupPruneAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	seedChatWakeup(t, sessionID, agentID, testUserID)

	req := withURLParam(newRequest(http.MethodDelete, "/api/chat/sessions/"+sessionID, nil), "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.DeleteChatSession(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteChatSession: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_wakeup WHERE chat_session_id = $1`, sessionID).Scan(&count); err != nil {
		t.Fatalf("count wakeups: %v", err)
	}
	if count != 0 {
		t.Errorf("wakeups must be pruned with their session, count = %d", count)
	}
}

// A wakeup turn queued while its runtime was offline does not run once the
// requester has lost the agent: the real claim endpoint fails it instead.
func TestClaimTaskByRuntime_ChatWakeupRechecksRequester(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Offline wakeup runtime")
	agentID, _ := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Offline wakeup agent")
	sessionID := createHandlerTestChatSession(t, agentID)
	sender := createWorkspaceMemberUser(t, "Wakeup sender", fmt.Sprintf("offline-wakeup-%d@multica.test", time.Now().UnixNano()))
	dbfx.Exec(t, `UPDATE agent SET permission_mode = 'public_to' WHERE id = $1`, agentID)
	dbfx.Exec(t, `INSERT INTO agent_invocation_target (agent_id, target_type, target_id) VALUES ($1, 'member', $2) ON CONFLICT DO NOTHING`, agentID, sender)
	sourceID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": sessionID, "status": "completed", "initiator_user_id": sender, "originator_user_id": sender, "accountable_user_id": sender})
	dbfx.Exec(t, `UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1`, sourceID)
	dbfx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": sessionID, "role": "user", "content": "Remind me later", "task_id": sourceID})
	source, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(sourceID))
	if err != nil {
		t.Fatal(err)
	}
	svc := &service.ChatWakeupService{Tasks: testHandler.TaskService}
	wakeup, err := svc.Create(ctx, source, service.ChatWakeupInput{Title: "Follow up", Instruction: "Report back", AfterSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM chat_wakeup WHERE id = $1`, wakeup.ID) })
	dbfx.Exec(t, `UPDATE chat_wakeup SET fire_at = now() - interval '1 second', next_eval_at = now() - interval '1 second', expires_at = now() - interval '1 second' WHERE id = $1`, wakeup.ID)
	if err := svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	wakeup, err = testHandler.Queries.GetChatWakeup(ctx, wakeup.ID)
	if err != nil || !wakeup.LastTaskID.Valid {
		t.Fatalf("no queued wakeup turn: %+v, %v", wakeup, err)
	}

	dbfx.Exec(t, `UPDATE agent SET permission_mode = 'private' WHERE id = $1`, agentID)
	req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil), "runtimeId", runtimeID)
	resp := testutil.Call(t, testHandler.ClaimTaskByRuntime, req)
	if strings.Contains(resp.Body.String(), "[WAKEUP]") || strings.Contains(resp.Body.String(), uuidToString(wakeup.LastTaskID)) {
		t.Fatalf("revoked requester's wakeup turn was dispatched (status %d)", resp.Code)
	}
	task, err := testHandler.Queries.GetAgentTask(ctx, wakeup.LastTaskID)
	if err != nil || task.Status != "failed" {
		t.Fatalf("wakeup turn after a refused claim = %q (%v), want failed", task.Status, err)
	}
}

func rescheduleChatWakeupAs(t *testing.T, userID, sessionID, wakeupID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequestAs(userID, http.MethodPatch, "/api/chat/sessions/"+sessionID+"/wakeups/"+wakeupID+"/timing", body)
	req = withURLParams(req, "sessionId", sessionID, "wakeupId", wakeupID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.RescheduleChatSessionWakeup(w, req)
	return w
}

type chatWakeupTimingRow struct {
	title, instruction string
	fireAt, nextEval   *time.Time
	expires            time.Time
	interval           *int64
}

func readChatWakeupTiming(t *testing.T, id string) chatWakeupTimingRow {
	t.Helper()
	var row chatWakeupTimingRow
	if err := testPool.QueryRow(context.Background(),
		`SELECT title, instruction, fire_at, next_eval_at, expires_at, interval_seconds FROM chat_wakeup WHERE id = $1`, id,
	).Scan(&row.title, &row.instruction, &row.fireAt, &row.nextEval, &row.expires, &row.interval); err != nil {
		t.Fatalf("read chat wakeup: %v", err)
	}
	return row
}

// A member moves a reminder without an agent turn; the agent's text stays.
func TestRescheduleChatSessionWakeup_Reminder(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "ChatWakeupRescheduleAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	bystander := createWorkspaceMemberUser(t, "Reschedule bystander", fmt.Sprintf("reschedule-bystander-%d@multica.test", time.Now().UnixNano()))
	id := seedChatWakeup(t, sessionID, agentID, testUserID)

	if w := rescheduleChatWakeupAs(t, bystander, sessionID, id, map[string]any{"after_seconds": 300}); w.Code != http.StatusForbidden {
		t.Fatalf("bystander: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if w := rescheduleChatWakeupAs(t, testUserID, sessionID, id, map[string]any{"interval_seconds": 900}); w.Code != http.StatusBadRequest {
		t.Fatalf("interval on a reminder: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if w := rescheduleChatWakeupAs(t, testUserID, sessionID, id, map[string]any{"after_seconds": -5}); w.Code != http.StatusBadRequest {
		t.Fatalf("past reminder: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	before := time.Now()
	if w := rescheduleChatWakeupAs(t, testUserID, sessionID, id, map[string]any{"after_seconds": 300}); w.Code != http.StatusOK {
		t.Fatalf("owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	row := readChatWakeupTiming(t, id)
	if row.fireAt == nil || row.fireAt.Before(before.Add(290*time.Second)) || row.fireAt.After(before.Add(310*time.Second)) {
		t.Fatalf("fire_at = %v, want about 5 minutes from now", row.fireAt)
	}
	if !row.nextEval.Equal(*row.fireAt) || !row.expires.Equal(*row.fireAt) {
		t.Fatalf("next_eval_at %v / expires_at %v should follow fire_at %v", row.nextEval, row.expires, row.fireAt)
	}
	if row.title != "Release shipped" || row.instruction != "Report back" {
		t.Fatalf("text changed: %q / %q", row.title, row.instruction)
	}
}

// A check takes a new interval and/or deadline; whatever is left out stays.
func TestRescheduleChatSessionWakeup_Check(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "ChatWakeupRescheduleCheckAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_wakeup (id, workspace_id, chat_session_id, agent_id, requester_id, title, instruction, kind, interval_seconds, next_eval_at, expires_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, 'Watch the deploy', 'Report progress', 'check', 1800, now() + interval '30 minutes', now() + interval '12 hours')
		RETURNING id`, testWorkspaceID, sessionID, agentID, testUserID).Scan(&id); err != nil {
		t.Fatalf("seed check wakeup: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM chat_wakeup WHERE id = $1`, id) })
	seeded := readChatWakeupTiming(t, id)

	if w := rescheduleChatWakeupAs(t, testUserID, sessionID, id, map[string]any{"interval_seconds": 60}); w.Code != http.StatusBadRequest {
		t.Fatalf("interval under the minimum: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if w := rescheduleChatWakeupAs(t, testUserID, sessionID, id, map[string]any{"after_seconds": 600}); w.Code != http.StatusBadRequest {
		t.Fatalf("reminder time on a check: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	before := time.Now()
	if w := rescheduleChatWakeupAs(t, testUserID, sessionID, id, map[string]any{"interval_seconds": 600}); w.Code != http.StatusOK {
		t.Fatalf("new interval: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	row := readChatWakeupTiming(t, id)
	if row.interval == nil || *row.interval != 600 {
		t.Fatalf("interval = %v, want 600", row.interval)
	}
	if !row.expires.Equal(seeded.expires) {
		t.Fatalf("deadline moved from %v to %v; only the interval was given", seeded.expires, row.expires)
	}
	if row.nextEval.Before(before.Add(590*time.Second)) || row.nextEval.After(before.Add(610*time.Second)) {
		t.Fatalf("next check %v, want about 10 minutes from now", row.nextEval)
	}

	nextCheck := *row.nextEval
	if w := rescheduleChatWakeupAs(t, testUserID, sessionID, id, map[string]any{"expires_in_seconds": 7200}); w.Code != http.StatusOK {
		t.Fatalf("new deadline: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	row = readChatWakeupTiming(t, id)
	if *row.interval != 600 || !row.nextEval.Equal(nextCheck) {
		t.Fatalf("deadline-only change moved the interval (%v) or next check (%v -> %v)", *row.interval, nextCheck, row.nextEval)
	}
	if row.expires.Before(before.Add(7190*time.Second)) || row.expires.After(time.Now().Add(7210*time.Second)) {
		t.Fatalf("deadline = %v, want about 2 hours from now", row.expires)
	}
}
