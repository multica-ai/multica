import {
  hashKey,
  type InfiniteData,
  type QueryClient,
  type QueryKey,
} from "@tanstack/react-query";
import {
  issueKeys,
  type IssueFlatFilter,
  type IssueSortParam,
  type MyIssuesFilter,
} from "./queries";
import { inboxKeys } from "../inbox/queries";
import { patchInboxIssueStatus } from "../inbox/ws-updaters";
import { projectKeys } from "../projects/queries";
import {
  decrementBucketTotal,
  findIssueLocation,
  moveBucketTotal,
  patchIssueInBuckets,
  removeIssueFromBuckets,
} from "./cache-helpers";
import {
  issueMatchesListFilter,
  listFilterDependsOn,
  type IssueChangedDims,
} from "./surface/membership";
import type {
  InboxItem,
  Issue,
  ListIssuesCache,
  ListIssuesResponse,
} from "../types";

export type IssueFlatCache = InfiniteData<ListIssuesResponse, number>;

/**
 * IssueCacheCoordinator — the one rules table for how a single issue change
 * propagates through the query cache.
 *
 * Every write path converges here: `useUpdateIssue` (onMutate optimistic +
 * onSuccess server reconcile), `useBatchUpdateIssues`, and the WS
 * `issue:updated` handler all call {@link applyIssueChange}, so "I changed
 * it" and "someone else changed it" follow the same rules by construction.
 *
 * The rules, per loaded bucketed list (workspace board + every myList
 * scope — My Issues, Project, actor panels, workspace members/agents tabs):
 *
 *   card present, filter untouched by the change → surgical patch (rebucket
 *     on status, position-slot insert; never a refetch — refetching the
 *     visible list is what made drags flicker)
 *   card present, no longer matches the list's filter → surgical REMOVE
 *     (bucket total decremented) — the "issue left this surface" case that a
 *     filter-blind patch used to leave behind (MUL-3669)
 *   card present, membership undecidable client-side (involves / my:all) →
 *     patch + mark the key stale
 *   card absent, change can't affect this list → skip
 *   card absent, stayed a member + status changed → move one unit of the
 *     server total between the two buckets immediately, then mark the key
 *     stale: the row's slot in the destination bucket's loaded window is
 *     server knowledge, and under staleTime: Infinity an explicit
 *     invalidation is the only channel that ever fills it in
 *   card absent, left the list (reassigned / re-projected) → old status
 *     bucket total -1
 *   card absent, may have ENTERED, or anything undecidable (no base entity,
 *     unknown membership) → mark the key stale (never hard-insert: the
 *     right page/slot under the list's sort+filter is server knowledge)
 *
 * Stale keys are NOT invalidated here — timing is the caller's contract:
 * mutations defer them to onSettled (invalidating mid-flight would refetch
 * uncommitted state and stomp the optimistic patch), the WS path invalidates
 * immediately (the server already committed).
 *
 * The detail cache and the Inbox `issue_status` projection are patched in the
 * same pass. Aggregate projections that cannot be recomputed from one entity
 * (assignee-grouped boards, Gantt, project metrics) go through
 * {@link invalidateIssueDerivatives}.
 */

export interface IssueCacheChangeResult {
  /** Pre-change snapshots of every cache this change touched — feed back to
   *  {@link rollbackIssueChange} from onError. */
  prevLists: [QueryKey, ListIssuesCache][];
  prevFlatLists: [QueryKey, IssueFlatCache][];
  prevDetail: Issue | undefined;
  prevInboxList: InboxItem[] | undefined;
  /** Loaded list keys whose server result may have drifted (membership
   *  unknown, possible enter/leave beyond the loaded window, bucket-count
   *  drift). Invalidate on settle (mutation) or immediately (WS). */
  staleKeys: QueryKey[];
  /** The freshest pre-change copy of the issue found while reconciling —
   *  callers use it for parent/children bookkeeping without re-scanning. */
  prevIssue: Issue | undefined;
}

