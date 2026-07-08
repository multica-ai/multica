/**
 * Notification preferences query — mirrors packages/core/notification-preferences/queries.ts
 * but binds to mobile's api client + key shape (per mobile CLAUDE.md
 * "Mobile-owned updaters" rule: don't import web queries, copy the design).
 *
 * The cache key includes wsId so workspace switching auto-invalidates.
 * Backend endpoint is workspace-scoped via X-Workspace-Slug header (set by
 * ApiClient.fetch), so the URL itself has no workspace path segment.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const notificationPreferenceKeys = {
  all: (wsId: string | null) => ["notification-preferences", wsId] as const,
};

export const notificationPreferenceOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: notificationPreferenceKeys.all(wsId),
    queryFn: ({ signal }) => api.getNotificationPreferences({ signal }),
    enabled: !!wsId,
  });
