// Package attribution implements the accountable-human resolution contract for
// agent task runs (MUL-4302, "Human Attribution"). Every run enqueued into
// agent_task_queue must be traceable to exactly one accountable human, and the
// attribution must be EXPLAINABLE: it records not just who, but at which
// waterfall level the human was resolved (a direct member action, a delegation
// copy across an agent hop, the comment-source chain, an autopilot rule owner,
// or a degraded owner fallback).
//
// This package owns the vocabulary (Source, EvidenceKind, TriggerKind) and the
// PURE classification rules. The database reads that gather the facts stay in
// the caller (service.TaskService); the caller passes already-fetched facts
// into the Classify* functions so the rules remain side-effect-free and fully
// unit-testable without a database.
//
// Hard invariant (MUL-4302 §1.3): attribution is "on behalf of", never blame
// and never authorization. Nothing in this package is consulted for permission
// decisions — it labels provenance for visibility, audit, and cost only. In
// particular the accountable-human value stamped here mirrors the existing
// originator_user_id, which is computed by the caller; this package never
// widens or narrows who that human is.
package attribution

import "github.com/jackc/pgx/v5/pgtype"

// Source is the waterfall level that resolved the accountable human for a run.
// Stored verbatim in agent_task_queue.originator_source. Kept as free strings
// (no DB CHECK) so a newly-modeled trigger path can introduce a source without
// a schema migration (MUL-4302 §7).
type Source string

const (
	// SourceDirectHuman — a member's own action enqueued the run (comment,
	// mention, assign, promote, manual trigger, rerun). The member IS the
	// accountable human.
	SourceDirectHuman Source = "direct_human"
	// SourceDelegation — an agent running on behalf of a human caused the
	// enqueue (agent @-mentions another agent, agent creates a sub-issue,
	// stage-completion wakeup). The parent task's accountable human is COPIED,
	// not chained, so delegation cycles stay harmless (MUL-4302 §3.2).
	SourceDelegation Source = "delegation"
	// SourceCommentSource — the issue's standing assignee reacted to an
	// agent/system-authored comment; the human is resolved through
	// comment.source_task_id (a special case of delegation, MUL-4302 §3.3).
	SourceCommentSource Source = "comment_source"
	// SourceRuleOwner — an autopilot trigger enqueued the run; the accountable
	// human is the publisher of the rule's active version (MUL-4302 §3.4).
	SourceRuleOwner Source = "rule_owner"
	// SourceOwnerFallback — nothing above resolved a human, so attribution
	// degrades to the agent owner. This is DEGRADED, not compliance-grade, and
	// must be surfaced distinctly (MUL-4302 §3.5).
	SourceOwnerFallback Source = "owner_fallback"
	// SourceBackfill — a historical row attributed after the fact by the
	// backfill command; never impersonates a real-time attribution.
	SourceBackfill Source = "backfill"
	// SourceUnattributed — no human could be resolved and no fallback was
	// applied. Distinct from a NULL (pre-migration) source: it is an explicit
	// "we looked and found no human in the chain" marker.
	SourceUnattributed Source = "unattributed"
)

// Precise reports whether src is a compliance-grade (non-degraded) attribution.
// owner_fallback, backfill, and unattributed are degraded and count against the
// attribution-coverage health metric (MUL-4302 §9).
func (src Source) Precise() bool {
	switch src {
	case SourceDirectHuman, SourceDelegation, SourceCommentSource, SourceRuleOwner:
		return true
	default:
		return false
	}
}

// String returns the raw source label (defaults to unattributed when empty) so
// callers never stamp a zero value.
func (src Source) String() string {
	if src == "" {
		return string(SourceUnattributed)
	}
	return string(src)
}

// EvidenceKind tags the direct cause of a run so every attribution can jump to
// its evidence row. Free strings, paired with an evidence ref id.
type EvidenceKind string

const (
	EvidenceComment         EvidenceKind = "comment"
	EvidenceIssueAssignment EvidenceKind = "issue_assignment"
	EvidenceAutopilotRun    EvidenceKind = "autopilot_run"
	EvidenceRuleVersion     EvidenceKind = "rule_version"
	EvidenceRerun           EvidenceKind = "rerun"
)

// TriggerKind enumerates every path that can enqueue a run. Kept as an explicit
// taxonomy so that adding a new trigger path is a visible, deliberate change
// that has to declare its attribution rule (MUL-4302 §2 architecture
// invariant: no enqueue path may exist without a declared attribution).
type TriggerKind string

