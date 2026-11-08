package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) acceptInvitationForExistingMember(w http.ResponseWriter, r *http.Request, inv db.WorkspaceInvitation, user db.User, existing db.Member) {
	ctx := r.Context()
	capacityQueries := h.Queries
	if h.seatCapacityEnabled() {
		// Share the worker's lock order so a previously interrupted consume
		// cannot race the durable release below.
		q, unlock, err := h.lockSeatCapacityWorkspace(ctx, uuid.UUID(inv.WorkspaceID.Bytes))
		if err != nil {
			writeSeatCapacityError(w, err)
			return
		}
		defer unlock()
		capacityQueries = q
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to accept invitation")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	current, err := qtx.GetInvitationForUpdate(ctx, inv.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to accept invitation")
		return
	}
	if current.Status != "pending" && current.Status != "accepted" {
		writeError(w, http.StatusBadRequest, "invitation is not pending")
		return
	}
	if current.Status == "pending" && current.ExpiresAt.Valid && current.ExpiresAt.Time.Before(time.Now()) {
		writeError(w, http.StatusGone, "invitation has expired")
		return
	}
	member, err := qtx.GetMemberByUserAndWorkspaceForUpdate(ctx, db.GetMemberByUserAndWorkspaceForUpdateParams{
		UserID: user.ID, WorkspaceID: current.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// The member left while this request was waiting. Never fall back to
		// recreating a membership observed at the start of this request.
		writeError(w, http.StatusBadRequest, "membership no longer exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load membership")
		return
	}
	if member.ID != existing.ID {
		writeError(w, http.StatusConflict, "membership changed, please retry")
		return
	}
	if current.Status == "accepted" {
		writeJSON(w, http.StatusOK, h.memberWithUserResponse(member, user))
		return
	}
	accepted, err := qtx.AcceptInvitation(ctx, current.ID)
	if err == nil && h.seatCapacityEnabled() {
		// The invitation's unused reservation is distinct from the existing
		// member's seat. A confirm intent is protected by the outbox query.
		err = enqueueCapacityRelease(ctx, qtx, uuid.UUID(current.WorkspaceID.Bytes), uuid.UUID(current.ID.Bytes))
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to accept invitation")
		return
	}
	if h.seatCapacityEnabled() {
		h.compensateCapacityIntentLocked(ctx, capacityQueries, uuid.UUID(current.ID.Bytes))
	}
	response := h.memberWithUserResponse(member, user)
	// Only the invitation changed: do not announce another member:added.
	h.publish(protocol.EventInvitationAccepted, uuidToString(accepted.WorkspaceID), "member", uuidToString(user.ID), map[string]any{
		"invitation_id": uuidToString(accepted.ID), "member": response,
	})
	writeJSON(w, http.StatusOK, response)
}
