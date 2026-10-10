import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const appConfigOptions = () =>
  queryOptions({
    queryKey: ["config"] as const,
    queryFn: ({ signal }) => api.getConfig({ signal }),
    staleTime: 5 * 60 * 1000,
  });

// Mirrors packages/core/billing/workspace-subscription-queries.ts. The route
// resolves the workspace from X-Workspace-Slug, while the cache key prevents a
// summary from one workspace being rendered after a mobile workspace switch.
export const workspaceSubscriptionKeys = {
  all: (wsId: string | null) => ["workspace-subscriptions", wsId] as const,
  summary: (wsId: string | null) =>
    [...workspaceSubscriptionKeys.all(wsId), "summary"] as const,
};

export const workspaceSubscriptionSummaryOptions = (
  wsId: string | null,
  enabled: boolean,
) =>
  queryOptions({
    queryKey: workspaceSubscriptionKeys.summary(wsId),
    queryFn: ({ signal }) => api.getWorkspaceSubscriptionSummary({ signal }),
    enabled: !!wsId && enabled,
    // Recovery actions are Cloud-authoritative and may change immediately
    // after an upgrade, so opening a notice must revalidate this summary.
    staleTime: 0,
    retry: false,
  });
