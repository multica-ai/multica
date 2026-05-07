// CEREBRO-PATCH(core-runtimes-mutations): cerebro modification of upstream file
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { AgentRuntime } from "../types";
import { api } from "../api";
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

// useUpdateRuntimeSandbox flips the per-runtime sandbox override (JEH-418).
// Optimistic so the toggle feels instant; on failure we roll back the cache
// to the snapshot taken before the mutation. The server publishes a
// daemon_register realtime event on success, but we still invalidate on
// settle to be safe against missed events.
export function useUpdateRuntimeSandbox(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (params: {
      runtimeId: string;
      sandboxEnabled: boolean | null;
    }) => api.updateRuntimeSandbox(params.runtimeId, params.sandboxEnabled),
    onMutate: async (params) => {
      const keys = [runtimeKeys.list(wsId), runtimeKeys.listMine(wsId)];
      await Promise.all(
        keys.map((k) => qc.cancelQueries({ queryKey: k })),
      );
      const snapshots = keys.map(
        (k) => [k, qc.getQueryData<AgentRuntime[]>(k)] as const,
      );
      for (const [k, list] of snapshots) {
        if (!list) continue;
        qc.setQueryData<AgentRuntime[]>(
          k,
          list.map((r) =>
            r.id === params.runtimeId
              ? { ...r, sandbox_enabled: params.sandboxEnabled }
              : r,
          ),
        );
      }
      return { snapshots };
    },
    onError: (_err, _params, context) => {
      if (!context) return;
      for (const [k, value] of context.snapshots) {
        qc.setQueryData(k, value);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}
