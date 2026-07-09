import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const notificationPreferenceKeys = {
  all: (wsId: string) => ["notification-preferences", wsId] as const,
};

/**
 * `workspaceSlug` scopes the underlying request to a specific workspace via
 * the `X-Workspace-Slug` override. The query key stays keyed on `wsId` (slug
 * and id are 1:1), so the cache entry is still per-workspace; the slug only
 * ensures a cold-cache fetch reads the intended workspace rather than the
 * active one. Omit it to follow the active workspace (the Settings page).
 */
export function notificationPreferenceOptions(wsId: string, workspaceSlug?: string) {
  return queryOptions({
    queryKey: notificationPreferenceKeys.all(wsId),
    queryFn: () => api.getNotificationPreferences(workspaceSlug),
  });
}
