import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { AgentApp, AppAccountScope, AppActionClass, AppsOverview, UpdateAgentAppsRequest } from "../types";
import { workspaceKeys } from "../workspace/queries";
import { connectedAppsKeys } from "./queries";

/** Explicitly replaces saved restrictions; never runs automatically on load. */
export function useRestoreAppAccess() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: async (target: { slug: string; scope: AppAccountScope } | { catalog: true }) => {
      if ("catalog" in target) {
        await api.updateAppSettings({ catalog_mode: "all", allowed_toolkits: [] });
      } else {
        await api.updateAppRules(target.slug, target.scope,
          (["read", "write", "send", "delete", "share"] as AppActionClass[])
            .map((action_class) => ({ action_class, policy: "allow" })));
      }
    },
    onSettled: () => qc.invalidateQueries({ queryKey: connectedAppsKeys.all(wsId) }),
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

/** Removes one of the caller's own accounts from every workspace. */
export function useDisconnectPersonalApp() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (connectionId: string) => api.deleteComposioConnection(connectionId),
    onSettled: () => qc.invalidateQueries({ queryKey: connectedAppsKeys.all(wsId) }),
  });
}

export function useRequestTeamApp() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input: { slug: string; agentId?: string }) => api.requestTeamApp(input.slug, input.agentId),
    onSettled: () => qc.invalidateQueries({ queryKey: connectedAppsKeys.all(wsId) }),
  });
}

/** An admin declines everything waiting for an app's team account; a member
 * withdraws their own ask. */
export function useDismissTeamAppRequests() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (slug: string) => api.dismissTeamAppRequests(slug),
    onSettled: () => qc.invalidateQueries({ queryKey: connectedAppsKeys.all(wsId) }),
  });
}

/** Replaces an agent's apps. The new list shows at once in the agent's apps
 * and the overview, and rolls back if the save fails. The legacy
 * `composio_toolkit_allowlist` on the agent follows server-side, so the agent
 * list is refreshed too, once the last save in flight settles. */
export function useUpdateAgentApps(agentId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const overviewKey = connectedAppsKeys.overview(wsId);
  const appsKey = connectedAppsKeys.agentApps(wsId, agentId);
  const showInOverview = (apps: AgentApp[]) =>
    qc.setQueryData<AppsOverview>(overviewKey, (old) =>
      old && { ...old, agent_apps: [...old.agent_apps.filter((a) => a.agent_id !== agentId), ...apps] });
  return useMutation({
    mutationKey: ["agent-apps", wsId],
    mutationFn: (body: UpdateAgentAppsRequest) => api.updateAgentApps(agentId, body),
    onMutate: async (body) => {
      await Promise.all([qc.cancelQueries({ queryKey: overviewKey }), qc.cancelQueries({ queryKey: appsKey })]);
      const prev = {
        overview: qc.getQueryData<AppsOverview>(overviewKey)?.agent_apps.filter((a) => a.agent_id === agentId),
        apps: qc.getQueryData<AgentApp[]>(appsKey),
      };
      const next = body.apps.map((a) => ({ agent_id: agentId, ...a }));
      showInOverview(next);
      if (prev.apps) qc.setQueryData(appsKey, next);
      return prev;
    },
    onError: (_err, _body, prev) => {
      // Only this agent's apps roll back; another agent's save stays shown.
      if (prev?.overview) showInOverview(prev.overview);
      if (prev?.apps) qc.setQueryData(appsKey, prev.apps);
    },
    onSuccess: (data) => {
      if (data) {
        qc.setQueryData(appsKey, data.apps);
        showInOverview(data.apps);
      } else void qc.invalidateQueries({ queryKey: appsKey });
    },
    onSettled: () => {
      // A refetch landing while another agent's save is in flight would
      // briefly undo that save on screen; the last one to settle refetches.
      if (qc.isMutating({ mutationKey: ["agent-apps", wsId] }) > 1) return;
      qc.invalidateQueries({ queryKey: overviewKey });
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
