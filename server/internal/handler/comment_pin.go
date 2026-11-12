package handler

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) PinComment(w http.ResponseWriter, r *http.Request)   { h.setCommentPin(w, r, true) }
func (h *Handler) UnpinComment(w http.ResponseWriter, r *http.Request) { h.setCommentPin(w, r, false) }

func (h *Handler) setCommentPin(w http.ResponseWriter, r *http.Request, pinned bool) {
	comment, workspaceID, actorType, actorID, ok := h.loadCommentForActor(w, r)
	if !ok {
		return
	}
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	// Navigation metadata does not produce wakeup business events.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to pin comment")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	// Read state only after the lock, so concurrent identical requests publish once.
	locked, err := q.LockLiveComment(r.Context(), db.LockLiveCommentParams{ID: comment.ID, WorkspaceID: comment.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock comment")
		return
	}
	updated := locked
	changed := locked.PinnedAt.Valid != pinned
	if changed {
		if pinned {
			updated, err = q.PinComment(r.Context(), db.PinCommentParams{ID: comment.ID, PinnedByType: pgtype.Text{String: actorType, Valid: true}, PinnedByID: actorUUID})
		} else {
			updated, err = q.UnpinComment(r.Context(), comment.ID)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update pin")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit pin")
		return
	}
	cid := uuidToString(updated.ID)
	resp := commentToResponse(updated, h.groupReactions(r, []pgtype.UUID{updated.ID})[cid], h.groupAttachments(r, []pgtype.UUID{updated.ID})[cid])
	if changed {
		event := protocol.EventCommentUnpinned
		if pinned {
			event = protocol.EventCommentPinned
		}
		h.publish(event, workspaceID, actorType, actorID, map[string]any{"comment": resp})
	}
	writeJSON(w, http.StatusOK, resp)
}
