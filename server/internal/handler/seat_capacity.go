package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/seatcapacity"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	errSeatCapacityFull          = errors.New("no purchased member seats are available")
	errSeatCapacityOvercommitted = errors.New("workspace members exceed purchased seat capacity")
	errSeatCapacityUnavailable   = errors.New("seat capacity service is unavailable")
)

func isPersistentSeatCapacityAdmissionRejection(err error) bool {
	return errors.Is(err, errSeatCapacityFull) || errors.Is(err, errSeatCapacityOvercommitted)
}

const seatCapacityWorkspaceLockWait = 2 * time.Second

func (h *Handler) seatCapacityEnabled() bool {
	return h != nil && h.SeatCapacity != nil
}

func (h *Handler) lockSeatCapacityWorkspace(ctx context.Context, workspaceID uuid.UUID) (*db.Queries, func(), error) {
	if h == nil {
		return nil, nil, errors.New("seat capacity handler is unavailable")
	}
	if h.SeatCapacityLocker == nil {
		return h.Queries, func() {}, nil
	}
	lockCtx, cancel := context.WithTimeout(ctx, seatCapacityWorkspaceLockWait)
	defer cancel()
	lockedDB, unlock, err := h.SeatCapacityLocker.Lock(lockCtx, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	return db.New(lockedDB), unlock, nil
}

func capacityIntentParams(workspaceID, token uuid.UUID, action string, due time.Time) db.UpsertSeatCapacityIntentParams {
	return db.UpsertSeatCapacityIntentParams{
		WorkspaceID:    uuidToPG(workspaceID),
		OperationToken: uuidToPG(token),
		Action:         action,
		NextAttemptAt:  pgtype.Timestamptz{Time: due, Valid: true},
	}
}

func (h *Handler) reserveInvitationCapacity(ctx context.Context, workspaceID, invitationID uuid.UUID, expiresAt time.Time) error {
	if !h.seatCapacityEnabled() {
		return nil
	}
	q, unlock, err := h.lockSeatCapacityWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("%w: acquire workspace capacity lock: %v", errSeatCapacityUnavailable, err)
	}
	defer unlock()
	params := capacityIntentParams(workspaceID, invitationID, seatcapacity.ActionReserveInvitation, seatcapacity.RecoveryDue(time.Now()).Time)
	params.SubjectID = uuidToPG(invitationID)
	params.InvitationID = uuidToPG(invitationID)
	params.ExpiresAt = pgtype.Timestamptz{Time: expiresAt, Valid: true}
	if _, err := q.UpsertSeatCapacityIntent(ctx, params); err != nil {
		return fmt.Errorf("record invitation capacity intent: %w", err)
	}
	decision, err := h.SeatCapacity.ReserveInvitation(ctx, workspaceID, invitationID, expiresAt)
	if err != nil {
		if seatcapacity.IsRateLimited(err) {
			if deleteErr := deleteCapacityIntentForAction(ctx, q, invitationID, seatcapacity.ActionReserveInvitation); deleteErr != nil {
				return fmt.Errorf("discard rate-limited invitation capacity intent: %w", deleteErr)
			}
			return fmt.Errorf("reserve invitation capacity: %w", err)
		}
		if seatcapacity.IsCapacityOvercommitted(err) {
			if deleteErr := deleteCapacityIntentForAction(ctx, q, invitationID, seatcapacity.ActionReserveInvitation); deleteErr != nil {
				return fmt.Errorf("discard overcommitted invitation capacity intent: %w", deleteErr)
			}
			return errSeatCapacityOvercommitted
		}
		h.compensateCapacityIntentLocked(ctx, q, invitationID)
		return fmt.Errorf("%w: %v", errSeatCapacityUnavailable, err)
	}
	if !decision.Managed {
		return deleteCapacityIntentForAction(ctx, q, invitationID, seatcapacity.ActionReserveInvitation)
	}
	if !decision.Allowed {
		if deleteErr := deleteCapacityIntentForAction(ctx, q, invitationID, seatcapacity.ActionReserveInvitation); deleteErr != nil {
			return fmt.Errorf("discard rejected invitation capacity intent: %w", deleteErr)
		}
		if decision.Reason == "capacity_full" {
			return errSeatCapacityFull
		}
		return fmt.Errorf("%w: reservation rejected in state %s", errSeatCapacityUnavailable, decision.Reason)
	}
	if err := q.MarkSeatCapacityIntentDelivered(ctx, db.MarkSeatCapacityIntentDeliveredParams{
		OperationToken: uuidToPG(invitationID), Action: seatcapacity.ActionReserveInvitation,
	}); err != nil {
		h.compensateCapacityIntentLocked(ctx, q, invitationID)
		return fmt.Errorf("record invitation capacity reservation: %w", err)
	}
	return nil
}

