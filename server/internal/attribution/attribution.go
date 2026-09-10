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
// Hard invariant (MUL-4302 §1.3): the accountable human is "on behalf of", never
// blame. The SOURCE label is provenance only — no permission decision reads it.
//
// The originator (Result.UserID) IS the authorization value, and since MUL-6951
// this package decides it for autopilot runs: TriggerOwner names the human an
// armed autopilot acts as, which canInvokeAgent then honors. That grant is exactly
// that human's own rights — the gate is unchanged, only the principal handed to it
// — but it does mean a change to TriggerOwner is an AUTHORIZATION change, not a
// labelling one. Every other constructor here only mirrors a human the caller
// computed.
//
// It does NOT decide Composio access: since MUL-3963 the connected-apps overlay is
// built from the AGENT OWNER's connections and allow-list and ignores the
// originator entirely (integrations/composio/dispatch.go). Naming the originator
// here therefore hands out no third-party credentials.
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
	// the human is the member who CREATED that specific trigger (set up the schedule
	// / registered the webhook). Preferred over rule_owner: a run belongs to whoever
	// armed the trigger that fired it, not to whoever last published the rule
	// (MUL-4302; Bohan's refinement). Since MUL-6951 that human is the ORIGINATOR as
	// well as the accountable — arming a trigger authorizes the run the same way
	// clicking "run now" does — so this label marks HOW the human was resolved, not a
	// weaker grant. The label is what still distinguishes an autonomous fire from a
	// manual one in the audit trail.
	SourceTriggerOwner Source = "trigger_owner"
	// SourceRuleOwner — an autopilot trigger enqueued the run but records no
	// provable creator (a trigger predating per-trigger creators); the ACCOUNTABLE
	// human degrades to the publisher of the rule's active version (MUL-4302 §3.4),
	// while authorization carries none. Precise for audit, deliberately powerless.
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
	// EvidenceDelegatedFailure points at the terminal worker task that handed
	// control back to its source coordinator.
	EvidenceDelegatedFailure EvidenceKind = "delegated_failure"
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
//     originator_user_id. It is the value canInvokeAgent reads; it is legitimately
//     invalid (NULL) when no human authorized the run. It does NOT select Composio
//     connections — those follow the agent owner (MUL-3963).
//   - AccountableUserID is the AUDIT human written into accountable_user_id. The
//     one-way invariant (enforced by finalizeAttribution): when UserID is valid,
//     AccountableUserID equals it. When UserID is NULL (no human authorized the
//     run), the two may diverge — owner_fallback names the agent owner, and
//     rule_owner the rule publisher, as the accountable human while authorization
//     correctly carries none. trigger_owner is NOT such a case since MUL-6951: an
//     armed autopilot carries its trigger creator's authorization, so both columns
//     hold that human.
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

// RuleOwner builds attribution for an autopilot-triggered run whose firing
// trigger records no provable creator (MUL-4302 §3.4) — the coarser fallback
// behind TriggerOwner. publisherUserID, the member who published the active rule
// version, becomes the AUDIT-accountable human only; UserID (the originator, the
// authorization value) stays NULL.
//
// This asymmetry with TriggerOwner is deliberate (MUL-6951, Elon review). The rule
// publisher never armed anything — it is a guess at "who probably owns this rule",
// recovered from a version snapshot. Promoting it to the authorization principal
// would hand a legacy trigger somebody's invoke rights without that person having
// authorized a single run. A run that lands here therefore carries no originator
// and the invoke gate fails closed, which is the intended outcome for a trigger
// too old to name its creator.
//
// ruleVersionID records which snapshot resolved it; evidence points the caller
// supplies (autopilot_run for run_only, the issue for create_issue). A missing
// publisher (system-published rule, or no version resolved yet) degrades to
// unattributed so we never fabricate a human.
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
// the firing trigger's CREATOR (MUL-4302; MUL-6951). It sets UserID — the ORIGINATOR — so the run carries
// that human's authorization context, exactly as a manual "run now" by the same
// person would (MUL-6951; Bohan's call). finalizeAttribution mirrors it onto the
// accountable side, satisfying the originator == accountable invariant migration
// 190/197 enforces. Evidence is caller-supplied (autopilot_run for run_only, the
// issue for create_issue). An invalid creator degrades to unattributed so callers
// that lost the creator fall back to rule_owner rather than fabricating a human.
//
// WHY the originator and not accountable-only (MUL-6951): arming a trigger IS the
// authorization — the same act as clicking "run now", just deferred. Withholding
// the originator did not remove that authority, it only made every capability the
// automation legitimately needed arrive as a separately-bounded borrow path
// (MUL-4857, then MUL-6691, then #7902), each with a hand-written scope no
// operator could see. Carrying the human here grants exactly that human's own
// rights — canInvokeAgent still runs unchanged, so this is not an escalation —
// and lets those borrow paths be deleted.
//
// The caller (service.ResolveAutopilotTriggerPrincipal) resolves that human from
// the trigger's IMMUTABLE created_by, never published_by: published_by transfers
// on a substantive edit, so using it would let a collaborator editing a cron
// expression silently move whose rights the automation runs with. The same
// resolver feeds dispatch admission, so one run can never be admitted as one human
// and executed as another.
func TriggerOwner(creatorUserID pgtype.UUID, evidenceKind EvidenceKind, evidenceRefID pgtype.UUID) Result {
	r := Result{
		EvidenceKind:  evidenceKind,
		EvidenceRefID: evidenceRefID,
	}
	if creatorUserID.Valid {
		r.Source = SourceTriggerOwner
		r.UserID = creatorUserID
	} else {
		r.Source = SourceUnattributed
	}
	return finalizeAttribution(r)
}

