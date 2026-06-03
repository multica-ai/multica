// Package commentguard owns Cerebro's "no agent comment without a target"
// guardrail (FIR-2674).
//
// Agents used to post comments with no addressee, so a reader could not tell
// who a comment was for. This guard rejects an agent-authored comment that
// references no target at all. A "target" is any mention: a member, an agent,
// a squad, or an issue. An issue link (mention://issue/...) counts and has no
// side effect, so an agent can always satisfy the rule without waking another
// agent — only an agent mention triggers a run, so the guard never forces a
// loop. Member-authored comments are never gated.
//
// The guard is gated by the cerebro feature flag cerebro_comment_target_guard,
// exactly like every other cerebro extension (registry.ts). It is resolved per
// workspace at request time, so the guard can be turned on/off from the Multica
// feature-flags screen with no env var and no server restart. Default OFF: with
// no override row prod behaviour is unchanged until an admin turns it on.
package commentguard

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// MissingTargetMessage is returned to the caller (e.g. the CLI an agent posts
// through) when a comment is rejected, so the agent can re-post with a target.
const MissingTargetMessage = "comment must mention a target — a person, an agent, or an issue (e.g. MUL-123). Comments with no target are not allowed (FIR-2674)."

// FlagCommentTargetGuard is the cerebro feature flag (registry.ts) that gates
// this guard. Default OFF: with no override row the guard stays inactive, so
// prod behaviour is unchanged until an admin turns the flag on (FIR-2674).
const FlagCommentTargetGuard = "cerebro_comment_target_guard"

// flagReader is the subset of the cerebro Queries the guard needs to resolve
// its feature flag. Satisfied by *cerebrodb.Queries; the interface keeps the
// guard unit-testable without a database.
type flagReader interface {
	ListCerebroWorkspaceFeatureFlags(ctx context.Context, workspaceID pgtype.UUID) ([]cerebrodb.ListCerebroWorkspaceFeatureFlagsRow, error)
}

// Service is the Cerebro comment-target guard. A nil Service, or one built with
// a nil flag reader, is a safe no-op (guard disabled) so the wiring can never
// block by accident.
type Service struct{ flags flagReader }

// New returns a guard that resolves its feature flag through the cerebro
// Queries. Passing nil yields an always-disabled guard.
func New(flags flagReader) *Service { return &Service{flags: flags} }

// RejectComment reports whether a comment must be rejected before it is stored.
// It returns (message, ok): ok=true means the comment passes; ok=false means it
// must be rejected and message explains why.
//
// Only agent-authored comments are ever gated, and only when the
// cerebro_comment_target_guard flag is ON for the workspace. content must
// already have had bare issue identifiers (e.g. "MUL-123") expanded into
// mention links, so plain issue references count as a target.
func (s *Service) RejectComment(ctx context.Context, workspaceID pgtype.UUID, authorType, content string) (string, bool) {
	// Members may comment freely; the guard only applies to agents.
	if s == nil || authorType != "agent" {
		return "", true
	}
	// Off unless the workspace has the feature flag turned on.
	if !s.enabled(ctx, workspaceID) {
		return "", true
	}
	if len(util.ParseMentions(content)) == 0 {
		return MissingTargetMessage, false
	}
	return "", true
}

// enabled resolves the workspace-level cerebro_comment_target_guard flag. No
// override row means default OFF; a DB error fails closed (guard inactive) and
// is logged. This is the server mirror of the workspace resolution in
// packages/cerebro-feature-flags/store.ts.
func (s *Service) enabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	if s.flags == nil || !workspaceID.Valid {
		return false
	}
	rows, err := s.flags.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("commentguard: workspace flag lookup failed", "error", err)
		}
		return false
	}
	for _, r := range rows {
		if r.FlagKey == FlagCommentTargetGuard {
			return r.Enabled
		}
	}
	return false
}
