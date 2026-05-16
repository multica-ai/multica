// Package inbox holds cerebro-only HTTP handlers for the inbox feature
// (mute / unmute / mark-unread). They live here so upstream merges of the
// upstream inbox handler don't conflict on cerebro-only routes.
//
// Pattern mirrors server/internal/cerebro/notifications/handler.go: own struct
// with its own deps, package-private auth/response helpers, and a single
// CEREBRO-PATCH(cerebro-inbox-routes) marker line in the router that wires
// the routes in.
package inbox

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handler exposes the cerebro-only inbox HTTP endpoints. Carries both the
// upstream sqlc Queries (for the workspace-scoped item lookup that enforces
// access control) and the cerebro Queries (for the new mute/unread mutations).
type Handler struct {
	Upstream *db.Queries
	Cerebro  *cerebrodb.Queries
}

// New constructs the handler. The router wires both query packages in.
func New(upstream *db.Queries, cerebro *cerebrodb.Queries) *Handler {
	return &Handler{Upstream: upstream, Cerebro: cerebro}
}

// inboxItemResponse is the JSON shape returned by mute / unmute / unread.
// Matches the cerebro-only fields the frontend cares about; the row is
// re-fetched in full via the existing list query, this is just the ack of
// the mutation.
type inboxItemResponse struct {
	ID         string  `json:"id"`
	Read       bool    `json:"read"`
	Archived   bool    `json:"archived"`
	MutedUntil *string `json:"muted_until"`
}

// MuteInboxItem mutes an inbox item until the timestamp supplied in the
// request body. The timestamp is computed client-side ("next 08:00 in user
// local time" by default) so the server stays timezone-agnostic.
//
// Body: {"muted_until": "2026-05-08T08:00:00+02:00"}
func (h *Handler) MuteInboxItem(w http.ResponseWriter, r *http.Request) {
	itemID, ok := h.loadItemID(w, r)
	if !ok {
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
	item, err := h.Cerebro.SetInboxMutedUntil(r.Context(), cerebrodb.SetInboxMutedUntilParams{
		ID:         itemID,
		MutedUntil: pgtype.Timestamptz{Time: parsed, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mute inbox item")
		return
	}
	if item.IssueID.Valid {
		_, _ = h.Cerebro.SetInboxMutedUntilByIssue(r.Context(), cerebrodb.SetInboxMutedUntilByIssueParams{
			WorkspaceID:   item.WorkspaceID,
			RecipientType: item.RecipientType,
			RecipientID:   item.RecipientID,
			IssueID:       item.IssueID,
			MutedUntil:    pgtype.Timestamptz{Time: parsed, Valid: true},
		})
	}
	writeJSON(w, http.StatusOK, toResponse(item))
}

// UnmuteInboxItem clears the muted_until timestamp.
func (h *Handler) UnmuteInboxItem(w http.ResponseWriter, r *http.Request) {
	itemID, ok := h.loadItemID(w, r)
	if !ok {
		return
	}
	item, err := h.Cerebro.ClearInboxMute(r.Context(), itemID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unmute inbox item")
		return
	}
	if item.IssueID.Valid {
		_, _ = h.Cerebro.ClearInboxMuteByIssue(r.Context(), cerebrodb.ClearInboxMuteByIssueParams{
			WorkspaceID:   item.WorkspaceID,
			RecipientType: item.RecipientType,
			RecipientID:   item.RecipientID,
			IssueID:       item.IssueID,
		})
	}
	writeJSON(w, http.StatusOK, toResponse(item))
}

// MarkInboxUnread sets read=false on an inbox item. Counterpart to the
// upstream POST /api/inbox/{id}/read; we add a separate cerebro endpoint
// rather than patch the upstream one so the path stays cerebro-only and
// merges stay trivial.
func (h *Handler) MarkInboxUnread(w http.ResponseWriter, r *http.Request) {
	itemID, ok := h.loadItemID(w, r)
	if !ok {
		return
	}
	item, err := h.Cerebro.SetInboxUnread(r.Context(), itemID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark inbox item unread")
		return
	}
	writeJSON(w, http.StatusOK, toResponse(item))
}

// UnarchiveInboxItem restores an archived inbox item to the active inbox.
// Mirror of the upstream POST /api/inbox/{id}/archive. Lives in the cerebro
// package so the route stays cerebro-only and upstream merges don't touch it.
// Issue-level: if the item has an issue_id we unarchive every sibling row for
// the same recipient + issue, mirroring ArchiveInboxByIssue.
func (h *Handler) UnarchiveInboxItem(w http.ResponseWriter, r *http.Request) {
	itemID, ok := h.loadItemID(w, r)
	if !ok {
		return
	}
	item, err := h.Cerebro.UnarchiveInboxItem(r.Context(), itemID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unarchive inbox item")
		return
	}
	if item.IssueID.Valid {
		_, _ = h.Cerebro.UnarchiveInboxByIssue(r.Context(), cerebrodb.UnarchiveInboxByIssueParams{
			WorkspaceID:   item.WorkspaceID,
			RecipientType: item.RecipientType,
			RecipientID:   item.RecipientID,
			IssueID:       item.IssueID,
		})
	}
	writeJSON(w, http.StatusOK, toResponse(item))
}

func toResponse(i cerebrodb.InboxItem) inboxItemResponse {
	resp := inboxItemResponse{
		ID:       util.UUIDToString(i.ID),
		Read:     i.Read,
		Archived: i.Archived,
	}
	if i.MutedUntil.Valid {
		s := i.MutedUntil.Time.Format(time.RFC3339)
		resp.MutedUntil = &s
	}
	return resp
}

// loadItemID resolves and authorizes the inbox item id from the URL. On any
// failure it writes the appropriate HTTP response and returns ok=false.
//
// Mirrors handler.loadInboxItemForUser but reimplemented here so this package
// stays free of the main handler's dependency surface.
func (h *Handler) loadItemID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return pgtype.UUID{}, false
	}
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return pgtype.UUID{}, false
	}
	wsUUID, err := util.ParseUUID(wsID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return pgtype.UUID{}, false
	}
	itemUUID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid inbox item id")
		return pgtype.UUID{}, false
	}
	item, err := h.Upstream.GetInboxItemInWorkspace(r.Context(), db.GetInboxItemInWorkspaceParams{
		ID:          itemUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "inbox item not found")
		return pgtype.UUID{}, false
	}
	if item.RecipientType != "member" || util.UUIDToString(item.RecipientID) != userID {
		writeError(w, http.StatusForbidden, "not your inbox item")
		return pgtype.UUID{}, false
	}
	return item.ID, true
}

// requireUserID mirrors handler.requireUserID — kept private so this package
// compiles without importing the upstream handler package.
func requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", false
	}
	return userID, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
