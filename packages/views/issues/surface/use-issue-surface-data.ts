"use client";

import { useMemo } from "react";
import { useQuery, type QueryKey } from "@tanstack/react-query";
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
import {
  useIssueSurfaceActivity,
  type IssueSurfaceActivity,
} from "./activity";

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
  /** The rows the agents-working filter would leave on screen. See the
   *  `workingScopeIssues` memo for why this is a projection of the render
   *  pipeline rather than a second derivation from the task snapshot. */
  workingScopeIssues: Issue[];
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
  ganttShowCompleted,
  sort,
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
  /** Gantt's "show completed" display toggle. The canvas hides done/cancelled
   *  rows without it, so the working scope has to honour it too. */
  ganttShowCompleted: boolean;
  sort: IssueSortParam;
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
  const activity = useIssueSurfaceActivity();
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
    enabled: !usesAssigneeBoard && !usesGantt,
  });
  const assigneeGroupsQuery = useQuery({
    ...activeAssigneeGroupsOptions,
    enabled: usesAssigneeBoard,
  });
  const ganttIssuesQuery = useQuery({
    ...issueSurfaceGanttOptions(wsId, projectId ?? ""),
    enabled: usesGantt,
  });

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
  const surfaceIssues = usesGantt ? ganttIssues : bucketedIssues;

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
    propertyFilters,
    showSubIssues,
    surfaceIssues,
    usesAssigneeBoard,
    usesGantt,
  ]);

  const { data: childProgressMap = EMPTY_CHILD_PROGRESS } = useQuery(
    childIssueProgressOptions(wsId),
  );
  const { data: projects = EMPTY_PROJECTS } = useQuery({
    ...projectListOptions(wsId),
    enabled: loadProjects,
  });
  const projectMap = useMemo(
    () => new Map(projects.map((project) => [project.id, project])),
    [projects],
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
      : statusIssuesQuery.isLoading;

  // Placeholder-backed revalidation of the ACTIVE query only. First loads are
  // isLoading (no previous data to place-hold); gantt has no placeholder
  // phase (its key carries no sort/filter).
  const isRefreshing = usesAssigneeBoard
    ? assigneeGroupsQuery.isPlaceholderData
    : usesGantt
      ? false
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
    isLoading,
    isRefreshing,
    // isEmpty asserts "this window has no issues". The board/list/swimlane
    // data IS the full window, so an empty result proves it. The gantt query
    // is a scheduled-only PROJECTION — an empty subset cannot prove the
    // window is empty, so never claim it (same "uncertain → don't assert"
    // rule as surface membership). GanttView renders its own accurate
    // "no scheduled issues" empty state instead of the generic create-issue
    // one.
    isEmpty: !isLoading && !usesGantt && surfaceIssues.length === 0,
  };
}
