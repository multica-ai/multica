package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Cancellation keeps processing builds active until the worker confirms exit.
func (h *Handler) CancelWorkspaceCodeGraph(w http.ResponseWriter, r *http.Request) {
	ws := workspaceIDFromURL(r, "id")
	wsID, ok := parseUUIDOrBadRequest(w, ws, "workspace id")
	if !ok || !h.requireCodeGraphWriter(w, r, ws) {
		return
	}
	repoID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "codeGraphId"), "code graph id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "Cannot cancel build")
		return
	}
	defer tx.Rollback(r.Context())
	var locked pgtype.UUID
	err = tx.QueryRow(r.Context(), `SELECT id FROM code_graph_repository WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, repoID, wsID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "Repository not found")
		return
	}
	if err != nil {
		writeError(w, 500, "Cannot lock repository")
		return
	}
	var state string
	err = tx.QueryRow(r.Context(), `UPDATE code_graph_build SET cancel_requested=true,phase=CASE WHEN status='queued' THEN 'cancelled' ELSE 'cancelling' END,phase_started_at=now(),error=CASE WHEN status='queued' THEN 'Build cancelled by user' ELSE error END,finished_at=CASE WHEN status='queued' THEN now() ELSE finished_at END,status=CASE WHEN status='queued' THEN 'failed' ELSE status END WHERE repository_id=$1 AND workspace_id=$2 AND status IN ('queued','processing') RETURNING status`, repoID, wsID).Scan(&state)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 500, "Cannot cancel build")
		return
	}
	if err == nil {
		phase := "cancelling"
		if state == "failed" {
			phase = "cancelled"
		}
		_, err = tx.Exec(r.Context(), `UPDATE code_graph_repository SET status=CASE WHEN $2='cancelled' THEN 'failed' ELSE status END,internal_status=$2,sync_error=CASE WHEN $2='cancelled' THEN 'Build cancelled by user' ELSE NULL END,updated_at=now() WHERE id=$1`, repoID, phase)
		if err != nil {
			writeError(w, 500, "Cannot update cancellation")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "Cannot save cancellation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
