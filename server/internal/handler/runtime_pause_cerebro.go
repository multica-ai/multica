// CEREBRO-PATCH(runtime-pause-cerebro): cerebro-only HTTP handlers for the
// runtime pause/unpause feature (JEH-848). Net-new fork file in the upstream-
// zone path. Delegates business logic to the cerebro runtime Service via the
// RuntimePauseInvoker seam declared on *Handler so the upstream-handler
// package never imports the cerebro implementation directly.
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// pauseRuntimeRequest is the JSON body for POST /api/runtimes/{runtimeId}/pause.
// Both fields are optional: an empty body pauses indefinitely with reason='manual'.
type pauseRuntimeRequest struct {
	UnpauseAt string `json:"unpause_at"` // RFC3339; empty = no auto-unpause
	Reason    string `json:"reason"`     // free-form slug; defaults to "manual"
}

// PauseRuntime puts a runtime to sleep. Owner/admin or runtime owner only.
// Tasks in flight are interrupted with failure_reason='runtime_paused' and
// will be resumed on unpause.
func (h *Handler) PauseRuntime(w http.ResponseWriter, r *http.Request) {
	rt, member, ok := h.loadRuntimeForPauseMutation(w, r)
	if !ok {
		return
	}

	var req pauseRuntimeRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	opts := RuntimePauseOptions{Reason: req.Reason}
	if req.UnpauseAt != "" {
		t, err := time.Parse(time.RFC3339, req.UnpauseAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "unpause_at must be RFC3339 (e.g. 2026-05-04T18:50:00Z)")
			return
		}
		if t.Before(time.Now().Add(-1 * time.Minute)) {
			writeError(w, http.StatusBadRequest, "unpause_at must be in the future")
			return
		}
		opts.UnpauseAt = t
	}
	if opts.Reason == "" {
		opts.Reason = "manual"
	}

	if h.RuntimePause == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime pause service not available")
		return
	}
	if _, err := h.RuntimePause.PauseRuntime(r.Context(), rt.ID, opts); err != nil {
		slog.Warn("pause runtime failed", "runtime_id", uuidToString(rt.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to pause runtime")
		return
	}

	// Re-fetch so the response sees the row exactly as the DB sees it,
	// including any concurrent updated_at touches. Cheaper than threading
	// the full AgentRuntime back through the cerebro seam.
	updated, err := h.Queries.GetAgentRuntime(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to refetch runtime")
		return
	}

	slog.Info("runtime paused",
		"runtime_id", uuidToString(rt.ID),
		"reason", opts.Reason,
		"unpause_at", req.UnpauseAt,
		"actor", uuidToString(member.UserID),
	)

	writeJSON(w, http.StatusOK, runtimeToResponse(updated))
}

// UnpauseRuntime wakes a paused runtime back up and resumes any work that
// was suspended by the pause. Idempotent on already-unpaused runtimes.
func (h *Handler) UnpauseRuntime(w http.ResponseWriter, r *http.Request) {
	rt, member, ok := h.loadRuntimeForPauseMutation(w, r)
	if !ok {
		return
	}

	if h.RuntimePause == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime pause service not available")
		return
	}
	if _, err := h.RuntimePause.UnpauseRuntime(r.Context(), rt.ID); err != nil {
		slog.Warn("unpause runtime failed", "runtime_id", uuidToString(rt.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to unpause runtime")
		return
	}

	updated, err := h.Queries.GetAgentRuntime(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to refetch runtime")
		return
	}

	slog.Info("runtime unpaused",
		"runtime_id", uuidToString(rt.ID),
		"actor", uuidToString(member.UserID),
	)

	writeJSON(w, http.StatusOK, runtimeToResponse(updated))
}

// loadRuntimeForPauseMutation centralises runtime lookup + permission check
// for pause/unpause. Owner-of-runtime OR workspace owner/admin may mutate.
// On failure it writes the response and returns ok=false.
//
// Named distinctly from a hypothetical generic loadRuntimeForMutation so a
// future upstream-side helper with the same name doesn't collide on the
// next sync.
func (h *Handler) loadRuntimeForPauseMutation(w http.ResponseWriter, r *http.Request) (db.AgentRuntime, db.Member, bool) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return db.AgentRuntime{}, db.Member{}, false
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return db.AgentRuntime{}, db.Member{}, false
	}

	wsID := uuidToString(rt.WorkspaceID)
	member, ok := h.requireWorkspaceMember(w, r, wsID, "runtime not found")
	if !ok {
		return db.AgentRuntime{}, db.Member{}, false
	}

	userID := uuidToString(member.UserID)
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	isOwner := rt.OwnerID.Valid && uuidToString(rt.OwnerID) == userID
	if !isAdmin && !isOwner {
		writeError(w, http.StatusForbidden, "you can only manage your own runtimes")
		return db.AgentRuntime{}, db.Member{}, false
	}

	return rt, member, true
}
