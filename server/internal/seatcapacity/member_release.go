package seatcapacity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const memberReleasePendingConfirmReason = "deferred: pending confirm for member"

type pendingMemberConfirmQueries interface {
	ExistsPendingSeatCapacityConfirmForMember(context.Context, db.ExistsPendingSeatCapacityConfirmForMemberParams) (bool, error)
}

type memberReleaseDeferQueries interface {
	pendingMemberConfirmQueries
	DeferSeatCapacityIntentForAction(context.Context, db.DeferSeatCapacityIntentForActionParams) (int64, error)
}

type claimedMemberReleaseDeferQueries interface {
	pendingMemberConfirmQueries
	DeferClaimedSeatCapacityIntent(context.Context, db.DeferClaimedSeatCapacityIntentParams) (int64, error)
}

func MemberReleaseSettleError(decision Decision, releaseErr error) error {
	if releaseErr != nil {
		if IsNotFound(releaseErr) {
			return nil
		}
		return releaseErr
	}
	if decision.Managed && !decision.Allowed && decision.Reason != "released" {
		return fmt.Errorf("capacity member release rejected in state %s", decision.Reason)
	}
	return nil
}

func DeferMemberReleaseIfPendingConfirm(ctx context.Context, q memberReleaseDeferQueries, workspaceID, memberID, operationToken uuid.UUID, now time.Time) (bool, time.Time, error) {
	pending, err := hasPendingMemberConfirm(ctx, q, workspaceID, memberID)
	if err != nil || !pending {
		return false, time.Time{}, err
	}
	nextAttemptAt := memberReleaseDeferDue(now)
	_, err = q.DeferSeatCapacityIntentForAction(ctx, db.DeferSeatCapacityIntentForActionParams{
		LastError:      memberReleasePendingConfirmReason,
		NextAttemptAt:  nextAttemptAt,
		OperationToken: uuidToPG(operationToken),
		Action:         ActionReleaseMember,
	})
	return true, nextAttemptAt.Time, err
}

func DeferClaimedMemberReleaseIfPendingConfirm(ctx context.Context, q claimedMemberReleaseDeferQueries, intent db.SeatCapacityOutbox, now time.Time) (bool, time.Time, error) {
	workspaceID, memberID := uuidFromPG(intent.WorkspaceID), uuidFromPG(intent.MemberID)
	pending, err := hasPendingMemberConfirm(ctx, q, workspaceID, memberID)
	if err != nil || !pending {
		return false, time.Time{}, err
	}
	nextAttemptAt := memberReleaseDeferDue(now)
	_, err = q.DeferClaimedSeatCapacityIntent(ctx, db.DeferClaimedSeatCapacityIntentParams{
		LastError:      memberReleasePendingConfirmReason,
		NextAttemptAt:  nextAttemptAt,
		OperationToken: intent.OperationToken,
		Action:         intent.Action,
		LeaseToken:     intent.LeaseToken,
	})
	return true, nextAttemptAt.Time, err
}

func LogMemberReleaseDeferred(ctx context.Context, logger *slog.Logger, workspaceID, memberID uuid.UUID, nextAttemptAt time.Time) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.WarnContext(ctx, "seat capacity member release deferred",
		"workspace_id", workspaceID.String(),
		"member_id", memberID.String(),
		"reason", "pending_confirm",
		"next_attempt_at", nextAttemptAt)
}

func hasPendingMemberConfirm(ctx context.Context, q pendingMemberConfirmQueries, workspaceID, memberID uuid.UUID) (bool, error) {
	if memberID == uuid.Nil {
		return false, errors.New("seat capacity member release omitted member id")
	}
	return q.ExistsPendingSeatCapacityConfirmForMember(ctx, db.ExistsPendingSeatCapacityConfirmForMemberParams{
		WorkspaceID: uuidToPG(workspaceID),
		MemberID:    uuidToPG(memberID),
	})
}

func memberReleaseDeferDue(now time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: now.Add(defaultReconcileInterval), Valid: true}
}
