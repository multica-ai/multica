"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { hashKey, keepPreviousData, useQuery } from "@tanstack/react-query";
import type { QueryKey } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type {
  Issue,
  IssueAssigneeGroup,
  IssueStatus,
  IssueTableFacetSpec,
  IssueTableFacetsResponse,
  IssueTableGroupsRequest,
  IssueTableQuerySpec,
  Project,
  WorkingAgentSummary,
} from "@multica/core/types";
import { workspaceWorkingAgentsOptions } from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import { ALL_STATUSES } from "@multica/core/issues/config";
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
import {
  useIssueStatusBranches,
  type IssueStatusPagination,
} from "./use-issue-status-branches";
import {
  useIssueGroupBranches,
  type IssueGroupBranches,
} from "./use-issue-group-branches";

interface UseIssueSurfaceControllerInput {
  scope: IssueScope;
  modes: IssueSurfaceMode[];
  createDefaults?: IssueCreateDefaults;
  search?: string;
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
  /** Agents currently working inside THIS surface, under the surface's active
   *  filters — the header chip's count, so clicking it leaves exactly these
   *  agents' rows (MUL-4884, MUL-5525). `undefined` means the projection has
   *  not resolved yet; the chip renders an indeterminate state rather than a
   *  number it cannot stand behind. */
  workingAgents: WorkingAgentSummary[] | undefined;
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
  /** Exact server counts plus cursor controls for List/status Board. */
  statusPagination?: IssueStatusPagination;
  /** Exact group catalog plus independent row cursors for Assignee/Property
   * Board and compound Swimlane cells. */
  groupBranches?: IssueGroupBranches;
  activeFilters: Omit<IssueFilters, "statusFilters">;
  /** Any filter that `clearFilters()` would reset is on. Lets an empty surface
   *  say "your filters hid everything" instead of "there is nothing here". */
  hasActiveFilters: boolean;
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
  /** Exact disjunctive counts for the active server-backed filter submenu. */
  tableFacetCounts?: IssueTableFacetsResponse;
  /** Whether scopedIssues is a complete client window for local count use. */
  facetCountsExact: boolean;
  /** Load one server facet when its filter submenu is opened. */
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

/** One shared reference for every un-settled list default in this hook.
 *
 * `const { data = [] } = useQuery(...)` allocates a new array on every render
 * for as long as the query has no data — which is the whole window right after
 * a workspace switch. Downstream that array is a memo dependency, so the empty
 * default alone was enough to rebuild the derived Sets, the table query spec,
 * and the branch query list once per render (MUL-5477). */
const EMPTY_LIST: never[] = [];

/**
 * Pin a derived value's identity to its CONTENT.
 *
 * `tableQuerySpec` is rebuilt from 17 dependencies, so any one of them losing
 * referential stability hands every consumer a new object even though the query
 * it describes is unchanged. Consumers use it as a memo dependency and, in the
 * Table's case, as the source of a `useQueries` list — so a new-but-equal spec
 * rebuilt that list on every render.
 *
 * Hashed with TanStack's own `hashKey` rather than `JSON.stringify` so this
 * agrees exactly with how the same spec is hashed into a `queryKey`: object
 * keys are sorted, so two specs that resolve to one query also resolve to one
 * identity here.
 */
function useStableByContent<T>(value: T): T {
  const contentKey = hashKey([value]);
  // eslint-disable-next-line react-hooks/exhaustive-deps -- identity follows the content hash, not the reference
  return useMemo(() => value, [contentKey]);
}

export function useIssueSurfaceController({
  scope,
  modes,
  createDefaults,
  search = "",
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
  const listCollapsedStatuses = useViewStore((s) => s.listCollapsedStatuses);
  const [tableSearch, setTableSearch] = useState("");

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
  const { data: workspaceProperties = EMPTY_LIST, isSuccess: catalogSettled } = useQuery(propertyListOptions(wsId));
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

  const groupingPropertyId = propertyIdFromViewKey(grouping);
  const activeGroupingProperty = groupingPropertyId
    ? workspaceProperties.find(
        (property) =>
          property.id === groupingPropertyId && property.type === "select",
      ) ?? null
    : null;
  const effectiveGrouping =
    groupingPropertyId && catalogSettled && !activeGroupingProperty
      ? "status"
      : grouping;
  const usesAssigneeBoard =
    effectiveViewMode === "board" && effectiveGrouping === "assignee";
  const usesGantt = effectiveViewMode === "gantt" && !!projectId;
  const usesTable = effectiveViewMode === "table";
  const activeSearch = usesTable ? tableSearch : search;
  const debouncedActiveSearch = useDebouncedTableSearch(activeSearch);
  const usesServerStatusSurface =
    effectiveViewMode === "list" ||
    (effectiveViewMode === "board" && effectiveGrouping === "status");
  const usesServerGroupSurface =
    (effectiveViewMode === "board" && effectiveGrouping !== "status") ||
    effectiveViewMode === "swimlane";
  const usesServerFacets =
    usesTable || usesServerStatusSurface || usesServerGroupSurface;
  const serverStatuses = useMemo<IssueStatus[]>(
    () => {
      const visible =
        statusFilters.length > 0
          ? ALL_STATUSES.filter((status) => statusFilters.includes(status))
          : [...ALL_STATUSES];
      return effectiveViewMode === "list"
        ? visible.filter((status) => !listCollapsedStatuses.includes(status))
        : visible;
    },
    [effectiveViewMode, listCollapsedStatuses, statusFilters],
  );

  const projectFilterState = useMemo(
    () => ({
      projectFilters: scope.type === "project" ? [] : projectFilters,
      includeNoProject: scope.type === "project" ? false : includeNoProject,
    }),
    [includeNoProject, projectFilters, scope.type],
  );
  const { projectFilters: viewProjectFilters, includeNoProject: viewIncludeNoProject } =
    projectFilterState;

  // Exactly the filters `clearFilters()` resets, so an empty surface that
  // reports "filters hid everything" can always offer a button that fixes it.
  // Display toggles (show sub-issues) and per-surface search are deliberately
  // out: they have their own affordances and clearing filters would not undo
  // them.
  const hasActiveFilters =
    statusFilters.length > 0 ||
    priorityFilters.length > 0 ||
    assigneeFilters.length > 0 ||
    includeNoAssignee ||
    creatorFilters.length > 0 ||
    viewProjectFilters.length > 0 ||
    viewIncludeNoProject ||
    labelFilters.length > 0 ||
    Object.keys(effectivePropertyFilters).length > 0 ||
    dateFilter != null ||
    agentRunningFilter === true;

  const workingAgentMineRelation =
    scope.type === "my"
      ? scope.relation === "all"
        ? "any"
        : scope.relation
      : undefined;
  const { data: workspaceWorkingAgents = EMPTY_LIST } = useQuery(
    workspaceWorkingAgentsOptions(wsId, "issue", workingAgentMineRelation),
  );
  const workingIssueIDs = useMemo(() => {
    const issueIDs = new Set<string>();
    for (const agent of workspaceWorkingAgents) {
      for (const issueID of agent.issue_ids) issueIDs.add(issueID);
    }
    return issueIDs;
  }, [workspaceWorkingAgents]);

  const derivedTableQuerySpec = useMemo<IssueTableQuerySpec>(() => {
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
        ...(agentRunningFilter
          ? { working_issue_ids: [...workingIssueIDs] }
          : {}),
        include_sub_issues: showSubIssues,
      },
      ...(debouncedActiveSearch ? { search: debouncedActiveSearch } : {}),
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
    debouncedActiveSearch,
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
    workingIssueIDs,
  ]);
  // Every consumer below — the facet request, the status/group branch hooks and
  // the Table's own `useQueries` list — keys off this object's identity. Pin it
  // to the content so an unstable dependency upstream cannot rebuild all of
  // them for a query that did not change.
  const tableQuerySpec = useStableByContent(derivedTableQuerySpec);

  const [activeTableFacet, setActiveTableFacet] =
    useState<IssueTableFacetSpec | null>(null);
  const requestedFacets = useMemo<IssueTableFacetSpec[]>(() => {
    const facets: IssueTableFacetSpec[] = [];
    if (usesServerStatusSurface) facets.push({ kind: "status" });
    if (
      activeTableFacet &&
      !facets.some(
        (facet) =>
          facet.kind === activeTableFacet.kind &&
          (facet.kind !== "property" ||
            activeTableFacet.kind !== "property" ||
            facet.property_id === activeTableFacet.property_id),
      )
    ) {
      facets.push(activeTableFacet);
    }
    // The request shape remains total while disabled.
    return facets.length > 0 ? facets : [{ kind: "status" }];
  }, [activeTableFacet, usesServerStatusSurface]);
  const tableFacetRequest = useMemo(
    () => ({
      query: tableQuerySpec,
      facets: requestedFacets,
      // Status surfaces consume the facet total as their authoritative empty
      // state. Table rows/groups already own the displayed total.
      include_total: usesServerStatusSurface,
    }),
    [requestedFacets, tableQuerySpec, usesServerStatusSurface],
  );
  const tableFacetsQuery = useQuery({
    ...issueTableFacetsOptions(wsId, tableFacetRequest),
    placeholderData: keepPreviousData,
    // Counts are only visible inside one open filter submenu. Eagerly loading
    // every custom-property facet made a Table mount issue up to 47 SQL
    // statements and repeatedly scan the issue table after invalidation.
    enabled:
      usesServerStatusSurface ||
      ((usesTable || usesServerGroupSurface) && activeTableFacet !== null),
  });
  // The header chip's count, kept on its own query rather than folded into the
  // submenu facet request above. Two reasons: that request is deliberately
  // lazy (an always-on facet would re-enable it for Table/grouped surfaces on
  // mount), and this spec drops `working_issue_ids` so toggling the filter
  // does not change the query identity — the number must not flicker when you
  // click the very chip it labels.
  const workingAgentsQuerySpec = useMemo<IssueTableQuerySpec>(() => {
    if (!agentRunningFilter) return tableQuerySpec;
    const { working_issue_ids: _working, ...filters } = tableQuerySpec.filters;
    return { ...tableQuerySpec, filters };
  }, [agentRunningFilter, tableQuerySpec]);
  const workingAgentsFacetRequest = useMemo(
    () => ({
      query: workingAgentsQuerySpec,
      facets: [{ kind: "working_agents" } as const],
      include_total: false,
    }),
    [workingAgentsQuerySpec],
  );
  const workingAgentsFacetQuery = useQuery({
    ...issueTableFacetsOptions(wsId, workingAgentsFacetRequest),
    placeholderData: keepPreviousData,
    // Gantt owns its own count: its canvas projection is client-side and not
    // expressible as a Table query spec, so a facet answer would over-count.
    //
    // The actor panel (member / agent detail) renders no agents-working chip at
    // all, so nothing would read the answer — and with no control to toggle it,
    // `agentRunningFilter` is unreachable there. Skip the aggregation rather
    // than pay for it on every panel mount. Adding the chip to that header
    // means dropping this clause.
    enabled: !usesGantt && scope.type !== "actor",
  });
  const facetWorkingAgents = useMemo<WorkingAgentSummary[] | undefined>(() => {
    const facet = workingAgentsFacetQuery.data?.facets.find(
      (candidate) => candidate.kind === "working_agents",
    );
    // A backend without this facet (older deploy) answers with an error or a
    // response that omits it. Stay indeterminate instead of claiming zero.
    if (!facet) return undefined;
    return facet.values.map((value) => ({
      id: value.key,
      running_task_count: value.count,
    }));
  }, [workingAgentsFacetQuery.data]);
  useEffect(() => {
    if (!usesServerFacets) setActiveTableFacet(null);
  }, [usesServerFacets]);
  const requestActiveTableFacet = useCallback(
    (facet: IssueTableFacetSpec | null) => {
      setActiveTableFacet(usesServerFacets ? facet : null);
    },
    [usesServerFacets],
  );
  const serverStatusBranches = useIssueStatusBranches({
    wsId,
    query: tableQuerySpec,
    statuses: serverStatuses,
    facets: tableFacetsQuery.data,
    facetsPending: tableFacetsQuery.isPending,
    facetsFetching: tableFacetsQuery.isFetching,
    enabled: usesServerStatusSurface,
  });
  const serverGroupSpec = useMemo<IssueTableGroupsRequest["group"]>(() => {
    if (effectiveViewMode === "swimlane") {
      return {
        kind: "compound",
        primary: swimlaneGrouping,
        secondary: "status",
        secondary_values: serverStatuses,
      };
    }
    const propertyId = propertyIdFromViewKey(effectiveGrouping);
    if (propertyId) {
      return {
        kind: "property",
        property_id: propertyId,
        include_empty: true,
      };
    }
    return { kind: "assignee" };
  }, [
    effectiveGrouping,
    effectiveViewMode,
    serverStatuses,
    swimlaneGrouping,
  ]);
  const serverGroupQuery = useMemo<IssueTableQuerySpec>(() => {
    if (effectiveViewMode !== "swimlane") return tableQuerySpec;
    const { statuses: _statuses, ...filters } = tableQuerySpec.filters;
    return { ...tableQuerySpec, filters };
  }, [effectiveViewMode, tableQuerySpec]);
  const serverGroupBranches = useIssueGroupBranches({
    wsId,
    query: serverGroupQuery,
    group: serverGroupSpec,
    secondaryValues:
      effectiveViewMode === "swimlane" ? serverStatuses : undefined,
    observeEmptyBranches:
      effectiveViewMode === "swimlane" ||
      (effectiveViewMode === "board" && activeGroupingProperty !== null),
    enabled: usesServerGroupSurface,
  });

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
        debouncedActiveSearch,
      ]),
    [
      agentRunningFilter,
      assigneeFilters,
      creatorFilters,
      dateParams,
      debouncedActiveSearch,
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
    serverStatusBranches,
    serverGroupBranches,
    ganttShowCompleted,
    sort,
    statusFilters,
    priorityFilters,
    assigneeFilters,
    includeNoAssignee,
    agentRunningFilter,
    creatorFilters,
    projectFilters: viewProjectFilters,
    includeNoProject: viewIncludeNoProject,
    labelFilters,
    propertyFilters: effectivePropertyFilters,
    workingIssueIDs,
    showSubIssues,
    loadProjects:
      cardProperties.project ||
      (usesTable && tableColumns.some((column) => column.key === "project")) ||
      (effectiveViewMode === "swimlane" && swimlaneGrouping === "project"),
  });

  // Gantt draws a client-materialized canvas, so its chip counts the agents
  // holding those canvas rows. Every other view mode takes the server facet.
  const workingAgents = useMemo<WorkingAgentSummary[] | undefined>(() => {
    if (!usesGantt) return facetWorkingAgents;
    const rows = data.ganttWorkingScopeIssues;
    if (!rows) return undefined;
    const visible = new Set(rows.map((issue) => issue.id));
    const summaries: WorkingAgentSummary[] = [];
    for (const agent of workspaceWorkingAgents) {
      const running = agent.issue_ids.filter((id) => visible.has(id)).length;
      if (running > 0) {
        summaries.push({ id: agent.id, running_task_count: running });
      }
    }
    return summaries;
  }, [
    data.ganttWorkingScopeIssues,
    facetWorkingAgents,
    usesGantt,
    workspaceWorkingAgents,
  ]);

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

  const { ganttWorkingScopeIssues: _ganttWorkingScope, ...surfaceData } = data;

  return {
    scopeKey,
    projectId,
    createDefaults: resolvedCreateDefaults,
    viewMode: effectiveViewMode,
    allowGantt: allowedModes.has("gantt") && !!projectId,
    ...surfaceData,
    workingAgents,
    hasActiveFilters,
    statusPagination: usesServerStatusSurface
      ? data.statusPagination
      : undefined,
    groupBranches: usesServerGroupSurface
      ? serverGroupBranches
      : undefined,
    // Keep TableView mounted for an empty search result so its local search
    // control remains available to refine or clear the query. Include the
    // debounced value as well to avoid a brief empty-screen flash while a
    // cleared query is waiting to re-fetch the unsearched window.
    isEmpty:
      data.isEmpty &&
      !data.isRefreshing &&
      !(usesTable && (tableSearch.trim() || debouncedActiveSearch)),
    sort,
    actions,
    selection,
    tableSearch,
    tableQuerySpec,
    tableFacetCounts:
      usesServerStatusSurface ||
      ((usesTable || usesServerGroupSurface) && activeTableFacet !== null)
        ? tableFacetsQuery.data
        : undefined,
    facetCountsExact:
      !usesTable && !usesServerStatusSurface && !usesServerGroupSurface,
    setActiveTableFacet: requestActiveTableFacet,
    setTableSearch,
    openCreateIssue,
    moveIssue,
    exportTableIssues,
  };
}
