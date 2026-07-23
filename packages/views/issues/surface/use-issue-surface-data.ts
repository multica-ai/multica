"use client";

import { useCallback, useMemo } from "react";
import {
  useQuery,
  type QueryKey,
} from "@tanstack/react-query";
import type { Issue, IssueAssigneeGroup, Project } from "@multica/core/types";
import { ALL_STATUSES } from "@multica/core/issues/config";
import { projectListOptions } from "@multica/core/projects/queries";
import {
  childIssueProgressOptions,
  type AssigneeGroupedIssuesFilter,
  type IssueSortParam,
  type MyIssuesFilter,
} from "@multica/core/issues/queries";
import {
  issueSurfaceAssigneeGroupsOptions,
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
import type { ChildProgress } from "../components/list-row";
import type { IssueSurfaceActivity } from "./activity";
import type {
  IssueStatusBranches,
  IssueStatusPagination,
} from "./use-issue-status-branches";
import type { IssueGroupBranches } from "./use-issue-group-branches";

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
  /** The rows the agents-working filter would leave on screen. `undefined`
   *  means the set is genuinely unknown: Table membership is server-owned,
   *  and the activity chip must not reconstruct a complete issue window just
   *  to decorate the header. */
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
  statusPagination: IssueStatusPagination;
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
  serverStatusBranches,
  serverGroupBranches,
  ganttShowCompleted,
  sort,
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
  serverStatusBranches: IssueStatusBranches;
  serverGroupBranches: IssueGroupBranches;
  /** Gantt's "show completed" display toggle. The canvas hides done/cancelled
   *  rows without it, so the working scope has to honour it too. */
  ganttShowCompleted: boolean;
  sort: IssueSortParam;
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
    enabled:
      !usesAssigneeBoard &&
      !usesGantt &&
      !usesTable &&
      !serverStatusBranches.enabled &&
      !serverGroupBranches.enabled,
  });
  const assigneeGroupsQuery = useQuery({
    ...activeAssigneeGroupsOptions,
    enabled: usesAssigneeBoard && !serverGroupBranches.enabled,
  });
  const ganttIssuesQuery = useQuery({
    ...issueSurfaceGanttOptions(wsId, projectId ?? ""),
    enabled: usesGantt,
  });
  const hasRunningIssues = activity.runningIssueIds.size > 0;
  const bucketedIssues = useMemo(() => {
    return serverStatusBranches.enabled
      ? serverStatusBranches.issues
      : serverGroupBranches.enabled
      ? serverGroupBranches.issues
      : usesAssigneeBoard
      ? (assigneeGroupsQuery.data?.groups.flatMap((group) => group.issues) ?? [])
      : (statusIssuesQuery.data ?? EMPTY_ISSUES);
  }, [
    assigneeGroupsQuery.data?.groups,
    serverStatusBranches.enabled,
    serverStatusBranches.issues,
    serverGroupBranches.enabled,
    serverGroupBranches.issues,
    statusIssuesQuery.data,
    usesAssigneeBoard,
  ]);

  // `cancelled` is a first-class default status (MUL-4290): it is fetched into
  // the cache like every other status and flows straight through to list /
  // board / swimlane columns, header facet counts, batch selection, and the
  // isEmpty check. The status filter narrows this set like any other status —
  // it no longer unlocks an otherwise-hidden bucket.
  const ganttIssues = ganttIssuesQuery.data ?? EMPTY_ISSUES;
  const surfaceIssues = usesGantt
    ? ganttIssues
    : usesTable
      ? EMPTY_ISSUES
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
    () =>
      serverStatusBranches.enabled
        ? surfaceIssues
        : applyIssueFilters(surfaceIssues, baseFilterState, filterContext),
    [
      baseFilterState,
      filterContext,
      serverStatusBranches.enabled,
      surfaceIssues,
    ],
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
  //   - table          → unknown unless the running set is empty; Table uses
  //     server cursor branches and never materializes a second full window
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
    if (usesAssigneeBoard && !serverGroupBranches.enabled) {
      return (
        filterAssigneeGroups(assigneeGroupsQuery.data?.groups, {
          showSubIssues,
          agentRunningFilter: true,
          runningIssueIds: activity.runningIssueIds,
          propertyFilters,
        }) ?? []
      ).flatMap((group) => group.issues);
    }
    if (usesTable || serverStatusBranches.enabled || serverGroupBranches.enabled) {
      // Table membership is server-owned and cursor paged. Do not rebuild a
      // second complete issue window merely to decorate the activity chip:
      // that was the final hidden auto-materialization loop behind the old
      // 1,000-row ceiling. An empty task set is trivially known; otherwise
      // keep the chip indeterminate until a bounded server facet supplies
      // the matching task/issue projection.
      if (!hasRunningIssues) return EMPTY_ISSUES;
      return undefined;
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
    serverStatusBranches.enabled,
    serverGroupBranches.enabled,
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

  const isLoading = serverGroupBranches.enabled
    ? serverGroupBranches.isLoading
    : usesAssigneeBoard
      ? assigneeGroupsQuery.isLoading
      : usesGantt
      ? ganttIssuesQuery.isLoading
      : usesTable
        ? false
        : serverStatusBranches.enabled
          ? serverStatusBranches.isLoading
          : statusIssuesQuery.isLoading;

  // Placeholder-backed revalidation of the ACTIVE query only. First loads are
  // isLoading (no previous data to place-hold); gantt has no placeholder
  // phase (its key carries no sort/filter).
  const isRefreshing = serverGroupBranches.enabled
    ? serverGroupBranches.isRefreshing
    : usesAssigneeBoard
      ? assigneeGroupsQuery.isPlaceholderData
      : usesGantt
      ? false
      : usesTable
        ? false
        : serverStatusBranches.enabled
          ? serverStatusBranches.isRefreshing
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
    statusPagination: serverStatusBranches.pagination,
    activeFilters,
    activity,
    childProgressMap,
    projectMap,
    resolveTableExportLookups,
    isLoading,
    isRefreshing,
    // isEmpty asserts "this window has no issues". The board/list/swimlane
    // data IS the full window, so an empty result proves it. The gantt query
    // is a scheduled-only PROJECTION — an empty subset cannot prove the
    // window is empty, so never claim it (same "uncertain → don't assert"
    // rule as surface membership). GanttView renders its own accurate
    // "no scheduled issues" empty state instead of the generic create-issue
    // one. Table owns its own branch-level loading, empty and retry states,
    // so this shared legacy surface projection never asserts Table empty.
    isEmpty:
      !isLoading &&
      !usesGantt &&
      !usesTable &&
      (serverStatusBranches.enabled
        ? serverStatusBranches.isTotalKnown &&
          serverStatusBranches.total === 0
        : serverGroupBranches.enabled
          ? !serverGroupBranches.isError &&
            serverGroupBranches.total === 0
        : surfaceIssues.length === 0),
  };
}
