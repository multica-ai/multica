package handler

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
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
// comment here (comment.go CreateComment only stamps source_task_id when the
// authoring task's issue matches), so it could otherwise borrow a stranger
// autopilot creator's invoke rights just by mentioning on that autopilot's issue.
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

// commentSourceTaskIDForIssue returns the agent's currently-executing task (from
// the X-Task-ID header) when it is running on the given issue, else an invalid
// UUID. This is the exact issue-scoped lineage CreateComment stamps onto
// source_task_id; a cross-issue (or missing) task yields invalid so the persisted
// lineage — and every authority/originator resolution that reads it — fails closed
// (MUL-4857).
func (h *Handler) commentSourceTaskIDForIssue(r *http.Request, issue db.Issue) pgtype.UUID {
	task, ok := h.taskFromRequestHeader(r)
	if !ok || !task.IssueID.Valid || uuidToString(task.IssueID) != uuidToString(issue.ID) {
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
