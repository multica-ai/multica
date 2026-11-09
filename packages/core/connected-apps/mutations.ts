import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { UpdateAgentAppsRequest } from "../types";
import { workspaceKeys } from "../workspace/queries";
import { connectedAppsKeys } from "./queries";

export function useDisconnectTeamApp() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (connectionId: string) => api.disconnectTeamApp(connectionId),
    onSettled: () => qc.invalidateQueries({ queryKey: connectedAppsKeys.all(wsId) }),
  });
}

/** Replaces an agent's apps. The legacy `composio_toolkit_allowlist` on the
 * agent follows server-side, so the agent list is refreshed too. */
export function useUpdateAgentApps(agentId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (body: UpdateAgentAppsRequest) => api.updateAgentApps(agentId, body),
    onSuccess: (data) => {
      if (data) qc.setQueryData(connectedAppsKeys.agentApps(wsId, agentId), data.apps);
      else void qc.invalidateQueries({ queryKey: connectedAppsKeys.agentApps(wsId, agentId) });
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: connectedAppsKeys.overview(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    },
  });
}

export function useDecideAppRequest() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input:
      | { id: string; decision: "approve"; arguments?: Record<string, unknown> }
      | { id: string; decision: "reject"; reason?: string }) =>
      input.decision === "approve"
        ? api.approveAppRequest(input.id, input.arguments)
        : api.rejectAppRequest(input.id, input.reason),
    onSettled: () => qc.invalidateQueries({ queryKey: connectedAppsKeys.requests(wsId) }),
  });
}
