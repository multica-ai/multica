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
//
// TECH-3099 extends the guard with three additional sub-issue checks, each
// behind its own feature flag (all default OFF).
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

// OwnerMentionOnSubIssueMessage is returned when an agent on a sub-issue tries
// to @mention the workspace owner directly (TECH-3099).
const OwnerMentionOnSubIssueMessage = "Comments on sub-issues must not mention the workspace owner directly — post on the parent issue instead."

// MissingAgentTagOnSubIssueMessage is returned when an agent posts on a
// sub-issue without tagging any agent (TECH-3099).
const MissingAgentTagOnSubIssueMessage = "Comments on sub-issues must mention the parent agent to keep them in the loop."

// SplitSessionMessage is returned when the same task session has already
// posted on the parent issue and now tries to post on the sub-issue (TECH-3099).
const SplitSessionMessage = "This task session already posted on the parent issue — do not split a single conversation across both parent and sub-issue."

// FlagCommentTargetGuard is the cerebro feature flag (registry.ts) that gates
// this guard. Default OFF: with no override row the guard stays inactive, so
// prod behaviour is unchanged until an admin turns the flag on (FIR-2674).
const FlagCommentTargetGuard = "cerebro_comment_target_guard"

// TECH-3099: three additional sub-issue guard flags (all default OFF).
const FlagSubIssueNoOwnerMention  = "cerebro_sub_issue_no_owner_mention"
const FlagSubIssueRequireAgentTag = "cerebro_sub_issue_require_agent_tag"
const FlagSubIssueNoSplitSession  = "cerebro_sub_issue_no_split_session"

// flagReader is the subset of the cerebro Queries the guard needs to resolve
// its feature flags. Satisfied by *cerebrodb.Queries; the interface keeps the
// guard unit-testable without a database.
type flagReader interface {
	ListCerebroWorkspaceFeatureFlags(ctx context.Context, workspaceID pgtype.UUID) ([]cerebrodb.ListCerebroWorkspaceFeatureFlagsRow, error)
}

// Service is the Cerebro comment-target guard. A nil Service, or one built with
// a nil flag reader, is a safe no-op (guard disabled) so the wiring can never
// block by accident.
type Service struct{ flags flagReader }

// New returns a guard that resolves its feature flags through the cerebro
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
//
// Additional sub-issue checks (TECH-3099) are enabled via their own flags:
//   - isSubIssue: whether the target issue has a parent (parent_issue_id != nil)
//   - ownerUserIDs: workspace owner user IDs (mention://member/<user_id>)
//   - taskPostedOnParent: true when the same task session already posted on
//     the parent issue (pre-computed by the handler via cerebro_comment_task)
func (s *Service) RejectComment(
	ctx context.Context,
	workspaceID pgtype.UUID,
	authorType, content string,
	isSubIssue bool,
	ownerUserIDs []string,
	taskPostedOnParent bool,
) (string, bool) {
	// Members may comment freely; the guard only applies to agents.
	if s == nil || authorType != "agent" {
		return "", true
	}

	flags := s.loadFlags(ctx, workspaceID)

	// Off unless the workspace has the feature flag turned on.
	if !flags[FlagCommentTargetGuard] {
		return "", true
	}

	// Base check: comment must mention at least one target.
	if len(util.ParseMentions(content)) == 0 {
		return MissingTargetMessage, false
	}

	// TECH-3099 checks — only run on sub-issues.
	if isSubIssue {
		// Check 1: must not mention the workspace owner.
		if flags[FlagSubIssueNoOwnerMention] && mentionsOwner(content, ownerUserIDs) {
			return OwnerMentionOnSubIssueMessage, false
		}
		// Check 2: must mention at least one agent.
		if flags[FlagSubIssueRequireAgentTag] && !mentionsAgent(content) {
			return MissingAgentTagOnSubIssueMessage, false
		}
		// Check 3: same task must not have posted on the parent issue already.
		if flags[FlagSubIssueNoSplitSession] && taskPostedOnParent {
			return SplitSessionMessage, false
		}
	}

	return "", true
}

// mentionsOwner reports whether content contains a member mention whose ID
// matches any entry in ownerUserIDs.
func mentionsOwner(content string, ownerUserIDs []string) bool {
	for _, m := range util.ParseMentions(content) {
		if m.Type != "member" {
			continue
		}
		for _, id := range ownerUserIDs {
			if m.ID == id {
				return true
			}
		}
	}
	return false
}

// mentionsAgent reports whether content contains at least one agent mention.
func mentionsAgent(content string) bool {
	for _, m := range util.ParseMentions(content) {
		if m.Type == "agent" {
			return true
		}
	}
	return false
}

// loadFlags fetches all workspace-level cerebro feature flag overrides in one
// query and returns them as a key → enabled map. An absent key means no
// override row, which the guard treats as the TypeScript default (false for
// every guard flag). A DB error fails open (flags all false) and is logged.
func (s *Service) loadFlags(ctx context.Context, workspaceID pgtype.UUID) map[string]bool {
	result := make(map[string]bool)
	if s.flags == nil || !workspaceID.Valid {
		return result
	}
	rows, err := s.flags.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("commentguard: workspace flag lookup failed", "error", err)
		}
		return result
	}
	for _, r := range rows {
		result[r.FlagKey] = r.Enabled
	}
	return result
}
