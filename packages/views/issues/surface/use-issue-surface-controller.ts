"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
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
  IssueFlatFilter,
  IssueSortParam,
  MyIssuesFilter,
} from "@multica/core/issues/queries";
import {
  buildIssueSurfaceQueryPlan,
  type IssueSurfaceQueryPlan,
} from "@multica/core/issues/surface/query-plan";
import type { IssueScope } from "@multica/core/issues/surface/scope";
import { issueSurfaceFlatExportOptions } from "@multica/core/issues/surface/repository";
import type { IssueDateFilter, SortField } from "@multica/core/issues/stores/view-store";
import { propertyListOptions } from "@multica/core/properties";
import { propertyIdFromViewKey } from "@multica/core/issues/stores/view-store";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import type { IssueFilters } from "../utils/filter";
import type { ChildProgress } from "../components/list-row";
import type { IssueSurfaceMode } from "./types";
import { useIssueSurfaceActivity, type IssueSurfaceActivity } from "./activity";
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
  /** See IssueSurfaceData.workingScopeIssues — undefined means UNKNOWN. */
  workingScopeIssues: Issue[] | undefined;
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
  /** See IssueSurfaceData.flatWindowError. */
  flatWindowError: boolean;
  /** See IssueSurfaceData.flatWindowColdError. */
  flatWindowColdError: boolean;
  /** See IssueSurfaceData.refetchFlatWindow. */
  refetchFlatWindow: () => Promise<unknown>;
  tableSearch: string;
  setTableSearch: (query: string) => void;
  exportTableIssues: () => Promise<Issue[]>;
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

function useDebouncedTableSearch(value: string, delayMs = 250) {
  const [debouncedValue, setDebouncedValue] = useState(value.trim());

  useEffect(() => {
    const timer = window.setTimeout(
      () => setDebouncedValue(value.trim()),
      delayMs,
    );
    return () => window.clearTimeout(timer);
  }, [delayMs, value]);

  return debouncedValue;
}

