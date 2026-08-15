import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import { canAssignAgentToIssue, canEditAgent } from "../permissions/rules";
import type { PermissionContext } from "../permissions/types";
import type { QianwenAgentSummary } from "../types";
import { agentListOptions } from "../workspace/queries";

export const qianwenKeys = {
  all: (wsId: string) => ["qianwen", wsId] as const,
  installationsRoot: (wsId: string) => [
    ...qianwenKeys.all(wsId),
    "installations",
  ] as const,
  installations: (wsId: string, currentUserId: string) => [
    ...qianwenKeys.installationsRoot(wsId),
    "user",
    currentUserId,
  ] as const,
};

export const qianwenInstallationsOptions = (
  wsId: string,
  currentUserId: string,
) => queryOptions({
  queryKey: qianwenKeys.installations(wsId, currentUserId),
  queryFn: () => api.listQianwenInstallations(wsId),
  enabled: !!wsId && !!currentUserId,
});

/**
 * Reuses the shared legacy Agent cache while exposing only the camel-case,
 * caller-relative fields needed by the Qianwen Settings view.
 */
export function qianwenAgentListOptions(
  wsId: string,
  permissionContext: PermissionContext,
) {
  return queryOptions({
    ...agentListOptions(wsId),
    select: (agents): QianwenAgentSummary[] =>
      agents.map((agent) => ({
        id: agent.id,
        name: agent.name,
        archivedAt: agent.archived_at,
        canManage: canEditAgent(agent, permissionContext).allowed,
        canInvoke: canAssignAgentToIssue(agent, permissionContext).allowed,
      })),
  });
}
