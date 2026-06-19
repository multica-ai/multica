import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";

import {
  createReminder,
  deleteReminder,
  markReminderDone,
  snoozeReminder,
} from "./api";
import { reminderKeys } from "./queries";
import type { CreateReminderInput } from "./types";

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
      void qc.invalidateQueries({ queryKey: reminderKeys.all(wsId) });
    },
  });
}

export function useMarkReminderDone() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => markReminderDone(id),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: reminderKeys.all(wsId) });
    },
  });
}

export function useDeleteReminder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => deleteReminder(id),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: reminderKeys.all(wsId) });
    },
  });
}
