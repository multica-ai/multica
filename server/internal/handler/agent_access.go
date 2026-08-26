package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Agent invocation permission model (MUL-3963).
//
// Two distinct questions, previously conflated in canAccessPrivateAgent:
//
//   - "can this actor SEE / open this agent in the UI"  -> canAccessPrivateAgent
//   - "can this actor TRIGGER a run for this agent"      -> canInvokeAgent
//
// The invoke gate is the security-critical one: a workspace admin must NOT be
// able to invoke someone's private agent (and thereby use that owner's
// Composio/OAuth connections) just because they are an admin. Admin retains
// management + inventory visibility, not the ability to run.
//
// permission_mode drives invoke:
//   - private   -> only the agent owner may invoke; NO admin bypass, NO A2A bypass.
//   - public_to -> the agent_invocation_target allow-list decides:
//       * workspace target -> any workspace member (and workspace-internal
//         agent/system principals) may invoke.
//       * member target    -> only the specific user may invoke.
//       * team target       -> reserved, inert in V1.
//
// A2A is judged by the top-of-chain human originator, never by the immediate
// agent actor: if user U triggers agent A and A @-mentions agent B, B is only
// invocable when U (the originator) is in B's allow-list. This prevents agents
// from forming a channel that bypasses the owner's white-list.

// canInvokeAgent reports whether a run may be enqueued for `agent` on behalf of
// the given actor. Judgement is by the *effective invoking user*:
//   - member actor -> the member themselves (actorID)
//   - agent actor  -> the top-of-chain human originator (originatorUserID)
//   - system actor -> the originator when one was resolved, else no user
//
// originatorUserID is the empty string when no human could be attributed. For
// private agents that means "deny" (unless the actor is the owner). For
// public_to agents, a workspace target still admits workspace-internal
// agent/system principals, but member/team targets fail closed without a
// matching human.
func (h *Handler) canInvokeAgent(ctx context.Context, agent db.Agent, actorType, actorID, originatorUserID, workspaceID string) bool {
	allowed := h.invokeAgentDecision(ctx, agent, actorType, actorID, originatorUserID, workspaceID)
	if !allowed && actorType != "member" && originatorUserID == "" {
		// MUL-6490: the wire reason stays the deliberately generic
		// invocation_not_allowed (dispatch/reason.go — it must not reveal whether a
		// target exists), which leaves the one denial a workspace CAN fix looking
		// identical to a plain permission error. Name the root cause here, where
		// only operators read it: the chain reached this gate with no human at its
		// top, so member-scoped allow-lists could not match anyone. Without this
		// line the reported failure mode (a broken originator chain N hops back)
		// was only diagnosable by querying agent_task_queue directly.
		slog.WarnContext(ctx, "agent invoke denied: delegation chain carries no human originator",
			"agent_id", uuidToString(agent.ID),
			"agent_permission_mode", agent.PermissionMode,
			"actor_type", actorType,
			"actor_id", actorID,
			"workspace_id", workspaceID,
		)
	}
	return allowed
}

