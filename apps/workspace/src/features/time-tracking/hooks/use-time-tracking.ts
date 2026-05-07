import {
  useQuery,
  useMutation,
  useQueryClient,
  type UseQueryOptions,
} from "@tanstack/react-query";
import type { TimeEntry, CreateTimeEntryRequest, UpdateTimeEntryRequest } from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { useWorkspaceStore } from "@/features/workspace";
import { useTimeTrackingStore } from "../store";

// ── Queries ────────────────────────────────────────────────────────────────────

/**
 * Fetches the running timer for the current user every 30 seconds.
 * Returns null when no timer is active.
 */
export function useCurrentTimerQuery(options?: Partial<UseQueryOptions<TimeEntry | null>>) {
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useQuery({
    queryKey: queryKeys.timeTracking.current(workspaceId),
    queryFn: () => api.getCurrentTimeEntry(),
    refetchInterval: 30_000,
    refetchOnWindowFocus: true,
    enabled: !!workspaceId,
    ...options,
  });
}

/** Fetches time entries for the current user.
 *
 * Supports two modes:
 * - Date-range: pass `since` + `until` (ISO 8601) to get all entries in the window.
 *   Use this for calendar/day-grouped views so you never miss entries.
 * - Pagination: pass `limit` + `offset` as a fallback.
 *
 * Each unique set of params is cached independently under the broad `entries` key,
 * so `queryClient.invalidateQueries(queryKeys.timeTracking.entries(wsId))` invalidates all variants.
 */
export function useTimeEntriesQuery(params?: {
  limit?: number;
  offset?: number;
  since?: string;
  until?: string;
}) {
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  // Stable params object for the queryKey — normalise to avoid cache misses.
  const stableParams: Record<string, unknown> = {};
  if (params?.since) stableParams.since = params.since;
  if (params?.until) stableParams.until = params.until;
  if (params?.limit) stableParams.limit = params.limit;
  if (params?.offset) stableParams.offset = params.offset;

  return useQuery({
    queryKey: queryKeys.timeTracking.entriesParams(workspaceId, stableParams),
    queryFn: () => api.listTimeEntries(params),
    enabled: !!workspaceId,
    staleTime: 30_000,
  });
}

/** Time entries linked to a specific issue. */
export function useIssueTimeEntriesQuery(issueId: string) {
  return useQuery({
    queryKey: queryKeys.timeTracking.issueEntries(issueId),
    queryFn: () => api.listIssueTimeEntries(issueId),
    enabled: !!issueId,
    staleTime: 30_000,
  });
}

// ── Mutations ─────────────────────────────────────────────────────────────────

/**
 * Start a live timer or create a manual time entry.
 * Applies optimistic update so the UI responds immediately.
 */
export function useStartTimerMutation() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");

  return useMutation({
    mutationFn: (data: CreateTimeEntryRequest) => api.startTimeEntry(data),
    onMutate: async (data) => {
      const currentKey = queryKeys.timeTracking.current(workspaceId);

      // Cancel in-flight refetches to avoid overwriting optimistic state.
      await queryClient.cancelQueries({ queryKey: currentKey });

      const previous = queryClient.getQueryData<TimeEntry | null>(currentKey);

      // Construct a temporary entry with a fake id; replaced on success.
      const now = new Date(data.start_time);
      const optimistic: TimeEntry = {
        id: `optimistic-${Date.now()}`,
        workspace_id: workspaceId,
        user_id: "",
        issue_id: data.issue_id ?? null,
        description: data.description ?? null,
        start_time: data.start_time,
        stop_time: null,
        // Toggl running convention: duration = -start.Unix()
        duration_seconds: -Math.floor(now.getTime() / 1000),
        created_at: data.start_time,
        updated_at: data.start_time,
      };

      queryClient.setQueryData(currentKey, optimistic);
      useTimeTrackingStore.getState().setCurrentEntry(optimistic);

      return { previous };
    },
    onError: (_err, _vars, context) => {
      if (context?.previous !== undefined) {
        const currentKey = queryKeys.timeTracking.current(workspaceId);
        queryClient.setQueryData(currentKey, context.previous);
        useTimeTrackingStore.getState().setCurrentEntry(context.previous ?? null);
      }
    },
    onSuccess: (entry) => {
      const currentKey = queryKeys.timeTracking.current(workspaceId);
      queryClient.setQueryData(currentKey, entry);
      useTimeTrackingStore.getState().setCurrentEntry(entry);
      queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(workspaceId) });
    },
  });
}

