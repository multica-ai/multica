"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { QueryKey } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type {
  Issue,
  IssueAssigneeGroup,
  IssueStatus,
  IssueTableFacetSpec,
  IssueTableFacetsResponse,
  IssueTableQuerySpec,
  Project,
} from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { dateOnlyToLocalDate } from "@multica/core/issues/date";
import type {
  AssigneeGroupedIssuesFilter,
  IssueSortParam,
  MyIssuesFilter,
} from "@multica/core/issues/queries";
import { issueTableFacetsOptions } from "@multica/core/issues/queries";
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
import { IssueTableExportIntegrityError } from "../components/table-view-model";
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
  tableSearch: string;
  /** Canonical server-owned Table membership. */
  tableQuerySpec: IssueTableQuerySpec;
  /** Exact disjunctive counts for the active Table filter submenu. */
  tableFacetCounts?: IssueTableFacetsResponse;
  /** Load one Table facet when its filter submenu is opened. */
  setActiveTableFacet: (facet: IssueTableFacetSpec | null) => void;
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

  const activity = useIssueSurfaceActivity();

  const tableQuerySpec = useMemo<IssueTableQuerySpec>(() => {
    let queryScope: IssueTableQuerySpec["scope"];
    switch (scope.type) {
      case "workspace":
        queryScope = {
          kind: "workspace",
          ...(scope.actorKind === "members"
            ? { assignee_types: ["member" as const] }
            : scope.actorKind === "agents"
              ? { assignee_types: ["agent" as const, "squad" as const] }
              : {}),
        };
        break;
      case "project":
        queryScope = { kind: "project", project_id: scope.projectId };
        break;
      case "my":
        queryScope = {
          kind: "my",
          relation: scope.relation === "all" ? "any" : scope.relation,
        };
        break;
      case "actor":
        queryScope = {
          kind: scope.relation === "assigned" ? "assignee" : "creator",
          actor: { type: scope.actorType, id: scope.actorId },
        };
        break;
      case "team":
        throw new Error("Team issue scope is not supported by the Table query");
    }

    const date =
      dateParams.date_field && dateParams.date_start && dateParams.date_end
        ? {
            field: dateParams.date_field,
            start: dateParams.date_start,
            end: dateParams.date_end,
          }
        : undefined;
    return {
      scope: queryScope,
      filters: {
        ...(statusFilters.length > 0 ? { statuses: statusFilters } : {}),
        ...(priorityFilters.length > 0 ? { priorities: priorityFilters } : {}),
        ...(assigneeFilters.length > 0 ? { assignees: assigneeFilters } : {}),
        ...(includeNoAssignee ? { include_no_assignee: true } : {}),
        ...(creatorFilters.length > 0 ? { creators: creatorFilters } : {}),
        ...(viewProjectFilters.length > 0
          ? { project_ids: viewProjectFilters }
          : {}),
        ...(viewIncludeNoProject ? { include_no_project: true } : {}),
        ...(labelFilters.length > 0 ? { label_ids: labelFilters } : {}),
        ...(Object.keys(effectivePropertyFilters).length > 0
          ? { properties: effectivePropertyFilters }
          : {}),
        ...(date ? { date } : {}),
        ...(agentRunningFilter ? { working_only: true } : {}),
        include_sub_issues: showSubIssues,
      },
      ...(debouncedTableSearch ? { search: debouncedTableSearch } : {}),
      sort: {
        field: sort.sort_by ?? "position",
        direction: sort.sort_direction ?? "asc",
      },
    };
  }, [
    agentRunningFilter,
    assigneeFilters,
    creatorFilters,
    dateParams,
    debouncedTableSearch,
    effectivePropertyFilters,
    includeNoAssignee,
    labelFilters,
    priorityFilters,
    scope,
    showSubIssues,
    sort.sort_by,
    sort.sort_direction,
    statusFilters,
    viewIncludeNoProject,
    viewProjectFilters,
  ]);

  const [activeTableFacet, setActiveTableFacet] =
    useState<IssueTableFacetSpec | null>(null);
  const tableFacetRequest = useMemo(
    () => ({
      query: tableQuerySpec,
      // The endpoint requires at least one facet. This fallback is never
      // fetched while activeTableFacet is null; it only keeps the request
      // shape total for React Query's option factory.
      facets: [activeTableFacet ?? { kind: "status" as const }],
      // Filter option badges do not consume the query-wide total; rows/groups
      // already own the displayed Table total.
      include_total: false,
    }),
    [activeTableFacet, tableQuerySpec],
  );
  const tableFacetsQuery = useQuery({
    ...issueTableFacetsOptions(wsId, tableFacetRequest),
    // Counts are only visible inside one open filter submenu. Eagerly loading
    // every custom-property facet made a Table mount issue up to 47 SQL
    // statements and repeatedly scan the issue table after invalidation.
    enabled: usesTable && activeTableFacet !== null,
  });
  useEffect(() => {
    if (!usesTable) setActiveTableFacet(null);
  }, [usesTable]);
  const requestActiveTableFacet = useCallback(
    (facet: IssueTableFacetSpec | null) => {
      setActiveTableFacet(usesTable ? facet : null);
    },
    [usesTable],
  );

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
    const issues: Issue[] = [];
    const seenIssueIds = new Set<string>();
    const seenCursors = new Set<string>();
    let fingerprint: string | null = null;
    let expectedTotal: number | null = null;
    let cursor: string | null = null;
    do {
      if (cursor !== null) {
        if (seenCursors.has(cursor)) throw new IssueTableExportIntegrityError();
        seenCursors.add(cursor);
      }
      const page = await api.listIssueTableRows({
        query: tableQuerySpec,
        group: { kind: "none" },
        group_key: null,
        hierarchy: { enabled: false },
        parent_id: null,
        page: { limit: 100, cursor },
      });
      // parseWithFallback deliberately protects interactive views from schema
      // drift with an empty response. Export must fail closed instead: an empty
      // fingerprint is the fallback sentinel and must never create a truncated
      // CSV that looks successful.
      if (!page.query_fingerprint) throw new IssueTableExportIntegrityError();
      fingerprint ??= page.query_fingerprint;
      if (cursor === null) expectedTotal = page.total;
      if (
        page.query_fingerprint !== fingerprint ||
        page.group_key !== null ||
        page.parent_id !== null
      ) {
        throw new IssueTableExportIntegrityError();
      }
      for (const row of page.rows) {
        if (seenIssueIds.has(row.issue.id)) {
          throw new IssueTableExportIntegrityError();
        }
        seenIssueIds.add(row.issue.id);
        issues.push(row.issue);
      }
      cursor = page.next_cursor;
    } while (cursor);
    if (issues.length !== (expectedTotal ?? 0)) {
      throw new IssueTableExportIntegrityError();
    }
    return issues;
  }, [tableQuerySpec]);

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
    // Keep TableView mounted for an empty search result so its local search
    // control remains available to refine or clear the query. Include the
    // debounced value as well to avoid a brief empty-screen flash while a
    // cleared query is waiting to re-fetch the unsearched window.
    isEmpty:
      data.isEmpty &&
      !(usesTable && (tableSearch.trim() || debouncedTableSearch)),
    sort,
    actions,
    selection,
    tableSearch,
    tableQuerySpec,
    tableFacetCounts:
      usesTable && activeTableFacet !== null
        ? tableFacetsQuery.data
        : undefined,
    setActiveTableFacet: requestActiveTableFacet,
    setTableSearch,
    openCreateIssue,
    moveIssue,
    exportTableIssues,
  };
}
