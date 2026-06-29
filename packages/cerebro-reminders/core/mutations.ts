import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { appBadgeKeys, inboxKeys } from "@multica/core/inbox/queries";

import {
  createReminder,
  deleteReminder,
  markReminderDone,
  snoozeReminder,
} from "./api";
import { reminderKeys } from "./queries";
import type { CreateReminderInput } from "./types";

// FIR-2278: snooze/done/delete archive the fired reminder's inbox row server-
// side, so the inbox feed and the OS badge must refetch too — not only the
// reminder overview.
function invalidateReminderAndInbox(qc: QueryClient, wsId: string) {
  void qc.invalidateQueries({ queryKey: reminderKeys.all(wsId) });
  void qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
  void qc.invalidateQueries({ queryKey: appBadgeKeys.unreadCount() });
}

// All mutations invalidate the whole reminder cache for the workspace so both
// the overview list and any open reminder card re-fetch.
export function useCreateReminder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input: CreateReminderInput) => createReminder(input),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: reminderKeys.all(wsId) });
    },
  });
}

export function useSnoozeReminder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, until }: { id: string; until: string }) =>
      snoozeReminder(id, until),
    onSettled: () => {
      invalidateReminderAndInbox(qc, wsId);
    },
  });
}

export function useMarkReminderDone() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => markReminderDone(id),
    onSettled: () => {
      invalidateReminderAndInbox(qc, wsId);
    },
  });
}

export function useDeleteReminder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => deleteReminder(id),
    onSettled: () => {
      invalidateReminderAndInbox(qc, wsId);
    },
  });
}
