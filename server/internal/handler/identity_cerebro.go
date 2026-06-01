// CEREBRO-PATCH(identity-cerebro): cerebro-only glue for the FIR-2523 Google
// Workspace identity-source integration. Net-new fork file in the upstream
// zone path. Defines the seam the cerebro identity service plugs into and the
// hook the upstream findOrCreateUser path calls after a user row is created.
//
// The hook is best-effort: a failure here must never block the login response
// because the user row already exists. We log + carry on; the next admin
// browse of the workspace will surface the missing membership.
package handler

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IdentityProvisionerInvoker is the upstream-side seam that the cerebro
// identity service plugs into. The concrete implementation lives in
// server/internal/cerebro/identity so the upstream handler package never
// imports the cerebro tree (which would create an import cycle).
type IdentityProvisionerInvoker interface {
	// ProvisionMembershipForNewUser is called once for every user row that the
	// findOrCreateUser path just inserted. The implementation looks up every
	// workspace whose cerebro_workspace_auth_settings.google_signup_domains
	// includes the user's email domain and creates a member row in each. Auth
	// source ("google" or "code") is passed through so the cerebro service can
	// later restrict auto-provisioning to specific entry points; today both
	// sources are honored.
	ProvisionMembershipForNewUser(ctx context.Context, user db.User, authSource string) (ProvisionMembershipResult, error)
}

// ProvisionMembershipResult is the upstream-facing view of the cerebro
// provisioning outcome. Both fields are observational — the upstream handler
// uses them only for structured logging.
type ProvisionMembershipResult struct {
	WorkspaceIDs []pgtype.UUID
	// CEREBRO-PATCH(auth-identity-group-placement): FIR-2523 default-group placement result.
	// GroupIDs are the cerebro default-groups the user was placed into (one per
	// matching workspace that has a default group configured). Observational —
	// used only for structured logging.
	GroupIDs   []pgtype.UUID
	Skipped    bool
	SkipReason string
}

// provisionMembershipForNewUser is the cerebro hook called from
// findOrCreateUser right after a brand-new user row is inserted. It is
// intentionally best-effort: failures are logged at warn level but never
// propagate. The user still exists and the login response still succeeds —
// the admin can recover by toggling the workspace setting or inviting the
// user manually.
func (h *Handler) provisionMembershipForNewUser(ctx context.Context, user db.User, authSource string) {
	if h.IdentityProvisioner == nil {
		return
	}
	res, err := h.IdentityProvisioner.ProvisionMembershipForNewUser(ctx, user, authSource)
	if err != nil {
		slog.Warn("identity provisioner hook: provision failed",
			"user_id", uuidToString(user.ID),
			"email", user.Email,
			"auth_source", authSource,
			"error", err)
		return
	}
	if res.Skipped {
		slog.Debug("identity provisioner hook: skipped",
			"user_id", uuidToString(user.ID),
			"email", user.Email,
			"reason", res.SkipReason)
		return
	}
	if len(res.WorkspaceIDs) == 0 {
		return
	}
	wsIDs := make([]string, len(res.WorkspaceIDs))
	for i, id := range res.WorkspaceIDs {
		wsIDs[i] = uuidToString(id)
	}
	groupIDs := make([]string, len(res.GroupIDs))
	for i, id := range res.GroupIDs {
		groupIDs[i] = uuidToString(id)
	}
	slog.Info("identity provisioner hook: user auto-provisioned",
		"user_id", uuidToString(user.ID),
		"email", user.Email,
		"auth_source", authSource,
		"workspace_ids", wsIDs,
		"group_ids", groupIDs)
}
