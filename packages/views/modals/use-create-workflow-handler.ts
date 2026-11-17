import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { isAgentRuntimeBound } from "@multica/core/agents";
import {
  useProjectWithWorkflow,
  workflowStep,
} from "@multica/core/issue-workflows";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import {
  agentListOptions,
  memberListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import { stepHandlerActor } from "../workflows/step-handler";

/** Use the same available actors as the create pickers; the server still authorizes the create. */
export function useCreateWorkflowHandler(
  projectId: string | null | undefined,
  status: string,
) {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const { project, workflow } = useProjectWithWorkflow(wsId, projectId);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const step = workflowStep(workflow, status);
  const actor =
    step?.handler.type === "creator" && userId
      ? { type: "member" as const, id: userId }
      : stepHandlerActor(step, project);
  if (!projectId || !actor) return null;
  if (actor.type === "member") {
    return members.some((m) => m.user_id === actor.id) ? actor : null;
  }
  const currentRole = members.find((m) => m.user_id === userId)?.role;
  const role =
    currentRole === "owner" ||
    currentRole === "admin" ||
    currentRole === "member"
      ? currentRole
      : null;
  const agentId =
    actor.type === "squad"
      ? squads.find((s) => s.id === actor.id && !s.archived_at)?.leader_id
      : actor.id;
  const agent = agents.find((a) => a.id === agentId);
  return agent &&
    !agent.archived_at &&
    isAgentRuntimeBound(agent) &&
    canAssignAgentToIssue(agent, { userId: userId ?? null, role }).allowed
    ? actor
    : null;
}
