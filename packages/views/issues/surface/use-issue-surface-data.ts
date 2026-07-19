"use client";

import { useCallback, useEffect, useMemo } from "react";
import {
  useInfiniteQuery,
  useQuery,
  type QueryKey,
} from "@tanstack/react-query";
import type { Issue, IssueAssigneeGroup, Project } from "@multica/core/types";
import { ALL_STATUSES } from "@multica/core/issues/config";
import { projectListOptions } from "@multica/core/projects/queries";
import {
  childIssueProgressOptions,
  type AssigneeGroupedIssuesFilter,
  type IssueFlatFilter,
  type IssueSortParam,
  type MyIssuesFilter,
} from "@multica/core/issues/queries";
import {
  issueSurfaceAssigneeGroupsOptions,
  issueSurfaceFlatOptions,
  issueSurfaceGanttOptions,
  issueSurfaceListOptions,
} from "@multica/core/issues/surface/repository";
import type { IssueSurfaceQueryPlan } from "@multica/core/issues/surface/query-plan";
import type { IssueStatus } from "@multica/core/types";
import {
  applyIssueFilters,
  filterAssigneeGroups,
  type IssueFilterState,
  type IssueFilters,
} from "../utils/filter";
import { shouldAutoLoadNextWindowPage } from "../components/table-view-model";
import type { ChildProgress } from "../components/list-row";
import type { IssueSurfaceActivity } from "./activity";

const EMPTY_ISSUES: Issue[] = [];
const EMPTY_CHILD_PROGRESS = new Map<string, ChildProgress>();
const EMPTY_PROJECTS: Project[] = [];

/**
 * The rows the gantt canvas actually draws, on top of the shared filters.
 *
 * The canvas adds two rules of its own: a row needs a date to be placed, and
 * completed work is hidden unless the user asks for it. The data source only
 * delivers scheduled issues (server-side `scheduled=true`), but a row can
 * still arrive without a date — e.g. a WS-driven optimistic patch that just
 * cleared start_date / due_date and is waiting for the cache to refetch — so
 * the date check stays defensive.
 *
 * These rules live HERE rather than privately inside GanttView so the header
 * chip can narrow the same set the canvas draws. A view that filters its own
 * rows in secret is exactly how the chip's count drifted from the list in the
 * first place (MUL-4884); duplicating the rules in both places would just
 * reintroduce the drift with extra steps.
 */
function ganttCanvasRows(issues: Issue[], showCompleted: boolean): Issue[] {
  const dated = issues.filter((i) => i.start_date || i.due_date);
  if (showCompleted) return dated;
  return dated.filter((i) => i.status !== "done" && i.status !== "cancelled");
}

export interface IssueSurfaceData {
  surfaceIssues: Issue[];
  projectIssues: Issue[];
  issues: Issue[];
  swimlaneIssues: Issue[];
  /** The rows the agents-working filter would leave on screen — or
   *  `undefined` when that set is genuinely UNKNOWN (table mode while the
   *  ids-facet window is still resolving, failed, or too large to
   *  materialize). Consumers must present unknown as unknown; substituting
   *  another incomplete window would publish a precise-looking wrong number
   *  (round-5 review P2). See the `workingScopeIssues` memo for why the known
   *  case is a projection of the render pipeline. */
  workingScopeIssues: Issue[] | undefined;
  filteredGanttIssues: Issue[];
  assigneeGroups?: IssueAssigneeGroup[];
  assigneeGroupQueryKey?: QueryKey;
  assigneeGroupFilter?: AssigneeGroupedIssuesFilter;
  filter: MyIssuesFilter;
  loadMoreScope?: string;
  loadMoreFilter?: MyIssuesFilter;
  ganttIssues: Issue[];
  visibleStatuses: IssueStatus[];
  hiddenStatuses: IssueStatus[];
  activeFilters: Omit<IssueFilters, "statusFilters" | "runningIssueIds">;
  activity: IssueSurfaceActivity;
  childProgressMap: Map<string, ChildProgress>;
  projectMap: Map<string, Project>;
  resolveTableExportLookups: (needs: {
    projects: boolean;
    childProgress: boolean;
  }) => Promise<{
    projectMap: Map<string, Project>;
    childProgressMap: Map<string, ChildProgress>;
  }>;
  fetchNextFlatPage: () => Promise<unknown>;
  hasNextFlatPage: boolean;
  isFetchingNextFlatPage: boolean;
  flatTotal: number;
  /** The flat window query is in error state (initial or next-page fetch,
   *  retries exhausted). Auto-advance loops MUST stop on this — re-firing
   *  after every failed attempt is a request storm — and surface an explicit
   *  Retry instead. */
  flatWindowError: boolean;
  /** The flat window failed before producing ANY data (cold load, retries
   *  exhausted). This is NOT an empty workspace: isEmpty stays false and the
   *  surface must render an error state with a Retry instead of the
   *  create-issue empty state (round-5 review P2). */
  flatWindowColdError: boolean;
  /** Explicit recovery for flatWindowColdError — refetches the flat window. */
  refetchFlatWindow: () => Promise<unknown>;
  filterIssuesForExport: (issues: Issue[]) => Issue[];
  isLoading: boolean;
  /** The window's data is being revalidated while the previous snapshot is
   *  shown as a placeholder (sort/date change, or any grouped-board filter
   *  change). Drives the header's deferred refresh indicator — content stays
   *  put, so this is NOT a loading state. */
  isRefreshing: boolean;
  isEmpty: boolean;
}

