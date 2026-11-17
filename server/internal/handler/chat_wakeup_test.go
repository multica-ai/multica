package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
