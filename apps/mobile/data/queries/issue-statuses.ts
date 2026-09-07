/**
 * Workspace issue status catalog (MUL-6243).
 *
 * Key shape mirrors web's `packages/core/issue-statuses/queries.ts` —
 * `["issue-statuses", wsId, "list"]` — so the cross-platform mental model
 * stays the same. Keying on wsId means a workspace switch naturally refetches.
 *
 * The catalog only changes when an admin edits it, which is rare, so a generous
 * `staleTime` keeps it off the critical path of every render that needs a
 * status label. Mobile has no catalog mutations (settings live on web), so
 * nothing local ever has to invalidate it.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const issueStatusKeys = {
  all: (wsId: string | null) => ["issue-statuses", wsId] as const,
  list: (wsId: string | null) =>
    [...issueStatusKeys.all(wsId), "list"] as const,
};

export const issueStatusListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: issueStatusKeys.list(wsId),
    // Archived entries included on purpose — see `api.listIssueStatuses`.
    queryFn: async ({ signal }) => {
      const res = await api.listIssueStatuses(true, { signal });
      return res.statuses;
    },
    enabled: !!wsId,
    staleTime: 5 * 60_000,
  });
