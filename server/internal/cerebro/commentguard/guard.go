// Package commentguard owns Cerebro's "no agent comment without a target"
// guardrail (FIR-2674).
//
// Agents used to post comments with no addressee, so a reader could not tell
// who a comment was for. This guard rejects an agent-authored comment that
// addresses no recipient. A "recipient" is a member, an agent, a squad, or an
// @all mention. An issue link (mention://issue/...) does NOT count: it points
// at a case, not a person, so agents were satisfying the rule with a bare
// MUL-123 link while still addressing no one — exactly the behaviour the guard
// exists to stop (TECH-3279). Member-authored comments are never gated.
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
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// MissingTargetMessage is returned to the caller (e.g. the CLI an agent posts
// through) when a comment is rejected, so the agent can re-post with a target.
const MissingTargetMessage = "comment must mention a recipient — a person, another agent, or a squad. A bare issue link (e.g. MUL-123) and mentioning yourself do not count; address someone else or schedule a wakeup (FIR-2674)."

// OwnerMentionOnSubIssueMessage is returned when an agent on a sub-issue tries
// to @mention the user the task was started for directly (TECH-3099).
const OwnerMentionOnSubIssueMessage = "Comments on sub-issues must not mention the user this task was started for directly — post on the parent issue instead."

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
const FlagSubIssueNoOwnerMention = "cerebro_sub_issue_no_owner_mention"
const FlagSubIssueRequireAgentTag = "cerebro_sub_issue_require_agent_tag"
const FlagSubIssueNoSplitSession = "cerebro_sub_issue_no_split_session"

// FlagCommentTargetGuardWakeupExempt (TECH-3761) is a sub-toggle of the base
// guard. When ON, an agent that has already scheduled a still-active wakeup on
// this issue is exempt from the base recipient requirement: the wakeup is the
// follow-up action, so the comment does not also have to tag a human. Default
// OFF — the base guard behaves exactly as before until an admin turns it on.
// The sub-issue checks (TECH-3099) are unaffected by this exemption.
const FlagCommentTargetGuardWakeupExempt = "cerebro_comment_target_guard_wakeup_exempt"

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
// mention links — but issue mentions and the agent's own mention do not satisfy
// the rule (TECH-3279/FIR-1501); the expansion only keeps parsing consistent
// with the rest of the comment pipeline.
//
// Additional sub-issue checks (TECH-3099) are enabled via their own flags:
//   - isSubIssue: whether the target issue has a parent (parent_issue_id != nil)
//   - ownerUserIDs: workspace owner user IDs (mention://member/<user_id>)
//   - taskPostedOnParent: true when the same task session already posted on
//     the parent issue (pre-computed by the handler via cerebro_comment_task)
//   - agentHasActiveWakeup: true when this agent already has a pending/claimed
//     wakeup on this issue (pre-computed by the handler). With the
//     FlagCommentTargetGuardWakeupExempt flag ON this waives the base recipient
//     requirement only — the wakeup is the follow-up action (TECH-3761).
func (s *Service) RejectComment(
	ctx context.Context,
	workspaceID pgtype.UUID,
	authorType, authorID, content string,
	isSubIssue bool,
	ownerUserIDs []string,
	taskPostedOnParent bool,
	agentHasActiveWakeup bool,
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

	// Base check: comment must address at least one recipient. An issue link
	// does not count — it points at a case, not a person (TECH-3279).
	//
	// TECH-3761 exemption: if the agent has already scheduled a still-active
	// wakeup on this issue and the exemption flag is on, the recipient
	// requirement is waived — the wakeup is itself the follow-up action, so a
	// human tag is not also required. The sub-issue checks below still run.
	if !hasRecipient(content, authorID) {
		exempt := flags[FlagCommentTargetGuardWakeupExempt] && agentHasActiveWakeup
		if !exempt {
			return MissingTargetMessage, false
		}
	}

	// TECH-3099 checks — only run on sub-issues.
	if isSubIssue {
		// Check 1: must not mention the workspace owner.
		if flags[FlagSubIssueNoOwnerMention] && mentionsOwner(content, ownerUserIDs) {
			return OwnerMentionOnSubIssueMessage, false
		}
		// Check 2: must mention at least one agent.
		if flags[FlagSubIssueRequireAgentTag] && !mentionsAgent(content, authorID) {
			return MissingAgentTagOnSubIssueMessage, false
		}
		// Check 3: same task must not have posted on the parent issue already.
		if flags[FlagSubIssueNoSplitSession] && taskPostedOnParent {
			return SplitSessionMessage, false
		}
	}

	return "", true
}

// hasRecipient reports whether content addresses at least one recipient — a
// member, another agent, a squad, or @all. An issue mention
// (mention://issue/...) does NOT count: it points at a case, not a person, so it
// cannot satisfy the rule that every agent comment must be addressed to someone
// (TECH-3279). The authoring agent's own mention does not count either
// (FIR-1501); self-tagging does not notify or hand off to anyone.
func hasRecipient(content, authorAgentID string) bool {
	for _, m := range util.ParseMentions(content) {
		if m.Type == "issue" {
			continue
		}
		if isSelfAgentMention(m, authorAgentID) {
			continue
		}
		return true
	}
	return false
}

func isSelfAgentMention(m util.Mention, authorAgentID string) bool {
	return m.Type == "agent" && authorAgentID != "" && strings.EqualFold(m.ID, authorAgentID)
}

// mentionsAgent reports whether content contains at least one agent mention
// that is not the authoring agent itself.
func mentionsAgent(content, authorAgentID string) bool {
	for _, m := range util.ParseMentions(content) {
		if m.Type == "agent" && !isSelfAgentMention(m, authorAgentID) {
			return true
		}
	}
	return false
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
