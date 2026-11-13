package handler

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SetWorkspaceOrder replaces only the caller's personal workspace positions.
func (h *Handler) SetWorkspaceOrder(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		WorkspaceIDs []string `json:"workspace_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WorkspaceIDs == nil {
		writeError(w, http.StatusBadRequest, "workspace_ids must be an array")
		return
	}
	ids := make([]pgtype.UUID, len(req.WorkspaceIDs))
	seen := make(map[pgtype.UUID]bool, len(ids))
	for i, value := range req.WorkspaceIDs {
		id, valid := parseUUIDOrBadRequest(w, value, "workspace_ids[]")
		if !valid {
			return
		}
		if seen[id] {
			writeError(w, http.StatusBadRequest, "duplicate workspace id")
			return
		}
		ids[i] = id
		seen[id] = true
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save workspace order")
		return
	}
	defer tx.Rollback(r.Context())
	queries := h.Queries.WithTx(tx)
	userUUID := parseUUID(userID)
	members, err := queries.LockWorkspaceOrderMembers(r.Context(), userUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workspaces")
		return
	}
	owned := make(map[pgtype.UUID]bool, len(members))
	for _, id := range members {
		owned[id] = true
	}
	for _, id := range ids {
		if !owned[id] {
			writeError(w, http.StatusBadRequest, "workspace is not a membership of the current user")
			return
		}
	}
	if len(ids) != len(members) {
		writeError(w, http.StatusConflict, "workspace membership changed; reload and try again")
		return
	}
	for i, id := range ids {
		if err := queries.SetWorkspacePosition(r.Context(), db.SetWorkspacePositionParams{
			UserID: userUUID, WorkspaceID: id, Position: int32(i),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save workspace order")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save workspace order")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