// SubscriptionFacts are the already-fetched facts about an agent-created issue,
// used to decide who inherits VISIBILITY of it (MUL-5483). Same shape of
// contract as the Classify* inputs: the caller does the DB reads, the rule
// stays pure.
type SubscriptionFacts struct {
	// CreatorType is the issue's own creator_type. Only agent-created issues
	// can carry a delegated subscription; a member-created issue already
	// subscribes its human through the ordinary 'creator' rule.
	CreatorType string

	// OriginType / OriginOriginator mirror DirectFacts: the issue's provenance
	// stamp and the origin task's originator_user_id, loaded by the caller.
	OriginType       string
	OriginOriginator pgtype.UUID

	// OriginRootSource is the originator_source of the CHAIN ROOT behind the
	// origin task — the run that actually resolved OriginOriginator, not the hop
	// that copied it. The caller walks delegated_from_task_id to find it
	// (GetDelegatedSubscriptionFacts); an unreachable or unlabelled root leaves
	// it empty, which the rule reads as "not a direct human act".
	//
	// It exists because OriginOriginator alone stopped answering "did a human ask
	// for this?" in MUL-6951: an armed autopilot trigger now carries its
	// creator's authorization, so the two cases became indistinguishable by
	// value. See DelegatedSubscriber.
	OriginRootSource Source
}

// DelegatedSubscriber resolves the human who should be auto-subscribed to an
// agent-created issue, and the reason label to record.
//
// It deliberately mirrors ClassifyDirect's origin branch rather than inventing
// a second notion of "whose behalf is this" — the whole defect this fixes was
// attribution and notification disagreeing about that (MUL-5483). The waterfall
// is narrower than ClassifyDirect's on purpose:
//
//   - OriginOriginator valid AND the chain root is a DIRECT human act →
//     subscribe. Because attribution COPIES the accountable human across every
//     agent hop rather than chaining it (see SourceDelegation), the originator
//     resolves the ORIGINAL human at any depth, and the delivery tier — not a
//     depth cutoff — is what bounds the noise a deep chain makes. The root check
//     is what keeps "has a human" from being read as "a human asked", see below.
//
//     WHO the human is therefore has no depth limit; proving the chain BEGAN
//     with them does: the caller reads the root by walking the lineage, and that
//     walk stops after 32 hops (GetDelegatedSubscriptionFacts). Past that the
//     root is reported as unproven and nobody is subscribed, which is a real
//     truncation and not a formality — accepted deliberately (MUL-7051, Bohan's
//     call) because no observed chain approaches it. Raising it is a one-number
//     change; moving the root onto the run at enqueue time would remove the
//     limit outright.
//
//   - quick_create → reason 'creator', agent_create → reason 'delegated'. The
//     quick-create human asked for THAT issue by name, so it is direct intent and
//     keeps full notifications; an agent_create child is the agent's own decision
//     made under a broader mandate, so it takes the reduced delegated tier.
//
//   - Anything else → no subscription. Notably origin_type='autopilot' is
//     excluded: an autopilot already has an explicitly configured
//     autopilot_subscriber template, and that list — not the member who happened
//     to arm the trigger — is the intended audience for its issues. Degraded
//     attribution (owner_fallback / unattributed) is excluded for the same
//     reason it is degraded: we do not fabricate a human to notify.
//
// WHY the root source is checked at all (MUL-7051). This rule was written when
// SourceDirectHuman was the only root that left an originator behind, so "the
// origin run carries a human" and "a human asked for this work" were the same
// statement and only the first had to be tested. MUL-6951 separated them: an
// armed schedule/webhook trigger now runs with its creator's authorization
// (SourceTriggerOwner), and that human is copied down the whole chain exactly
// like a requester would be. The untested half of the old equivalence is what
// broke — every issue an autopilot's agent filed started subscribing whoever
// armed the trigger, which is the case the origin_type='autopilot' exclusion
// above already says must not happen, arriving one hop lower.
//
// So the condition is stated as what it means — the chain BEGAN with a member
// acting — and it is a whitelist. A future root that resolves a human some other
// way stays silent here until someone decides it should subscribe people, rather
// than turning this rule on for a population nobody chose.
func DelegatedSubscriber(f SubscriptionFacts) (pgtype.UUID, string, bool) {
	if f.CreatorType != "agent" || !f.OriginOriginator.Valid {
		return pgtype.UUID{}, "", false
	}
	if f.OriginRootSource != SourceDirectHuman {
		return pgtype.UUID{}, "", false
	}
	switch f.OriginType {
	case "quick_create":
		return f.OriginOriginator, "creator", true
	case "agent_create":
		return f.OriginOriginator, "delegated", true
	default:
		return pgtype.UUID{}, "", false
	}
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
