package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ApproveCascade POST /api/issues/{id}/cascade/approve.
//
// Marks the issue as the root of an approved cascade so subsequent
// agent runs flip into cascade-execution mode instead of asking for
// per-PR confirmation. Invoked by the /plan-and-implement skill on
// the first user "погнали" — that user signal converts to a single
// SQL UPDATE here, and from that point on every PR4 worker spawn
// for this issue runs without further user input until the cascade
// completes, the user pauses it, or the loop guard trips.
//
// Idempotent: re-calling on an already-approved issue is a no-op
// (the existing cascade_started_at is preserved so the audit trail
// stays accurate).
//
// Auth: standard workspace-member gate. The endpoint is mounted in
// cmd/server/router.go under the issue {id} subtree which already
// enforces RequireWorkspaceMember.
func (h *Handler) ApproveCascade(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	// The UPDATE is gated on cascade_started_at IS NULL so the
	// timestamp is set exactly once. Subsequent calls are no-ops on
	// the timestamp but still allowed (idempotent), and they reset
	// cascade_state to 'approved' so a paused → re-approve flow
	// works without a separate endpoint.
	_, err := h.DB.Exec(ctx, `
UPDATE issue
SET cascade_state = 'approved',
    cascade_started_at = COALESCE(cascade_started_at, now()),
    cascade_last_event_at = now(),
    updated_at = now()
WHERE id = $1`,
		pgtype.UUID{Bytes: issue.ID.Bytes, Valid: true},
	)
	if err != nil {
		slog.Warn("cascade approve failed", "issue_id", uuidToString(issue.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to approve cascade")
		return
	}

	resp := map[string]any{
		"issue_id":      uuidToString(issue.ID),
		"cascade_state": "approved",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
