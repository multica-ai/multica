import type { QueryClient } from "@tanstack/react-query";
import { issueKeys } from "./queries";
import { labelKeys } from "../labels/queries";
import {
  addIssueToBuckets,
  findIssueLocation,
  patchIssueInBuckets,
  removeIssueFromBuckets,
} from "./cache-helpers";
import type { Issue, IssueLabelsResponse, Label } from "../types";
import type { ListIssuesCache } from "../types";

export function onIssueCreated(
  qc: QueryClient,
  wsId: string,
  issue: Issue,
) {
  qc.setQueryData<ListIssuesCache>(issueKeys.list(wsId), (old) =>
    old ? addIssueToBuckets(old, issue) : old,
  );
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
  const listData = qc.getQueryData<ListIssuesCache>(issueKeys.list(wsId));
  const detailData = qc.getQueryData<Issue>(issueKeys.detail(wsId, issue.id));
  const oldParentId =
    detailData?.parent_issue_id ??
    (listData ? findIssueLocation(listData, issue.id)?.issue.parent_issue_id : null) ??
    null;
  // The NEW parent comes from the WS payload when parent_issue_id changed
  const newParentId = issue.parent_issue_id ?? null;
  const parentChanged =
    issue.parent_issue_id !== undefined && newParentId !== oldParentId;

  qc.setQueryData<ListIssuesCache>(issueKeys.list(wsId), (old) =>
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
 * Patch an issue's labels in-place across the list cache, my-issues caches,
 * the detail cache, and the per-issue label cache. Triggered by the
 * `issue_labels:changed` WS event after attach/detach so list/board chips
 * and the issue-detail Properties LabelPicker update without a refetch.
 *
 * The byIssue cache backs `LabelPicker`; without patching it, externally
 * driven label changes (agents, other tabs) leave the picker stale until it
 * remounts — `staleTime: Infinity` + `refetchOnWindowFocus: false` (see
 * `query-client.ts`) means focus changes won't recover it.
 */
export function onIssueLabelsChanged(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  labels: Label[],
) {
  qc.setQueryData<ListIssuesCache>(issueKeys.list(wsId), (old) =>
    old ? patchIssueInBuckets(old, issueId, { labels }) : old,
  );
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
    old ? { ...old, labels } : old,
  );
  qc.setQueryData<IssueLabelsResponse>(labelKeys.byIssue(wsId, issueId), (old) =>
    old ? { ...old, labels } : old,
  );
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
}

export function onIssueDeleted(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  // Look up the issue before removing it to check for parent_issue_id
  const listData = qc.getQueryData<ListIssuesCache>(issueKeys.list(wsId));
  const deleted = listData ? findIssueLocation(listData, issueId)?.issue : undefined;

  qc.setQueryData<ListIssuesCache>(issueKeys.list(wsId), (old) =>
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
