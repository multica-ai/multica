"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "../hooks";
import { useAuthStore } from "../auth";
import { agentListOptions, memberListOptions } from "../workspace/queries";
import { canAssignAgentToIssue } from "../permissions";

/**
 * Three-state availability for "does the current user have any agent
 * they can chat with in this workspace?".
 *
 * Why three states (not a boolean): the answer to "is there an agent?"
 * lives on the server. Until the agent-list query resolves, the answer
 * is genuinely *unknown*. Callers must distinguish "loading" from
 * "confirmed empty" — collapsing them to a boolean causes UIs to flash
 * disabled/empty states for the first few hundred ms after mount, even
 * when the workspace actually has agents.
 *
 *   "loading"   — agent or member list still in flight (be neutral in UI)
 *   "none"      — both queries resolved, user has zero assignable agents
 *   "available" — at least one agent passes archive + visibility filters
 */
export type WorkspaceAgentAvailability = "loading" | "none" | "available";

/**
 * Mirrors the per-agent visibility/archived filter used by AssigneePicker
 * and the chat agent dropdown, so the three pickers can never disagree on
 * "is this agent reachable?".
 *
 * Members are queried because `canAssignAgentToIssue` reads the caller's
 * role to decide visibility for `private` agents — without member data,
 * a freshly-loaded agent list could still produce wrong answers.
 */
export function useWorkspaceAgentAvailability(): WorkspaceAgentAvailability {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const { data: agents, isFetched: agentsFetched } = useQuery(
    agentListOptions(wsId),
  );
  const { data: members, isFetched: membersFetched } = useQuery(
    memberListOptions(wsId),
  );

  if (!agentsFetched || !membersFetched) return "loading";

  const rawRole = members?.find((m) => m.user_id === userId)?.role;
  const role =
    rawRole === "owner" || rawRole === "admin" || rawRole === "member"
      ? rawRole
      : null;

  const hasVisibleAgent = (agents ?? []).some(
    (a) =>
      !a.archived_at &&
      canAssignAgentToIssue(a, { userId: userId ?? null, role }).allowed,
  );

  return hasVisibleAgent ? "available" : "none";
}
