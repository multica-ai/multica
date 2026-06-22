import type { IssueDateFilter, IssueViewState } from "@multica/core/issues/stores/view-store";

import type { FilterSnapshot } from "./types";

// CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — structural id for a date
// condition. Relative ranges key on their spec (the from/to are a drifting
// cache); everything else keys on field+from+to+mode.
function dateSignature(d: IssueDateFilter): string {
  if (d.relative) {
    return `${d.field}:rel:${d.relative.direction}:${d.relative.amount}:${d.relative.unit}`;
  }
  return `${d.field}:${d.from}:${d.to}:${d.mode ?? "range"}`;
}

/**
 * extractFilterSnapshot copies the filter-relevant slice out of the live view
 * store. Arrays are shallow-cloned so the snapshot never aliases store state.
 */
export function extractFilterSnapshot(state: IssueViewState): FilterSnapshot {
  return {
    statusFilters: [...state.statusFilters],
    priorityFilters: [...state.priorityFilters],
    assigneeFilters: state.assigneeFilters.map((a) => ({ ...a })),
    includeNoAssignee: state.includeNoAssignee,
    creatorFilters: state.creatorFilters.map((a) => ({ ...a })),
    projectFilters: [...state.projectFilters],
    includeNoProject: state.includeNoProject,
    labelFilters: [...state.labelFilters],
    onBehalfOfFilters: [...state.onBehalfOfFilters],
    agentRunningFilter: state.agentRunningFilter,
    // CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — capture stacked dates.
    dateFilters: state.dateFilters.map((d) => ({ ...d })),
  };
}

/**
 * snapshotFromState produces the partial state object that applies a snapshot to
 * the store. Returned as a plain object so the caller drives it through the
 * store's own setState — no new store action is added to the upstream view-store.
 */
export function snapshotToState(snapshot: FilterSnapshot): Partial<IssueViewState> {
  return {
    statusFilters: [...snapshot.statusFilters],
    priorityFilters: [...snapshot.priorityFilters],
    assigneeFilters: snapshot.assigneeFilters.map((a) => ({ ...a })),
    includeNoAssignee: snapshot.includeNoAssignee,
    creatorFilters: snapshot.creatorFilters.map((a) => ({ ...a })),
    projectFilters: [...snapshot.projectFilters],
    includeNoProject: snapshot.includeNoProject,
    labelFilters: [...snapshot.labelFilters],
    onBehalfOfFilters: [...snapshot.onBehalfOfFilters],
    agentRunningFilter: snapshot.agentRunningFilter,
    // CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — restore stacked dates.
    dateFilters: (snapshot.dateFilters ?? []).map((d) => ({ ...d })),
  };
}

/** Total number of distinct filter selections in a snapshot. */
export function snapshotFilterCount(s: FilterSnapshot): number {
  return (
    s.statusFilters.length +
    s.priorityFilters.length +
    s.assigneeFilters.length +
    (s.includeNoAssignee ? 1 : 0) +
    s.creatorFilters.length +
    s.projectFilters.length +
    (s.includeNoProject ? 1 : 0) +
    s.labelFilters.length +
    s.onBehalfOfFilters.length +
    (s.agentRunningFilter ? 1 : 0) +
    // CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — each date condition counts.
    (s.dateFilters?.length ?? 0)
  );
}

export function isSnapshotEmpty(s: FilterSnapshot): boolean {
  return snapshotFilterCount(s) === 0;
}

/** Order-insensitive structural equality, used to highlight the active filter. */
export function snapshotsEqual(a: FilterSnapshot, b: FilterSnapshot): boolean {
  const setEq = (x: string[], y: string[]) => {
    if (x.length !== y.length) return false;
    const s = new Set(x);
    return y.every((v) => s.has(v));
  };
  const actorEq = (x: { type: string; id: string }[], y: { type: string; id: string }[]) => {
    if (x.length !== y.length) return false;
    const s = new Set(x.map((v) => `${v.type}:${v.id}`));
    return y.every((v) => s.has(`${v.type}:${v.id}`));
  };
  return (
    setEq(a.statusFilters, b.statusFilters) &&
    setEq(a.priorityFilters, b.priorityFilters) &&
    actorEq(a.assigneeFilters, b.assigneeFilters) &&
    a.includeNoAssignee === b.includeNoAssignee &&
    actorEq(a.creatorFilters, b.creatorFilters) &&
    setEq(a.projectFilters, b.projectFilters) &&
    a.includeNoProject === b.includeNoProject &&
    setEq(a.labelFilters, b.labelFilters) &&
    setEq(a.onBehalfOfFilters, b.onBehalfOfFilters) &&
    a.agentRunningFilter === b.agentRunningFilter &&
    // CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — compare stacked dates
    // (order-insensitive). A relative range is identified by its spec, not its
    // cached from/to (which drift daily); absolute ranges use field+from+to+mode.
    setEq(
      (a.dateFilters ?? []).map(dateSignature),
      (b.dateFilters ?? []).map(dateSignature),
    )
  );
}
