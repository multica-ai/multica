import type {
  InfiniteData,
  QueryClient,
  QueryKey,
} from "@tanstack/react-query";
import {
  agentActivityKeys,
  agentRunCountsKeys,
  agentTaskSnapshotKeys,
  agentTasksKeys,
} from "../agents/queries";
import { labelKeys } from "../labels/queries";
import type {
  Issue,
  ListIssuesCache,
  ListIssuesResponse,
} from "../types";
import { useRecentContextStore } from "../chat/recent-context-store";
import { findIssueLocation, removeIssueFromBuckets } from "./cache-helpers";
import { issueKeys } from "./queries";
import { useRecentIssuesStore } from "./stores/recent-issues-store";

export type DeletedIssueCacheMetadata = {
  parentIssueIds: string[];
  /**
   * The deleted issue's human-readable identifier ("MUL-123"), when any cache
   * still held the row when this was collected.
   *
   * Two caches are keyed by the identifier rather than the UUID and would
   * otherwise outlive the issue: `issueKeys.identifier` (the `MUL-123`
   * autolink resolver) and the identifier-keyed `issueKeys.detail` alias
   * `useCanonicalIssue` mirrors. Both must be collected BEFORE the pruning
   * pass runs, which is why this rides along with the parent ids instead of
   * being looked up at cleanup time.
   */
  identifier: string | null;
};

function collectParentId(
  parentIssueIds: Set<string>,
  parentId: string | null | undefined,
) {
  if (parentId) parentIssueIds.add(parentId);
}

function parentIdFromChildrenKey(key: QueryKey) {
  const parentId = key[key.length - 1];
  return typeof parentId === "string" ? parentId : null;
}

export function collectDeletedIssueCacheMetadata(
  qc: QueryClient,
  wsId: string,
  issueId: string,
): DeletedIssueCacheMetadata {
  const parentIssueIds = new Set<string>();
  let identifier: string | null = null;

  // Every cache below can carry the row, so each one is also a chance to learn
  // the identifier. First hit wins — the value never changes for an issue.
  const collectFromRow = (issue: Issue | undefined) => {
    if (!issue) return;
    collectParentId(parentIssueIds, issue.parent_issue_id);
    identifier ??= issue.identifier || null;
  };

  // Every detail entry, not just the UUID-keyed one: `useCanonicalIssue`
  // mirrors the row into an identifier-keyed alias, and that alias can be the
  // only detail entry a session holds.
  for (const [, data] of qc.getQueriesData<Issue>({
    queryKey: [...issueKeys.all(wsId), "detail"],
  })) {
    if (data?.id === issueId) collectFromRow(data);
  }

  // The identifier resolver caches the whole Issue under the identifier key, so
  // it can be the ONLY cache in the client that knows this issue exists — a page
  // whose single reference to it is a `MUL-123` autolink inside a comment. A
  // cross-client delete carries just the UUID, so without this scan the metadata
  // has no identifier, nothing gets removed, and the chip resolves as a live
  // link for the rest of the session.
  for (const [key, data] of qc.getQueriesData<Issue | null>({
    queryKey: [...issueKeys.all(wsId), "identifier"],
  })) {
    if (data?.id !== issueId) continue;
    collectFromRow(data);
    const fromKey = key[key.length - 1];
    if (!identifier && typeof fromKey === "string") identifier = fromKey;
  }

  for (const [, data] of qc.getQueriesData<ListIssuesCache>({
    queryKey: issueKeys.list(wsId),
  })) {
    collectFromRow(data ? findIssueLocation(data, issueId)?.issue : undefined);
  }

  for (const [, data] of qc.getQueriesData<
    InfiniteData<ListIssuesResponse, number>
  >({ queryKey: issueKeys.flatAll(wsId) })) {
    for (const page of data?.pages ?? []) {
      collectFromRow(page.issues.find((issue) => issue.id === issueId));
    }
  }

  for (const [, data] of qc.getQueriesData<ListIssuesCache>({
    queryKey: issueKeys.myAll(wsId),
  })) {
    collectFromRow(data ? findIssueLocation(data, issueId)?.issue : undefined);
  }

  for (const [key, data] of qc.getQueriesData<Issue[]>({
    queryKey: [...issueKeys.all(wsId), "children"],
  })) {
    const child = data?.find((issue) => issue.id === issueId);
    if (!child) continue;
    collectFromRow(child);
    collectParentId(parentIssueIds, parentIdFromChildrenKey(key));
  }

  return { parentIssueIds: Array.from(parentIssueIds), identifier };
}

export function pruneDeletedIssueFromListCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  for (const [key] of qc.getQueriesData<ListIssuesCache>({
    queryKey: issueKeys.list(wsId),
  })) {
    qc.setQueryData<ListIssuesCache>(key, (old) =>
      old ? removeIssueFromBuckets(old, issueId) : old,
    );
  }

  for (const [key] of qc.getQueriesData<ListIssuesCache>({
    queryKey: issueKeys.myAll(wsId),
  })) {
    qc.setQueryData<ListIssuesCache>(key, (old) =>
      old ? removeIssueFromBuckets(old, issueId) : old,
    );
  }

  for (const [key, data] of qc.getQueriesData<
    InfiniteData<ListIssuesResponse, number>
  >({ queryKey: issueKeys.flatAll(wsId) })) {
    if (!data?.pages) continue;
    const found = data.pages.some((page) =>
      page.issues.some((issue) => issue.id === issueId),
    );
    if (!found) continue;
    qc.setQueryData<InfiniteData<ListIssuesResponse, number>>(key, {
      ...data,
      pages: data.pages.map((page) => ({
        ...page,
        total: Math.max(0, page.total - 1),
        issues: page.issues.filter((issue) => issue.id !== issueId),
      })),
    });
  }
}

export function pruneDeletedIssueFromParentChildrenCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  metadata: Pick<DeletedIssueCacheMetadata, "parentIssueIds">,
) {
  for (const parentId of metadata.parentIssueIds) {
    qc.setQueryData<Issue[]>(issueKeys.children(wsId, parentId), (old) =>
      old?.filter((issue) => issue.id !== issueId),
    );
  }
}

export function invalidateDeletedIssueParentCaches(
  qc: QueryClient,
  wsId: string,
  metadata: Pick<DeletedIssueCacheMetadata, "parentIssueIds">,
) {
  if (metadata.parentIssueIds.length === 0) return;
  for (const parentId of metadata.parentIssueIds) {
    qc.invalidateQueries({ queryKey: issueKeys.children(wsId, parentId) });
  }
  qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.childrenByParentsAll(wsId) });
}

export function invalidateDeletedIssueDependentCaches(
  qc: QueryClient,
  wsId: string,
) {
  qc.invalidateQueries({ queryKey: agentTaskSnapshotKeys.list(wsId) });
  qc.invalidateQueries({ queryKey: agentActivityKeys.last30d(wsId) });
  qc.invalidateQueries({ queryKey: agentRunCountsKeys.last30d(wsId) });
  qc.invalidateQueries({ queryKey: agentTasksKeys.all(wsId) });
}

export function invalidateIssueScopedCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
  qc.invalidateQueries({ queryKey: issueKeys.reactions(issueId) });
  qc.invalidateQueries({ queryKey: issueKeys.subscribers(issueId) });
  qc.invalidateQueries({ queryKey: issueKeys.usage(issueId) });
  qc.invalidateQueries({ queryKey: issueKeys.attachments(issueId) });
  qc.invalidateQueries({ queryKey: issueKeys.tasks(issueId) });
  qc.invalidateQueries({ queryKey: issueKeys.children(wsId, issueId) });
  qc.invalidateQueries({ queryKey: labelKeys.byIssue(wsId, issueId) });
}

export function cleanupDeletedIssueCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  metadata = collectDeletedIssueCacheMetadata(qc, wsId, issueId),
) {
  pruneDeletedIssueFromListCaches(qc, wsId, issueId);
  pruneDeletedIssueFromParentChildrenCaches(qc, wsId, issueId, metadata);
  invalidateDeletedIssueParentCaches(qc, wsId, metadata);

  qc.removeQueries({ queryKey: issueKeys.detail(wsId, issueId) });
  // Identifier-keyed entries. `issueKeys.detail(wsId, "MUL-123")` is the alias
  // `useCanonicalIssue` mirrors so an identifier URL resolves without a second
  // request, and `issueKeys.identifier` backs the `MUL-123` autolink chip.
  // Neither is reachable from the UUID key, so dropping only the UUID entry
  // leaves a deleted issue rendering as a live chip (and resolving as a live
  // route) until those entries fall out of gc.
  if (metadata.identifier) {
    qc.removeQueries({ queryKey: issueKeys.detail(wsId, metadata.identifier) });
    qc.removeQueries({ queryKey: issueKeys.identifier(wsId, metadata.identifier) });
  }
  qc.removeQueries({ queryKey: issueKeys.timeline(issueId) });
  qc.removeQueries({ queryKey: issueKeys.reactions(issueId) });
  qc.removeQueries({ queryKey: issueKeys.subscribers(issueId) });
  qc.removeQueries({ queryKey: issueKeys.usage(issueId) });
  qc.removeQueries({ queryKey: issueKeys.attachments(issueId) });
  qc.removeQueries({ queryKey: issueKeys.tasks(issueId) });
  qc.removeQueries({ queryKey: issueKeys.children(wsId, issueId) });
  qc.removeQueries({ queryKey: labelKeys.byIssue(wsId, issueId) });

  qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.flatAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
  // Project Gantt cache lives outside `myAll`, so it needs an explicit
  // refresh when an issue is removed — the deleted row may have been a
  // scheduled bar visible right now.
  qc.invalidateQueries({ queryKey: issueKeys.projectGanttAll(wsId) });
  invalidateDeletedIssueDependentCaches(qc, wsId);

  // Both stores persist to localStorage and survive reloads, so a deleted id
  // left behind keeps firing 404s on every open — the Cmd+K command bar for
  // recent issues, the chat composer's `@` context picker for recent contexts
  // (which also falls back to its stale snapshot when the fetch misses, so the
  // dead issue stays *visible*, not just requested). Both the delete mutation
  // and the WS delete event flow through here, so a single call covers
  // self-delete and cross-client delete.
  useRecentIssuesStore.getState().forgetIssue(wsId, issueId);
  useRecentContextStore.getState().forgetContext(wsId, {
    type: "issue",
    id: issueId,
  });
}