// invokeAgentDecision is canInvokeAgent's pure verdict, split out so the gate has
// exactly one place to observe a denial from.
func (h *Handler) invokeAgentDecision(ctx context.Context, agent db.Agent, actorType, actorID, originatorUserID, workspaceID string) bool {
	effectiveUser := actorID
	if actorType != "member" {
		// agent / system: never trust the immediate principal, only the
		// resolved human originator at the top of the chain.
		effectiveUser = originatorUserID
	}

	// The agent owner may always invoke their own agent.
	if effectiveUser != "" && uuidToString(agent.OwnerID) == effectiveUser {
		return true
	}

	if agent.PermissionMode != "public_to" {
		// private (or any unknown mode) is deny-by-default: no admin bypass,
		// no A2A bypass. Only the owner branch above passes.
		return false
	}

	targets, err := h.Queries.ListAgentInvocationTargets(ctx, agent.ID)
	if err != nil {
		return false
	}

	// Agents and system triggers are workspace-internal principals: a
	// workspace target admits them even when no human originator resolved.
	// This is a DELIBERATE, product-approved exception (MUL-3963): webhook /
	// system / workspace-wide automation must be able to trigger a
	// `public_to workspace` agent even though there is no human at the top of
	// the chain. It is scoped tightly — it ONLY relaxes the *workspace* target.
	// member/team targets still require a resolved human originator to match,
	// so an unattributed agent/system trigger FAILS CLOSED against a
	// member-/team-scoped private-ish allow-list and can never smuggle itself
	// onto someone's specific-people grant.
	workspaceBroad := actorType == "agent" || actorType == "system"
	isWorkspaceMember := false
	if effectiveUser != "" {
		if _, err := h.getWorkspaceMember(ctx, effectiveUser, workspaceID); err == nil {
			isWorkspaceMember = true
		}
	}

	for _, t := range targets {
		switch t.TargetType {
		case "workspace":
			if isWorkspaceMember || workspaceBroad {
				return true
			}
		case "member":
			// Requires a resolved human. agent/system triggers with no
			// originator (effectiveUser == "") never match here — fail closed.
			if effectiveUser != "" && uuidToString(t.TargetID) == effectiveUser {
				return true
			}
		case "team":
			// Reserved: team membership does not exist yet in V1, so team
			// targets never admit anyone (also fail-closed for system/agent).
		}
	}
	return false
}

// canAccessPrivateAgent gates the VIEW surfaces (list/detail navigation, chat
// transcript read, task-cancel authorization). It is NOT the trigger gate —
// see canInvokeAgent for that.
//
// Rules:
//   - agent actors always pass (A2A collaboration + inspection preserved).
//   - the agent owner always passes.
//   - workspace owner/admin pass (governance / inventory visibility retained).
//   - a regular member passes for a public_to agent only when they hit a
//     workspace or member target; private agents stay owner+admin only.
func (h *Handler) canAccessPrivateAgent(ctx context.Context, agent db.Agent, actorType, actorID, workspaceID string) bool {
	if actorType == "agent" {
		return true
	}
	if uuidToString(agent.OwnerID) == actorID {
		return true
	}
	member, err := h.getWorkspaceMember(ctx, actorID, workspaceID)
	if err != nil {
		return false
	}
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	if agent.PermissionMode != "public_to" {
		return false
	}
	targets, err := h.Queries.ListAgentInvocationTargets(ctx, agent.ID)
	if err != nil {
		return false
	}
	return memberHitsInvocationTargets(targets, actorID)
}

// memberHitsInvocationTargets is the pure predicate deciding whether a regular
// member is on a public_to agent's allow-list, used by both the single-agent
// view gate and the ListAgents batch filter. A workspace target admits any
// member; a member target admits the matching user; team targets are inert.
func memberHitsInvocationTargets(targets []db.AgentInvocationTarget, userID string) bool {
	for _, t := range targets {
		switch t.TargetType {
		case "workspace":
			return true
		case "member":
			if uuidToString(t.TargetID) == userID {
				return true
			}
		}
	}
	return false
}

// memberAllowedToViewAgent is the ListAgents / aggregation filter predicate.
// Caller supplies the agent's already-batch-loaded invocation targets so the
// list endpoint avoids an N+1. Workspace owner/admin and the agent owner see
// everything; a regular member sees a public_to agent only when on its
// allow-list, and never sees other members' private agents.
func memberAllowedToViewAgent(agent db.Agent, targets []db.AgentInvocationTarget, userID, role string) bool {
	if roleAllowed(role, "owner", "admin") {
		return true
	}
	if uuidToString(agent.OwnerID) == userID {
		return true
	}
	if agent.PermissionMode != "public_to" {
		return false
	}
	return memberHitsInvocationTargets(targets, userID)
}

// invokeOriginatorFromRequest resolves the top-of-chain human user id for an
// invocation initiated over HTTP. Members are their own originator; agent
// actors inherit the originator from the task named by the X-Task-ID header
// (set by the CLI on every request), matching
// TaskService.resolveOriginatorFromTriggerComment. Returns "" when no human
// can be attributed — canInvokeAgent then fails closed for member/team targets.
func (h *Handler) invokeOriginatorFromRequest(r *http.Request, actorType, actorID string) string {
	if actorType == "member" {
		return actorID
	}
	if actorType == "agent" {
		if taskIDHeader := r.Header.Get("X-Task-ID"); taskIDHeader != "" {
			if taskUUID, err := util.ParseUUID(taskIDHeader); err == nil {
				if task, err := h.Queries.GetAgentTask(r.Context(), taskUUID); err == nil {
					return uuidToString(task.OriginatorUserID)
				}
			}
		}
	}
	return ""
}