export function useIssueSurfaceData({
  wsId,
  queryPlan,
  projectId,
  usesAssigneeBoard,
  usesGantt,
  usesTable,
  ganttShowCompleted,
  sort,
  tableFacets,
  workingFacets,
  activity,
  statusFilters,
  priorityFilters,
  assigneeFilters,
  includeNoAssignee,
  creatorFilters,
  projectFilters,
  includeNoProject,
  labelFilters,
  propertyFilters,
  agentRunningFilter,
  showSubIssues,
  loadProjects,
}: {
  wsId: string;
  queryPlan: IssueSurfaceQueryPlan;
  projectId?: string;
  usesAssigneeBoard: boolean;
  usesGantt: boolean;
  usesTable: boolean;
  /** Gantt's "show completed" display toggle. The canvas hides done/cancelled
   *  rows without it, so the working scope has to honour it too. */
  ganttShowCompleted: boolean;
  sort: IssueSortParam;
  tableFacets: IssueFlatFilter;
  /** tableFacets restricted to the running set (ids facet) — the working
   *  chip's authoritative scope, and the table window itself while the
   *  agents-working filter is on (identical query key in that state). */
  workingFacets: IssueFlatFilter;
  /** Owned by the controller so the agents-working facet and the client
   *  display filters read the same task snapshot. */
  activity: IssueSurfaceActivity;
  statusFilters: IssueStatus[];
  priorityFilters: IssueFilterState["priorityFilters"];
  assigneeFilters: IssueFilterState["assigneeFilters"];
  includeNoAssignee: boolean;
  creatorFilters: IssueFilterState["creatorFilters"];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
  propertyFilters: Record<string, string[]>;
  agentRunningFilter: boolean;
  showSubIssues: boolean;
  loadProjects: boolean;
}): IssueSurfaceData {
  const filterContext = useMemo(
    () => ({ activityByIssueId: activity.activityByIssueId }),
    [activity.activityByIssueId],
  );

  const assigneeGroupFilter = useMemo<AssigneeGroupedIssuesFilter>(
    () => ({
      ...queryPlan.groupedScopeFilter,
      statuses: statusFilters.length > 0 ? statusFilters : [...ALL_STATUSES],
      priorities: priorityFilters,
      assignee_filters: assigneeFilters,
      include_no_assignee: includeNoAssignee,
      creator_filters: creatorFilters,
      project_ids: projectFilters,
      include_no_project: includeNoProject,
      label_ids: labelFilters,
    }),
    [
      assigneeFilters,
      creatorFilters,
      includeNoAssignee,
      includeNoProject,
      labelFilters,
      priorityFilters,
      projectFilters,
      queryPlan.groupedScopeFilter,
      statusFilters,
    ],
  );

  const activeAssigneeGroupsOptions = issueSurfaceAssigneeGroupsOptions(
    wsId,
    queryPlan,
    assigneeGroupFilter,
    sort,
  );

  const statusIssuesQuery = useQuery({
    ...issueSurfaceListOptions(wsId, queryPlan, sort),
    enabled: !usesAssigneeBoard && !usesGantt && !usesTable,
  });
  const assigneeGroupsQuery = useQuery({
    ...activeAssigneeGroupsOptions,
    enabled: usesAssigneeBoard,
  });
  const ganttIssuesQuery = useQuery({
    ...issueSurfaceGanttOptions(wsId, projectId ?? ""),
    enabled: usesGantt,
  });
  const flatIssuesQuery = useInfiniteQuery({
    ...issueSurfaceFlatOptions(wsId, queryPlan, sort, tableFacets),
    enabled: usesTable,
  });

  const flatIssues = useMemo(
    () =>
      flatIssuesQuery.data?.pages.flatMap((page) => page.issues) ??
      EMPTY_ISSUES,
    [flatIssuesQuery.data?.pages],
  );
  const { fetchNextPage: fetchNextFlatPage } = flatIssuesQuery;
  const fetchNextFlatPageNoCancel = useCallback(
    () => fetchNextFlatPage({ cancelRefetch: false }),
    [fetchNextFlatPage],
  );
  // The LATEST page's total, not page 1's: totals drift while concurrent
  // writes land, and the pagination protocol itself advances on the latest
  // page's total — a consumer holding page 1's stale (smaller) total while
  // hasNextPage kept advancing is how the structure ceiling stopped being a
  // hard limit (round-4 review P1#2).
  const flatPages = flatIssuesQuery.data?.pages;
  const flatTotal = flatPages?.[flatPages.length - 1]?.total ?? 0;

  // Running-restricted window — the table branch of the workingScopeIssues
  // projection below. While the agents-working filter is on this observer
  // shares the main flat query's key (one fetch); while it is off, it keeps
  // the filter's window warm and gives the chip its authoritative scope.
  const hasRunningIssues = activity.runningIssueIds.size > 0;
  const workingWindowEnabled = usesTable && hasRunningIssues;
  const workingWindowQuery = useInfiniteQuery({
    ...issueSurfaceFlatOptions(wsId, queryPlan, sort, workingFacets),
    enabled: workingWindowEnabled,
  });
  // Materialize the running window ONLY under the same hard gates as the
  // structure loop — while the agents-working filter is on this query IS the
  // main table window (shared key), so an ungated chip-driven loop would
  // stuff the main cache past the very ceiling TableView just enforced, and
  // the running set has no server-side size bound (round-5 review P1). Under
  // the gates the loop is bounded: an over-ceiling window stops after page 1
  // (fresh total > ceiling) and presents as UNKNOWN instead of a number; an
  // error stops the loop the same way.
  //
  // Ownership: this effect drives the query ONLY while the filter is OFF
  // (background chip scope). Once the filter is on, the shared query already
  // has pagination owners — TableView's structure loop and the scroll
  // sentinel — and a second responder issuing fetchNextPage() from the same
  // render snapshot cancel/restarts the first one's fetch. The abandoned
  // HTTP request is not abortable (the queryFn does not thread AbortSignal),
  // so every offset would be requested twice (round-6 review R1).
  const {
    hasNextPage: workingWindowHasNext,
    isFetchingNextPage: workingWindowFetchingNext,
    isError: workingWindowError,
    isPlaceholderData: workingWindowIsPlaceholder,
    fetchNextPage: fetchNextWorkingWindowPage,
  } = workingWindowQuery;
  const workingWindowPages = workingWindowQuery.data?.pages;
  const workingWindowTotal =
    workingWindowPages?.[workingWindowPages.length - 1]?.total ?? 0;
  const workingWindowLoaded = useMemo(
    () =>
      workingWindowPages?.reduce((count, page) => count + page.issues.length, 0) ??
      0,
    [workingWindowPages],
  );
  useEffect(() => {
    if (
      shouldAutoLoadNextWindowPage({
        windowWanted: workingWindowEnabled && !agentRunningFilter,
        total: workingWindowTotal,
        loadedCount: workingWindowLoaded,
        hasNextPage: workingWindowHasNext,
        isFetchingNextPage: workingWindowFetchingNext,
        hasError: workingWindowError,
      })
    ) {
      // cancelRefetch: false — if some other observer already has a fetch in
      // flight for this query, do nothing rather than cancel/restart it.
      void fetchNextWorkingWindowPage({ cancelRefetch: false });
    }
  }, [
    agentRunningFilter,
    fetchNextWorkingWindowPage,
    workingWindowEnabled,
    workingWindowError,
    workingWindowFetchingNext,
    workingWindowHasNext,
    workingWindowLoaded,
    workingWindowTotal,
  ]);
  const bucketedIssues = useMemo(() => {
    return usesAssigneeBoard
      ? (assigneeGroupsQuery.data?.groups.flatMap((group) => group.issues) ?? [])
      : (statusIssuesQuery.data ?? EMPTY_ISSUES);
  }, [assigneeGroupsQuery.data?.groups, statusIssuesQuery.data, usesAssigneeBoard]);

  // `cancelled` is a first-class default status (MUL-4290): it is fetched into
  // the cache like every other status and flows straight through to list /
  // board / swimlane columns, header facet counts, batch selection, and the
  // isEmpty check. The status filter narrows this set like any other status —
  // it no longer unlocks an otherwise-hidden bucket.
  const ganttIssues = ganttIssuesQuery.data ?? EMPTY_ISSUES;
  const surfaceIssues = usesGantt
    ? ganttIssues
    : usesTable
      ? flatIssues
      : bucketedIssues;

  const baseFilterState = useMemo<IssueFilterState>(
    () => ({
      statusFilters,
      priorityFilters,
      assigneeFilters,
      includeNoAssignee,
      creatorFilters,
      projectFilters,
      includeNoProject,
      labelFilters,
      propertyFilters,
      workingOnly: agentRunningFilter,
      showSubIssues,
    }),
    [
      agentRunningFilter,
      assigneeFilters,
      creatorFilters,
      includeNoAssignee,
      includeNoProject,
      labelFilters,
      priorityFilters,
      projectFilters,
      propertyFilters,
      showSubIssues,
      statusFilters,
    ],
  );

  const issues = useMemo(
    () => applyIssueFilters(surfaceIssues, baseFilterState, filterContext),
    [baseFilterState, filterContext, surfaceIssues],
  );
  const filterIssuesForExport = useCallback(
    (exportIssues: Issue[]) =>
      applyIssueFilters(exportIssues, baseFilterState, filterContext),
    [baseFilterState, filterContext],
  );

  const statuslessFilterState = useMemo<IssueFilterState>(
    () => ({
      ...baseFilterState,
      statusFilters: [],
    }),
    [baseFilterState],
  );

  const swimlaneIssues = useMemo(
    () => applyIssueFilters(surfaceIssues, statuslessFilterState, filterContext),
    [filterContext, statuslessFilterState, surfaceIssues],
  );

  const filteredGanttIssues = useMemo(
    () =>
      ganttCanvasRows(
        applyIssueFilters(ganttIssues, baseFilterState, filterContext),
        ganttShowCompleted,
      ),
    [baseFilterState, filterContext, ganttIssues, ganttShowCompleted],
  );

  // The assignee-grouped board renders straight from `groups`, bypassing the
  // flat applyIssueFilters output — re-apply the client-only display filters
  // (Show sub-issues + agents-working) per group.
  const filteredAssigneeGroups = useMemo(
    () =>
      filterAssigneeGroups(assigneeGroupsQuery.data?.groups, {
        showSubIssues,
        agentRunningFilter,
        runningIssueIds: activity.runningIssueIds,
        propertyFilters,
      }),
    [
      activity.runningIssueIds,
      agentRunningFilter,
      assigneeGroupsQuery.data?.groups,
      propertyFilters,
      showSubIssues,
    ],
  );

  // The rows the agents-working filter leaves on screen — i.e. exactly what
  // you get when you click the header chip.
  //
  // This is deliberately a PROJECTION OF THE RENDER PIPELINE, not a second
  // pass over the task snapshot: it reuses the same predicates, the same
  // filter state and the same per-mode source as the rows below, with
  // `workingOnly` forced on. Turning the filter on only adds `workingOnly` to
  // this same pipeline, so the set is the post-click list whether the filter
  // is currently on or off.
  //
  // The chip counts AGENTS, not this list's length, so these are not equal
  // (one agent can hold two of these rows). What this set does decide is
  // WHICH agents the chip counts — only those working on rows that survive
  // the filters. Re-deriving that scope from the snapshot instead is what
  // made the chip disagree with the list it was filtering: any active
  // status/assignee/label filter, or a sub-issue hidden by the display
  // toggle, moved the list but not the chip (MUL-4884).
  //
  // Each branch below must take the SAME source the matching branch of
  // IssueSurface renders:
  //   - gantt          → the canvas set (scheduled + dated + showCompleted)
  //   - assignee board → the grouped response, not the flat list
  //   - table          → the ids-facet window (the query the filter itself
  //     runs) — the table's offset pages are only a SLICE of its window, so
  //     a loaded-rows projection says "nothing working" whenever the running
  //     issues sit on unfetched pages (round-3 review P2#3)
  //   - board / list / swimlane → the flat filtered list
  //
  // Swimlane deliberately has no branch: SwimLaneView draws its cards from
  // `issues` (status filter applied) and only uses the statusless
  // `swimlaneIssues` for LANE DISCOVERY, so scoping the chip to the
  // statusless set would count rows the canvas never draws.
  const workingScopeIssues = useMemo(() => {
    if (usesGantt) {
      return ganttCanvasRows(
        applyIssueFilters(
          ganttIssues,
          { ...baseFilterState, workingOnly: true },
          filterContext,
        ),
        ganttShowCompleted,
      );
    }
    if (usesAssigneeBoard) {
      return (
        filterAssigneeGroups(assigneeGroupsQuery.data?.groups, {
          showSubIssues,
          agentRunningFilter: true,
          runningIssueIds: activity.runningIssueIds,
          propertyFilters,
        }) ?? []
      ).flatMap((group) => group.issues);
    }
    if (usesTable) {
      // The table's loaded pages are only a SLICE of its window, so no
      // fallback to them can honestly claim a precise working set. The scope
      // is either the COMPLETE ids-facet window (fetched to the end for THIS
      // key, no error), an empty running set (trivially complete without a
      // request), or UNKNOWN — resolving, failed, or over the
      // materialization ceiling (round-5 review P2). Consumers render
      // unknown as unknown; they never get a number to mis-present.
      //
      // Placeholder data is explicitly EXCLUDED from completeness: on a
      // re-key (running set or facet change) keepPreviousData shows the OLD
      // key's window, and pairing those rows with the NEW task snapshot
      // publishes a precise-looking number for a scope nobody fetched —
      // including re-publishing a ceiling-capped old window as if it were
      // complete (round-6 review P2#1). While the new key resolves, the
      // scope is unknown.
      if (!hasRunningIssues) return EMPTY_ISSUES;
      const workingWindowComplete =
        workingWindowQuery.data !== undefined &&
        !workingWindowIsPlaceholder &&
        !workingWindowHasNext &&
        !workingWindowError;
      if (!workingWindowComplete) return undefined;
      return applyIssueFilters(
        workingWindowQuery.data.pages.flatMap((page) => page.issues),
        { ...baseFilterState, workingOnly: true },
        filterContext,
      );
    }
    return applyIssueFilters(
      surfaceIssues,
      { ...baseFilterState, workingOnly: true },
      filterContext,
    );
  }, [
    activity.runningIssueIds,
    assigneeGroupsQuery.data?.groups,
    baseFilterState,
    filterContext,
    ganttIssues,
    ganttShowCompleted,
    hasRunningIssues,
    propertyFilters,
    showSubIssues,
    surfaceIssues,
    usesAssigneeBoard,
    usesGantt,
    usesTable,
    workingWindowError,
    workingWindowHasNext,
    workingWindowIsPlaceholder,
    workingWindowQuery.data,
  ]);

  const {
    data: childProgressData,
    refetch: refetchChildProgress,
  } = useQuery(childIssueProgressOptions(wsId));
  const childProgressMap = childProgressData ?? EMPTY_CHILD_PROGRESS;
  const {
    data: projectData,
    refetch: refetchProjects,
  } = useQuery({
    ...projectListOptions(wsId),
    enabled: loadProjects,
  });
  const projects = projectData ?? EMPTY_PROJECTS;
  const projectMap = useMemo(
    () => new Map(projects.map((project) => [project.id, project])),
    [projects],
  );
  const resolveTableExportLookups = useCallback(
    async (needs: { projects: boolean; childProgress: boolean }) => {
      const [projectResult, progressResult] = await Promise.all([
        needs.projects ? refetchProjects() : Promise.resolve(null),
        needs.childProgress
          ? refetchChildProgress()
          : Promise.resolve(null),
      ]);
      if (projectResult?.error) throw projectResult.error;
      if (progressResult?.error) throw progressResult.error;
      if (needs.projects && !projectResult?.data) {
        throw new Error("Failed to load project data for export");
      }
      if (needs.childProgress && !progressResult?.data) {
        throw new Error("Failed to load child progress for export");
      }
      const resolvedProjects = projectResult?.data ?? projects;
      return {
        projectMap: new Map(
          resolvedProjects.map((project) => [project.id, project]),
        ),
        childProgressMap: progressResult?.data ?? childProgressMap,
      };
    },
    [
      childProgressMap,
      projects,
      refetchChildProgress,
      refetchProjects,
    ],
  );

  const visibleStatuses = useMemo<IssueStatus[]>(() => {
    // Default view shows every lifecycle status, `cancelled` last (its
    // canonical position in ALL_STATUSES). An active status filter narrows to
    // the selected subset while preserving that order.
    if (statusFilters.length > 0) {
      return ALL_STATUSES.filter((s) => statusFilters.includes(s));
    }
    return ALL_STATUSES;
  }, [statusFilters]);

  // Hidden columns are the lifecycle statuses not currently visible, so
  // `cancelled` participates in the board show/hide controls exactly like the
  // rest of the statuses.
  const hiddenStatuses = useMemo<IssueStatus[]>(
    () => ALL_STATUSES.filter((s) => !visibleStatuses.includes(s)),
    [visibleStatuses],
  );

  const activeFilters = useMemo(
    () => ({
      priorityFilters,
      assigneeFilters,
      includeNoAssignee,
      creatorFilters,
      projectFilters,
      includeNoProject,
      labelFilters,
      propertyFilters,
      agentRunningFilter,
      showSubIssues,
    }),
    [
      agentRunningFilter,
      assigneeFilters,
      creatorFilters,
      includeNoAssignee,
      includeNoProject,
      labelFilters,
      propertyFilters,
      priorityFilters,
      projectFilters,
      showSubIssues,
    ],
  );

  const isLoading = usesAssigneeBoard
    ? assigneeGroupsQuery.isLoading
    : usesGantt
      ? ganttIssuesQuery.isLoading
      : usesTable
        ? flatIssuesQuery.isLoading
        : statusIssuesQuery.isLoading;

  // Placeholder-backed revalidation of the ACTIVE query only. First loads are
  // isLoading (no previous data to place-hold); gantt has no placeholder
  // phase (its key carries no sort/filter).
  const isRefreshing = usesAssigneeBoard
    ? assigneeGroupsQuery.isPlaceholderData
    : usesGantt
      ? false
      : usesTable
        ? flatIssuesQuery.isPlaceholderData
        : statusIssuesQuery.isPlaceholderData;

  return {
    surfaceIssues,
    projectIssues: surfaceIssues,
    issues,
    swimlaneIssues,
    workingScopeIssues,
    filteredGanttIssues,
    assigneeGroups: usesAssigneeBoard ? filteredAssigneeGroups : undefined,
    assigneeGroupQueryKey: usesAssigneeBoard
      ? activeAssigneeGroupsOptions.queryKey
      : undefined,
    assigneeGroupFilter: usesAssigneeBoard ? assigneeGroupFilter : undefined,
    filter: queryPlan.queryFilter,
    loadMoreScope: queryPlan.loadMoreScope,
    loadMoreFilter: queryPlan.loadMoreFilter,
    ganttIssues,
    visibleStatuses,
    hiddenStatuses,
    activeFilters,
    activity,
    childProgressMap,
    projectMap,
    resolveTableExportLookups,
    // cancelRefetch: false — the structure loop and the scroll sentinel are
    // independent responders on this query; the default cancel/restart
    // semantics turn a same-snapshot double call into a duplicated HTTP
    // request, because the abandoned fetch is not abortable (the queryFn
    // does not thread AbortSignal) (round-6 review R1).
    fetchNextFlatPage: fetchNextFlatPageNoCancel,
    hasNextFlatPage: flatIssuesQuery.hasNextPage ?? false,
    isFetchingNextFlatPage: flatIssuesQuery.isFetchingNextPage,
    flatTotal,
    flatWindowError: flatIssuesQuery.isError,
    flatWindowColdError: flatIssuesQuery.isError && flatIssuesQuery.data === undefined,
    refetchFlatWindow: flatIssuesQuery.refetch,
    filterIssuesForExport,
    isLoading,
    isRefreshing,
    // isEmpty asserts "this window has no issues". The board/list/swimlane
    // data IS the full window, so an empty result proves it. The gantt query
    // is a scheduled-only PROJECTION — an empty subset cannot prove the
    // window is empty, so never claim it (same "uncertain → don't assert"
    // rule as surface membership). GanttView renders its own accurate
    // "no scheduled issues" empty state instead of the generic create-issue
    // one. A FAILED table fetch proves nothing either: total defaults to 0
    // after a cold-load error, and claiming empty there swaps a 5xx/offline
    // for a "create your first issue" screen with no recovery path (round-5
    // review P2) — only a successful zero-result window is empty.
    isEmpty:
      !isLoading &&
      !usesGantt &&
      (usesTable
        ? !flatIssuesQuery.isError && flatTotal === 0
        : surfaceIssues.length === 0),
  };
}
