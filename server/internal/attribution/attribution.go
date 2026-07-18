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
	// SourceTriggerOwner — an autopilot schedule/webhook trigger enqueued the run;
	// the accountable human is the member who CREATED that specific trigger (set up
	// the schedule / registered the webhook). Preferred over rule_owner: a run is
	// accountable to whoever armed the trigger that fired it, not to whoever last
	// published the rule (MUL-4302; Bohan's refinement). originator stays NULL — an
	// autonomous fire carries no human authorization — so this is the same authz-safe
	// audit-only divergence as rule_owner.
	SourceTriggerOwner Source = "trigger_owner"
	// SourceRuleOwner — an autopilot trigger enqueued the run but its creator is not
	// recoverable (a trigger created before per-trigger creators were recorded); the
	// accountable human degrades to the publisher of the rule's active version
	// (MUL-4302 §3.4). Precise, but coarser than trigger_owner.
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
	case SourceDirectHuman, SourceDelegation, SourceCommentSource, SourceTriggerOwner, SourceRuleOwner:
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
	// EvidenceChat points the uniform evidence pair at the chat session that
	// triggered the run — the chat analogue of autopilot_run/issue_assignment.
	// The dedicated chat_session_id column still exists for its own consumers;
	// this makes the attribution UI's jump-to-evidence path uniform (MUL-4302 §2).
	EvidenceChat EvidenceKind = "chat"
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

// Result is the attribution stamped onto a queued run.
//
//   - UserID is the AUTHORIZATION human the caller writes into
//     originator_user_id. It is the value canInvokeAgent and the Composio overlay
//     read; it is legitimately invalid (NULL) when no human authorized the run.
//   - AccountableUserID is the AUDIT human written into accountable_user_id. The
//     one-way invariant (enforced by finalizeAttribution): when UserID is valid,
//     AccountableUserID equals it. When UserID is NULL (no human authorized the
//     run), the two may diverge — rule_owner names the rule publisher and
//     owner_fallback names the agent owner as the accountable human while
//     authorization correctly carries none.
//
// The remaining fields are audit metadata written into the Phase 1 provenance
// columns. Construct a Result through ClassifyComment / ClassifyDirect /
// DirectHumanRun / Unattributed / RuleOwner so AccountableUserID is always
// finalized; never stamp a hand-built literal onto the queue.
type Result struct {
	UserID              pgtype.UUID
	AccountableUserID   pgtype.UUID
	Source              Source
	DelegatedFromTaskID pgtype.UUID
	RuleVersionID       pgtype.UUID
	RetryOfTaskID       pgtype.UUID
	RerunOfTaskID       pgtype.UUID
	EvidenceKind        EvidenceKind
	EvidenceRefID       pgtype.UUID
}

// finalizeAttribution enforces the one-way Phase 1 accountability invariant
// (MUL-4302 §11): a resolved originator IS the accountable human, so
// `originator_user_id IS NOT NULL ⟹ accountable_user_id = originator_user_id`.
// Every Result flows through here. It mirrors UserID onto AccountableUserID
// whenever UserID is valid; when UserID is NULL (no human authorized the run) it
// leaves AccountableUserID exactly as the caller set it — the single divergence
// point where rule_owner (rule publisher) and owner_fallback (agent owner) name an
// accountable human for audit while authorization correctly carries none. The
// enqueue call sites never special-case this.
func finalizeAttribution(r Result) Result {
	if r.UserID.Valid {
		r.AccountableUserID = r.UserID
	}
	return r
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

	// ParentAccountable is the source task's accountable_user_id (MUL-4302 §3.2).
	// It lets an autopilot-rooted chain — where the parent has NO authorizing human
	// (ParentOriginator NULL) but IS accountable to someone (trigger creator / rule
	// publisher) — copy that responsible human down the delegation, instead of
	// dropping the chain root to unattributed. Loaded by the caller alongside
	// ParentOriginator; invalid when the parent has no accountable human either.
	ParentAccountable pgtype.UUID
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
		return finalizeAttribution(Result{
			UserID:        f.AuthorID,
			Source:        SourceDirectHuman,
			EvidenceKind:  EvidenceComment,
			EvidenceRefID: f.CommentID,
		})
	case "agent":
		r := Result{EvidenceKind: EvidenceComment, EvidenceRefID: f.CommentID}
		if !f.SourceTaskID.Valid {
			// Agent comment with no source task: cannot walk the chain.
			r.Source = SourceUnattributed
			return finalizeAttribution(r)
		}
		r.DelegatedFromTaskID = f.SourceTaskID
		if f.ParentOriginator.Valid {
			r.UserID = f.ParentOriginator
			r.Source = agentAuthoredSource
		} else if f.ParentAccountable.Valid {
			// The parent had no authorizing human (autopilot-rooted chain:
			// originator NULL, accountable = trigger creator / rule publisher) but
			// IS accountable to someone. Copy that accountable down so the
			// responsibility chain root stays stable at any depth (MUL-4302 §3.2);
			// originator stays NULL so authorization is unchanged and a fail-closed
			// workspace does not reject a fan-out that has a precise responsible human.
			r.AccountableUserID = f.ParentAccountable
			r.Source = agentAuthoredSource
		} else {
			// Source task exists but has no human at its own top of chain.
			r.Source = SourceUnattributed
		}
		return finalizeAttribution(r)
	default:
		return finalizeAttribution(Result{Source: SourceUnattributed, EvidenceKind: EvidenceComment, EvidenceRefID: f.CommentID})
	}
}