// assignAuthorityScope tells the invoke gate WHAT an assignment is being made
// against, so an unattributed autopilot run can only borrow authority for work
// it is verifiably doing itself (MUL-6691).
//
// Exactly which shape the caller vouched for decides what may be borrowed, so
// the kinds are distinct rather than a single "some issue" pointer. The zero
// value grants nothing — a caller that supplies no scope keeps the pre-MUL-4857
// behavior of "real originator or deny", which is what the member-only entry
// points want.
type assignAuthorityScope struct {
	// Issue is the already-loaded issue the assignment binds to: the PARENT for
	// scopeKindChildOf, the issue ITSELF for scopeKindExistingIssue. Its
	// workspace must match the request's, and the speaking run must be
	// verifiably attached to it (issueBoundToAutopilotTask) before any authority
	// is borrowed.
	Issue *db.Issue

	Kind assignScopeKind
}

// assignScopeKind distinguishes the assignment shapes, because they do NOT admit
// the same fallbacks: only child creation — the surface MUL-4857 already shipped
// — may fall back to the coarse autopilot-creator authority.
type assignScopeKind int

const (
	// scopeKindNone: no autopilot authority may be borrowed at all.
	scopeKindNone assignScopeKind = iota
	// scopeKindChildOf: creating a child under Issue.
	scopeKindChildOf
	// scopeKindExistingIssue: assigning Issue itself.
	scopeKindExistingIssue
	// scopeKindNewTopLevelIssue: creating a parentless issue, which does not
	// exist yet, so the binding is the speaking task's own autopilot run.
	scopeKindNewTopLevelIssue
)

// scopeChildOf binds the assignment to the parent a child is being created
// under. A nil parent yields the empty scope, which grants nothing.
func scopeChildOf(parent *db.Issue) assignAuthorityScope {
	if parent == nil {
		return assignAuthorityScope{}
	}
	return assignAuthorityScope{Issue: parent, Kind: scopeKindChildOf}
}

// scopeExistingIssue binds the assignment to the issue being updated.
func scopeExistingIssue(issue *db.Issue) assignAuthorityScope {
	if issue == nil {
		return assignAuthorityScope{}
	}
	return assignAuthorityScope{Issue: issue, Kind: scopeKindExistingIssue}
}

// scopeNewTopLevelIssue marks a create with no parent, where the issue being
// assigned does not exist yet.
func scopeNewTopLevelIssue() assignAuthorityScope {
	return assignAuthorityScope{Kind: scopeKindNewTopLevelIssue}
}

// scopeNoDelegation disables the autopilot fallback entirely — for entry points
// that only ever run as a member and must not gain an agent-borrowed path.
func scopeNoDelegation() assignAuthorityScope {
	return assignAuthorityScope{}
}

// effectiveInvocationAuthorityFromRequest returns the human principal used by
// canInvokeAgent without changing attribution. A real top-of-chain originator
// always wins. Only an unattributed AGENT request may fall back to an autopilot
// authority, and only within the scope the caller vouched for.
func (h *Handler) effectiveInvocationAuthorityFromRequest(r *http.Request, scope assignAuthorityScope, actorType, actorID, workspaceID string) string {
	originatorUserID := h.invokeOriginatorFromRequest(r, actorType, actorID)
	if originatorUserID != "" {
		return originatorUserID
	}
	return h.autopilotAssignAuthorityFromRequest(r, scope, actorType, actorID, workspaceID)
}

