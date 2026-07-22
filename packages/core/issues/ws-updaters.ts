import type { QueryClient } from "@tanstack/react-query";
import { issueKeys } from "./queries";
import { labelKeys } from "../labels/queries";
import { projectKeys } from "../projects/queries";
import {
  applyIssueChange,
  invalidateIssueDerivatives,
  invalidateStaleListKeys,
  invalidateUpdatedAtSortedIssueLists,
  type IssueFlatCache,
} from "./cache-coordinator";
import {
  addIssueToBuckets,
  findIssueLocation,
  patchIssueInBuckets,
} from "./cache-helpers";
import { cleanupDeletedIssueCaches } from "./delete-cache";
import type {
  Issue,
  IssueLabelsResponse,
  IssueMetadata,
  IssuePropertyValues,
  IssueTableRowsResponse,
  Label,
} from "../types";
import type { ListIssuesCache } from "../types";

function patchIssueInFlatCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  patch: Partial<Issue>,
) {
  for (const [key, data] of qc.getQueriesData<IssueFlatCache>({
    queryKey: issueKeys.flatAll(wsId),
  })) {
    if (!data?.pages) continue;
    qc.setQueryData<IssueFlatCache>(key, {
      ...data,
      pages: data.pages.map((page) => ({
        ...page,
        issues: page.issues.map((issue) =>
          issue.id === issueId ? { ...issue, ...patch } : issue,
        ),
      })),
    });
  }
}

function patchIssueInTableCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  patch: Partial<Issue>,
) {
  for (const [key, data] of qc.getQueriesData<unknown>({
    queryKey: issueKeys.tableAll(wsId),
  })) {
    if (
      !data ||
      typeof data !== "object" ||
      !Array.isArray((data as IssueTableRowsResponse).rows)
    ) {
      continue;
    }
    const page = data as IssueTableRowsResponse;
    if (!page.rows.some((row) => row.issue.id === issueId)) continue;
    qc.setQueryData<IssueTableRowsResponse>(key, {
      ...page,
      rows: page.rows.map((row) =>
        row.issue.id === issueId
          ? { ...row, issue: { ...row.issue, ...patch } }
          : row,
      ),
    });
  }
}

function findIssueInFlatCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  for (const [, data] of qc.getQueriesData<IssueFlatCache>({
    queryKey: issueKeys.flatAll(wsId),
  })) {
    for (const page of data?.pages ?? []) {
      const issue = page.issues.find((candidate) => candidate.id === issueId);
      if (issue) return issue;
    }
  }
  return undefined;
}

export function onIssueCreated(
  qc: QueryClient,
  wsId: string,
  issue: Issue,
) {
  for (const [key, data] of qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) })) {
    if (data) qc.setQueryData<ListIssuesCache>(key, addIssueToBuckets(data, issue));
  }
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.flatAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
  if (issue.project_id) {
    qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
  }
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
  // assigneeChanged / statusChanged / projectChanged come from the server's
  // issue:updated flags — authoritative "did this write move a membership
  // dimension" signals. They feed the coordinator's changed-dims input so a
  // non-membership change (title / position / priority / label) keeps every
  // loaded list in place instead of refetching.
  meta: {
    assigneeChanged?: boolean;
    statusChanged?: boolean;
    projectChanged?: boolean;
  } = {},
) {
  // Look up the OLD parent + cached entity before mutating cache state, so we
  // can keep the parent's children cache in sync (powers the sub-issues list
  // shown on the parent issue page) and diff-fallback the change flags.
  const listQueries = qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) });
  const firstListData = listQueries[0]?.[1];
  const detailData = qc.getQueryData<Issue>(issueKeys.detail(wsId, issue.id));
  const cachedIssue =
    detailData ??
    (firstListData ? findIssueLocation(firstListData, issue.id)?.issue : undefined) ??
    findIssueInFlatCaches(qc, wsId, issue.id);
  const oldParentId =
    detailData?.parent_issue_id ?? cachedIssue?.parent_issue_id ?? null;
  // The NEW parent comes from the WS payload when parent_issue_id changed
  const newParentId = issue.parent_issue_id ?? null;
  const parentChanged =
    issue.parent_issue_id !== undefined && newParentId !== oldParentId;

  // Prefer the server's flags (authoritative, set on the wire). Fall back to
  // diffing the payload against the cached copy only when a flag is absent
  // (older backend): the diff is unreliable once a local optimistic move has
  // overwritten the cached value, but it still covers remote/agent changes
  // and keeps a new frontend on an old backend from regressing (MUL-3669 /
  // #4548). The local move itself is covered by useUpdateIssue's own
  // coordinator pass, which never depends on these flags.
  const oldProjectId = detailData?.project_id ?? cachedIssue?.project_id ?? null;
  const changed = {
    assignee:
      meta.assigneeChanged ??
      (cachedIssue !== undefined &&
        ((issue.assignee_id !== undefined &&
          issue.assignee_id !== cachedIssue.assignee_id) ||
          (issue.assignee_type !== undefined &&
            issue.assignee_type !== cachedIssue.assignee_type))),
    project:
      meta.projectChanged ??
      (issue.project_id !== undefined && (issue.project_id ?? null) !== oldProjectId),
    status:
      meta.statusChanged ??
      (cachedIssue !== undefined &&
        issue.status !== undefined &&
        issue.status !== cachedIssue.status),
  };

  // The coordinator applies the same rules table the local mutations use:
  // surgical patch/rebucket where the card is loaded and still belongs,
  // surgical remove where the change moved it off a filtered surface, and
  // stale keys for the drift a patch cannot fix (enter/leave beyond the
  // loaded window, undecidable membership, off-screen bucket counts). The
  // server has already committed, so stale keys are flushed immediately.
  const change = applyIssueChange(qc, wsId, issue.id, issue, {
    changed,
    baseIssue: cachedIssue,
  });
  invalidateStaleListKeys(qc, change.staleKeys);
  invalidateIssueDerivatives(qc, wsId, {
    statusOrProjectChanged:
      issue.status !== undefined || issue.project_id !== undefined,
  });
  // Group counts, branch membership and hierarchy are server-owned. Never
  // guess deltas from a partial branch; refetch the active Table queries.
  qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });

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
    qc.invalidateQueries({ queryKey: issueKeys.childrenByParentsAll(wsId) });
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
  patchIssueLabels(qc, wsId, issueId, labels);
  invalidateIssueLabelDerivatives(qc, wsId);
}