export function useIssueSurfaceController({
  scope,
  modes,
  createDefaults,
}: UseIssueSurfaceControllerInput): IssueSurfaceController {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
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
  const tableColumns = useViewStore((s) => s.tableColumns);
  const [tableSearch, setTableSearch] = useState("");
  const debouncedTableSearch = useDebouncedTableSearch(tableSearch);

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

  const usesAssigneeBoard =
    effectiveViewMode === "board" && grouping === "assignee";
  const usesGantt = effectiveViewMode === "gantt" && !!projectId;
  const usesTable = effectiveViewMode === "table";

  const projectFilterState = useMemo(
    () => ({
      projectFilters: scope.type === "project" ? [] : projectFilters,
      includeNoProject: scope.type === "project" ? false : includeNoProject,
    }),
    [includeNoProject, projectFilters, scope.type],
  );
  const { projectFilters: viewProjectFilters, includeNoProject: viewIncludeNoProject } =
    projectFilterState;

  // The agents-working filter is a live client-side signal (WS-driven task
  // snapshot), but the table window is server-paginated — filtering loaded
  // pages would permanently hide matches on unfetched pages (round-2 review
  // P1#1). Send the running set as a server `ids` facet instead: total,
  // pagination, and export all see the same window, and snapshot changes
  // re-key the query. An EMPTY set is sent as an empty facet (empty window),
  // never dropped.
  const activity = useIssueSurfaceActivity();
  const sortedRunningIds = useMemo(
    () => [...activity.runningIssueIds].sort(),
    [activity.runningIssueIds],
  );

  const baseTableFacets = useMemo<IssueFlatFilter>(
    () => ({
      ...(debouncedTableSearch ? { q: debouncedTableSearch } : {}),
      ...(statusFilters.length > 0 ? { statuses: statusFilters } : {}),
      ...(priorityFilters.length > 0 ? { priorities: priorityFilters } : {}),
      ...(assigneeFilters.length > 0
        ? { assignee_filters: assigneeFilters }
        : {}),
      ...(includeNoAssignee ? { include_no_assignee: true } : {}),
      ...(creatorFilters.length > 0
        ? { creator_filters: creatorFilters }
        : {}),
      ...(viewProjectFilters.length > 0
        ? { project_ids: viewProjectFilters }
        : {}),
      ...(viewIncludeNoProject ? { include_no_project: true } : {}),
      ...(labelFilters.length > 0 ? { label_ids: labelFilters } : {}),
      ...(showSubIssues === false ? { top_level_only: true } : {}),
    }),
    [
      assigneeFilters,
      creatorFilters,
      debouncedTableSearch,
      includeNoAssignee,
      labelFilters,
      priorityFilters,
      showSubIssues,
      statusFilters,
      viewIncludeNoProject,
      viewProjectFilters,
    ],
  );
  // The running-restricted variant of the window. It IS the table window
  // while the filter is on; while the filter is off the data hook still
  // subscribes to it (same query key, so toggling the filter hits a warm
  // cache) to give the working chip its authoritative in-window count —
  // deriving that count from loaded rows says "0 working" whenever the only
  // running issue sits on an unfetched page (round-3 review P2#3).
  const workingFacets = useMemo<IssueFlatFilter>(
    () => ({ ...baseTableFacets, ids: sortedRunningIds }),
    [baseTableFacets, sortedRunningIds],
  );
  const tableFacets = agentRunningFilter ? workingFacets : baseTableFacets;

  // Selection is only meaningful within the current membership window: batch
  // actions act on selected ids while export/common-field consumers intersect
  // with visible rows, so a selection that survives a membership change lets
  // "1 selected" mean different sets to different consumers (round-2 review
  // P1#2). Reset whenever any membership-affecting input changes. Sort is
  // excluded on purpose — reordering does not change membership. The live
  // running set is also excluded: while the agents-working filter is on, a
  // task finishing should not wipe the user's selection mid-action.
  const membershipKey = useMemo(
    () =>
      JSON.stringify([
        statusFilters,
        priorityFilters,
        assigneeFilters,
        includeNoAssignee,
        creatorFilters,
        viewProjectFilters,
        viewIncludeNoProject,
        labelFilters,
        effectivePropertyFilters,
        agentRunningFilter,
        showSubIssues,
        dateParams,
        debouncedTableSearch,
      ]),
    [
      agentRunningFilter,
      assigneeFilters,
      creatorFilters,
      dateParams,
      debouncedTableSearch,
      effectivePropertyFilters,
      includeNoAssignee,
      labelFilters,
      priorityFilters,
      showSubIssues,
      statusFilters,
      viewIncludeNoProject,
      viewProjectFilters,
    ],
  );
  const selection = useCreateIssueSurfaceSelection(
    scopeKey,
    `${scopeKey}:${effectiveViewMode}:${membershipKey}`,
  );

  const data = useIssueSurfaceData({
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
    projectFilters: viewProjectFilters,
    includeNoProject: viewIncludeNoProject,
    labelFilters,
    propertyFilters: effectivePropertyFilters,
    agentRunningFilter,
    showSubIssues,
    loadProjects:
      cardProperties.project ||
      (usesTable && tableColumns.some((column) => column.key === "project")) ||
      (effectiveViewMode === "swimlane" && swimlaneGrouping === "project"),
  });

  const exportTableIssues = useCallback(async () => {
    const exportIssues = await queryClient.fetchQuery(
      issueSurfaceFlatExportOptions(wsId, queryPlan, sort, tableFacets),
    );
    return data.filterIssuesForExport(exportIssues);
  }, [data, queryClient, queryPlan, sort, tableFacets, wsId]);

  const { actions, openCreateIssue, moveIssue } = useIssueSurfaceActions({
    createDefaults: resolvedCreateDefaults,
  });

  const { filterIssuesForExport: _filterIssuesForExport, ...surfaceData } = data;

  return {
    scopeKey,
    projectId,
    createDefaults: resolvedCreateDefaults,
    viewMode: effectiveViewMode,
    allowGantt: allowedModes.has("gantt") && !!projectId,
    ...surfaceData,
    // Keep TableView mounted for an empty search result so its local search
    // control remains available to refine or clear the query. Include the
    // debounced value as well to avoid a brief empty-screen flash while a
    // cleared query is waiting to re-fetch the unsearched window.
    isEmpty:
      surfaceData.isEmpty &&
      !(usesTable && (tableSearch.trim() || debouncedTableSearch)),
    sort,
    actions,
    selection,
    tableSearch,
    setTableSearch,
    openCreateIssue,
    moveIssue,
    exportTableIssues,
  };
}