/** The server contract a bucketed list key encodes. `myListSorted` keys are
 *  `["issues", wsId, "my", scope, filter, sort]`; the workspace list carries
 *  no filter. The `byStatus` shape check upstream keeps grouped/flat caches
 *  under the same prefixes out of this path. */
function listContractFromKey(
  key: QueryKey,
): { scope: string | undefined; filter: MyIssuesFilter } {
  if (key[2] === "my") {
    return {
      scope: typeof key[3] === "string" ? key[3] : undefined,
      filter: (key[4] ?? {}) as MyIssuesFilter,
    };
  }
  return { scope: undefined, filter: {} };
}

function bucketedListEntries(
  qc: QueryClient,
  wsId: string,
): [QueryKey, ListIssuesCache][] {
  return [
    ...qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) }),
    ...qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.myAll(wsId) }),
  ].filter(
    (entry): entry is [QueryKey, ListIssuesCache] => !!entry[1]?.byStatus,
  );
}

function flatListEntries(
  qc: QueryClient,
  wsId: string,
): [QueryKey, IssueFlatCache][] {
  return qc
    .getQueriesData<IssueFlatCache>({ queryKey: issueKeys.flatAll(wsId) })
    .filter(
      (entry): entry is [QueryKey, IssueFlatCache] =>
        !!entry[1] && Array.isArray(entry[1].pages),
    );
}

function flatContractFromKey(key: QueryKey): {
  scope: string | undefined;
  filter: IssueFlatFilter;
  sort: IssueSortParam;
} {
  return {
    scope: typeof key[3] === "string" ? key[3] : undefined,
    filter: (key[4] ?? {}) as IssueFlatFilter,
    sort: (key[5] ?? {}) as IssueSortParam,
  };
}

function patchFieldChanged<K extends keyof Issue>(
  patch: Partial<Issue>,
  base: Issue | undefined,
  field: K,
) {
  if (!Object.prototype.hasOwnProperty.call(patch, field)) return false;
  return !base || !Object.is(patch[field], base[field]);
}

/** Whether a patch can change a flat window's membership or ordering. A
 * loaded row is always patched optimistically; only windows whose server
 * contract depends on the changed field need the follow-up refetch. */
function flatWindowNeedsReconcile(
  key: QueryKey,
  patch: Partial<Issue>,
  base: Issue | undefined,
  changed: IssueChangedDims,
) {
  const { scope, filter, sort } = flatContractFromKey(key);
  const anyIssueFieldChanged = Object.keys(patch).some((field) =>
    patchFieldChanged(patch, base, field as keyof Issue),
  );

  if (listFilterDependsOn(scope, filter, changed)) return true;
  if (filter.q && patchFieldChanged(patch, base, "title")) return true;
  if (changed.status && (filter.statuses?.length ?? 0) > 0) return true;
  if (
    patchFieldChanged(patch, base, "priority") &&
    (filter.priorities?.length ?? 0) > 0
  ) {
    return true;
  }
  if (
    changed.assignee &&
    ((filter.assignee_filters?.length ?? 0) > 0 || filter.include_no_assignee)
  ) {
    return true;
  }
  if (changed.project && ((filter.project_ids?.length ?? 0) > 0 || filter.include_no_project)) {
    return true;
  }
  if (patchFieldChanged(patch, base, "parent_issue_id") && filter.top_level_only) {
    return true;
  }
  if (sort.date_field === "updated_at" && anyIssueFieldChanged) return true;
  if (
    sort.date_field === "created_at" &&
    patchFieldChanged(patch, base, "created_at")
  ) {
    return true;
  }

  switch (sort.sort_by ?? "position") {
    case "title":
      return patchFieldChanged(patch, base, "title");
    case "status":
      return changed.status;
    case "priority":
      return patchFieldChanged(patch, base, "priority");
    case "created_at":
      return patchFieldChanged(patch, base, "created_at");
    case "updated_at":
      // Every persisted issue edit advances updated_at even though the
      // optimistic request payload does not carry the server timestamp.
      return anyIssueFieldChanged;
    case "start_date":
      return patchFieldChanged(patch, base, "start_date");
    case "due_date":
      return patchFieldChanged(patch, base, "due_date");
    case "position":
      return patchFieldChanged(patch, base, "position");
    default:
      // Custom-property ordering is reconciled by the dedicated property
      // mutation/WS pipeline, which has the full property-bag snapshot.
      return false;
  }
}

