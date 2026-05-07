import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { WSClient } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { useWorkspaceStore } from "@/features/workspace";
import { useTimeTrackingStore } from "../store";
import type {
  TimeEntryStartedPayload,
  TimeEntryStoppedPayload,
  TimeEntryUpdatedPayload,
  TimeEntryDeletedPayload,
} from "@/shared/types";

/**
 * Subscribes to WebSocket time_entry events and keeps the TanStack Query caches
 * and the Zustand store in sync.
 *
 * Follows the same pattern as useRealtimeSync: receives WSClient directly
 * rather than using useWSEvent/useWS, because it is called inside WSProvider
 * where the WSContext is not yet in scope.
 */
export function useTimeTrackingSync(ws: WSClient | null) {
  const queryClient = useQueryClient();

  useEffect(() => {
    if (!ws) return;

    // Read workspace id lazily so the effect doesn't re-run on workspace changes.
    const wid = () => useWorkspaceStore.getState().workspace?.id ?? "";

    const unsubStarted = ws.on("time_entry:started", (raw) => {
      const { time_entry: entry } = raw as TimeEntryStartedPayload;
      const w = wid();
      // Only update the current-timer slot when the entry is actually running.
      // Manual entries (stop_time set, duration_seconds > 0) should not become currentEntry.
      if (entry.duration_seconds < 0) {
        queryClient.setQueryData(queryKeys.timeTracking.current(w), entry);
        useTimeTrackingStore.getState().setCurrentEntry(entry);
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(w) });
      if (entry.issue_id) {
        void queryClient.invalidateQueries({
          queryKey: queryKeys.timeTracking.issueEntries(entry.issue_id),
        });
      }
    });

    const unsubStopped = ws.on("time_entry:stopped", (raw) => {
      const { time_entry: entry } = raw as TimeEntryStoppedPayload;
      const w = wid();
      const currentKey = queryKeys.timeTracking.current(w);
      const current = queryClient.getQueryData(currentKey);
      if ((current as { id?: string })?.id === entry.id) {
        queryClient.setQueryData(currentKey, null);
        useTimeTrackingStore.getState().setCurrentEntry(null);
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(w) });
      if (entry.issue_id) {
        void queryClient.invalidateQueries({
          queryKey: queryKeys.timeTracking.issueEntries(entry.issue_id),
        });
      }
    });

    const unsubUpdated = ws.on("time_entry:updated", (raw) => {
      const { time_entry: entry } = raw as TimeEntryUpdatedPayload;
      const w = wid();
      void queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(w) });
      if (entry.issue_id) {
        void queryClient.invalidateQueries({
          queryKey: queryKeys.timeTracking.issueEntries(entry.issue_id),
        });
      }
      const currentKey = queryKeys.timeTracking.current(w);
      const current = queryClient.getQueryData(currentKey);
      if ((current as { id?: string })?.id === entry.id) {
        queryClient.setQueryData(currentKey, entry);
        useTimeTrackingStore.getState().setCurrentEntry(entry);
      }
    });

    const unsubDeleted = ws.on("time_entry:deleted", (raw) => {
      const { time_entry_id } = raw as TimeEntryDeletedPayload;
      const w = wid();
      const currentKey = queryKeys.timeTracking.current(w);
      const current = queryClient.getQueryData(currentKey);
      if ((current as { id?: string })?.id === time_entry_id) {
        queryClient.setQueryData(currentKey, null);
        useTimeTrackingStore.getState().setCurrentEntry(null);
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(w) });
      // Broad invalidation for all issue-level caches (no issue_id in delete payload).
      void queryClient.invalidateQueries({ queryKey: ["time-tracking", "issue"] });
    });

    return () => {
      unsubStarted();
      unsubStopped();
      unsubUpdated();
      unsubDeleted();
    };
  }, [ws, queryClient]);
}
