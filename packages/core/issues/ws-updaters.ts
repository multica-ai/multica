import { issueStatusCategory } from "./status-category";
import type { QueryClient } from "@tanstack/react-query";
import { issueKeys } from "./queries";
import { labelKeys } from "../labels/queries";
import { projectKeys } from "../projects/queries";
import {
  applyIssueChange,
  bucketedListEntries,
  flatListEntries,
  invalidateIssueDerivatives,
  invalidateLastActivitySortedIssueLists,
  invalidateStaleListKeys,
  invalidateUpdatedAtSortedIssueLists,
  issueArrayEntries,
  tableRowEntries,
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

const auxiliaryIssueRevisions = new WeakMap<QueryClient, Map<string, number>>();

type AuxiliaryIssueProjection = "generic" | "labels" | "metadata" | "properties";

function auxiliaryIssueRevisionPrefix(wsId: string, issueId: string) {
  return `${wsId}:${issueId}:`;
}

function auxiliaryIssueRevisionKey(
  wsId: string,
  issueId: string,
  projection: AuxiliaryIssueProjection,
) {
  return `${auxiliaryIssueRevisionPrefix(wsId, issueId)}${projection}`;
}

function recordAuxiliaryIssueRevision(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  revision: number,
  projection: AuxiliaryIssueProjection,
) {
  let revisions = auxiliaryIssueRevisions.get(qc);
  if (!revisions) {
    revisions = new Map();
    auxiliaryIssueRevisions.set(qc, revisions);
  }
  const key = auxiliaryIssueRevisionKey(wsId, issueId, projection);
  if ((revisions.get(key) ?? 0) < revision) revisions.set(key, revision);
}

function isOlderThanRecordedAuxiliaryProjection(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  revision: number | undefined,
  projection: AuxiliaryIssueProjection,
) {
  if (revision === undefined) return false;
  const recorded = auxiliaryIssueRevisions
    .get(qc)
    ?.get(auxiliaryIssueRevisionKey(wsId, issueId, projection));
  return recorded !== undefined && revision < recorded;
}

export function reconcileIssueFullSnapshotRevision(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  fullRevision: number | undefined,
) {
  if (fullRevision === undefined) return;
  const revisions = auxiliaryIssueRevisions.get(qc);
  if (!revisions) return;
  const prefix = auxiliaryIssueRevisionPrefix(wsId, issueId);
  let newestAuxiliaryRevision: number | undefined;
  for (const [key, revision] of revisions) {
    if (!key.startsWith(prefix)) continue;
    if (fullRevision >= revision) {
      revisions.delete(key);
    } else if (
      newestAuxiliaryRevision === undefined ||
      revision > newestAuxiliaryRevision
    ) {
      newestAuxiliaryRevision = revision;
    }
  }
  if (newestAuxiliaryRevision === undefined) return;
  // setQueryData clears an inactive query's stale flag. Restore it when this
  // full snapshot is still behind an auxiliary event we already observed.
  invalidateStaleIssueOwnerProjections(
    qc,
    wsId,
    issueId,
    newestAuxiliaryRevision,
  );
}

function mergeIssuePatch(
  issue: Issue,
  patch: Partial<Issue>,
  orderRevision?: number,
): Issue {
  if (
    orderRevision !== undefined &&
    issue.revision !== undefined &&
    orderRevision < issue.revision
  ) {
    return issue;
  }
  return { ...issue, ...patch };
}

function patchIssueInFlatCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  patch: Partial<Issue>,
  orderRevision?: number,
) {
  for (const [key, data] of flatListEntries(qc, wsId)) {
    qc.setQueryData<IssueFlatCache>(key, {
      ...data,
      pages: data.pages.map((page) => ({
        ...page,
        issues: page.issues.map((issue) =>
          issue.id === issueId
            ? mergeIssuePatch(issue, patch, orderRevision)
            : issue,
        ),
      })),
    });
  }
}

/** Patch denormalized issue snapshots in every loaded per-parent cache. */
function patchIssueInChildrenCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  patch: Partial<Issue>,
  orderRevision?: number,
) {
  for (const [key, data] of issueArrayEntries(qc, issueKeys.childrenAll(wsId))) {
    if (!data.some((child) => child.id === issueId)) continue;
    qc.setQueryData<Issue[]>(
      key,
      data.map((child) =>
        child.id === issueId
          ? mergeIssuePatch(child, patch, orderRevision)
          : child,
      ),
    );
  }
}

function patchIssueInTableCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  patch: Partial<Issue>,
  orderRevision?: number,
) {
  for (const [key, page] of tableRowEntries(qc, wsId)) {
    if (!page.rows.some((row) => row.issue.id === issueId)) continue;
    qc.setQueryData<IssueTableRowsResponse>(key, {
      ...page,
      rows: page.rows.map((row) =>
        row.issue.id === issueId
          ? {
              ...row,
              issue: mergeIssuePatch(row.issue, patch, orderRevision),
            }
          : row,
      ),
    });
  }
}

