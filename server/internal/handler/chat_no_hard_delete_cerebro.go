// CEREBRO-PATCH(chat-no-hard-delete): TECH-3664 — a 1-to-1 agent chat must
// never be permanently deleted; history is always preserved. This net-new fork
// file turns the chat-session DELETE endpoint into an ARCHIVE: the conversation
// is stopped and put away (status='archived'), not destroyed. The upstream
// chat.go DeleteChatSession keeps a single marked guard line that delegates here
// before any hard delete runs. Channels/DMs already behave this way; this brings
// agent chats to parity.
package handler

import (
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// archiveChatSessionInsteadOfDelete handles the chat-session "delete" request by
// archiving the session rather than hard-deleting it. It returns true when it
// has fully handled the response (the caller must then return immediately and
// must NOT proceed to the hard delete). It returns false only when the session
// could not be loaded for the caller (loadChatSessionForUser already wrote the
// error response in that case), so the caller also returns without deleting.
func (h *Handler) archiveChatSessionInsteadOfDelete(w http.ResponseWriter, r *http.Request, userID, workspaceID, sessionID string) bool {
	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		// Response already written by loadChatSessionForUser. Caller returns.
		return true
	}

	// Stop any in-flight agent work for this session so archiving behaves like
	// the old delete (the conversation goes quiet), just without losing rows.
	cancelled, err := h.Queries.CancelAgentTasksByChatSession(r.Context(), session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cancel chat session tasks")
		return true
	}

	if session.Status != "archived" {
		updated, err := h.Queries.UpdateChatSessionStatus(r.Context(), db.UpdateChatSessionStatusParams{
			ID:     session.ID,
			Status: "archived",
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to archive chat session")
			return true
		}
		session = updated
	}

	h.TaskService.BroadcastCancelledTasks(r.Context(), cancelled)

	resolvedSessionID := uuidToString(session.ID)
	// Emit the same "updated → archived" event the header archive action uses,
	// so every open client drops the row from its active list (the row is also
	// still reachable under the archived filter — nothing is lost).
	h.publishChat(protocol.EventChatSessionUpdated, workspaceID, "member", userID, resolvedSessionID, protocol.ChatSessionUpdatedPayload{
		ChatSessionID: resolvedSessionID,
		Title:         session.Title,
		Status:        session.Status,
	})

	w.WriteHeader(http.StatusNoContent)
	return true
}