func (h *Handler) beginCapacityConsume(ctx context.Context, workspaceID, token, invitationID, userID uuid.UUID) (bool, error) {
	if !h.seatCapacityEnabled() {
		return false, nil
	}
	q, unlock, err := h.lockSeatCapacityWorkspace(ctx, workspaceID)
	if err != nil {
		return false, fmt.Errorf("%w: acquire workspace capacity lock: %v", errSeatCapacityUnavailable, err)
	}
	defer unlock()
	params := capacityIntentParams(workspaceID, token, seatcapacity.ActionConsumeInvitation, seatcapacity.RecoveryDue(time.Now()).Time)
	params.SubjectID = uuidToPG(token)
	params.InvitationID = uuidToPG(invitationID)
	params.UserID = uuidToPG(userID)
	if _, err := q.UpsertSeatCapacityIntent(ctx, params); err != nil {
		return false, fmt.Errorf("record invitation consume intent: %w", err)
	}
	decision, err := h.SeatCapacity.Consume(ctx, workspaceID, token)
	if err != nil {
		if seatcapacity.IsRateLimited(err) {
			return false, fmt.Errorf("consume invitation capacity: %w", err)
		}
		return false, fmt.Errorf("%w: %v", errSeatCapacityUnavailable, err)
	}
	if !decision.Managed {
		return false, deleteCapacityIntentForAction(ctx, q, token, seatcapacity.ActionConsumeInvitation)
	}
	if !decision.Allowed {
		if expireErr := q.ExpireInvitationForCapacityRecovery(ctx, uuidToPG(invitationID)); expireErr != nil {
			return false, fmt.Errorf("expire invitation after rejected capacity consume: %w", expireErr)
		}
		if deleteErr := deleteCapacityIntentForAction(ctx, q, token, seatcapacity.ActionConsumeInvitation); deleteErr != nil {
			return false, fmt.Errorf("discard rejected invitation consume intent: %w", deleteErr)
		}
		return false, errSeatCapacityFull
	}
	err = q.MarkSeatCapacityIntentDelivered(ctx, db.MarkSeatCapacityIntentDeliveredParams{
		OperationToken: uuidToPG(token), Action: seatcapacity.ActionConsumeInvitation,
	})
	return err == nil, err
}