// autopilotAssignAuthorityFromRequest resolves the borrowed authority for an
// assignment made by an unattributed agent run, in precedence order:
//
//  1. the speaking task's OWN accountable human (autopilotTaskAssignAuthority) —
//     precise, already recorded on the task row, and available for both
//     autopilot execution modes;
//  2. the autopilot's member creator (autopilotDelegationAuthority, MUL-4857) —
//     the coarser pre-existing rule, kept ONLY where it already shipped: child
//     creation under an `origin_type=autopilot` issue, so a leader task that
//     predates attribution stamping does not lose the path it has today.
//
// (2) is deliberately NOT available to the newly-wired surfaces. It performs no
// liveness or attribution check of its own, so extending it to the assign verb
// would have let a completed task, an `owner_fallback` task, or a task with no
// attribution at all assign private agents whenever the autopilot's creator
// happened to have rights — a strictly wider grant than this fix needs. The
// request path additionally requires the speaking task to still be `running`;
// the comment-replay resolvers keep their own semantics and are untouched.
func (h *Handler) autopilotAssignAuthorityFromRequest(r *http.Request, scope assignAuthorityScope, actorType, actorID, workspaceID string) string {
	if actorType != "agent" || scope.Kind == scopeKindNone {
		return ""
	}
	task, ok := h.taskFromRequestHeader(r)
	if !ok {
		return ""
	}
	// A finished run never lends authority over HTTP, on either branch below.
	if task.Status != "running" {
		return ""
	}
	if user := h.autopilotTaskAssignAuthority(r.Context(), scope, actorType, actorID, workspaceID, task); user != "" {
		return user
	}
	if scope.Kind != scopeKindChildOf || scope.Issue == nil {
		return ""
	}
	return h.autopilotDelegationAuthority(r.Context(), *scope.Issue, actorType, actorID, task)
}

// autopilotTaskAssignAuthority resolves the effective invoking human for an
// assignment performed by an unattributed autopilot run, keyed on the run's OWN
// accountable human rather than on the autopilot's creator (MUL-6691).
//
// WHY the accountable user and not autopilot.created_by_id: since MUL-4302 the
// accountable human for a schedule/webhook run is the trigger_owner — whoever
// last shaped the firing trigger — which need NOT be the autopilot's original
// creator. Keying authorization on the creator while the audit trail names the
// trigger owner would let the audit record one human and the private-agent /
// OAuth access come from another. The task row already carries the resolved
// value, so this reads it instead of re-deriving a different one.
//
// SECURITY. The gate is unchanged; only the human handed to it is. Every one of
// these must hold, and anything missing, mismatched, or unreadable returns ""
// and leaves canInvokeAgent fail-closed:
//
//   - the actor is an agent and IS the speaking task's agent;
//   - the task is still `running` — a finished run cannot keep lending rights;
//   - the task carries NO originator (a real one is preferred by the caller and
//     must never be silently replaced by this coarser value);
//   - originator_source is `trigger_owner` or `rule_owner`. This IS the
//     autopilot-lineage proof: only the autopilot enqueue paths stamp those two
//     labels. `owner_fallback` is deliberately excluded — that value is the
//     AGENT OWNER, and borrowing it would let any unattributed autopilot run
//     invoke its own owner's private agents, which is precisely the owner
//     white-list bypass canInvokeAgent exists to prevent. `delegation` is
//     excluded too, so authority does not propagate to descendant runs;
//   - the assignment is bound to work this run verifiably owns —
//     issueBoundToAutopilotTask for an existing issue (which for a run_only-created
//     issue re-proves the run_only lineage, so a create_issue task cannot escape
//     its same-issue bound via a fresh top-level issue), or the task's own
//     verified autopilot run when creating a parentless issue;
//   - the issue (when there is one) is in the request's workspace;
//   - the accountable human is STILL a member of that workspace.
//
// The returned id is used for AUTHORIZATION only: the new issue and any task it
// enqueues are attributed separately and stay unattributed, so the audit
// semantics of "no human directly started this" are preserved.
func (h *Handler) autopilotTaskAssignAuthority(ctx context.Context, scope assignAuthorityScope, actorType, actorID, workspaceID string, task db.AgentTaskQueue) string {
	if actorType != "agent" {
		return ""
	}
	if !task.AgentID.Valid || uuidToString(task.AgentID) != actorID {
		return ""
	}
	if task.Status != "running" {
		return ""
	}
	if task.OriginatorUserID.Valid || !task.AccountableUserID.Valid {
		return ""
	}
	if !task.OriginatorSource.Valid {
		return ""
	}
	switch attribution.Source(task.OriginatorSource.String) {
	case attribution.SourceTriggerOwner, attribution.SourceRuleOwner:
	default:
		return ""
	}

	switch scope.Kind {
	case scopeKindChildOf, scopeKindExistingIssue:
		if scope.Issue == nil {
			return ""
		}
		if uuidToString(scope.Issue.WorkspaceID) != workspaceID {
			return ""
		}
		if !h.issueBoundToAutopilotTask(ctx, *scope.Issue, task, workspaceID) {
			return ""
		}
	case scopeKindNewTopLevelIssue:
		if !h.taskRunsAutopilotInWorkspace(ctx, task, workspaceID) {
			return ""
		}
	default:
		return ""
	}

	accountable := uuidToString(task.AccountableUserID)
	if accountable == "" {
		return ""
	}
	if _, err := h.getWorkspaceMember(ctx, accountable, workspaceID); err != nil {
		return ""
	}
	return accountable
}

