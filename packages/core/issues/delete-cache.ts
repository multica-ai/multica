import type { QueryClient, QueryKey } from "@tanstack/react-query";
import {
  agentActivityKeys,
  agentRunCountsKeys,
  agentTaskSnapshotKeys,
  agentTasksKeys,
} from "../agents/queries";
import { labelKeys } from "../labels/queries";
import type { Issue, ListIssuesCache } from "../types";
import { findIssueLocation, removeIssueFromBuckets } from "./cache-helpers";
import { issueKeys } from "./queries";

export type DeletedIssueCacheMetadata = {
  parentIssueIds: string[];
};

function collectParentId(
  parentIssueIds: Set<string>,
  parentId: string | null | undefined,
) {
  if (parentId) parentIssueIds.add(parentId);
}

function collectParentFromListCache(
  parentIssueIds: Set<string>,
  data: ListIssuesCache | undefined,
  issueId: string,
) {
  const parentId = data
    ? findIssueLocation(data, issueId)?.issue.parent_issue_id
    : undefined;
  collectParentId(parentIssueIds, parentId);
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

  const detail = qc.getQueryData<Issue>(issueKeys.detail(wsId, issueId));
  collectParentId(parentIssueIds, detail?.parent_issue_id);

  collectParentFromListCache(
    parentIssueIds,
    qc.getQueryData<ListIssuesCache>(issueKeys.list(wsId)),
    issueId,
  );

  for (const [, data] of qc.getQueriesData<ListIssuesCache>({
    queryKey: issueKeys.myAll(wsId),
  })) {
    collectParentFromListCache(parentIssueIds, data, issueId);
  }

  for (const [key, data] of qc.getQueriesData<Issue[]>({
    queryKey: [...issueKeys.all(wsId), "children"],
  })) {
    const child = data?.find((issue) => issue.id === issueId);
    if (!child) continue;
    collectParentId(parentIssueIds, child.parent_issue_id);
    collectParentId(parentIssueIds, parentIdFromChildrenKey(key));
  }

  return { parentIssueIds: Array.from(parentIssueIds) };
}

export function pruneDeletedIssueFromListCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  qc.setQueryData<ListIssuesCache>(issueKeys.list(wsId), (old) =>
    old ? removeIssueFromBuckets(old, issueId) : old,
  );

  for (const [key] of qc.getQueriesData<ListIssuesCache>({
    queryKey: issueKeys.myAll(wsId),
  })) {
    qc.setQueryData<ListIssuesCache>(key, (old) =>
      old ? removeIssueFromBuckets(old, issueId) : old,
    );
  }
}

export function pruneDeletedIssueFromParentChildrenCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  metadata: DeletedIssueCacheMetadata,
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
  metadata: DeletedIssueCacheMetadata,
) {
  if (metadata.parentIssueIds.length === 0) return;
  for (const parentId of metadata.parentIssueIds) {
    qc.invalidateQueries({ queryKey: issueKeys.children(wsId, parentId) });
  }
  qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
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
  invalidateDeletedIssueDependentCaches(qc, wsId);
}
