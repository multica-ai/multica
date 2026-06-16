package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDeleteChatSession_ArchivesInsteadOfDelete pins the TECH-3664 guarantee:
// the chat-session DELETE endpoint must ARCHIVE the session, never hard-delete
// it, so a 1-to-1 agent conversation's history is permanent. It mirrors the
// TestUpdateChatSession_* setup (same handler prerequisites).
func TestDeleteChatSession_ArchivesInsteadOfDelete(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "ChatNoHardDeleteAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	req := newRequest(http.MethodDelete, "/api/chat/sessions/"+sessionID, nil)
	req = withURLParam(req, "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()

	testHandler.DeleteChatSession(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from delete→archive, got %d: %s", w.Code, w.Body.String())
	}

	// The row must STILL exist (not destroyed) and be archived.
	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM chat_session WHERE id = $1`, sessionID,
	).Scan(&status); err != nil {
		t.Fatalf("chat_session row should still exist after delete→archive, but lookup failed: %v", err)
	}
	if status != "archived" {
		t.Fatalf("expected session status 'archived' after delete, got %q", status)
	}
}