func (h *Handler) beginShareJoinCapacity(ctx context.Context, workspaceID, shareLinkID, userID uuid.UUID) (uuid.UUID, error) {
	if !h.seatCapacityEnabled() {
		return uuid.Nil, nil
	}
	q, unlock, err := h.lockSeatCapacityWorkspace(ctx, workspaceID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: acquire workspace capacity lock: %v", errSeatCapacityUnavailable, err)
	}
	defer unlock()
	intent, err := q.CreateOrReactivateShareJoinCapacityIntent(ctx, db.CreateOrReactivateShareJoinCapacityIntentParams{
		WorkspaceID: uuidToPG(workspaceID), OperationToken: uuidToPG(uuid.New()),
		ShareLinkID: uuidToPG(shareLinkID), UserID: uuidToPG(userID),
		NextAttemptAt: seatcapacity.RecoveryDue(time.Now()),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("record share-join capacity intent: %w", err)
	}
	token := uuid.UUID(intent.OperationToken.Bytes)
	decision, err := h.SeatCapacity.ClaimShareJoin(ctx, workspaceID, token)
	if err != nil {
		if seatcapacity.IsRateLimited(err) {
			if deleteErr := deleteCapacityIntentForAction(ctx, q, token, seatcapacity.ActionClaimShareJoin); deleteErr != nil {
				return uuid.Nil, fmt.Errorf("discard rate-limited share-join capacity intent: %w", deleteErr)
			}
			return uuid.Nil, fmt.Errorf("claim share-join capacity: %w", err)
		}
		if seatcapacity.IsCapacityOvercommitted(err) {
			if deleteErr := deleteCapacityIntentForAction(ctx, q, token, seatcapacity.ActionClaimShareJoin); deleteErr != nil {
				return uuid.Nil, fmt.Errorf("discard overcommitted share-join capacity intent: %w", deleteErr)
			}
			return uuid.Nil, errSeatCapacityOvercommitted
		}
		return uuid.Nil, fmt.Errorf("%w: %v", errSeatCapacityUnavailable, err)
	}
	if !decision.Managed {
		return uuid.Nil, deleteCapacityIntentForAction(ctx, q, token, seatcapacity.ActionClaimShareJoin)
	}
	if !decision.Allowed {
		if deleteErr := deleteCapacityIntentForAction(ctx, q, token, seatcapacity.ActionClaimShareJoin); deleteErr != nil {
			return uuid.Nil, fmt.Errorf("discard rejected share-join capacity intent: %w", deleteErr)
		}
		return uuid.Nil, errSeatCapacityFull
	}
	if err := q.MarkSeatCapacityIntentDelivered(ctx, db.MarkSeatCapacityIntentDeliveredParams{
		OperationToken: uuidToPG(token), Action: seatcapacity.ActionClaimShareJoin,
	}); err != nil {
		return uuid.Nil, err
	}
	return token, nil
}

func transitionCapacityIntentToConfirm(ctx context.Context, q *db.Queries, token, memberID uuid.UUID, currentAction string) error {
	rows, err := q.TransitionSeatCapacityIntent(ctx, db.TransitionSeatCapacityIntentParams{
		NextAction: seatcapacity.ActionConfirm, CurrentAction: currentAction,
		MemberID: uuidToPG(memberID), OperationToken: uuidToPG(token),
		NextAttemptAt: seatcapacity.RetryDue(time.Now()),
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("seat capacity intent changed concurrently")
	}
	return nil
}

func enqueueCapacityRelease(ctx context.Context, q *db.Queries, workspaceID, token uuid.UUID) error {
	_, err := q.EnqueueSeatCapacityRelease(ctx, db.EnqueueSeatCapacityReleaseParams{
		WorkspaceID: uuidToPG(workspaceID), OperationToken: uuidToPG(token),
		SubjectID: uuidToPG(token), NextAttemptAt: seatcapacity.RetryDue(time.Now()),
	})
	return err
}

func enqueueMemberCapacityRelease(ctx context.Context, q *db.Queries, workspaceID, memberID uuid.UUID) error {
	operationToken := uuid.New()
	params := capacityIntentParams(workspaceID, operationToken, seatcapacity.ActionReleaseMember, time.Now())
	params.MemberID = uuidToPG(memberID)
	_, err := q.UpsertSeatCapacityIntent(ctx, params)
	return err
}

func (h *Handler) confirmCapacityIntent(ctx context.Context, workspaceID, token, memberID uuid.UUID) {
	if !h.seatCapacityEnabled() {
		return
	}
	q, unlock, err := h.lockSeatCapacityWorkspace(ctx, workspaceID)
	if err != nil {
		slog.WarnContext(ctx, "seat capacity confirm deferred while workspace lock is unavailable",
			"workspace_id", workspaceID.String(), "operation_token", token.String(), "error", err)
		return
	}
	defer unlock()
	intent, err := q.GetSeatCapacityIntent(ctx, uuidToPG(token))
	if err != nil || intent.Action != seatcapacity.ActionConfirm {
		return
	}
	decision, err := h.SeatCapacity.Confirm(ctx, workspaceID, token, memberID)
	if err == nil && (!decision.Managed || decision.Allowed) {
		if deleteErr := deleteCapacityIntentForAction(ctx, q, token, seatcapacity.ActionConfirm); deleteErr == nil {
			return
		} else {
			err = deleteErr
		}
	}
	if err == nil {
		err = fmt.Errorf("confirm rejected in state %s", decision.Reason)
	}
	recordCapacityFailure(ctx, q, token, seatcapacity.ActionConfirm, err)
}

func (h *Handler) compensateCapacityIntent(ctx context.Context, token uuid.UUID) {
	if !h.seatCapacityEnabled() {
		return
	}
	intent, err := h.Queries.GetSeatCapacityIntent(ctx, uuidToPG(token))
	if err != nil {
		return
	}
	q, unlock, err := h.lockSeatCapacityWorkspace(ctx, uuid.UUID(intent.WorkspaceID.Bytes))
	if err != nil {
		return
	}
	defer unlock()
	h.compensateCapacityIntentLocked(ctx, q, token)
}

func (h *Handler) compensateCapacityIntentLocked(ctx context.Context, q *db.Queries, token uuid.UUID) {
	intent, err := q.GetSeatCapacityIntent(ctx, uuidToPG(token))
	if err != nil {
		return
	}
	// A confirm row belongs to a product transaction that already committed.
	// A losing duplicate request must never release the winning member's seat.
	if intent.Action == seatcapacity.ActionConfirm || intent.Action == seatcapacity.ActionReleaseMember {
		return
	}
	rows, err := q.TransitionSeatCapacityIntent(ctx, db.TransitionSeatCapacityIntentParams{
		NextAction: seatcapacity.ActionRelease, CurrentAction: intent.Action,
		OperationToken: uuidToPG(token), NextAttemptAt: seatcapacity.RetryDue(time.Now()),
	})
	if err != nil || rows != 1 {
		return
	}
	decision, releaseErr := h.SeatCapacity.Release(ctx, uuid.UUID(intent.WorkspaceID.Bytes), token)
	if (releaseErr == nil && (!decision.Managed || decision.Allowed || decision.Reason == "released")) || seatcapacity.IsNotFound(releaseErr) {
		_ = deleteCapacityIntentForAction(ctx, q, token, seatcapacity.ActionRelease)
		return
	}
	if releaseErr == nil {
		releaseErr = fmt.Errorf("release rejected in state %s", decision.Reason)
	}
	recordCapacityFailure(ctx, q, token, seatcapacity.ActionRelease, releaseErr)
}

func (h *Handler) settleMemberCapacityRelease(ctx context.Context, workspaceID, memberID uuid.UUID) {
	if !h.seatCapacityEnabled() {
		return
	}
	q, unlock, err := h.lockSeatCapacityWorkspace(ctx, workspaceID)
	if err != nil {
		return
	}
	defer unlock()
	intent, err := q.GetMemberReleaseCapacityIntent(ctx, db.GetMemberReleaseCapacityIntentParams{
		WorkspaceID: uuidToPG(workspaceID), MemberID: uuidToPG(memberID),
	})
	if err != nil {
		return
	}
	decision, releaseErr := h.SeatCapacity.ReleaseMember(ctx, workspaceID, memberID)
	if (releaseErr == nil && (!decision.Managed || decision.Allowed || decision.Reason == "released")) || seatcapacity.IsNotFound(releaseErr) {
		_ = deleteCapacityIntentForAction(ctx, q, uuid.UUID(intent.OperationToken.Bytes), seatcapacity.ActionReleaseMember)
		return
	}
	if releaseErr == nil {
		releaseErr = fmt.Errorf("member release rejected in state %s", decision.Reason)
	}
	recordCapacityFailure(ctx, q, uuid.UUID(intent.OperationToken.Bytes), intent.Action, releaseErr)
}

func deleteCapacityIntentForAction(ctx context.Context, q *db.Queries, token uuid.UUID, action string) error {
	return q.DeleteSeatCapacityIntentForAction(ctx, db.DeleteSeatCapacityIntentForActionParams{
		OperationToken: uuidToPG(token), Action: action,
	})
}

func recordCapacityFailure(ctx context.Context, q *db.Queries, token uuid.UUID, action string, capacityErr error) {
	if capacityErr == nil {
		return
	}
	_ = q.MarkSeatCapacityIntentFailed(ctx, db.MarkSeatCapacityIntentFailedParams{
		LastError: capacityErr.Error(), NextAttemptAt: pgtype.Timestamptz{Time: time.Now().Add(5 * time.Second), Valid: true},
		OperationToken: uuidToPG(token), Action: action,
	})
	slog.WarnContext(ctx, "seat capacity intent deferred to outbox",
		"operation_token", token.String(), "action", action, "error", capacityErr)
}

func writeSeatCapacityError(w http.ResponseWriter, err error) {
	switch {
	case seatcapacity.IsRateLimited(err):
		retryAfter := seatcapacity.RateLimitRetryAfter(err)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		retryAfterSeconds := int64((retryAfter + time.Second - 1) / time.Second)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfterSeconds))
		writeErrorCode(w, http.StatusTooManyRequests, "seat_capacity_rate_limited", "Member capacity is busy. Please retry shortly.")
	case errors.Is(err, errSeatCapacityOvercommitted):
		writeErrorCode(w, http.StatusConflict, "seat_capacity_overcommitted", "Workspace members exceed purchased seats. Add enough seats or remove members before adding another member.")
	case errors.Is(err, errSeatCapacityFull):
		writeErrorCode(w, http.StatusConflict, "seat_capacity_full", "No purchased member seats are available. Add seats in Billing before adding another member.")
	default:
		writeErrorCode(w, http.StatusServiceUnavailable, "seat_capacity_unavailable", "Member capacity could not be verified. Please try again.")
	}
}

func uuidToPG(value uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: value, Valid: value != uuid.Nil}
}
