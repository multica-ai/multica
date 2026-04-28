import type { QueryClient } from "@tanstack/react-query";
import { issueKeys } from "./queries";
import {
  addIssueToBuckets,
  findIssueLocation,
  patchIssueInBuckets,
  removeIssueFromBuckets,
} from "./cache-helpers";
import type { Issue, Label } from "../types";
import type { ListIssuesCache } from "../types";

/** Apply an updater to every workspace list cache (all filter combinations). */
function updateAllListCaches(
  qc: QueryClient,
  wsId: string,
  updater: (old: ListIssuesCache | undefined) => ListIssuesCache | undefined,
) {
  qc.setQueriesData<ListIssuesCache>(
    { queryKey: issueKeys.listPrefix(wsId) },
    updater,
  );
}

function findIssueAcrossListCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  const entries = qc.getQueriesData<ListIssuesCache>({
    queryKey: issueKeys.listPrefix(wsId),
  });
  for (const [, cache] of entries) {
    if (!cache) continue;
    const loc = findIssueLocation(cache, issueId);
    if (loc) return loc.issue;
  }
  return undefined;
}

export function onIssueCreated(
  qc: QueryClient,
  wsId: string,
  issue: Issue,
) {
  // Insert into every active list cache. Filter mismatches are corrected
  // by the trailing `invalidateQueries` on the listPrefix.
  updateAllListCaches(qc, wsId, (old) =>
    old ? addIssueToBuckets(old, issue) : old,
  );
  qc.invalidateQueries({ queryKey: issueKeys.listPrefix(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  if (issue.parent_issue_id) {
    qc.invalidateQueries({ queryKey: issueKeys.children(wsId, issue.parent_issue_id) });
    qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
  }
}

export function onIssueUpdated(
  qc: QueryClient,
  wsId: string,
  issue: Partial<Issue> & { id: string },
) {
  // Look up the OLD parent before mutating list state, so we can keep
  // the parent's children cache in sync (powers the sub-issues list
  // shown on the parent issue page).
  const detailData = qc.getQueryData<Issue>(issueKeys.detail(wsId, issue.id));
  const oldParentId =
    detailData?.parent_issue_id ??
    findIssueAcrossListCaches(qc, wsId, issue.id)?.parent_issue_id ??
    null;
  // The NEW parent comes from the WS payload when parent_issue_id changed
  const newParentId = issue.parent_issue_id ?? null;
  const parentChanged =
    issue.parent_issue_id !== undefined && newParentId !== oldParentId;

  updateAllListCaches(qc, wsId, (old) =>
    old ? patchIssueInBuckets(old, issue.id, issue) : old,
  );
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issue.id), (old) =>
    old ? { ...old, ...issue } : old,
  );

  // Invalidate old parent's children (issue was removed from it)
  if (oldParentId) {
    if (parentChanged) {
      qc.invalidateQueries({ queryKey: issueKeys.children(wsId, oldParentId) });
    } else {
      qc.setQueryData<Issue[]>(issueKeys.children(wsId, oldParentId), (old) =>
        old?.map((c) => (c.id === issue.id ? { ...c, ...issue } : c)),
      );
    }
  }
  // Invalidate new parent's children (issue was added to it)
  if (newParentId && parentChanged) {
    qc.invalidateQueries({ queryKey: issueKeys.children(wsId, newParentId) });
  }
  if (oldParentId || newParentId) {
    if (issue.status !== undefined || issue.parent_issue_id !== undefined) {
      qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
    }
  }
}

/**
 * Patch an issue's `labels` field in-place across the list cache, my-issues
 * caches, and the detail cache. Triggered by the `issue_labels:changed` WS
 * event after attach/detach so list/board chips update without a refetch.
 *
 * Also invalidates the listPrefix because a label change can move issues in
 * or out of an active label filter.
 */
export function onIssueLabelsChanged(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  labels: Label[],
) {
  updateAllListCaches(qc, wsId, (old) =>
    old ? patchIssueInBuckets(old, issueId, { labels }) : old,
  );
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
    old ? { ...old, labels } : old,
  );
  qc.invalidateQueries({ queryKey: issueKeys.listPrefix(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
}

export function onIssueDeleted(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  // Look up the issue before removing it to check for parent_issue_id.
  const deleted = findIssueAcrossListCaches(qc, wsId, issueId);

  updateAllListCaches(qc, wsId, (old) =>
    old ? removeIssueFromBuckets(old, issueId) : old,
  );
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  qc.removeQueries({ queryKey: issueKeys.detail(wsId, issueId) });
  qc.removeQueries({ queryKey: issueKeys.timeline(issueId) });
  qc.removeQueries({ queryKey: issueKeys.reactions(issueId) });
  qc.removeQueries({ queryKey: issueKeys.subscribers(issueId) });
  qc.removeQueries({ queryKey: issueKeys.children(wsId, issueId) });
  if (deleted?.parent_issue_id) {
    qc.invalidateQueries({ queryKey: issueKeys.children(wsId, deleted.parent_issue_id) });
    qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
  }
}
