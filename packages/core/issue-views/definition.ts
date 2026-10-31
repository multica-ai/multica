import { createStore } from "zustand/vanilla";
import type { IssueView } from "../api/schemas";
import {
  effectiveNegatedFilters,
  filterDimensionHasSelection,
  mergeViewStatePersisted,
  NEGATABLE_FILTER_DIMENSIONS,
  viewStoreSlice,
  type FilterSnapshot,
  type IssueViewState,
  type NegatableFilterDimension,
} from "../issues/stores/view-store";
import {
  advancedFilterSignature,
  pruneAdvancedFilter,
  sanitizeAdvancedFilter,
  simpleFromAdvancedFilter,
} from "../issues/stores/filter-tree";

/** What a saved view stores: its filters, and the display it opens with. */
export interface IssueViewDefinition {
  query: Record<string, unknown>;
  display: Record<string, unknown>;
}

export function issueViewDefinitionFromState(state: IssueViewState): IssueViewDefinition {
  return {
    query: {
      statusFilters: state.statusFilters,
      priorityFilters: state.priorityFilters,
      assigneeFilters: state.assigneeFilters,
      includeNoAssignee: state.includeNoAssignee,
      creatorFilters: state.creatorFilters,
      projectFilters: state.projectFilters,
      includeNoProject: state.includeNoProject,
      projectStatusFilters: state.projectStatusFilters,
      labelFilters: state.labelFilters,
      relationFilters: state.relationFilters,
      dueDateFilters: state.dueDateFilters,
      negatedFilters: effectiveNegatedFilters(state),
      propertyFilters: state.propertyFilters,
      advancedFilter: pruneAdvancedFilter(state.advancedFilter),
      agentRunningFilter: state.agentRunningFilter,
    },
    display: {
      showSubIssues: state.showSubIssues,
      viewMode: state.viewMode,
      grouping: state.grouping,
      sortBy: state.sortBy,
      sortDirection: state.sortDirection,
      cardProperties: state.cardProperties,
      cardPropertyIds: state.cardPropertyIds,
      tableColumns: state.tableColumns,
      tableGrouping: state.tableGrouping,
      tableHierarchy: state.tableHierarchy,
      tableCalculation: state.tableCalculation,
      swimlaneGrouping: state.swimlaneGrouping,
      ganttZoom: state.ganttZoom,
      ganttShowCompleted: state.ganttShowCompleted,
    },
  };
}

/**
 * The definition a view opens with, sanitized exactly like the surface seed,
 * so comparing it with live state never reports a fresh view as modified.
 */
export function issueViewSeedDefinition(
  view: Pick<IssueView, "query" | "display">,
): IssueViewDefinition {
  const defaults = createStore<IssueViewState>()(viewStoreSlice).getState();
  return issueViewDefinitionFromState(
    mergeViewStatePersisted({ ...view.query, ...view.display }, defaults),
  );
}

function stableStringify(value: unknown): string {
  return JSON.stringify(value, (_key, inner: unknown) =>
    inner && typeof inner === "object" && !Array.isArray(inner)
      ? Object.fromEntries(Object.entries(inner).sort(([a], [b]) => a.localeCompare(b)))
      : inner,
  );
}

const DIMENSION_FIELDS: Record<NegatableFilterDimension, readonly string[]> = {
  status: ["statusFilters"],
  priority: ["priorityFilters"],
  assignee: ["assigneeFilters", "includeNoAssignee"],
  creator: ["creatorFilters"],
  project: ["projectFilters", "includeNoProject"],
  projectStatus: ["projectStatusFilters"],
  label: ["labelFilters"],
  relation: ["relationFilters"],
  dueDate: ["dueDateFilters"],
};

/**
 * A query reduced to what it filters, for comparisons. Entering advanced mode
 * rewrites the same filter as a tree with fresh ids, so a tree simple mode
 * can say compares as those simple fields, and any other tree by its
 * id-free signature.
 */
function filterMeaning(query: Record<string, unknown>): Record<string, unknown> {
  const tree = sanitizeAdvancedFilter(query.advancedFilter);
  if (!tree) return { ...query, advancedFilter: "" };
  const simple = simpleFromAdvancedFilter(tree);
  const flatEmpty =
    NEGATABLE_FILTER_DIMENSIONS.every(
      (dimension) => !filterDimensionHasSelection(query as unknown as FilterSnapshot, dimension),
    ) &&
    Object.values((query.propertyFilters ?? {}) as Record<string, unknown[]>).every(
      (values) => values.length === 0,
    );
  if (simple && flatEmpty) return { ...query, ...simple, advancedFilter: "" };
  return { ...query, advancedFilter: advancedFilterSignature(tree) };
}

/** Whether two filter queries select the same issues by construction. */
export function sameFilterMeaning(
  a: Record<string, unknown>,
  b: Record<string, unknown>,
): boolean {
  return stableStringify(filterMeaning(a)) === stableStringify(filterMeaning(b));
}

/**
 * The filter dimensions (chip keys) whose condition differs from what the
 * view saves: its values, or its "is" / "is not".
 */
export function modifiedFilterDimensions(
  view: Pick<IssueView, "query" | "display">,
  state: IssueViewState,
): Set<string> {
  const seed = filterMeaning(issueViewSeedDefinition(view).query);
  const live = filterMeaning(issueViewDefinitionFromState(state).query);
  const seedNegated = new Set(seed.negatedFilters as NegatableFilterDimension[]);
  const liveNegated = new Set(live.negatedFilters as NegatableFilterDimension[]);
  const modified = new Set<string>();
  for (const dimension of NEGATABLE_FILTER_DIMENSIONS) {
    const fields = DIMENSION_FIELDS[dimension];
    const changed =
      seedNegated.has(dimension) !== liveNegated.has(dimension) ||
      fields.some((field) => stableStringify(seed[field]) !== stableStringify(live[field]));
    if (changed) modified.add(dimension);
  }
  const seedProperties = (seed.propertyFilters ?? {}) as Record<string, unknown>;
  const liveProperties = (live.propertyFilters ?? {}) as Record<string, unknown>;
  for (const id of new Set([...Object.keys(seedProperties), ...Object.keys(liveProperties)])) {
    if (stableStringify(seedProperties[id] ?? []) !== stableStringify(liveProperties[id] ?? [])) {
      modified.add(`property:${id}`);
    }
  }
  if (seed.advancedFilter !== live.advancedFilter) modified.add("advanced");
  if (seed.agentRunningFilter !== live.agentRunningFilter) modified.add("agentRunning");
  return modified;
}

/** Whether the live state drifted from what the view saves. */
export function isIssueViewModified(
  view: Pick<IssueView, "query" | "display">,
  state: IssueViewState,
): { filters: boolean; display: boolean } {
  const seed = issueViewSeedDefinition(view);
  const live = issueViewDefinitionFromState(state);
  return {
    filters: !sameFilterMeaning(seed.query, live.query),
    display: stableStringify(seed.display) !== stableStringify(live.display),
  };
}