export function applyIssueChange(
  qc: QueryClient,
  wsId: string,
  id: string,
  patch: Partial<Issue>,
  opts: {
    /** Which membership dimensions this change actually moved — compute via
     *  `issueChangedDims` (mutations) or the server's WS flags. */
    changed: IssueChangedDims;
    /** Freshest full pre-change entity, used to judge membership for lists
     *  where the card is not loaded. Omitting it degrades those judgments to
     *  "unknown" → a deferred refetch, never a wrong patch. */
    baseIssue?: Issue;
  },
): IssueCacheChangeResult {
  const { changed, baseIssue } = opts;
  const prevLists: [QueryKey, ListIssuesCache][] = [];
  const prevFlatLists: [QueryKey, IssueFlatCache][] = [];
  const staleKeys: QueryKey[] = [];
  let prevIssue: Issue | undefined = baseIssue;

  for (const [key, data] of bucketedListEntries(qc, wsId)) {
    const { scope, filter } = listContractFromKey(key);
    const loc = findIssueLocation(data, id);
    const filterTouched = listFilterDependsOn(scope, filter, changed);

    if (loc) {
      if (!prevIssue) prevIssue = loc.issue;
      let next: ListIssuesCache;
      if (filterTouched) {
        const membership = issueMatchesListFilter(
          { ...loc.issue, ...patch },
          scope,
          filter,
        );
        if (membership === false) {
          next = removeIssueFromBuckets(data, id);
        } else {
          next = patchIssueInBuckets(data, id, patch);
          if (membership === "unknown") staleKeys.push(key);
        }
      } else {
        next = patchIssueInBuckets(data, id, patch);
      }
      if (next !== data) {
        prevLists.push([key, data]);
        qc.setQueryData<ListIssuesCache>(key, next);
      }
      continue;
    }

    // Card not loaded here. Only a change that can move the issue across
    // this list's filter — or shift a per-status count for an issue beyond
    // the loaded window — needs a reconcile; anything else is a no-op.
    if (!filterTouched && !changed.status) continue;
    const wasMember = baseIssue
      ? issueMatchesListFilter(baseIssue, scope, filter)
      : "unknown";
    const isMember = issueMatchesListFilter(
      { ...baseIssue, ...patch },
      scope,
      filter,
    );
    // Neither before nor after the change does this issue belong to the
    // list — its pages and counts are untouched.
    if (wasMember === false && isMember === false) continue;

    // Certain count arithmetic — branch on the membership OUTCOME, never on
    // which field changed, so status / assignee / project (and future team)
    // all flow through the same two cases. wasMember === true implies a
    // baseIssue exists, so the old status is known.
    if (wasMember === true && baseIssue) {
      if (isMember === true) {
        // Still a member. Only a status change moves a count between
        // buckets; anything else (e.g. member→member reassignment) leaves
        // this list's pages and counts untouched.
        if (!changed.status || patch.status === undefined) continue;
        const next = moveBucketTotal(data, baseIssue.status, patch.status);
        if (next !== data) {
          prevLists.push([key, data]);
          qc.setQueryData<ListIssuesCache>(key, next);
          // The count moved but the row can't be inserted client-side (its
          // page/slot under the list's sort is server knowledge). Leave the
          // reconcile trigger: with staleTime: Infinity nothing else ever
          // brings the destination bucket's window in line with its total.
          staleKeys.push(key);
        }
        continue;
      }
      if (isMember === false) {
        // Left the list entirely — the bucket it was counted in loses one.
        const next = decrementBucketTotal(data, baseIssue.status);
        if (next !== data) {
          prevLists.push([key, data]);
          qc.setQueryData<ListIssuesCache>(key, next);
        }
        continue;
      }
    }
    // Entering (its page/slot under the list's sort is server knowledge) or
    // any uncertainty (no base, unknown membership) → refetch instead of
    // guessing.
    staleKeys.push(key);
  }

  // Patch loaded flat rows immediately. Only refetch windows whose encoded
  // server filter/sort actually depends on this change; e.g. a title edit in
  // a position-sorted unfiltered table is fully reconciled by the patch.
  for (const [key, data] of flatListEntries(qc, wsId)) {
    let found: Issue | undefined;
    const pages = data.pages.map((page) => ({
      ...page,
      issues: page.issues.map((issue) => {
        if (issue.id !== id) return issue;
        found = issue;
        return { ...issue, ...patch };
      }),
    }));
    if (found) {
      if (!prevIssue) prevIssue = found;
      prevFlatLists.push([key, data]);
      qc.setQueryData<IssueFlatCache>(key, { ...data, pages });
    }
    if (flatWindowNeedsReconcile(key, patch, found ?? baseIssue, changed)) {
      staleKeys.push(key);
    }
  }

  const prevDetail = qc.getQueryData<Issue>(issueKeys.detail(wsId, id));
  if (prevDetail) {
    qc.setQueryData<Issue>(issueKeys.detail(wsId, id), {
      ...prevDetail,
      ...patch,
    });
    if (!prevIssue) prevIssue = prevDetail;
  }

  // Inbox rows carry an `issue_status` display snapshot; the issue's status
  // is the real state, so the projection follows every status write.
  let prevInboxList: InboxItem[] | undefined;
  if (patch.status !== undefined) {
    prevInboxList = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
    if (prevInboxList) patchInboxIssueStatus(qc, wsId, id, patch.status);
  }

  return {
    prevLists,
    prevFlatLists,
    prevDetail,
    prevInboxList,
    staleKeys,
    prevIssue,
  };
}