// DirectFacts are the facts for a run with no trigger comment: a direct issue
// assignment/creation, or an agent-created issue with a quick-create origin.
type DirectFacts struct {
	IssueID     pgtype.UUID
	CreatorType string
	CreatorID   pgtype.UUID

	// ActorUserID is the member who PERFORMED the action that enqueued this run
	// (assigned the issue, promoted the backlog child, created-with-assignee).
	// When valid it is the accountable human per MUL-4302 §4 ("执行 assign /
	// promote 的成员") and takes precedence over the issue creator: the person who
	// acted, not whoever happened to file the issue, is on the hook. Left invalid
	// by non-actor paths (comment chain, rerun, autopilot) which resolve the human
	// elsewhere and fall back to the creator. Because a direct action is the human
	// lending authority, the actor becomes BOTH originator (authorization) and
	// accountable — finalizeAttribution keeps them equal, honoring the invariant.
	ActorUserID pgtype.UUID

	// OriginType/OriginTaskID describe an agent-created issue's provenance
	// ("quick_create" or "agent_create"); OriginOriginator is that origin task's
	// originator_user_id, loaded by the caller. Empty OriginType means none.
	OriginType       string
	OriginTaskID     pgtype.UUID
	OriginOriginator pgtype.UUID

	// OriginAccountable is the origin task's accountable_user_id — the DirectFacts
	// analogue of CommentFacts.ParentAccountable (MUL-4302 §3.2). An agent-created
	// sub-issue whose origin task is autopilot-rooted (OriginOriginator NULL,
	// accountable set) inherits that accountable via delegation instead of dropping
	// to unattributed.
	OriginAccountable pgtype.UUID
}

// ClassifyDirect resolves attribution for a run with no trigger comment.
func ClassifyDirect(f DirectFacts) Result {
	// A member who directly assigned/promoted the issue is the accountable human,
	// ahead of the issue's creator (MUL-4302 §4). Evidence points at the issue the
	// action targeted.
	if f.ActorUserID.Valid {
		return finalizeAttribution(Result{
			UserID:        f.ActorUserID,
			Source:        SourceDirectHuman,
			EvidenceKind:  EvidenceIssueAssignment,
			EvidenceRefID: f.IssueID,
		})
	}
	if f.CreatorType == "member" && f.CreatorID.Valid {
		return finalizeAttribution(Result{
			UserID:        f.CreatorID,
			Source:        SourceDirectHuman,
			EvidenceKind:  EvidenceIssueAssignment,
			EvidenceRefID: f.IssueID,
		})
	}
	switch f.OriginType {
	case "quick_create", "agent_create":
		r := Result{
			DelegatedFromTaskID: f.OriginTaskID,
			EvidenceKind:        EvidenceIssueAssignment,
			EvidenceRefID:       f.IssueID,
		}
		if f.OriginOriginator.Valid {
			r.UserID = f.OriginOriginator
			r.Source = SourceDelegation
		} else if f.OriginAccountable.Valid {
			// Autopilot-rooted origin task: no authorizing human, but accountable
			// to the trigger creator / rule publisher. Copy accountable down so the
			// chain root stays stable; originator stays NULL (MUL-4302 §3.2).
			r.AccountableUserID = f.OriginAccountable
			r.Source = SourceDelegation
		} else {
			r.Source = SourceUnattributed
		}
		return finalizeAttribution(r)
	default:
		return finalizeAttribution(Result{Source: SourceUnattributed, EvidenceKind: EvidenceIssueAssignment, EvidenceRefID: f.IssueID})
	}
}