const (
	KindMemberComment     TriggerKind = "member_comment"
	KindMemberMention     TriggerKind = "member_mention"
	KindMemberAssign      TriggerKind = "member_assign"
	KindAgentMention      TriggerKind = "agent_mention"
	KindAgentComment      TriggerKind = "agent_comment"
	KindSubIssueCreate    TriggerKind = "sub_issue_create"
	KindStageWakeup       TriggerKind = "stage_wakeup"
	KindQuickCreate       TriggerKind = "quick_create"
	KindChat              TriggerKind = "chat"
	KindAutopilotSchedule TriggerKind = "autopilot_schedule"
	KindAutopilotWebhook  TriggerKind = "autopilot_webhook"
	KindAutopilotManual   TriggerKind = "autopilot_manual"
	KindRetry             TriggerKind = "retry"
	KindRerun             TriggerKind = "rerun"
	KindDeferredFallback  TriggerKind = "deferred_fallback"
)

// Result is the attribution stamped onto a queued run. UserID mirrors the
// accountable human the caller also writes into originator_user_id; the other
// fields are audit metadata written into the Phase 1 provenance columns.
type Result struct {
	UserID              pgtype.UUID
	Source              Source
	DelegatedFromTaskID pgtype.UUID
	RuleVersionID       pgtype.UUID
	RetryOfTaskID       pgtype.UUID
	RerunOfTaskID       pgtype.UUID
	EvidenceKind        EvidenceKind
	EvidenceRefID       pgtype.UUID
}

// CommentFacts are the already-fetched facts about a trigger comment, gathered
// by the caller from the DB and passed in so classification stays pure.
type CommentFacts struct {
	CommentID  pgtype.UUID
	AuthorType string // "member" | "agent" | other
	AuthorID   pgtype.UUID

	// For agent-authored comments: the source task the comment was written
	// from (comment.source_task_id) and that task's originator_user_id. The
	// caller resolves ParentOriginator by loading the source task; it is left
	// invalid when the source task is missing or itself unattributed.
	SourceTaskID     pgtype.UUID
	ParentOriginator pgtype.UUID
}

// ClassifyComment resolves attribution for a comment-triggered run from
// already-fetched comment facts. agentAuthoredSource selects the label used
// when the trigger comment is agent-authored: SourceCommentSource for the
// issue-assignee-reacting path, SourceDelegation for an explicit mention /
// thread-parent / squad-leader path. The returned UserID is byte-identical to
// the legacy originator resolution so authorization behavior is unchanged.
func ClassifyComment(f CommentFacts, agentAuthoredSource Source) Result {
	switch f.AuthorType {
	case "member":
		return Result{
			UserID:        f.AuthorID,
			Source:        SourceDirectHuman,
			EvidenceKind:  EvidenceComment,
			EvidenceRefID: f.CommentID,
		}
	case "agent":
		r := Result{EvidenceKind: EvidenceComment, EvidenceRefID: f.CommentID}
		if !f.SourceTaskID.Valid {
			// Agent comment with no source task: cannot walk the chain.
			r.Source = SourceUnattributed
			return r
		}
		r.DelegatedFromTaskID = f.SourceTaskID
		if f.ParentOriginator.Valid {
			r.UserID = f.ParentOriginator
			r.Source = agentAuthoredSource
		} else {
			// Source task exists but has no human at its own top of chain.
			r.Source = SourceUnattributed
		}
		return r
	default:
		return Result{Source: SourceUnattributed, EvidenceKind: EvidenceComment, EvidenceRefID: f.CommentID}
	}
}

// DirectFacts are the facts for a run with no trigger comment: a direct issue
// assignment/creation, or an agent-created issue with a quick-create origin.
type DirectFacts struct {
	IssueID     pgtype.UUID
	CreatorType string
	CreatorID   pgtype.UUID

	// OriginType/OriginTaskID describe an agent-created issue's provenance
	// (e.g. "quick_create"); OriginOriginator is that origin task's
	// originator_user_id, loaded by the caller. Empty OriginType means none.
	OriginType       string
	OriginTaskID     pgtype.UUID
	OriginOriginator pgtype.UUID
}

// ClassifyDirect resolves attribution for a run with no trigger comment.
func ClassifyDirect(f DirectFacts) Result {
	if f.CreatorType == "member" && f.CreatorID.Valid {
		return Result{
			UserID:        f.CreatorID,
			Source:        SourceDirectHuman,
			EvidenceKind:  EvidenceIssueAssignment,
			EvidenceRefID: f.IssueID,
		}
	}
	switch f.OriginType {
	case "quick_create":
		r := Result{
			DelegatedFromTaskID: f.OriginTaskID,
			EvidenceKind:        EvidenceIssueAssignment,
			EvidenceRefID:       f.IssueID,
		}
		if f.OriginOriginator.Valid {
			r.UserID = f.OriginOriginator
			r.Source = SourceDelegation
		} else {
			r.Source = SourceUnattributed
		}
		return r
	default:
		return Result{Source: SourceUnattributed, EvidenceKind: EvidenceIssueAssignment, EvidenceRefID: f.IssueID}
	}
}
