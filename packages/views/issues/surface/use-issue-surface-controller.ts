"use client";

import { useEffect, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import type { QueryKey } from "@tanstack/react-query";
import type {
  Issue,
  IssueAssigneeGroup,
  IssueStatus,
  Project,
} from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { dateOnlyToLocalDate } from "@multica/core/issues/date";
import type {
  AssigneeGroupedIssuesFilter,
  IssueSortParam,
  MyIssuesFilter,
} from "@multica/core/issues/queries";
import {
  buildIssueSurfaceQueryPlan,
  type IssueSurfaceQueryPlan,
} from "@multica/core/issues/surface/query-plan";
import type { IssueScope } from "@multica/core/issues/surface/scope";
import type { IssueDateFilter, SortField } from "@multica/core/issues/stores/view-store";
import { propertyListOptions } from "@multica/core/properties";
import { propertyIdFromViewKey } from "@multica/core/issues/stores/view-store";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import type { IssueFilters } from "../utils/filter";
import type { ChildProgress } from "../components/list-row";
import type { IssueSurfaceMode } from "./types";
import type { IssueSurfaceActivity } from "./activity";
import type { IssueSurfaceActions } from "./actions-context";
import {
  type IssueSurfaceSelection,
  useCreateIssueSurfaceSelection,
} from "./selection-context";
import type { IssueCreateDefaults } from "./types";
import {
  useIssueSurfaceActions,
  type MoveIssueUpdates,
} from "./use-issue-surface-actions";
import { useIssueSurfaceData } from "./use-issue-surface-data";

interface UseIssueSurfaceControllerInput {
  scope: IssueScope;
  modes: IssueSurfaceMode[];
  createDefaults?: IssueCreateDefaults;
}

export interface IssueSurfaceController {
  scopeKey: string;
  projectId?: string;
  createDefaults: IssueCreateDefaults;
  viewMode: IssueSurfaceMode;
  allowGantt: boolean;
  surfaceIssues: Issue[];
  projectIssues: Issue[];
  issues: Issue[];
  swimlaneIssues: Issue[];
  /** The rows the agents-working filter would leave on screen. Feeds the
   *  header chip so its count IS the post-click row count (MUL-4884). */
  workingScopeIssues: Issue[];
  filteredGanttIssues: Issue[];
  assigneeGroups?: IssueAssigneeGroup[];
  assigneeGroupQueryKey?: QueryKey;
  assigneeGroupFilter?: AssigneeGroupedIssuesFilter;
  filter: MyIssuesFilter;
  loadMoreScope?: string;
  loadMoreFilter?: MyIssuesFilter;
  sort: IssueSortParam;
  ganttIssues: Issue[];
  visibleStatuses: IssueStatus[];
  hiddenStatuses: IssueStatus[];
  activeFilters: Omit<IssueFilters, "statusFilters" | "runningIssueIds">;
  activity: IssueSurfaceActivity;
  actions: IssueSurfaceActions;
  selection: IssueSurfaceSelection;
  childProgressMap: Map<string, ChildProgress>;
  projectMap: Map<string, Project>;
  isLoading: boolean;
  /** See IssueSurfaceData.isRefreshing — placeholder-backed revalidation. */
  isRefreshing: boolean;
  isEmpty: boolean;
  openCreateIssue: (defaults?: IssueCreateDefaults) => void;
  moveIssue: (
    issueId: string,
    updates: MoveIssueUpdates,
    onSettled?: () => void,
  ) => void;
}

function issueDateFilterToApiParams(filter: IssueDateFilter | null) {
  if (!filter) return {};

  const from = dateOnlyToLocalDate(filter.from);
  const to = dateOnlyToLocalDate(filter.to);
  if (!from || !to) return {};

  const start = from <= to ? from : to;
  const endSource = from <= to ? to : from;
  const end = new Date(endSource);
  end.setDate(end.getDate() + 1);

  return {
    date_field: filter.field,
    date_start: start.toISOString(),
    date_end: end.toISOString(),
  };
}

export function useIssueSurfaceController({
  scope,
  modes,
  createDefaults,
}: UseIssueSurfaceControllerInput): IssueSurfaceController {
  const wsId = useWorkspaceId();
  const queryPlan = useMemo<IssueSurfaceQueryPlan>(
    () => buildIssueSurfaceQueryPlan(scope),
    [scope],
  );
  const scopeKey = queryPlan.scopeKey;
  const projectId = scope.type === "project" ? scope.projectId : undefined;

  const viewMode = useViewStore((s) => s.viewMode);
  const setViewMode = useViewStore((s) => s.setViewMode);
  const grouping = useViewStore((s) => s.grouping);
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const dateFilter = useViewStore((s) => s.dateFilter);
  const statusFilters = useViewStore((s) => s.statusFilters);
  const priorityFilters = useViewStore((s) => s.priorityFilters);
  const assigneeFilters = useViewStore((s) => s.assigneeFilters);
  const includeNoAssignee = useViewStore((s) => s.includeNoAssignee);
  const creatorFilters = useViewStore((s) => s.creatorFilters);
  const projectFilters = useViewStore((s) => s.projectFilters);
  const includeNoProject = useViewStore((s) => s.includeNoProject);
  const labelFilters = useViewStore((s) => s.labelFilters);
  const propertyFilters = useViewStore((s) => s.propertyFilters);
  const agentRunningFilter = useViewStore((s) => s.agentRunningFilter);
  const showSubIssues = useViewStore((s) => s.showSubIssues);
  const ganttShowCompleted = useViewStore((s) => s.ganttShowCompleted);
  const cardProperties = useViewStore((s) => s.cardProperties);
  const swimlaneGrouping = useViewStore((s) => s.swimlaneGrouping);

  const allowedModes = useMemo(() => new Set<IssueSurfaceMode>(modes), [modes]);
  const fallbackMode = modes[0] ?? "list";
  const effectiveViewMode = allowedModes.has(viewMode as IssueSurfaceMode)
    ? (viewMode as IssueSurfaceMode)
    : fallbackMode;

  useEffect(() => {
    if (!allowedModes.has(viewMode as IssueSurfaceMode)) {
      setViewMode(fallbackMode);
    }
  }, [allowedModes, fallbackMode, setViewMode, viewMode]);

  const resolvedCreateDefaults = useMemo(
    () => ({ ...queryPlan.createDefaults, ...createDefaults }),
    [createDefaults, queryPlan.createDefaults],
  );

  const dateParams = useMemo(
    () => issueDateFilterToApiParams(dateFilter),
    [dateFilter],
  );
  // Active property catalog. Persisted view state can outlive definitions
  // (archive/delete): filters keyed by a non-active definition are stripped
  // before they reach the predicates, and a sort on a non-active definition
  // degrades to manual order — matching what the header already shows.
  const { data: workspaceProperties = [], isSuccess: catalogSettled } = useQuery(propertyListOptions(wsId));
  const activePropertyIds = useMemo(
    () => new Set(workspaceProperties.map((p) => p.id)),
    [workspaceProperties],
  );
  const effectivePropertyFilters = useMemo(() => {
    // While the catalog is still loading (or errored), persisted filters are
    // passed through UNCHANGED: treating a cold catalog as confirmed-empty
    // would silently drop the user's filters on first paint (clean-room
    // review F6). Old servers 404 into a SETTLED empty catalog, so the
    // stripping below still protects that path.
    if (!catalogSettled) return propertyFilters;
    const entries = Object.entries(propertyFilters).filter(
      ([propertyId, selected]) => selected.length > 0 && activePropertyIds.has(propertyId),
    );
    if (entries.length === Object.keys(propertyFilters).length) return propertyFilters;
    return Object.fromEntries(entries);
  }, [activePropertyIds, catalogSettled, propertyFilters]);

  // Custom-property sorts and filters are served by the backend: the sort
  // param carries `property:<id>` (typed ORDER BY expression server-side)
  // and the window bag carries the property filter, so results are correct
  // across pagination — not just the loaded window. A sort pinned to a
  // non-active definition degrades to position order.
  const rawPropertySortId = propertyIdFromViewKey(sortBy);
  const propertySortId =
    rawPropertySortId && (!catalogSettled || activePropertyIds.has(rawPropertySortId))
      ? rawPropertySortId
      : null;
  const sort = useMemo<IssueSortParam>(() => {
    const sortBy_: IssueSortParam["sort_by"] = propertySortId
      ? `property:${propertySortId}`
      : rawPropertySortId
        ? "position"
        : (sortBy as Exclude<SortField, `property:${string}`>);
    return {
      sort_by: sortBy_,
      sort_direction: sortBy_ !== "position" ? sortDirection : undefined,
      ...dateParams,
      ...(Object.keys(effectivePropertyFilters).length > 0
        ? { properties: effectivePropertyFilters }
        : {}),
    };
  }, [dateParams, effectivePropertyFilters, propertySortId, rawPropertySortId, sortBy, sortDirection]);

  const selection = useCreateIssueSurfaceSelection(
    scopeKey,
    `${scopeKey}:${effectiveViewMode}`,
  );

  const usesAssigneeBoard =
    effectiveViewMode === "board" && grouping === "assignee";
  const usesGantt = effectiveViewMode === "gantt" && !!projectId;

  const projectFilterState = useMemo(
    () => ({
      projectFilters: scope.type === "project" ? [] : projectFilters,
      includeNoProject: scope.type === "project" ? false : includeNoProject,
    }),
    [includeNoProject, projectFilters, scope.type],
  );
  const { projectFilters: viewProjectFilters, includeNoProject: viewIncludeNoProject } =
    projectFilterState;

  const data = useIssueSurfaceData({
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
    projectFilters: viewProjectFilters,
    includeNoProject: viewIncludeNoProject,
    labelFilters,
    propertyFilters: effectivePropertyFilters,
    agentRunningFilter,
    showSubIssues,
    loadProjects:
      cardProperties.project ||
      (effectiveViewMode === "swimlane" && swimlaneGrouping === "project"),
  });

  const { actions, openCreateIssue, moveIssue } = useIssueSurfaceActions({
    createDefaults: resolvedCreateDefaults,
  });

  return {
    scopeKey,
    projectId,
    createDefaults: resolvedCreateDefaults,
    viewMode: effectiveViewMode,
    allowGantt: allowedModes.has("gantt") && !!projectId,
    ...data,
    sort,
    actions,
    selection,
    openCreateIssue,
    moveIssue,
  };
}