/** Deterministic label snapshot patch used by optimistic mutation legs. */
export function patchIssueLabels(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  labels: Label[],
) {
  for (const [key, data] of qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) })) {
    if (data) qc.setQueryData<ListIssuesCache>(key, patchIssueInBuckets(data, issueId, { labels }));
  }
  patchIssueInFlatCaches(qc, wsId, issueId, { labels });
  patchIssueInTableCaches(qc, wsId, issueId, { labels });
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
}

/** Reconcile server-filtered label windows only after the write commits. */
export function invalidateIssueLabelDerivatives(qc: QueryClient, wsId: string) {
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
  qc.invalidateQueries({
    queryKey: issueKeys.flatAll(wsId),
    predicate: (query) =>
      query.queryKey.some((part) => {
        if (!part || typeof part !== "object" || Array.isArray(part)) return false;
        const labelIds = (part as { label_ids?: unknown }).label_ids;
        return Array.isArray(labelIds) && labelIds.length > 0;
      }),
  });
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
  for (const [key, data] of qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) })) {
    if (data) qc.setQueryData<ListIssuesCache>(key, patchIssueInBuckets(data, issueId, { metadata }));
  }
  patchIssueInFlatCaches(qc, wsId, issueId, { metadata });
  patchIssueInTableCaches(qc, wsId, issueId, { metadata });
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
    old ? { ...old, metadata } : old,
  );
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  // A metadata write bumps issue.updated_at server-side (SetIssueMetadataKey /
  // DeleteIssueMetadataKey), but the patches above keep each card's slot, so a
  // board/table sorted by "Updated date" would stay in the old order. This
  // event is server-committed, so refetch those keys to re-sort (MUL-5016).
  invalidateUpdatedAtSortedIssueLists(qc, wsId);
  // Server-backed Table counts, membership and cursor boundaries may also
  // depend on metadata-driven timestamps, so refresh its query graph too.
  qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
}

/**
 * Apply a custom-property bag snapshot to the issue detail + list caches.
 * Mirrors onIssueMetadataChanged: the server emits the FULL post-mutation
 * bag on every single-key write, so we replace rather than merge. Also used
 * directly by the useSetIssueProperty/useUnsetIssueProperty optimistic path.
 */
export function onIssuePropertiesChanged(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  properties: IssuePropertyValues,
) {
  patchIssueProperties(qc, wsId, issueId, properties);
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  // Plain assignee-group caches are never patched in place (their bucket
  // shape differs) and would otherwise hold stale chips forever under
  // staleTime:Infinity (clean-room review F2).
  qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
  invalidatePropertyWindowQueries(qc, wsId);
  // A property write also bumps issue.updated_at server-side
  // (SetIssuePropertyValue / DeleteIssuePropertyValue). invalidatePropertyWindow
  // Queries only refetches property-filtered/-sorted windows, so a status board
  // or flat table sorted by "Updated date" (no property param) would keep the
  // old order. Refetch those too. Only committed callers reach here (WS event +
  // mutation onSuccess); the optimistic leg uses patchIssueProperties (MUL-5016).
  invalidateUpdatedAtSortedIssueLists(qc, wsId);
}

/** Patch only deterministic entity snapshots. Optimistic mutation legs use
 * this helper so they never start a property-filter/sort refetch before the
 * server commit (which could return the old bag and stomp the optimistic one). */
export function patchIssueProperties(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  properties: IssuePropertyValues,
) {
  for (const [key, data] of qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) })) {
    if (data) qc.setQueryData<ListIssuesCache>(key, patchIssueInBuckets(data, issueId, { properties }));
  }
  patchIssueInFlatCaches(qc, wsId, issueId, { properties });
  patchIssueInTableCaches(qc, wsId, issueId, { properties });
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
    old ? { ...old, properties } : old,
  );
}

/**
 * Refetch every issue window whose SERVER-side shape depends on property
 * values: queries filtered by `properties` or sorted by `property:<id>`.
 * In-place patching keeps them stale under staleTime:Infinity — a value
 * edit can change an issue's page membership and ordering, and grouped
 * caches never self-heal (review round 3). Windows without property params
 * keep the cheap in-place patch above.
 */
export function invalidatePropertyWindowQueries(qc: QueryClient, wsId: string) {
  qc.invalidateQueries({
    queryKey: issueKeys.all(wsId),
    predicate: (query) =>
      query.queryKey.some((part) => {
        if (!part || typeof part !== "object" || Array.isArray(part)) return false;
        const rec = part as Record<string, unknown>;
        if (
          rec.properties &&
          typeof rec.properties === "object" &&
          Object.keys(rec.properties as Record<string, unknown>).length > 0
        ) {
          return true;
        }
        return typeof rec.sort_by === "string" && rec.sort_by.startsWith("property:");
      }),
  });
}

export function onIssueDeleted(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  cleanupDeletedIssueCaches(qc, wsId, issueId);
  qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
}
