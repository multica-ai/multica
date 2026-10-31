import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { api } from "../api";
import { runtimeKeys } from "./queries";
import { workspaceKeys } from "../workspace/queries";
import { agentTaskSnapshotKeys } from "../agents/queries";
import type { ClaudeConnection, ClaudeInput } from "./claude-managed-schema";
import type { AgentRuntime } from "../types";
export type { ClaudeConnection, ClaudeInput } from "./claude-managed-schema";

export const CLAUDE_MANAGED_PROVIDER = "claude_managed";
// The server runs the managed bridge under this daemon ID; a standalone
// bridge in the same workspace registers under its own.
export const claudeManagedDaemonId = (wsId: string) => `claude-managed-${wsId}`;

/** Whether a runtime is the one the server runs for the workspace's connection. */
export function isManagedClaudeRuntime(runtime: AgentRuntime): boolean {
  return (
    runtime.provider === CLAUDE_MANAGED_PROVIDER &&
    runtime.daemon_id === claudeManagedDaemonId(runtime.workspace_id)
  );
}

export const claudeManagedKey = (wsId: string) => ["claude-managed", wsId] as const;
export const claudeManagedOptions = (wsId: string) =>
  queryOptions({
    queryKey: claudeManagedKey(wsId),
    queryFn: () => api.getClaudeConnection(wsId),
    refetchInterval: 5000,
  });

function useConnectionMutation<T>(
  wsId: string,
  mutationFn: (input: T) => Promise<ClaudeConnection>,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: (data) => {
      qc.setQueryData(claudeManagedKey(wsId), data);
      void qc.invalidateQueries({ queryKey: ["claude-managed", wsId, "agents"] });
      void qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}

export function useSaveClaude(wsId: string) {
  return useConnectionMutation(wsId, (input: ClaudeInput) =>
    api.saveClaudeConnection(wsId, input),
  );
}
export function useResumeClaude(wsId: string) {
  return useConnectionMutation(wsId, (input: ClaudeInput) =>
    api.resumeClaudeConnection(wsId, input),
  );
}
export function useStopClaude(wsId: string) {
  return useConnectionMutation(wsId, () => api.stopClaudeConnection(wsId));
}
export function useCheckClaude(wsId: string) {
  return useMutation({
    mutationFn: (input: ClaudeInput) => api.checkClaudeConnection(wsId, input),
  });
}

export function useDisconnectClaude(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (expectedActiveAgentIds: string[]) =>
      api.disconnectClaudeConnection(wsId, expectedActiveAgentIds),
    onSuccess: (data) => {
      qc.setQueryData(claudeManagedKey(wsId), data);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: claudeManagedKey(wsId) });
      void qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
      void qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      void qc.invalidateQueries({ queryKey: agentTaskSnapshotKeys.all(wsId) });
    },
  });
}

export const claudeAgentsOptions = (wsId: string) =>
  queryOptions({
    queryKey: ["claude-managed", wsId, "agents"],
    queryFn: () => api.listClaudeAgents(wsId),
    staleTime: 30_000,
  });

export function useClaudeEnvironments(wsId: string) {
  return useMutation({
    mutationFn: ({ token, signal }: { token: string; signal?: AbortSignal }) =>
      api.listClaudeEnvironments(wsId, token, signal),
    gcTime: 0,
  });
}
