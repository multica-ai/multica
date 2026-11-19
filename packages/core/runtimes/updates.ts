import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { api } from "../api";
import type { RuntimeUpdate, RuntimeUpdateStatus } from "../types/agent";

// Runtime IDs are globally unique, so these keys follow the other per-runtime
// keys (usage, rate limits) and are not nested under a workspace.
export const runtimeUpdateKeys = {
  all: () => ["runtimes", "update"] as const,
  active: (runtimeId: string) =>
    [...runtimeUpdateKeys.all(), "active", runtimeId] as const,
  detail: (runtimeId: string, updateId: string) =>
    [...runtimeUpdateKeys.all(), runtimeId, updateId] as const,
};

const KNOWN_STATUSES: ReadonlySet<string> = new Set<RuntimeUpdateStatus>([
  "pending",
  "scheduled",
  "running",
  "completed",
  "failed",
  "timeout",
  "cancelled",
  "expired",
]);

/** A status this client knows how to render, or null for one from a newer server. */
export function knownRuntimeUpdateStatus(
  status: string | undefined,
): RuntimeUpdateStatus | null {
  return status && KNOWN_STATUSES.has(status)
    ? (status as RuntimeUpdateStatus)
    : null;
}

// Picking the request up and installing take seconds, so those poll fast.
// Waiting for the machine to go idle can take hours, and nothing on screen
// changes faster than the running-task count, so it polls slowly.
export const UPDATE_FAST_POLL_MS = 2_000;
export const UPDATE_SCHEDULED_POLL_MS = 10_000;

export function updatePollInterval(
  update: RuntimeUpdate | null | undefined,
): number | false {
  switch (knownRuntimeUpdateStatus(update?.status)) {
    case "pending":
    case "running":
      return UPDATE_FAST_POLL_MS;
    case "scheduled":
      return UPDATE_SCHEDULED_POLL_MS;
    default:
      return false;
  }
}

/** The runtime's pending, scheduled or running update, read once on mount. */
export function runtimeActiveUpdateOptions(runtimeId: string | null) {
  return queryOptions({
    queryKey: runtimeId
      ? runtimeUpdateKeys.active(runtimeId)
      : runtimeUpdateKeys.all(),
    queryFn: () => api.getActiveUpdate(runtimeId as string),
    enabled: Boolean(runtimeId),
    retry: false,
  });
}

/** One update request, polled until it settles. */
export function runtimeUpdateOptions(
  runtimeId: string | null,
  updateId: string | null,
) {
  return queryOptions({
    queryKey:
      runtimeId && updateId
        ? runtimeUpdateKeys.detail(runtimeId, updateId)
        : runtimeUpdateKeys.all(),
    queryFn: () => api.getUpdateResult(runtimeId as string, updateId as string),
    enabled: Boolean(runtimeId && updateId),
    refetchInterval: (query) => updatePollInterval(query.state.data),
    // A failed poll keeps the last known state on screen; the next interval
    // tries again.
    retry: false,
  });
}

export function useInitiateRuntimeUpdate(runtimeId: string | null) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (targetVersion: string) =>
      api.initiateUpdate(runtimeId as string, targetVersion),
    onSuccess: (update) => {
      if (!runtimeId || !update.id) return;
      qc.setQueryData(runtimeUpdateKeys.detail(runtimeId, update.id), update);
    },
  });
}

/**
 * Cancels an update through the runtime it was queued on, which can be another
 * runtime of the same machine than the one the section uses to start updates.
 */
export function useCancelRuntimeUpdate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ runtimeId, updateId }: { runtimeId: string; updateId: string }) =>
      api.cancelUpdate(runtimeId, updateId),
    onSuccess: (update, { runtimeId, updateId }) => {
      qc.setQueryData(runtimeUpdateKeys.detail(runtimeId, updateId), update);
    },
    // A 409 means installation started (or the daemon is picking the update
    // up right now): refetch so the section shows what actually happened.
    onSettled: (_data, _error, { runtimeId, updateId }) => {
      qc.invalidateQueries({ queryKey: runtimeUpdateKeys.detail(runtimeId, updateId) });
      qc.invalidateQueries({ queryKey: runtimeUpdateKeys.all() });
    },
  });
}
