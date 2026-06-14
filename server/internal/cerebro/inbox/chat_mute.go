package inbox

// TECH-3352 — cerebro-only chat-session inbox controls (snooze + mark-unread),
// aligning chat rows with issue/channel/DM rows. They live in the cerebro inbox
// handler (which already carries upstream + cerebro queries + the bus) so the
// routes stay cerebro-only and upstream merges don't touch them.

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	eventChatSessionMuted  = "chat:session_muted"
	eventChatSessionUnread = "chat:session_unread"
)

// MuteChatSession snoozes ("remind me") a chat session until the supplied time.
// Per-user (creator-scoped) — the UPDATE only touches the caller's own session.
func (h *Handler) MuteChatSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	sessionUUID, err := util.ParseUUID(chi.URLParam(r, "sessionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}

	var body struct {
		MutedUntil string `json:"muted_until"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	parsed, err := time.Parse(time.RFC3339, body.MutedUntil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "muted_until must be RFC3339")
		return
	}
	if !parsed.After(time.Now()) {
		writeError(w, http.StatusBadRequest, "muted_until must be in the future")
		return
	}

	if err := h.Cerebro.SetChatSessionMuted(r.Context(), cerebrodb.SetChatSessionMutedParams{
		ID:         sessionUUID,
		CreatorID:  userUUID,
		MutedUntil: pgtype.Timestamptz{Time: parsed, Valid: true},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mute chat session")
		return
	}

	mutedStr := parsed.UTC().Format(time.RFC3339Nano)
	h.publishChatState(r, eventChatSessionMuted, userID, util.UUIDToString(sessionUUID), &mutedStr)
	writeJSON(w, http.StatusOK, map[string]any{"muted_until": mutedStr})
}

// UnmuteChatSession clears the snooze for the caller's own chat session.
func (h *Handler) UnmuteChatSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	sessionUUID, err := util.ParseUUID(chi.URLParam(r, "sessionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	if err := h.Cerebro.ClearChatSessionMuted(r.Context(), cerebrodb.ClearChatSessionMutedParams{
		ID:        sessionUUID,
		CreatorID: userUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unmute chat session")
		return
	}
	h.publishChatState(r, eventChatSessionMuted, userID, util.UUIDToString(sessionUUID), nil)
	writeJSON(w, http.StatusOK, map[string]any{"muted_until": nil})
}

// MarkChatSessionUnread re-flags a read chat session as unread (sets
// unread_since if it is currently null). Reuses the upstream machinery.
func (h *Handler) MarkChatSessionUnread(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	sessionUUID, err := util.ParseUUID(chi.URLParam(r, "sessionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	if err := h.Upstream.SetUnreadSinceIfNull(r.Context(), sessionUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark chat session unread")
		return
	}
	h.publishChatState(r, eventChatSessionUnread, userID, util.UUIDToString(sessionUUID), nil)
	writeJSON(w, http.StatusOK, map[string]any{"unread": true})
}

// ChatMutedForUser returns the caller's snoozed chat sessions as a
// sessionID → muted_until (RFC3339) map, for the chat-list response overlay.
func (h *Handler) ChatMutedForUser(ctx context.Context, userID, workspaceID string) map[string]string {
	out := map[string]string{}
	uid, err := util.ParseUUID(userID)
	if err != nil {
		return out
	}
	wid, err := util.ParseUUID(workspaceID)
	if err != nil {
		return out
	}
	rows, err := h.Cerebro.ListChatMutedForUser(ctx, cerebrodb.ListChatMutedForUserParams{
		CreatorID:   uid,
		WorkspaceID: wid,
	})
	if err != nil {
		return out
	}
	for _, row := range rows {
		if row.MutedUntil.Valid {
			out[util.UUIDToString(row.ID)] = row.MutedUntil.Time.UTC().Format(time.RFC3339Nano)
		}
	}
	return out
}

// ClearChatSessionMute clears a chat snooze when the session is read (called
// from the upstream MarkChatSessionRead via the seam).
func (h *Handler) ClearChatSessionMute(ctx context.Context, sessionID, userID string) {
	sid, err := util.ParseUUID(sessionID)
	if err != nil {
		return
	}
	uid, err := util.ParseUUID(userID)
	if err != nil {
		return
	}
	_ = h.Cerebro.ClearChatSessionMuted(ctx, cerebrodb.ClearChatSessionMutedParams{
		ID:        sid,
		CreatorID: uid,
	})
}

// publishChatState broadcasts a chat inbox-state change to the acting user's
// own sessions so other tabs/devices invalidate their chat list.
func (h *Handler) publishChatState(r *http.Request, eventType, userID, sessionID string, mutedUntil *string) {
	if h.Bus == nil {
		return
	}
	h.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: middleware.WorkspaceIDFromContext(r.Context()),
		ActorType:   "member",
		ActorID:     userID,
		Payload: map[string]any{
			"chat_session_id": sessionID,
			"muted_until":     mutedUntil,
		},
		AudienceUserIDs: []string{userID},
	})
}
