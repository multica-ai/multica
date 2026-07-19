package main

// cerebro_eval_drift.go keeps the FIR-3496 permission adapter outside the
// upstream router wiring.

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrogrouppermissions "github.com/multica-ai/multica/server/internal/cerebro/grouppermissions"
)

// evalBlockingGateAdapter adapts grouppermissions.Service to the evals handler's
// BlockingGateAuthorizer: it resolves the member's effective group memberships
// and asks whether they may set a blocking eval gate. Admins short-circuit
// without a group lookup.
type evalBlockingGateAdapter struct {
	svc *cerebrogrouppermissions.Service
}

func (a evalBlockingGateAdapter) CanSetBlockingGate(ctx context.Context, workspaceID, userID uuid.UUID, isAdmin bool) (bool, error) {
	wsPG := pgtype.UUID{Bytes: workspaceID, Valid: true}
	viewer := cerebrogrouppermissions.Viewer{UserID: pgtype.UUID{Bytes: userID, Valid: true}, IsAdmin: isAdmin}
	if !isAdmin {
		groups, err := a.svc.ResolveGroupIDs(ctx, wsPG, viewer.UserID)
		if err != nil {
			return false, err
		}
		viewer.GroupIDs = groups
	}
	return a.svc.CanSetBlockingGate(ctx, viewer, wsPG)
}
