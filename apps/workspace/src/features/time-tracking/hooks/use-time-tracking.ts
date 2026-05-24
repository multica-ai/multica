import {
  useQuery,
  useMutation,
  useQueryClient,
  type UseQueryOptions,
} from "@tanstack/react-query";
import type { TimeEntry, TimeEntryLabel, CreateTimeEntryRequest, UpdateTimeEntryRequest } from "@/shared/types";
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

/**
 * Fetches workspace-level time aggregation grouped by member and by project.
 * Used by the Team Time review page.
 */
export function useTeamTimeStatsQuery(params: { since: string; until: string }) {
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useQuery({
    queryKey: queryKeys.timeTracking.teamStats(workspaceId, { since: params.since, until: params.until }),
    queryFn: () => api.getTeamTimeStats(params),
    enabled: !!workspaceId && !!params.since && !!params.until,
    staleTime: 60_000,
  });
}

/** Fetches workspace-scoped labels for time entries. */
export function useTimeEntryLabelsQuery() {
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");

  return useQuery({
    queryKey: queryKeys.timeTracking.labels(workspaceId),
    queryFn: () => api.listTimeEntryLabels(),
    enabled: !!workspaceId,
    staleTime: 60_000,
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
        type: "manual",
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

/** Workspace-level time-entry label CRUD and per-entry assignment helpers. */
export function useTimeEntryLabelMutations() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");

  function invalidateLabelLists() {
    queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.labels(workspaceId) });
  }

  function invalidateEntryLists() {
    queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(workspaceId) });
    queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.current(workspaceId) });
  }

  const createMutation = useMutation({
    mutationFn: (data: { name: string; color?: string }) => api.createTimeEntryLabel(data),
    onSuccess: invalidateLabelLists,
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, ...data }: { id: string; name: string; color: string }) =>
      api.updateTimeEntryLabel(id, data),
    onSuccess: invalidateLabelLists,
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deleteTimeEntryLabel(id),
    onSuccess: () => {
      invalidateLabelLists();
      invalidateEntryLists();
    },
  });

  const setEntryLabelsMutation = useMutation({
    mutationFn: ({ entryId, labelIds }: { entryId: string; labelIds: string[] }) =>
      api.setTimeEntryLabels(entryId, { label_ids: labelIds }),
    onSuccess: (entry) => {
      invalidateEntryLists();
      if (entry.issue_id) {
        queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.issueEntries(entry.issue_id) });
      }
    },
  });

  const addEntryLabelMutation = useMutation({
    mutationFn: ({ entryId, input }: { entryId: string; input: { label_id?: string; name?: string; color?: string } }) =>
      api.addTimeEntryLabel(entryId, input),
    onSuccess: (entry) => {
      invalidateEntryLists();
      if (entry.issue_id) {
        queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.issueEntries(entry.issue_id) });
      }
    },
  });

  const removeEntryLabelMutation = useMutation({
    mutationFn: ({ entryId, labelId }: { entryId: string; labelId: string }) =>
      api.removeTimeEntryLabel(entryId, labelId),
    onSuccess: (entry) => {
      invalidateEntryLists();
      if (entry.issue_id) {
        queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.issueEntries(entry.issue_id) });
      }
    },
  });

  return {
    createTimeEntryLabel: (data: { name: string; color?: string }) => createMutation.mutateAsync(data),
    updateTimeEntryLabel: (id: string, data: { name: string; color: string }) =>
      updateMutation.mutateAsync({ id, ...data }),
    deleteTimeEntryLabel: (id: string) => deleteMutation.mutateAsync(id),
    setEntryLabels: (entryId: string, labelIds: string[]) =>
      setEntryLabelsMutation.mutateAsync({ entryId, labelIds }),
    addEntryLabel: (entryId: string, input: { label_id?: string; name?: string; color?: string }) =>
      addEntryLabelMutation.mutateAsync({ entryId, input }),
    removeEntryLabel: (entryId: string, labelId: string) =>
      removeEntryLabelMutation.mutateAsync({ entryId, labelId }),
    creating: createMutation.isPending,
    updating: updateMutation.isPending,
    deleting: deleteMutation.isPending,
    setting: setEntryLabelsMutation.isPending,
    adding: addEntryLabelMutation.isPending,
    removing: removeEntryLabelMutation.isPending,
  };
}