// issueBoundToAutopilotTask reports whether `task` verifiably owns the work on
// `issue`, which is what keeps a borrowed autopilot authority from becoming a
// workspace-wide capability (the confused-deputy bound MUL-4857 established).
// Two shapes, one per autopilot execution mode:
//
//   - create_issue: the autopilot created the issue (origin_type=autopilot) and
//     the speaking task is the run working ON it (task.issue_id == issue.id) —
//     the exact MUL-4857 binding.
//   - run_only: no autopilot issue exists, so the binding is authorship. The
//     issue was created by THIS task, which CreateIssue records as
//     origin_type=agent_create + origin_id=<creating task id> (MUL-4305), AND the
//     task must independently prove run_only lineage (its own autopilot run, in
//     this workspace). Without that second half a create_issue-mode task could
//     escape its `task.issue_id == issue.id` bound simply by creating a fresh
//     top-level issue — which stamps its own task id — and then assigning that.
//     Using the task id rather than the autopilot id also stops two concurrent
//     runs of the same autopilot from borrowing on each other's issues, and needs
//     no migration or backfill.
//
// A run_only leader therefore reaches only the issues it created (and, through
// the child scope, their children) — never a pre-existing or foreign issue. A
// create_issue leader reaches only the autopilot's own issue.
func (h *Handler) issueBoundToAutopilotTask(ctx context.Context, issue db.Issue, task db.AgentTaskQueue, workspaceID string) bool {
	if !issue.OriginType.Valid || !issue.OriginID.Valid || !issue.ID.Valid {
		return false
	}
	switch issue.OriginType.String {
	case "autopilot":
		return task.IssueID.Valid && uuidToString(task.IssueID) == uuidToString(issue.ID)
	case "agent_create":
		if !task.ID.Valid || uuidToString(issue.OriginID) != uuidToString(task.ID) {
			return false
		}
		return h.taskRunsAutopilotInWorkspace(ctx, task, workspaceID)
	}
	return false
}

// taskRunsAutopilotInWorkspace verifies run_only lineage: the task must belong to
// an autopilot RUN whose autopilot lives in the request's workspace. Used both
// when there is no issue to bind to yet and when binding to a run_only-created
// issue. A create_issue-mode leader task carries no autopilot_run_id (it is
// enqueued through the ordinary issue-assignment path), so it never qualifies
// here and must go through the `origin_type=autopilot` binding instead.
func (h *Handler) taskRunsAutopilotInWorkspace(ctx context.Context, task db.AgentTaskQueue, workspaceID string) bool {
	if !task.AutopilotRunID.Valid {
		return false
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return false
	}
	run, err := h.Queries.GetAutopilotRun(ctx, task.AutopilotRunID)
	if err != nil || !run.AutopilotID.Valid {
		return false
	}
	// Workspace-scoped read: an autopilot run from another tenant resolves to
	// no autopilot here, so a foreign run id cannot lend authority (MUL-4252).
	if _, err := h.Queries.GetAutopilotInWorkspace(ctx, db.GetAutopilotInWorkspaceParams{
		ID:          run.AutopilotID,
		WorkspaceID: wsUUID,
	}); err != nil {
		return false
	}
	return true
}

