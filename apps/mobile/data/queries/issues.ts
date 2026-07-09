/**
 * Issue queries — workspace-wide list, single-issue detail, timeline.
 * Mobile-owned; mirrors a strict subset of packages/core/issues/queries.ts.
 *
 * Query keys live in ./issue-keys so detail / timeline / list / myList all
 * sit under the `issues/<wsId>` prefix — WS handlers can invalidate the
 * whole subtree with one call when needed.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";
import { issueKeys } from "./issue-keys";

export { issueKeys } from "./issue-keys";

/**
 * Workspace-wide issue list. Backend filters by `X-Workspace-Slug` header
 * (root CLAUDE.md "All queries filter by workspace_id"), so we pass an
 * empty params object — server returns every issue the user is allowed to
 * see in the current workspace.
 *
 * Cache shape: flat `Issue[]` (we strip `.issues` from the response) so
 * the WS updaters can patch this list with the same shape as
 * myIssueListOptions. Pagination is deferred — web's `IssuesPage` also
 * fetches all in one shot today (`packages/views/issues/components/
 * issues-page.tsx:30`).
 */
export const issueListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: issueKeys.list(wsId),
    queryFn: async ({ signal }) => {
      const res = await api.listIssues({}, { signal });
      return res.issues;
    },
    enabled: !!wsId,
  });

export const issueDetailOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: issueKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getIssue(id, { signal }),
    enabled: !!wsId && !!id,
  });

/**
 * Single query over the full issue timeline (ASC, oldest first). Mirrors
 * web's `issueTimelineOptions` post-#2322 — server returns the whole list
 * in one shot, client-side pagination was deleted.
 */
export const issueTimelineOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: issueKeys.timeline(wsId, id),
    queryFn: ({ signal }) => api.listTimeline(id, { signal }),
    enabled: !!wsId && !!id,
  });

/**
 * Currently-running tasks for an issue. WS events (task:queued/dispatch/
 * progress/completed/failed/cancelled) patch this cache directly via
 * `issue-ws-updaters.ts`, so refetches are rare in practice. The fetch is
 * still wired so the initial open + reconnect-invalidate path works.
 */
export const issueActiveTasksOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: issueKeys.activeTasks(wsId, id),
    queryFn: ({ signal }) => api.listActiveTasksForIssue(id, { signal }),
    enabled: !!wsId && !!id,
  });

/**
 * All tasks (any status) for an issue — drives the Runs sheet history
 * section. Same patching strategy as active tasks: WS moves entries between
 * the two caches without refetching.
 */
export const issueTasksOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: issueKeys.tasks(wsId, id),
    queryFn: ({ signal }) => api.listTasksByIssue(id, { signal }),
    enabled: !!wsId && !!id,
  });

/**
 * File attachments uploaded to this issue or any of its comments. The
 * mobile markdown renderer reads this list to resolve `mc://file/<id>`
 * URIs in image markdown to a real HTTPS `download_url` that iOS can
 * actually load — see `lib/markdown/markdown-image.tsx`.
 *
 * TanStack Query dedupes the request across concurrent callers, so it's
 * safe for both IssueDescription and CommentCard to fetch the same
 * issue's attachments — only one network request fires.
 */
export const issueAttachmentsOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: issueKeys.attachments(wsId, id),
    queryFn: ({ signal }) => api.listAttachments(id, { signal }),
    enabled: !!wsId && !!id,
  });