function patchIssueSnapshot(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  patch: Partial<Issue>,
  orderRevision?: number,
) {
  for (const [key, data] of bucketedListEntries(qc, wsId)) {
    const location = findIssueLocation(data, issueId);
    if (
      !location ||
      mergeIssuePatch(location.issue, patch, orderRevision) === location.issue
    ) {
      continue;
    }
    qc.setQueryData<ListIssuesCache>(key, patchIssueInBuckets(data, issueId, patch));
  }
  patchIssueInFlatCaches(qc, wsId, issueId, patch, orderRevision);
  patchIssueInTableCaches(qc, wsId, issueId, patch, orderRevision);
  patchIssueInChildrenCaches(qc, wsId, issueId, patch, orderRevision);
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
    old ? mergeIssuePatch(old, patch, orderRevision) : old,
  );
  for (const [key, data] of issueArrayEntries(
    qc,
    issueKeys.projectGanttAll(wsId),
  )) {
    qc.setQueryData<Issue[]>(
      key,
      data.map((issue) =>
        issue.id === issueId
          ? mergeIssuePatch(issue, patch, orderRevision)
          : issue,
      ),
    );
  }
}

function freshestCachedIssueRevision(
  qc: QueryClient,
  wsId: string,
  issueId: string,
): number | undefined {
  let freshest = qc.getQueryData<Issue>(issueKeys.detail(wsId, issueId))?.revision;
  const consider = (issue: Issue | undefined) => {
    if (issue?.revision !== undefined && (freshest === undefined || issue.revision > freshest)) {
      freshest = issue.revision;
    }
  };
  for (const [, data] of bucketedListEntries(qc, wsId)) {
    consider(findIssueLocation(data, issueId)?.issue);
  }
  for (const [, data] of flatListEntries(qc, wsId)) {
    for (const page of data.pages) {
      consider(page.issues.find((issue) => issue.id === issueId));
    }
  }
  for (const [, data] of tableRowEntries(qc, wsId)) {
    consider(data.rows.find((row) => row.issue.id === issueId)?.issue);
  }
  for (const prefix of [issueKeys.childrenAll(wsId), issueKeys.projectGanttAll(wsId)]) {
    for (const [, data] of issueArrayEntries(qc, prefix)) {
      consider(data.find((issue) => issue.id === issueId));
    }
  }
  return freshest;
}

function refetchForUnversionedIssueEvent(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  revision: number | undefined,
): boolean {
  if (revision !== undefined) return false;
  if (freshestCachedIssueRevision(qc, wsId, issueId) === undefined) return false;
  // Mixed-version rollout: once any projection has an ordered revision, an
  // older server's unversioned snapshot can no longer be applied safely.
  qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
  return true;
}

function invalidateIssueOwnerProjectionsWhere(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  shouldInvalidate: (issue: Issue | undefined) => boolean,
) {
  const invalidateExact = (queryKey: readonly unknown[]) => {
    qc.invalidateQueries({ queryKey, exact: true });
  };

  const detailKey = issueKeys.detail(wsId, issueId);
  if (shouldInvalidate(qc.getQueryData<Issue>(detailKey))) invalidateExact(detailKey);

  for (const [key, data] of bucketedListEntries(qc, wsId)) {
    if (shouldInvalidate(findIssueLocation(data, issueId)?.issue)) {
      invalidateExact(key);
    }
  }
  for (const [key, data] of flatListEntries(qc, wsId)) {
    if (data.pages.some((page) => shouldInvalidate(page.issues.find((issue) => issue.id === issueId)))) {
      invalidateExact(key);
    }
  }
  for (const [key, data] of tableRowEntries(qc, wsId)) {
    if (data.rows.some((row) => row.issue.id === issueId && shouldInvalidate(row.issue))) {
      invalidateExact(key);
    }
  }
  for (const prefix of [issueKeys.childrenAll(wsId), issueKeys.projectGanttAll(wsId)]) {
    for (const [key, data] of issueArrayEntries(qc, prefix)) {
      if (data.some((issue) => issue.id === issueId && shouldInvalidate(issue))) {
        invalidateExact(key);
      }
    }
  }
}

function invalidateStaleIssueOwnerProjections(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  revision: number,
) {
  invalidateIssueOwnerProjectionsWhere(
    qc,
    wsId,
    issueId,
    (issue) =>
      issue !== undefined &&
      (issue.revision === undefined || issue.revision < revision),
  );
}

/** Fallback for successful auxiliary mutations whose HTTP response cannot
 * carry an owner revision (notably 204 comment deletion). Only loaded
 * projections containing the owner are invalidated. */
export function invalidateIssueOwnerProjections(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  invalidateIssueOwnerProjectionsWhere(
    qc,
    wsId,
    issueId,
    (issue) => issue !== undefined,
  );
}

/**
 * An auxiliary event proves that a newer aggregate owner revision exists, but
 * it does not carry the full Issue snapshot for that revision. Advancing the
 * cached Issue.revision here would make an older-but-newer-full snapshot look
 * stale and could strand fields such as title forever. Keep Issue.revision as
 * the ordering of the full snapshot actually present in that cache, and mark
 * only stale projections for an authoritative refetch.
 */
