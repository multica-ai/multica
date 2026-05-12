// Cerebro-only React Query mutations for runtime pause/unpause. Lives in
// the cerebro-runtime package so the upstream views don't gain a hook
// they'd otherwise drag along through every sync.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { AgentRuntime } from "@multica/core/types/agent";
import { runtimeKeys } from "@multica/core/runtimes/queries";

export interface PauseRuntimeArgs {
  runtimeId: string;
  /** RFC3339 — when set, server schedules an auto-unpause at that time. */
  unpauseAt?: string;
  /** Free-form slug. Defaults to "manual" server-side. */
  reason?: string;
}

export function usePauseRuntime(workspaceId: string) {
  const qc = useQueryClient();
  return useMutation<AgentRuntime, Error, PauseRuntimeArgs>({
    mutationFn: ({ runtimeId, unpauseAt, reason }) =>
      api.pauseRuntime(runtimeId, { unpause_at: unpauseAt, reason }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.list(workspaceId) });
    },
  });
}

export function useUnpauseRuntime(workspaceId: string) {
  const qc = useQueryClient();
  return useMutation<AgentRuntime, Error, { runtimeId: string }>({
    mutationFn: ({ runtimeId }) => api.unpauseRuntime(runtimeId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.list(workspaceId) });
    },
  });
}
