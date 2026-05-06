package handler

import (
	"context"
	"log/slog"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EnsureLazyInviteMembership idempotently adds the user as a member of the
// workspace identified by their email domain in LAZY_INVITE_RULES.
//
// It runs in two situations:
//  1. The user is brand-new (just created in findOrCreateUser*).
//  2. The user is existing but has zero memberships (likely created before
//     the rule was added; we treat them like a new user for ergonomics).
//
// In all other cases it returns without touching the DB. Rationale: if a
// user has any membership, they've made it past the auth/invite flow once
// already, and adding them to the lazy-invite workspace would override an
// admin's decision to (a) place them in a specific workspace or (b) remove
// them from this one.
//
// Failure to insert is logged but never propagated — login must not depend
// on best-effort enrichment. The next sign-in retries via the
// zero-memberships path.
func (h *Handler) EnsureLazyInviteMembership(ctx context.Context, user db.User, isNew bool) {
	if len(h.LazyInvite) == 0 {
		return
	}
	rule, ok := h.LazyInvite.Match(user.Email)
	if !ok {
		return
	}

	if !isNew {
		count, err := h.Queries.CountMembershipsByUser(ctx, user.ID)
		if err != nil {
			// Fail closed: if we can't tell, don't add. Next attempt retries.
			slog.Warn("lazy-invite: count memberships failed", "user_id", uuidToString(user.ID), "error", err)
			return
		}
		if count > 0 {
			return
		}
	}

	_, err := h.Queries.CreateMember(ctx, db.CreateMemberParams{
		WorkspaceID: rule.WorkspaceID,
		UserID:      user.ID,
		Role:        "member",
	})
	if err != nil {
		if isUniqueViolation(err) {
			// Already a member. Idempotent no-op.
			slog.Debug("lazy-invite: user already member",
				"user_id", uuidToString(user.ID),
				"workspace_slug", rule.WorkspaceSlug,
			)
			return
		}
		slog.Warn("lazy-invite: create member failed",
			"user_id", uuidToString(user.ID),
			"workspace_slug", rule.WorkspaceSlug,
			"error", err,
		)
		return
	}
	slog.Info("lazy-invite: granted membership",
		"user_id", uuidToString(user.ID),
		"email", user.Email,
		"workspace_slug", rule.WorkspaceSlug,
	)
}
