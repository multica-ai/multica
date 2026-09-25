package handler

import (
	"context"
	"log/slog"
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
//
// A TERMINAL task lends nothing (MUL-6951, Elon review). A run represents its
// human only while it is live: once the task is completed / failed / cancelled,
// the work that human authorized is over. Task-token revocation at the terminal
// transition is the primary guard, but it is best-effort and explicitly non-fatal
// on failure (TaskService, MUL-2600) with a 24h token expiry behind it, so a
// revocation that fails would otherwise leave a window in which a finished run
// keeps spending a member's invoke rights. This check closes that window at the
// authorization boundary rather than the auth boundary, so a terminal task can
// still complete ordinary API work (posting its final comment) without being able
// to start new work as its human.
func (h *Handler) invokeOriginatorFromRequest(r *http.Request, actorType, actorID string) string {
	if actorType == "member" {
		return actorID
	}
	if actorType == "agent" {
		if taskIDHeader := r.Header.Get("X-Task-ID"); taskIDHeader != "" {
			if taskUUID, err := util.ParseUUID(taskIDHeader); err == nil {
				if task, err := h.Queries.GetAgentTask(r.Context(), taskUUID); err == nil {
					if isTerminalTaskStatus(task.Status) {
						return ""
					}
					return uuidToString(task.OriginatorUserID)
				}
			}
		}
	}
	return ""
}

// isTerminalTaskStatus reports whether a task has finished, in any of the three
// ways it can. Mirrors the agent_task_queue status CHECK constraint; a status the
// constraint does not know is treated as NOT terminal so an added in-flight state
// keeps working (an added terminal state would need adding here, which the invoke
// gate's fail-closed default makes visible rather than silent).
func isTerminalTaskStatus(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	}
	return false
}

// commentSourceTaskID returns the agent's currently-executing task (from the
// server-trusted X-Task-ID header), else an invalid UUID. This is the exact
// lineage CreateComment stamps onto source_task_id, so an edit re-stamps what a
// fresh authoring of the same content would have stamped and the two resolvers
// can never disagree (MUL-6490).
//
// The task is NOT required to be running on the edited comment's issue: the
// stamp records "which run wrote this", and a run may legitimately write on
// another issue. That is safe for the invoke gate because the gate keys on the
// task's own originator (MUL-6951) — a human who already holds those rights —
// rather than on a same-issue binding standing in for one.
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
