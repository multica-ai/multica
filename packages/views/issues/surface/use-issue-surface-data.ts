"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import type { Issue, Project } from "@multica/core/types";
import { ALL_STATUSES } from "@multica/core/issues/config";
import { projectListOptions } from "@multica/core/projects/queries";
import { childIssueProgressOptions } from "@multica/core/issues/queries";
import { issueSurfaceGanttOptions } from "@multica/core/issues/surface/repository";
import type { IssueSurfaceQueryPlan } from "@multica/core/issues/surface/query-plan";
import type { IssueStatus, IssueStatusCategory } from "@multica/core/types";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { issueBehavesAsAny, statusFilterColumns } from "@multica/core/issues";
import {
  applyIssueFilters,
  type IssueFilterState,
  type IssueFilters,
} from "../utils/filter";
import type { ChildProgress } from "../components/list-row";
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
  // By CATEGORY: a custom status in done/cancelled is completed work, and
  // "show completed" has to hide it too. (MUL-6243)
  return dated.filter((i) => !issueBehavesAsAny(i, ["done", "cancelled"]));
}

export interface IssueSurfaceData {
  surfaceIssues: Issue[];
  projectIssues: Issue[];
  issues: Issue[];
  swimlaneIssues: Issue[];
  /** Gantt only: the canvas rows the agents-working filter would leave on
   *  screen. `undefined` on every other view mode, where the header chip
   *  sources its count from the `working_agents` server facet instead. */
  ganttWorkingScopeIssues: Issue[] | undefined;
  filteredGanttIssues: Issue[];
  ganttIssues: Issue[];
  visibleStatuses: IssueStatusCategory[];
  hiddenStatuses: IssueStatusCategory[];
  statusPagination: IssueStatusPagination;
  activeFilters: Omit<IssueFilters, "statusFilters">;
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
  /**
   * The catalog request a CUSTOM status filter depends on failed. The filter
   * cannot be honoured without it, so the surface shows a retryable error
   * rather than an unexplained empty board. (MUL-6243)
   */
  isStatusCatalogError: boolean;
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
  usesGantt,
  usesTable,
  serverStatusBranches,
  serverGroupBranches,
  ganttShowCompleted,
  statusFilters,
  hiddenStatusCategories,
  statusFilterPending,
  statusFilterError,
  priorityFilters,
  assigneeFilters,
  includeNoAssignee,
  agentRunningFilter,
  creatorFilters,
  projectFilters,
  includeNoProject,
  labelFilters,
  propertyFilters,
  workingIssueIDs,
  showSubIssues,
  loadProjects,
}: {
  wsId: string;
  queryPlan: IssueSurfaceQueryPlan;
  projectId?: string;
  usesGantt: boolean;
  usesTable: boolean;
  serverStatusBranches: IssueStatusBranches;
  serverGroupBranches: IssueGroupBranches;
  /** Gantt's "show completed" display toggle. The canvas hides done/cancelled
   *  rows without it, so the working scope has to honour it too. */
  ganttShowCompleted: boolean;
  statusFilters: IssueStatus[];
  hiddenStatusCategories: IssueStatusCategory[];
  /** A custom status filter is waiting on the catalog — hold loading. */
  statusFilterPending: boolean;
  /** The catalog failed, so a custom status filter cannot be honoured. */
  statusFilterError: boolean;
  priorityFilters: IssueFilterState["priorityFilters"];
  assigneeFilters: IssueFilterState["assigneeFilters"];
  includeNoAssignee: boolean;
  agentRunningFilter: boolean;
  creatorFilters: IssueFilterState["creatorFilters"];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
  propertyFilters: Record<string, string[]>;
  /** Distinct running-task issue ids projected by `/api/working-agents`. */
  workingIssueIDs: ReadonlySet<string>;
  showSubIssues: boolean;
  loadProjects: boolean;
}): IssueSurfaceData {
  const ganttIssuesQuery = useQuery({
    ...issueSurfaceGanttOptions(wsId, projectId ?? "", queryPlan),
    enabled: usesGantt,
  });
  const workingFilterContext = useMemo(
    () => ({ runningIssueIds: workingIssueIDs }),
    [workingIssueIDs],
  );
  const bucketedIssues = serverStatusBranches.enabled
    ? serverStatusBranches.issues
    : serverGroupBranches.enabled
      ? serverGroupBranches.issues
      : EMPTY_ISSUES;

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
      assigneeFilters,
      agentRunningFilter,
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
        : applyIssueFilters(
            surfaceIssues,
            baseFilterState,
            workingFilterContext,
          ),
    [
      baseFilterState,
      serverStatusBranches.enabled,
      surfaceIssues,
      workingFilterContext,
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
    () =>
      applyIssueFilters(
        surfaceIssues,
        statuslessFilterState,
        workingFilterContext,
      ),
    [statuslessFilterState, surfaceIssues, workingFilterContext],
  );

  const filteredGanttIssues = useMemo(
    () =>
      ganttCanvasRows(
        applyIssueFilters(ganttIssues, baseFilterState, workingFilterContext),
        ganttShowCompleted,
      ),
    [
      baseFilterState,
      ganttIssues,
      ganttShowCompleted,
      workingFilterContext,
    ],
  );

  const workingFilterState = useMemo<IssueFilterState>(
    () => ({
      ...baseFilterState,
      workingOnly: true,
    }),
    [baseFilterState],
  );

  // The Gantt canvas rows the agents-working filter would leave on screen —
  // i.e. exactly what you get when you click the header chip in Gantt.
  //
  // Gantt is the one surface whose membership is NOT server-owned: it draws
  // from a fully materialized scheduled-issue window, and its canvas
  // projection (scheduled + dated + showCompleted) cannot be expressed in the
  // Table query spec. So the header chip cannot source its count from the
  // `working_agents` server facet here, and derives it from this set instead.
  //
  // Every other view mode (list / board / swimlane / table) resolves the same
  // question through that facet, against the identical scope + filters the
  // rows come from. Rebuilding a second complete issue window client-side to
  // decorate the chip is what the server facet exists to avoid.
  const ganttWorkingScopeIssues = useMemo(() => {
    if (!usesGantt) return undefined;
    return ganttCanvasRows(
      applyIssueFilters(ganttIssues, workingFilterState, workingFilterContext),
      ganttShowCompleted,
    );
  }, [
    ganttIssues,
    ganttShowCompleted,
    usesGantt,
    workingFilterState,
    workingFilterContext,
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

  const catalog = useIssueStatuses(wsId);

  const visibleStatuses = useMemo<IssueStatusCategory[]>(() => {
    // Board columns are CATEGORIES, not status keys — adding a custom status
    // must never add a column. Two independent things narrow them: hidden
    // columns (display state) and the status filter, which is expressed in
    // concrete KEYS and so has to be mapped back to the columns those keys land
    // in. Default view shows every category, `cancelled` last (its canonical
    // position in ALL_STATUSES). (MUL-6243)
    const resolved =
      statusFilters.length > 0 ? statusFilterColumns(statusFilters, catalog) : null;
    // Pending/error contribute no narrowing here; the surface's loading and
    // error states (statusFilterPending / statusFilterError) are what stop it
    // rendering as though the empty result were the answer.
    const selected = resolved?.state === "resolved" ? resolved.columns : null;
    return ALL_STATUSES.filter(
      (s) =>
        !hiddenStatusCategories.includes(s) &&
        (selected === null || selected.has(s)),
    );
  }, [statusFilters, hiddenStatusCategories, catalog]);

  // Hidden columns are the lifecycle statuses not currently visible, so
  // `cancelled` participates in the board show/hide controls exactly like the
  // rest of the statuses.
  const hiddenStatuses = useMemo<IssueStatusCategory[]>(
    () => ALL_STATUSES.filter((s) => !visibleStatuses.includes(s)),
    [visibleStatuses],
  );

  const activeFilters = useMemo(
    () => ({
      priorityFilters,
      assigneeFilters,
      includeNoAssignee,
      agentRunningFilter,
      runningIssueIds: workingIssueIDs,
      creatorFilters,
      projectFilters,
      includeNoProject,
      labelFilters,
      propertyFilters,
      showSubIssues,
    }),
    [
      assigneeFilters,
      agentRunningFilter,
      creatorFilters,
      includeNoAssignee,
      includeNoProject,
      labelFilters,
      propertyFilters,
      priorityFilters,
      projectFilters,
      showSubIssues,
      workingIssueIDs,
    ],
  );

  // `statusFilterPending` holds the surface in loading while a CUSTOM status
  // filter waits for the catalog to say which column it belongs to. Without it
  // the surface reported "loaded, zero results" — an empty board with no
  // spinner — for the whole cold-load window. (MUL-6243)
  const isLoading =
    statusFilterPending ||
    (serverGroupBranches.enabled
      ? serverGroupBranches.isLoading
      : usesGantt
        ? ganttIssuesQuery.isLoading
        : serverStatusBranches.enabled
          ? serverStatusBranches.isLoading
          : false);

  // Placeholder-backed revalidation of the ACTIVE query only. First loads are
  // isLoading (no previous data to place-hold); gantt has no placeholder
  // phase (its key carries no sort/filter).
  const isRefreshing = serverGroupBranches.enabled
    ? serverGroupBranches.isRefreshing
    : serverStatusBranches.enabled
      ? serverStatusBranches.isRefreshing
      : false;

  return {
    surfaceIssues,
    projectIssues: surfaceIssues,
    issues,
    swimlaneIssues,
    ganttWorkingScopeIssues,
    filteredGanttIssues,
    ganttIssues,
    visibleStatuses,
    hiddenStatuses,
    statusPagination: serverStatusBranches.pagination,
    activeFilters,
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
      !statusFilterError &&
      !usesGantt &&
      !usesTable &&
      (serverStatusBranches.enabled
        ? serverStatusBranches.isTotalKnown &&
          serverStatusBranches.total === 0
        : serverGroupBranches.enabled &&
          !serverGroupBranches.isError &&
          serverGroupBranches.total === 0),
    isStatusCatalogError: statusFilterError,
  };
}