// autopilotDelegationAuthority resolves the effective invoking human for the A2A
// invoke gate (canInvokeAgent) when a trigger comment is authored by an
// UNATTRIBUTED autopilot dispatch delegating mid-chain on the very issue that
// autopilot created (MUL-4857).
//
// A schedule/webhook autopilot run carries no top-of-chain human originator by
// design (MUL-4302). Without one, canInvokeAgent fails closed for the DEFAULT
// private agent (and member-scoped public_to agents), so a mid-run @mention
// delegation silently enqueues nothing — even though the SAME autopilot's first
// dispatch was admitted via the autopilot creator (autopilotAdmitInvoke ->
// canCreatorInvokeAgent). This restores exactly that first-dispatch authority for
// the mid-run delegation path: the gate still runs, now keyed on the autopilot
// creator, so NO unrestricted agent-to-agent bypass is reopened.
//
// SECURITY (confused-deputy defense, review MUL-4857): the creator's authority is
// granted ONLY when the SPEAKING run is verified to be doing work on THIS very
// autopilot-created issue. Binding to issue provenance + an empty originator alone
// is NOT enough — an agent running a task on some OTHER issue can legitimately
// comment here, and since MUL-6490 that cross-issue lineage IS persisted on
// source_task_id (so the human originator chain survives the hop), so it could
// otherwise borrow a stranger autopilot creator's invoke rights just by mentioning
// on that autopilot's issue. The task.issue_id check below is what keeps that shut:
// this function is the sole owner of the same-issue requirement, which is why the
// stamp itself no longer carries it.
// `task` MUST therefore come from a server-trusted source — the X-Task-ID header
// on create/preview, or the stored comment.source_task_id on reconcile/edit —
// never a client-supplied field, and authority is granted only when ALL hold:
//   - the comment author is an agent and IS the task's agent;
//   - the issue is autopilot-origin (origin_type=autopilot, origin_id set);
//   - the speaking task is running on THIS issue (task.issue_id == issue.id).
//
// That last check is the load-bearing one: every unattributed agent task whose
// issue_id is this autopilot issue is part of the work this autopilot set in
// motion (the dispatched leader task, or a descendant it @mentioned into being),
// while a foreign run's task carries a different issue_id and is rejected. Note we
// do NOT key on autopilot_run_id: in create_issue mode (the reported scenario) the
// leader task is enqueued through the ordinary issue-assignment path and carries
// no autopilot_run_id — the run links back via its own issue_id, not the task's.
//
// Any mismatch, missing lineage, or lookup error returns "" and the gate stays
// fail-closed. Only a MEMBER-created autopilot yields a user id; an agent-created
// autopilot has no human to key the gate on, and the existing agent-actor
// workspace-target exception in canInvokeAgent already covers the one case
// (public_to workspace) it should. The returned id is used for AUTHORIZATION only
// — the enqueued task's originator/attribution is computed separately and stays
// unattributed.
func (h *Handler) autopilotDelegationAuthority(ctx context.Context, issue db.Issue, authorType, authorID string, task db.AgentTaskQueue) string {
	if authorType != "agent" {
		return ""
	}
	if !issue.OriginType.Valid || issue.OriginType.String != "autopilot" || !issue.OriginID.Valid {
		return ""
	}
	// The speaking run must be authored by THIS agent and doing work on THIS
	// autopilot issue — not a foreign run that merely commented here.
	if !task.AgentID.Valid || uuidToString(task.AgentID) != authorID {
		return ""
	}
	if !task.IssueID.Valid || uuidToString(task.IssueID) != uuidToString(issue.ID) {
		return ""
	}
	ap, err := h.Queries.GetAutopilotInWorkspace(ctx, db.GetAutopilotInWorkspaceParams{
		ID:          issue.OriginID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || ap.CreatedByType != "member" || !ap.CreatedByID.Valid {
		return ""
	}
	return uuidToString(ap.CreatedByID)
}

// autopilotDelegationAuthorityFromRequest resolves the MUL-4857 delegation
// authority for a comment being created or previewed over HTTP. The speaking task
// is taken from the server-trusted X-Task-ID header (the CLI stamps it on every
// agent request); autopilotDelegationAuthority then verifies its lineage. Returns
// "" for member actors or when no valid task is named, keeping the gate closed.
func (h *Handler) autopilotDelegationAuthorityFromRequest(r *http.Request, issue db.Issue, actorType, actorID string) string {
	if actorType != "agent" {
		return ""
	}
	task, ok := h.taskFromRequestHeader(r)
	if !ok {
		return ""
	}
	return h.autopilotDelegationAuthority(r.Context(), issue, actorType, actorID, task)
}

// autopilotDelegationAuthorityFromComment resolves the MUL-4857 delegation
// authority when reconciling an already-persisted comment (retrigger after
// cancel). The speaking task is taken from the stored comment.source_task_id — the
// same server-trusted lineage CreateComment stamped for the authoring run — and
// its lineage is verified by autopilotDelegationAuthority.
func (h *Handler) autopilotDelegationAuthorityFromComment(ctx context.Context, issue db.Issue, comment db.Comment) string {
	if comment.AuthorType != "agent" || !comment.SourceTaskID.Valid {
		return ""
	}
	task, err := h.Queries.GetAgentTask(ctx, comment.SourceTaskID)
	if err != nil {
		return ""
	}
	return h.autopilotDelegationAuthority(ctx, issue, comment.AuthorType, uuidToString(comment.AuthorID), task)
}

// commentSourceTaskID returns the agent's currently-executing task (from the
// server-trusted X-Task-ID header), else an invalid UUID. This is the exact
// lineage CreateComment stamps onto source_task_id, so an edit re-stamps what a
// fresh authoring of the same content would have stamped and the two resolvers
// can never disagree (MUL-6490).
//
// The task is NOT required to be running on the edited comment's issue: the
// stamp records "which run wrote this", and a run may legitimately write on
// another issue. Authority rules that additionally require same-issue work keep
// that check themselves — autopilotDelegationAuthority verifies
// task.issue_id == issue.id before granting the autopilot creator's invoke
// rights, so a cross-issue lineage still fails closed there (MUL-4857).
func (h *Handler) commentSourceTaskID(r *http.Request) pgtype.UUID {
	task, ok := h.taskFromRequestHeader(r)
	if !ok {
		return pgtype.UUID{}
	}
	return task.ID
}

// taskFromRequestHeader resolves the agent's currently-executing task from the
// X-Task-ID header (set by the CLI on every request). Returns ok=false when the
// header is absent, malformed, or names no existing task.
func (h *Handler) taskFromRequestHeader(r *http.Request) (db.AgentTaskQueue, bool) {
	taskIDHeader := r.Header.Get("X-Task-ID")
	if taskIDHeader == "" {
		return db.AgentTaskQueue{}, false
	}
	taskUUID, err := util.ParseUUID(taskIDHeader)
	if err != nil {
		return db.AgentTaskQueue{}, false
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		return db.AgentTaskQueue{}, false
	}
	return task, true
}

// accessibleAgentIDs returns the set of agent IDs in the workspace the actor
// is allowed to see, for use by workspace-wide aggregation endpoints
// (run counts, activity histograms, task snapshots) that need to filter out
// private / non-allow-listed agents the member can't access. Returns nil and
// false on error.
func (h *Handler) accessibleAgentIDs(ctx context.Context, workspaceID, actorType, actorID, role string) (map[string]struct{}, bool) {
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, false
	}
	agents, err := h.Queries.ListAllAgents(ctx, wsUUID)
	if err != nil {
		return nil, false
	}
	targetsByAgent, ok := h.loadInvocationTargetsByAgent(ctx, agents)
	if !ok {
		return nil, false
	}
	allowed := make(map[string]struct{}, len(agents))
	for _, a := range agents {
		if actorType == "member" {
			if !memberAllowedToViewAgent(a, targetsByAgent[uuidToString(a.ID)], actorID, role) {
				continue
			}
		}
		allowed[uuidToString(a.ID)] = struct{}{}
	}
	return allowed, true
}

// restrictedAgentIDs returns the ids of agents that EXIST in the workspace but
// must not be named to this actor. Aggregation endpoints that cannot simply drop
// a row (the dashboard rollups, whose per-agent totals have to keep reconciling
// with the workspace-level series rendered beside them) use it to fold those
// agents onto a single sentinel instead.
//
// Two populations land in the set, for two different reasons:
//
//  1. Every `kind != "user"` agent — the hidden execution carriers behind agent
//     builder sessions. These are restricted for EVERYONE, owner and admin
//     included: no list endpoint returns them (ListAgents / ListAllAgents filter
//     on kind), so no client can ever resolve one to a name, yet they run real
//     tasks and book real usage that the rollups aggregate all the same. Left
//     alone they surface as a bare UUID whose spend and failure profile belong
//     to whoever opened that builder session.
//  2. For a plain member only, the user agents they may not view.
//
// Hard-deleted agents are deliberately NOT in the set: they are already absent
// from the agent table, have no visibility left to protect, and the dashboard
// renders them as their own "deleted agents" bucket — folding them in here
// would relabel a real deletion as a permission boundary.
//
// Because of (1) this always reads the agent table, but the per-agent
// invocation-target lookup is skipped for actors who see every user agent
// (agent actors, workspace owner/admin) since nothing there can change their
// answer.
func (h *Handler) restrictedAgentIDs(ctx context.Context, workspaceID, actorType, actorID, role string) (map[string]struct{}, bool) {
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, false
	}
	agents, err := h.Queries.ListAllAgentsAnyKind(ctx, wsUUID)
	if err != nil {
		return nil, false
	}

	// Whether per-agent visibility can restrict anything for this actor at all.
	judgeUserAgents := actorType == "member" && !roleAllowed(role, "owner", "admin")
	var targetsByAgent map[string][]db.AgentInvocationTarget
	if judgeUserAgents {
		var ok bool
		if targetsByAgent, ok = h.loadInvocationTargetsByAgent(ctx, agents); !ok {
			return nil, false
		}
	}

	restricted := make(map[string]struct{})
	for _, a := range agents {
		if a.Kind == "user" {
			if !judgeUserAgents ||
				memberAllowedToViewAgent(a, targetsByAgent[uuidToString(a.ID)], actorID, role) {
				continue
			}
		}
		restricted[uuidToString(a.ID)] = struct{}{}
	}
	return restricted, true
}

