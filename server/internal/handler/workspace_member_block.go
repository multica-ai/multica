package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Removing a member can block their email from joining the workspace again
// (MUL-10296); without it, any invitation or share link would let them back
// in. Admins see and lift blocks in Members settings.

const blockedFromWorkspaceMessage = "this account can no longer join this workspace"

var errBlockedFromWorkspace = errors.New(blockedFromWorkspaceMessage)

// checkJoinBlock is the first statement of a join transaction. It takes the
// same per-member lock member removal takes first, so a removal that blocks
// this email either commits before the check sees it or waits until the join
// is done.
func checkJoinBlock(ctx context.Context, qtx *db.Queries, workspaceID, userID pgtype.UUID, email string) error {
	if err := qtx.LockSubscriberWrites(ctx, db.LockSubscriberWritesParams{WorkspaceID: workspaceID, UserID: userID}); err != nil {
		return err
	}
	blocked, err := qtx.IsBlockedFromWorkspace(ctx, db.IsBlockedFromWorkspaceParams{WorkspaceID: workspaceID, Email: email})
	if err != nil {
		return err
	}
	if blocked {
		return errBlockedFromWorkspace
	}
	return nil
}

// isBlockedFromWorkspace is the early, unlocked check that refuses before any
// seat is reserved; the join transaction checks again under the lock.
func (h *Handler) isBlockedFromWorkspace(ctx context.Context, workspaceID pgtype.UUID, email string) (bool, error) {
	return h.Queries.IsBlockedFromWorkspace(ctx, db.IsBlockedFromWorkspaceParams{WorkspaceID: workspaceID, Email: email})
}

type MemberBlockResponse struct {
	ID        string  `json:"id"`
	Email     string  `json:"email"`
	BlockedBy *string `json:"blocked_by"`
	CreatedAt string  `json:"created_at"`
}

// ListMemberBlocks — GET /api/workspaces/{id}/member-blocks (owner/admin).
func (h *Handler) ListMemberBlocks(w http.ResponseWriter, r *http.Request) {
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceIDFromURL(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListWorkspaceMemberBlocks(r.Context(), workspaceUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list blocked members")
		return
	}
	resp := make([]MemberBlockResponse, 0, len(rows))
	for _, row := range rows {
		item := MemberBlockResponse{
			ID:        uuidToString(row.ID),
			Email:     row.Email,
			CreatedAt: row.CreatedAt.Time.UTC().Format(time.RFC3339),
		}
		if row.BlockedBy.Valid {
			by := uuidToString(row.BlockedBy)
			item.BlockedBy = &by
		}
		resp = append(resp, item)
	}
	writeJSON(w, http.StatusOK, resp)
}

// DeleteMemberBlock — DELETE /api/workspaces/{id}/member-blocks/{blockId}
// (owner/admin) lets the email be invited again.
func (h *Handler) DeleteMemberBlock(w http.ResponseWriter, r *http.Request) {
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceIDFromURL(r, "id"), "workspace id")
	if !ok {
		return
	}
	blockUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "blockId"), "block id")
	if !ok {
		return
	}
	n, err := h.Queries.DeleteWorkspaceMemberBlock(r.Context(), db.DeleteWorkspaceMemberBlockParams{ID: blockUUID, WorkspaceID: workspaceUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unblock")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "block not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
