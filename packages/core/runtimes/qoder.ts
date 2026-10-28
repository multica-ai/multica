import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { api } from "../api";
import { runtimeKeys } from "./queries";
import { workspaceKeys } from "../workspace/queries";
import { agentTaskSnapshotKeys } from "../agents/queries";
import type { QoderInput } from "./qoder-schema";
export type { QoderInput, QoderConnection, QoderRegion } from "./qoder-schema";
export {
  QODER_REGIONS,
  DEFAULT_CONCURRENCY,
  MAX_CONCURRENCY,
  DEFAULT_RUN_TIMEOUT,
  MIN_RUN_TIMEOUT,
  MAX_RUN_TIMEOUT,
} from "./qoder-schema";
export const qoderKey = (wsId: string) => ["qoder", wsId] as const;
export const qoderOptions = (wsId: string) =>
  queryOptions({
    queryKey: qoderKey(wsId),
    queryFn: () => api.getQoderConnection(wsId),
    refetchInterval: 5000,
  });
export function useSaveQoder(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: QoderInput) => api.saveQoderConnection(wsId, input),
    onSuccess: (data) => {
      qc.setQueryData(qoderKey(wsId), data);
      void qc.invalidateQueries({ queryKey: ["qoder", wsId, "agents"] });
      void qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}
export function useResumeQoder(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: QoderInput) => api.resumeQoderConnection(wsId, input),
    onSuccess: (data) => {
      qc.setQueryData(qoderKey(wsId), data);
      void qc.invalidateQueries({ queryKey: ["qoder", wsId, "agents"] });
      void qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}
export function useCheckQoder(wsId: string) {
  return useMutation({
    mutationFn: (input: QoderInput) => api.checkQoderConnection(wsId, input),
  });
}

export function useStopQoder(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.stopQoderConnection(wsId),
    onSuccess: (data) => {
      qc.setQueryData(qoderKey(wsId), data);
      void qc.invalidateQueries({ queryKey: ["qoder", wsId, "agents"] });
      void qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}

export function useDisconnectQoder(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (expectedActiveAgentIds: string[]) =>
      api.disconnectQoderConnection(wsId, expectedActiveAgentIds),
    onSuccess: (data) => {
      qc.setQueryData(qoderKey(wsId), data);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: qoderKey(wsId) });
      void qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
      void qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      void qc.invalidateQueries({ queryKey: agentTaskSnapshotKeys.all(wsId) });
    },
  });
}

export const qoderAgentsOptions = (wsId: string) =>
  queryOptions({
    queryKey: ["qoder", wsId, "agents"],
    queryFn: () => api.listQoderAgents(wsId),
    staleTime: 30_000,
  });

export function useQoderEnvironments(wsId: string) {
  return useMutation({
    mutationFn: ({
      token,
      baseUrl,
      signal,
    }: {
      token: string;
      baseUrl: string;
      signal?: AbortSignal;
    }) => api.listQoderEnvironments(wsId, token, baseUrl, signal),
    gcTime: 0,
  });
}