// loadInvocationTargetsByAgent batch-loads invocation targets for a set of
// agents and buckets them by agent id string. Avoids the per-agent query the
// list / aggregation paths would otherwise incur.
func (h *Handler) loadInvocationTargetsByAgent(ctx context.Context, agents []db.Agent) (map[string][]db.AgentInvocationTarget, bool) {
	ids := make([]pgtype.UUID, 0, len(agents))
	for _, a := range agents {
		ids = append(ids, a.ID)
	}
	out := make(map[string][]db.AgentInvocationTarget, len(agents))
	if len(ids) == 0 {
		return out, true
	}
	rows, err := h.Queries.ListAgentInvocationTargetsByAgentIDs(ctx, ids)
	if err != nil {
		return nil, false
	}
	for _, row := range rows {
		aid := uuidToString(row.AgentID)
		out[aid] = append(out[aid], row)
	}
	return out, true
}

// canEnqueueSquadLeader returns true when the given actor is allowed to
// trigger the squad's private leader. It loads the leader agent and delegates
// to canInvokeAgent so the leader-trigger path honours invocation permission
// exactly like a direct assignment/mention. Non-public leaders require owner /
// allow-list; system-initiated triggers (e.g. github webhooks) are judged as
// system principals (workspace target only).
func (h *Handler) canEnqueueSquadLeader(ctx context.Context, leaderID pgtype.UUID, actorType, actorID, originatorUserID, workspaceID string) bool {
	agent, err := h.Queries.GetAgent(ctx, leaderID)
	if err != nil {
		return false
	}
	return h.canInvokeAgent(ctx, agent, actorType, actorID, originatorUserID, workspaceID)
}