/**
 * Stop the running timer.
 * Clears the current timer from the cache immediately (optimistic).
 */
export function useStopTimerMutation() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");

  return useMutation({
    mutationFn: (entryId: string) => api.stopTimeEntry(entryId),
    onMutate: async (entryId) => {
      const currentKey = queryKeys.timeTracking.current(workspaceId);

      await queryClient.cancelQueries({ queryKey: currentKey });

      const previous = queryClient.getQueryData<TimeEntry | null>(currentKey);

      // Clear current timer immediately so the UI stops the counter.
      queryClient.setQueryData(currentKey, null);
      useTimeTrackingStore.getState().setCurrentEntry(null);

      // Mark the entry as stopped in any issue-entry caches.
      const nowIso = new Date().toISOString();
      queryClient.setQueriesData<TimeEntry[]>(
        { predicate: (q) => q.queryKey[0] === "time-tracking" && q.queryKey[1] === "issue" },
        (old) =>
          old?.map((e) => {
            if (e.id !== entryId) return e;
            const startMs = new Date(e.start_time).getTime();
            const durationSec = Math.max(0, Math.round((Date.now() - startMs) / 1000));
            return { ...e, stop_time: nowIso, duration_seconds: durationSec };
          }),
      );

      return { previous };
    },
    onError: (_err, _vars, context) => {
      if (!navigator.onLine) return;
      if (context?.previous !== undefined) {
        const currentKey = queryKeys.timeTracking.current(workspaceId);
        queryClient.setQueryData(currentKey, context.previous);
        useTimeTrackingStore.getState().setCurrentEntry(context.previous ?? null);
      }
    },
    onSuccess: (entry) => {
      const currentKey = queryKeys.timeTracking.current(workspaceId);
      queryClient.setQueryData(currentKey, null);
      queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(workspaceId) });
      // Refresh issue-level entry lists if this entry was linked to an issue.
      if (entry.issue_id) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.timeTracking.issueEntries(entry.issue_id),
        });
      }
    },
  });
}

/** Update description or linked issue on a time entry. */
export function useUpdateTimeEntryMutation() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");

  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: UpdateTimeEntryRequest }) =>
      api.updateTimeEntry(id, data),
    onSuccess: (entry) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(workspaceId) });
      if (entry.issue_id) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.timeTracking.issueEntries(entry.issue_id),
        });
      }
      // If this is the running entry, sync the current-timer cache.
      const currentKey = queryKeys.timeTracking.current(workspaceId);
      const current = queryClient.getQueryData<TimeEntry | null>(currentKey);
      if (current?.id === entry.id) {
        queryClient.setQueryData(currentKey, entry);
        useTimeTrackingStore.getState().setCurrentEntry(entry);
      }
    },
  });
}

/** Delete a time entry. */
export function useDeleteTimeEntryMutation() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");

  return useMutation({
    mutationFn: ({ id, issueId }: { id: string; issueId?: string | null }) =>
      api.deleteTimeEntry(id),
    onSuccess: (_data, { id, issueId }) => {
      // Clear from current timer if it was the running entry.
      const currentKey = queryKeys.timeTracking.current(workspaceId);
      const current = queryClient.getQueryData<TimeEntry | null>(currentKey);
      if (current?.id === id) {
        queryClient.setQueryData(currentKey, null);
        useTimeTrackingStore.getState().setCurrentEntry(null);
      }
      queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(workspaceId) });
      if (issueId) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.timeTracking.issueEntries(issueId),
        });
      }
    },
  });
}