// DirectHumanRun builds attribution for a run a member triggered directly through
// a path that carries no issue and no trigger comment — a chat message or a
// quick-create request. userID is the member who acted (the chat sender / the
// quick-create requester) and becomes both originator and accountable. An invalid
// userID (e.g. a Lark group message whose sender could not be resolved) yields an
// explicit unattributed result rather than a NULL-source bypass.
func DirectHumanRun(userID pgtype.UUID, evidenceKind EvidenceKind, evidenceRefID pgtype.UUID) Result {
	if !userID.Valid {
		return finalizeAttribution(Result{Source: SourceUnattributed, EvidenceKind: evidenceKind, EvidenceRefID: evidenceRefID})
	}
	return finalizeAttribution(Result{
		UserID:        userID,
		Source:        SourceDirectHuman,
		EvidenceKind:  evidenceKind,
		EvidenceRefID: evidenceRefID,
	})
}

// Unattributed builds an explicit "no human resolved" result for an enqueue path
// that currently carries no accountable human — today only the autopilot run_only
// dispatch, whose precise rule_owner attribution (accountable = the active rule
// version's publisher) lands with the rule-version snapshot table in a later
// Phase 1 increment. Stamping SourceUnattributed with real evidence keeps the row
// off the NULL-source bypass and distinguishes "classified, no human" from a
// pre-migration NULL, while leaving originator/accountable NULL so authorization
// still correctly says "no human authorized this run".
func Unattributed(evidenceKind EvidenceKind, evidenceRefID pgtype.UUID) Result {
	return finalizeAttribution(Result{Source: SourceUnattributed, EvidenceKind: evidenceKind, EvidenceRefID: evidenceRefID})
}

// RuleOwner builds attribution for an autopilot-triggered run (MUL-4302 §3.4).
// No human authorized it, so UserID (originator, the authorization value) stays
// NULL and canInvokeAgent / the Composio overlay keep seeing "no human"; the
// AUDIT-accountable human is publisherUserID — the member who published the active
// rule version — which is THE divergence the two-column split exists for
// (accountable set, originator NULL). ruleVersionID records which snapshot
// resolved it; evidence points the caller supplies (autopilot_run for run_only,
// the issue for create_issue). A missing publisher (system-published rule, or no
// version resolved yet) degrades to unattributed so we never fabricate a human.
func RuleOwner(publisherUserID, ruleVersionID pgtype.UUID, evidenceKind EvidenceKind, evidenceRefID pgtype.UUID) Result {
	r := Result{
		RuleVersionID: ruleVersionID,
		EvidenceKind:  evidenceKind,
		EvidenceRefID: evidenceRefID,
	}
	if publisherUserID.Valid {
		r.Source = SourceRuleOwner
		r.AccountableUserID = publisherUserID
	} else {
		r.Source = SourceUnattributed
	}
	return finalizeAttribution(r)
}

// TriggerOwner builds attribution for an autopilot schedule/webhook run keyed to
// the human who created the firing trigger (MUL-4302; Bohan's refinement). Like
// RuleOwner, no human authorized the run, so UserID (originator) stays NULL and
// only the audit-accountable side is set — to creatorUserID, the trigger's member
// creator. Evidence is caller-supplied (autopilot_run for run_only, the issue for
// create_issue). An invalid creator degrades to unattributed so callers that lost
// the creator fall back to rule_owner rather than fabricating a human.
func TriggerOwner(creatorUserID pgtype.UUID, evidenceKind EvidenceKind, evidenceRefID pgtype.UUID) Result {
	r := Result{
		EvidenceKind:  evidenceKind,
		EvidenceRefID: evidenceRefID,
	}
	if creatorUserID.Valid {
		r.Source = SourceTriggerOwner
		r.AccountableUserID = creatorUserID
	} else {
		r.Source = SourceUnattributed
	}
	return finalizeAttribution(r)
}

// OwnerFallback degrades an UNATTRIBUTED result to owner_fallback (MUL-4302 §3.5):
// the agent owner becomes the accountable human so no run is left without one, but
// this is a DEGRADED label (Source.Precise() == false) and must be surfaced
// distinctly in reporting. It is audit-only — originator (UserID) stays NULL, so
// authorization is unaffected. Applied at the enqueue boundary ONLY when the
// resolved source is unattributed and the workspace has not opted into fail-closed;
// a precise result, or an invalid ownerUserID (nothing to fall back to), passes
// through unchanged so a human is never fabricated.
func OwnerFallback(r Result, ownerUserID pgtype.UUID) Result {
	if r.Source != SourceUnattributed || !ownerUserID.Valid {
		return r
	}
	r.Source = SourceOwnerFallback
	r.AccountableUserID = ownerUserID
	return r
}
