import { useQueryClient } from "@tanstack/react-query";
import { useWSEvent } from "@/features/realtime";
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
 * Designed to be mounted once at the app-shell level so all surfaces
 * (GlobalTimerWidget, IssueTimerSection, MyTimePage) react automatically.
 */
export function useTimeTrackingSync() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");

  useWSEvent("time_entry:started", (raw: unknown) => {
    const { time_entry: entry } = raw as TimeEntryStartedPayload;
    const currentKey = queryKeys.timeTracking.current(workspaceId);
    queryClient.setQueryData(currentKey, entry);
    useTimeTrackingStore.getState().setCurrentEntry(entry);
    queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(workspaceId) });
    if (entry.issue_id) {
      queryClient.invalidateQueries({
        queryKey: queryKeys.timeTracking.issueEntries(entry.issue_id),
      });
    }
  });

  useWSEvent("time_entry:stopped", (raw: unknown) => {
    const { time_entry: entry } = raw as TimeEntryStoppedPayload;
    const currentKey = queryKeys.timeTracking.current(workspaceId);
    const current = queryClient.getQueryData(currentKey);
    if ((current as { id?: string })?.id === entry.id) {
      queryClient.setQueryData(currentKey, null);
      useTimeTrackingStore.getState().setCurrentEntry(null);
    }
    queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(workspaceId) });
    if (entry.issue_id) {
      queryClient.invalidateQueries({
        queryKey: queryKeys.timeTracking.issueEntries(entry.issue_id),
      });
    }
  });

  useWSEvent("time_entry:updated", (raw: unknown) => {
    const { time_entry: entry } = raw as TimeEntryUpdatedPayload;
    queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(workspaceId) });
    if (entry.issue_id) {
      queryClient.invalidateQueries({
        queryKey: queryKeys.timeTracking.issueEntries(entry.issue_id),
      });
    }
    const currentKey = queryKeys.timeTracking.current(workspaceId);
    const current = queryClient.getQueryData(currentKey);
    if ((current as { id?: string })?.id === entry.id) {
      queryClient.setQueryData(currentKey, entry);
      useTimeTrackingStore.getState().setCurrentEntry(entry);
    }
  });

  useWSEvent("time_entry:deleted", (raw: unknown) => {
    const { time_entry_id } = raw as TimeEntryDeletedPayload;
    const currentKey = queryKeys.timeTracking.current(workspaceId);
    const current = queryClient.getQueryData(currentKey);
    if ((current as { id?: string })?.id === time_entry_id) {
      queryClient.setQueryData(currentKey, null);
      useTimeTrackingStore.getState().setCurrentEntry(null);
    }
    queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(workspaceId) });
    // Broad invalidation for issue-level caches since we don't have issue_id in delete payload.
    queryClient.invalidateQueries({ queryKey: ["time-tracking", "issue"] });
  });
}
