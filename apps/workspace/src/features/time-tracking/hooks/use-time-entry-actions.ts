import { useState, useRef, useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { useWorkspaceStore } from "@/features/workspace";
import type {
  CreateTimeEntryRequest,
  SwitchTimeEntryRequest,
  TimeEntry,
  UpdateTimeEntryRequest,
} from "@/shared/types";
import { toast } from "sonner";
import { useTimeTrackingStore } from "../store";

interface PendingDelete {
  entry: TimeEntry;
  issueId: string | null;
}

interface PendingDeleteSnapshot {
  entry: TimeEntry;
  issueId: string | null;
  entryLists: Array<{ queryKey: readonly unknown[]; data: TimeEntry[] | undefined }>;
  issueEntries: TimeEntry[] | undefined;
  currentEntry: TimeEntry | null | undefined;
}

/**
 * Shared action layer for the time-entry recording flow.
 *
 * @param args - Current running entry, if the caller already has one in progress.
 * @returns The staged switch state plus actions for starting, confirming, clearing, creating, updating, and deleting entries.
 */
export function useTimeEntryActions(args?: { currentEntry?: TimeEntry | null }) {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  const [pendingSwitch, setPendingSwitch] = useState<SwitchTimeEntryRequest | null>(null);
  const [pendingDelete, setPendingDelete] = useState<PendingDelete | null>(null);
  const isMountedRef = useRef(true);
  const pendingDeleteTimers = useRef(new Map<string, number>());
  const pendingDeleteSnapshots = useRef(new Map<string, PendingDeleteSnapshot>());

  const isRunning = args?.currentEntry?.stop_time === null;

  function invalidateEntryQueries(issueId?: string | null) {
    if (!workspaceId) {
      return;
    }
    void queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(workspaceId) });
    void queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.current(workspaceId) });
    if (issueId) {
      void queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.issueEntries(issueId) });
    }
  }

  function restoreDeleteSnapshot(entryId: string) {
    const snapshot = pendingDeleteSnapshots.current.get(entryId);
    if (!snapshot) {
      return;
    }

    for (const { queryKey, data } of snapshot.entryLists) {
      queryClient.setQueryData(queryKey, data);
    }

    if (snapshot.issueId) {
      queryClient.setQueryData(queryKeys.timeTracking.issueEntries(snapshot.issueId), snapshot.issueEntries);
    }

    if (workspaceId) {
      queryClient.setQueryData(queryKeys.timeTracking.current(workspaceId), snapshot.currentEntry);
      useTimeTrackingStore.getState().setCurrentEntry(snapshot.currentEntry ?? null);
    }
  }

  async function commitDelete(entryId: string) {
    const snapshot = pendingDeleteSnapshots.current.get(entryId);
    if (!snapshot) {
      return;
    }

    const timeoutId = pendingDeleteTimers.current.get(entryId);
    if (timeoutId !== undefined) {
      window.clearTimeout(timeoutId);
      pendingDeleteTimers.current.delete(entryId);
    }

    try {
      await api.deleteTimeEntry(entryId);
    } catch (error) {
      restoreDeleteSnapshot(entryId);
      toast.error("Failed to delete time entry");
    } finally {
      pendingDeleteSnapshots.current.delete(entryId);
      if (isMountedRef.current) {
        setPendingDelete((current) => (current?.entry.id === entryId ? null : current));
      }
      invalidateEntryQueries(snapshot.issueId);
    }
  }

  /**
   * Starts a timer immediately when nothing is running.
   * If a timer is already active, the request is staged for explicit confirmation.
   */
  async function requestStart(data: SwitchTimeEntryRequest) {
    if (isRunning) {
      setPendingSwitch(data);
      return null;
    }

    return api.startTimeEntry(data satisfies CreateTimeEntryRequest);
  }

  /** Confirms a staged switch request and clears the pending state on success. */
  async function confirmSwitch() {
    if (!pendingSwitch) {
      return null;
    }

    const started = await api.switchTimeEntry(pendingSwitch);
    setPendingSwitch(null);
    return started;
  }

  /** Creates a historical time entry. */
  async function createHistoricalEntry(data: CreateTimeEntryRequest) {
    const entry = await api.startTimeEntry(data);
    invalidateEntryQueries(entry.issue_id);
    return entry;
  }

  /** Updates a time entry. */
  async function updateTimeEntry(id: string, data: UpdateTimeEntryRequest) {
    const entry = await api.updateTimeEntry(id, data);
    invalidateEntryQueries(entry.issue_id);
    if (workspaceId) {
      const currentKey = queryKeys.timeTracking.current(workspaceId);
      const currentEntry = queryClient.getQueryData<TimeEntry | null>(currentKey);
      if (currentEntry?.id === entry.id) {
        queryClient.setQueryData(currentKey, entry);
        useTimeTrackingStore.getState().setCurrentEntry(entry);
      }
    }
    return entry;
  }

  /**
   * Stages a delete for undo window (5 seconds).
   * The entry is not immediately deleted, allowing the user to undo.
   */
  function requestDelete(entry: TimeEntry, issueId?: string | null) {
    const resolvedIssueId = issueId ?? entry.issue_id ?? null;
    const entryLists = workspaceId
      ? queryClient.getQueriesData<TimeEntry[]>({ queryKey: queryKeys.timeTracking.entries(workspaceId) })
      : [];
    const currentEntry = workspaceId
      ? queryClient.getQueryData<TimeEntry | null>(queryKeys.timeTracking.current(workspaceId))
      : undefined;

    pendingDeleteSnapshots.current.set(entry.id, {
      entry,
      issueId: resolvedIssueId,
      entryLists: entryLists.map(([queryKey, data]) => ({ queryKey, data })),
      issueEntries: resolvedIssueId
        ? queryClient.getQueryData<TimeEntry[]>(queryKeys.timeTracking.issueEntries(resolvedIssueId))
        : undefined,
      currentEntry,
    });

    if (workspaceId) {
      queryClient.setQueriesData<TimeEntry[]>(
        { queryKey: queryKeys.timeTracking.entries(workspaceId) },
        (existing) => (Array.isArray(existing) ? existing.filter((candidate) => candidate.id !== entry.id) : existing),
      );
      if (currentEntry?.id === entry.id) {
        queryClient.setQueryData(queryKeys.timeTracking.current(workspaceId), null);
        useTimeTrackingStore.getState().setCurrentEntry(null);
      }
    }

    if (resolvedIssueId) {
      queryClient.setQueryData<TimeEntry[]>(
        queryKeys.timeTracking.issueEntries(resolvedIssueId),
        (existing) => (Array.isArray(existing) ? existing.filter((candidate) => candidate.id !== entry.id) : existing),
      );
    }

    setPendingDelete({ entry, issueId: resolvedIssueId });

    const timeoutId = window.setTimeout(() => {
      void commitDelete(entry.id);
    }, 5000);
    pendingDeleteTimers.current.set(entry.id, timeoutId);

    toast.success("Time entry deleted", {
      action: {
        label: "Undo",
        onClick: () => {
          undoDelete(entry);
        },
      },
    });
  }

  /** Confirms a staged delete and removes it from pending state. */
  async function confirmDelete(entryOrId?: TimeEntry | string) {
    const entryId = typeof entryOrId === "string"
      ? entryOrId
      : entryOrId?.id ?? pendingDelete?.entry.id;
    if (!entryId) {
      return;
    }
    await commitDelete(entryId);
  }

  /** Cancels a staged delete before confirmation. */
  function undoDelete(entryOrId?: TimeEntry | string) {
    const entryId = typeof entryOrId === "string"
      ? entryOrId
      : entryOrId?.id ?? pendingDelete?.entry.id;
    if (!entryId) {
      return;
    }

    const timeoutId = pendingDeleteTimers.current.get(entryId);
    if (timeoutId !== undefined) {
      window.clearTimeout(timeoutId);
      pendingDeleteTimers.current.delete(entryId);
    }

    restoreDeleteSnapshot(entryId);
    pendingDeleteSnapshots.current.delete(entryId);
    if (isMountedRef.current) {
      setPendingDelete((current) => (current?.entry.id === entryId ? null : current));
    }
  }

  // Cleanup on unmount.
  useEffect(() => {
    return () => {
      isMountedRef.current = false;
    };
  }, []);

  return {
    pendingSwitch,
    setPendingSwitch,
    requestStart,
    confirmSwitch,
    createHistoricalEntry,
    updateTimeEntry,
    pendingDelete,
    requestDelete,
    confirmDelete,
    undoDelete,
  };
}