export function onIssueAuxiliaryRevision(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  revision: number | undefined,
  projection: AuxiliaryIssueProjection = "generic",
) {
  if (!revision || revision <= 0) return;
  recordAuxiliaryIssueRevision(qc, wsId, issueId, revision, projection);
  invalidateStaleIssueOwnerProjections(qc, wsId, issueId, revision);
}

function findIssueInFlatCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  for (const [, data] of flatListEntries(qc, wsId)) {
    for (const page of data.pages) {
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
  // A custom status this client cannot resolve to a category has no bucket to
  // go in. Inserting nowhere would silently hide an issue that exists on the
  // server, so invalidate the list instead and let the refetch place it.
  // (MUL-6243)
  const bucketable = issueStatusCategory(issue) !== null;
  for (const [key, data] of qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) })) {
    if (!data) continue;
    if (bucketable) {
      qc.setQueryData<ListIssuesCache>(key, addIssueToBuckets(data, issue));
    } else {
      qc.invalidateQueries({ queryKey: key });
    }
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
  if (refetchForUnversionedIssueEvent(qc, wsId, issue.id, issue.revision)) {
    return;
  }
  if (
    issue.revision !== undefined &&
    cachedIssue?.revision !== undefined &&
    issue.revision < cachedIssue.revision
  ) {
    return;
  }
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
    acceptCurrent: (current) =>
      issue.revision === undefined ||
      current.revision === undefined ||
      issue.revision > current.revision,
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
        old?.map((c) =>
          c.id === issue.id &&
          (issue.revision === undefined ||
            c.revision === undefined ||
            issue.revision > c.revision)
            ? { ...c, ...issue }
            : c,
        ),
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
  reconcileIssueFullSnapshotRevision(qc, wsId, issue.id, issue.revision);
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
  revision?: number,
) {
  if (refetchForUnversionedIssueEvent(qc, wsId, issueId, revision)) {
    qc.invalidateQueries({ queryKey: labelKeys.byIssue(wsId, issueId) });
    return;
  }
  patchIssueLabels(qc, wsId, issueId, labels, revision);
  invalidateIssueLabelDerivatives(qc, wsId);
  invalidateLastActivitySortedIssueLists(qc, wsId);
}

/** Deterministic label snapshot patch used by optimistic mutation legs. */
export function patchIssueLabels(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  labels: Label[],
  revision?: number,
) {
  if (
    isOlderThanRecordedAuxiliaryProjection(
      qc,
      wsId,
      issueId,
      revision,
      "labels",
    )
  ) {
    return;
  }
  patchIssueSnapshot(qc, wsId, issueId, { labels }, revision);
  qc.setQueryData<IssueLabelsResponse>(labelKeys.byIssue(wsId, issueId), (old) =>
    old &&
    !(
      revision !== undefined &&
      old.issue_revision !== undefined &&
      revision <= old.issue_revision
    )
      ? {
          ...old,
          labels,
          ...(revision !== undefined ? { issue_revision: revision } : {}),
        }
      : old,
  );
  onIssueAuxiliaryRevision(qc, wsId, issueId, revision, "labels");
}

/** Reconcile server-filtered label windows only after the write commits. */
export function invalidateIssueLabelDerivatives(qc: QueryClient, wsId: string) {
  // A committed response/event must cancel or supersede any per-parent fetch
  // that started before the label write and could otherwise land afterward.
  qc.invalidateQueries({ queryKey: issueKeys.childrenAll(wsId) });
  // Batched children caches hold Map-shaped data (parentId → Issue[]) that
  // patchIssueLabels can't surgically update — refetch instead so swimlane
  // child lanes pick up the new label set.
  qc.invalidateQueries({ queryKey: issueKeys.childrenByParentsAll(wsId) });
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
  revision?: number,
) {
  if (refetchForUnversionedIssueEvent(qc, wsId, issueId, revision)) return;
  if (
    isOlderThanRecordedAuxiliaryProjection(
      qc,
      wsId,
      issueId,
      revision,
      "metadata",
    )
  ) {
    return;
  }
  patchIssueSnapshot(qc, wsId, issueId, { metadata }, revision);
  onIssueAuxiliaryRevision(qc, wsId, issueId, revision, "metadata");
  invalidateLastActivitySortedIssueLists(qc, wsId);
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
  revision?: number,
) {
  if (refetchForUnversionedIssueEvent(qc, wsId, issueId, revision)) return;
  patchIssueProperties(qc, wsId, issueId, properties, revision);
  invalidateLastActivitySortedIssueLists(qc, wsId);
  // Per-parent rows are patched for immediate UI feedback, then all children
  // projections are marked stale so older fetches cannot win after commit.
  qc.invalidateQueries({ queryKey: issueKeys.childrenAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.childrenByParentsAll(wsId) });
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
  revision?: number,
) {
  if (
    isOlderThanRecordedAuxiliaryProjection(
      qc,
      wsId,
      issueId,
      revision,
      "properties",
    )
  ) {
    return;
  }
  patchIssueSnapshot(qc, wsId, issueId, { properties }, revision);
  onIssueAuxiliaryRevision(qc, wsId, issueId, revision, "properties");
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
