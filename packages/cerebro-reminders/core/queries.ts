import { queryOptions } from "@tanstack/react-query";

import { getReminder, listReminders } from "./api";

// Query keys, workspace-scoped so switching workspaces swaps the cache.
export const reminderKeys = {
  all: (wsId: string) => ["cerebro-reminders", wsId] as const,
  list: (wsId: string) => [...reminderKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...reminderKeys.all(wsId), "detail", id] as const,
};

export function remindersListOptions(wsId: string) {
  return queryOptions({
    queryKey: reminderKeys.list(wsId),
    queryFn: () => listReminders(),
    enabled: Boolean(wsId),
  });
}

export function reminderDetailOptions(wsId: string, id: string | null) {
  return queryOptions({
    queryKey: reminderKeys.detail(wsId, id ?? ""),
    queryFn: () => getReminder(id ?? ""),
    enabled: Boolean(wsId && id),
  });
}