/** Restore every snapshot captured by {@link applyIssueChange} — the onError
 *  leg of the optimistic lifecycle. */
export function rollbackIssueChange(
  qc: QueryClient,
  wsId: string,
  id: string,
  result: Pick<
    IssueCacheChangeResult,
    "prevLists" | "prevFlatLists" | "prevDetail" | "prevInboxList"
  >,
) {
  for (const [key, snapshot] of result.prevLists) {
    qc.setQueryData(key, snapshot);
  }
  for (const [key, snapshot] of result.prevFlatLists) {
    qc.setQueryData(key, snapshot);
  }
  if (result.prevDetail !== undefined) {
    qc.setQueryData(issueKeys.detail(wsId, id), result.prevDetail);
  }
  if (result.prevInboxList !== undefined) {
    qc.setQueryData(inboxKeys.list(wsId), result.prevInboxList);
  }
}

/**
 * Refresh the aggregate projections a single-entity patch cannot recompute:
 * assignee-grouped boards (regrouping is server logic), every Project Gantt
 * (schedule membership + row mirrors), and project metrics when the change
 * could shift per-project counts.
 */
export function invalidateIssueDerivatives(
  qc: QueryClient,
  wsId: string,
  opts: { statusOrProjectChanged: boolean },
) {
  qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.projectGanttAll(wsId) });
  if (opts.statusOrProjectChanged) {
    qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
  }
}

/** Invalidate the stale keys reported by {@link applyIssueChange}, deduped —
 *  a batch over N issues can report the same key N times. */
export function invalidateStaleListKeys(qc: QueryClient, staleKeys: QueryKey[]) {
  const seen = new Set<string>();
  for (const key of staleKeys) {
    const hash = hashKey(key);
    if (seen.has(hash)) continue;
    seen.add(hash);
    qc.invalidateQueries({ queryKey: key, exact: true });
  }
}
