import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { AgentRuntime } from "../types";
import { runtimeKeys } from "./queries";

export function useDeleteRuntime(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (runtimeId: string) => api.deleteRuntime(runtimeId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}

// Pause a runtime. Optimistically patches the runtime list cache so the
// "Paused" badge shows up immediately rather than waiting for the WS event
// to round-trip — the WS event then no-ops on the same state.
export function usePauseRuntime(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: {
      runtimeId: string;
      unpause_at?: string;
      reason?: string;
    }) =>
      api.pauseRuntime(vars.runtimeId, {
        unpause_at: vars.unpause_at,
        reason: vars.reason,
      }),
    onSuccess: (updated, vars) => {
      patchRuntimeListCache(qc, wsId, vars.runtimeId, updated);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}

export function useUnpauseRuntime(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (runtimeId: string) => api.unpauseRuntime(runtimeId),
    onSuccess: (updated, runtimeId) => {
      patchRuntimeListCache(qc, wsId, runtimeId, updated);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}

// Splices a single updated runtime back into every cached
// `runtimes/{wsId}/...` list query so the response from the mutation is
// applied without waiting for a refetch. Other queries under the same key
// (filtered owner=me view, etc.) all match the same shape, so a single
// splice covers them all.
function patchRuntimeListCache(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  runtimeId: string,
  updated: AgentRuntime,
) {
  qc.setQueriesData<AgentRuntime[]>(
    { queryKey: runtimeKeys.all(wsId) },
    (prev) => {
      if (!prev) return prev;
      return prev.map((rt) => (rt.id === runtimeId ? updated : rt));
    },
  );
}
