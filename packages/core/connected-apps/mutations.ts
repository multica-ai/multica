import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type {
  AppAccountScope,
  AppActionClass,
  AppActionPolicy,
  AppCatalogMode,
  UpdateAgentAppsRequest,
} from "../types";
import { workspaceKeys } from "../workspace/queries";
import { connectedAppsKeys } from "./queries";

export function useUpdateAppSettings() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (mode: AppCatalogMode) => api.updateAppSettings(mode),
    onSettled: () => qc.invalidateQueries({ queryKey: connectedAppsKeys.all(wsId) }),
  });
}

export function useUpdateAppRules(slug: string, scope: AppAccountScope) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (rules: { action_class: AppActionClass; policy: AppActionPolicy }[]) =>
      api.updateAppRules(slug, scope, rules),
    onSuccess: (data) => qc.setQueryData(connectedAppsKeys.rules(wsId, slug, scope), data.rules),
  });
}

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
    onSuccess: (data) => qc.setQueryData(connectedAppsKeys.agentApps(wsId, agentId), data.apps),
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
