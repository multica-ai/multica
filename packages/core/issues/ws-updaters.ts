import type { QueryClient } from "@tanstack/react-query";
import { issueKeys } from "./queries";
import { labelKeys } from "../labels/queries";
import {
  addIssueToBuckets,
  findIssueLocation,
  patchIssueInBuckets,
} from "./cache-helpers";
import { cleanupDeletedIssueCaches } from "./delete-cache";
import type { Issue, IssueLabelsResponse, IssueMetadata, Label } from "../types";
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
  qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
  // Refresh every Project Gantt cache that might be observing this issue.
  // We invalidate the whole prefix rather than the issue's own project
  // because a fresh issue isn't necessarily scheduled yet; the active Gantt
  // page (if any) will refetch and pick it up if it qualifies.
  qc.invalidateQueries({ queryKey: issueKeys.projectGanttAll(wsId) });
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
  qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
  // Any field change can shift Gantt membership — start_date / due_date may
  // have moved in or out of the `scheduled` set, project_id may have
  // changed, or the row that is in the cache may need to mirror updated
  // metadata (title, status, assignee). Cheaper to invalidate the prefix
  // than to mirror the server filter here.
  qc.invalidateQueries({ queryKey: issueKeys.projectGanttAll(wsId) });
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
  // Patch the Project Gantt caches in-place: the Gantt view applies
  // `labelFilters` to the row data, so a stale `labels` array would silently
  // hide or surface bars after another tab/agent attached or detached a
  // label. Mutating in place (instead of invalidating) avoids a refetch of
  // the entire scheduled set on every label toggle.
  for (const [key, data] of qc.getQueriesData<Issue[]>({
    queryKey: issueKeys.projectGanttAll(wsId),
  })) {
    if (!data) continue;
    const next = data.map((issue) =>
      issue.id === issueId ? { ...issue, labels } : issue,
    );
    qc.setQueryData<Issue[]>(key, next);
  }
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
}

/**
 * Apply a metadata snapshot to the issue detail + list + my-issues caches.
 * The server emits this whenever a single key is set or deleted, so the
 * payload is always the FULL post-mutation map — we replace, not merge.
 *
 * Used for the read-only metadata strip in issue detail. Updates that arrive
 * while no view is mounted still keep the caches accurate so the next render
 * shows the latest state without a refetch.
 */
export function onIssueMetadataChanged(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  metadata: IssueMetadata,
) {
  qc.setQueryData<ListIssuesCache>(issueKeys.list(wsId), (old) =>
    old ? patchIssueInBuckets(old, issueId, { metadata }) : old,
  );
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
    old ? { ...old, metadata } : old,
  );
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
}

export function onIssueDeleted(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  cleanupDeletedIssueCaches(qc, wsId, issueId);
  qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
}
