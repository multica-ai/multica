"use client";

import { useMemo } from "react";
import { useQuery, type QueryKey } from "@tanstack/react-query";
import type { Issue, IssueAssigneeGroup, Project } from "@multica/core/types";
import { ALL_STATUSES, BOARD_STATUSES } from "@multica/core/issues/config";
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

export interface IssueSurfaceData {
  surfaceIssues: Issue[];
  projectIssues: Issue[];
  issues: Issue[];
  swimlaneIssues: Issue[];
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
  sort,
  statusFilters,
  priorityFilters,
  assigneeFilters,
  includeNoAssignee,
  creatorFilters,
  projectFilters,
  includeNoProject,
  labelFilters,
  agentRunningFilter,
  showSubIssues,
  loadProjects,
}: {
  wsId: string;
  queryPlan: IssueSurfaceQueryPlan;
  projectId?: string;
  usesAssigneeBoard: boolean;
  usesGantt: boolean;
  sort: IssueSortParam;
  statusFilters: IssueStatus[];
  priorityFilters: IssueFilterState["priorityFilters"];
  assigneeFilters: IssueFilterState["assigneeFilters"];
  includeNoAssignee: boolean;
  creatorFilters: IssueFilterState["creatorFilters"];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
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
      statuses: statusFilters.length > 0 ? statusFilters : [...BOARD_STATUSES],
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

  // Cancelled issues are always fetched into the cache (PAGINATED_STATUSES),
  // but stay hidden until the status filter explicitly selects "cancelled".
  // Gating the flattened list here means every downstream consumer — list /
  // board / swimlane columns, header facet counts, batch selection, and the
  // isEmpty check — excludes cancelled by default with no per-view branching.
  const showCancelled = statusFilters.includes("cancelled");
  const visibleBucketedIssues = useMemo(
    () =>
      showCancelled
        ? bucketedIssues
        : bucketedIssues.filter((issue) => issue.status !== "cancelled"),
    [bucketedIssues, showCancelled],
  );

  const ganttIssues = ganttIssuesQuery.data ?? EMPTY_ISSUES;
  const surfaceIssues = usesGantt ? ganttIssues : visibleBucketedIssues;

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
    () => applyIssueFilters(ganttIssues, baseFilterState, filterContext),
    [baseFilterState, filterContext, ganttIssues],
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
      }),
    [
      activity.runningIssueIds,
      agentRunningFilter,
      assigneeGroupsQuery.data?.groups,
      showSubIssues,
    ],
  );

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
    if (statusFilters.length > 0) {
      // ALL_STATUSES keeps canonical order and places `cancelled` last, so a
      // Cancelled section appears (only) when the filter explicitly selects it.
      return ALL_STATUSES.filter((s) => statusFilters.includes(s));
    }
    // Default view: the six board statuses, cancelled excluded.
    return BOARD_STATUSES;
  }, [statusFilters]);

  // Hidden columns come from the board set only, so `cancelled` is never
  // offered as a hideable/always-on board column (it is filter-gated, not a
  // persistent column).
  const hiddenStatuses = useMemo<IssueStatus[]>(
    () => BOARD_STATUSES.filter((s) => !visibleStatuses.includes(s)),
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
