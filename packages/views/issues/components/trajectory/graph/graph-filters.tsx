"use client";

import { cloneElement, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { useStore } from "zustand";
import { useShallow } from "zustand/react/shallow";
import type { IssueTableFilterGroup, IssueTableFilters } from "@multica/core/types";
import { createTransientIssueViewStore, type IssueViewState } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { stateToTableFilters, tableFiltersToState, type TableFilterState } from "@multica/core/issues/stores/table-filter-state";
import { validGraphPropertyFilters } from "@multica/core/trajectory/filter-validation";
import { Button } from "@multica/ui/components/ui/button";
import { DropdownMenuCheckboxItem, DropdownMenuItem } from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { Filter, ListTree, RotateCcw } from "@multica/ui/icons";
import { useT } from "../../../../i18n";
import { FILTER_ITEM_CLASS, HoverCheck } from "../../../../common/hover-check";
import { getActiveFilterCount, IssueFilterMenu } from "../../issues-header";

/** The server accepts one level of nested groups, 50 conditions and 500 values per condition. */
const validGroup = (group: IssueTableFilterGroup): boolean => {
  let conditions = 0;
  const walk = (current: IssueTableFilterGroup, depth: number): boolean => depth <= 1 && current.children.every(node => {
    if ("children" in node) return node.children.length > 0 && walk(node, depth + 1);
    return !!node.field && node.values.length > 0 && node.values.length <= 500 && ++conditions <= 50
      && (!node.field.startsWith("property:") || validGraphPropertyFilters({ [node.field]: node.values }));
  });
  return walk(group, 0);
};
const validFilters = (filters: IssueTableFilters): boolean =>
  validGraphPropertyFilters(filters.properties) && (!filters.advanced || validGroup(filters.advanced));
const filterState = (state: IssueViewState): TableFilterState => ({
  statusFilters: state.statusFilters, priorityFilters: state.priorityFilters, assigneeFilters: state.assigneeFilters,
  includeNoAssignee: state.includeNoAssignee, creatorFilters: state.creatorFilters, projectFilters: state.projectFilters,
  includeNoProject: state.includeNoProject, projectStatusFilters: state.projectStatusFilters, labelFilters: state.labelFilters,
  relationFilters: state.relationFilters, dueDateFilters: state.dueDateFilters, negatedFilters: state.negatedFilters,
  propertyFilters: state.propertyFilters, advancedFilter: state.advancedFilter, dateFilter: state.dateFilter,
  agentRunningFilter: state.agentRunningFilter, agentIdleFilter: state.agentIdleFilter, unreadFilter: state.unreadFilter,
  showSubIssues: state.showSubIssues,
});
const signature = (value: unknown) => JSON.stringify(value);

/**
 * The issues page filter menu over a page-local, in-memory view store: edits
 * apply at once to this view only and never reach the list's own filters.
 * Filters that arrive from outside (the list's, a restore) are shown as they
 * are and sent back unchanged until the user edits them.
 */
export function GraphFilters({ filters, initial, onChange, loadUnread, cancelUnread, openRequest = 0 }: {
  filters: IssueTableFilters; initial: IssueTableFilters;
  onChange: (filters: IssueTableFilters) => void;
  /** The viewer's unread issue IDs, read when the unread filter turns on. */
  loadUnread?: () => Promise<string[]>; cancelUnread?: () => void;
  /** Incrementing it opens the filters, as the trigger does. */
  openRequest?: number;
}) {
  const { t } = useT("issues");
  const [store] = useState(() => {
    const created = createTransientIssueViewStore();
    created.setState(tableFiltersToState(filters));
    return created;
  });
  const current = useStore(store, useShallow(filterState));
  const [open, setOpen] = useState(false);
  // The menu reads workspace catalogs as it mounts, so it mounts on first open.
  const [mounted, setMounted] = useState(false);
  const opening = useRef(false), mountedRef = useRef(false);
  mountedRef.current = mounted;
  const show = useCallback(() => { if (mountedRef.current) setOpen(true); else { opening.current = true; setMounted(true); } }, []);
  // Open after the menu mounts closed: it takes its "active" snapshot as `open` turns true.
  useEffect(() => { if (mounted && opening.current) { opening.current = false; setOpen(true); } }, [mounted]);
  const latest = useRef({ onChange, loadUnread, cancelUnread, t });
  latest.current = { onChange, loadUnread, cancelUnread, t };
  // The filters this menu shows: incoming ones equal to them are its own echo.
  const shown = useRef(signature(filters));
  const unreadIds = useRef(filters.unread_issue_ids);
  // A counter object rather than a ref: an unmount must invalidate the read in flight.
  const [unread] = useState(() => ({ generation: 0 }));
  const seeding = useRef(false);
  const silently = (update: () => void) => { seeding.current = true; try { update(); } finally { seeding.current = false; } };
  const seed = (next: IssueTableFilters) => {
    shown.current = signature(next); unreadIds.current = next.unread_issue_ids;
    unread.generation++; latest.current.cancelUnread?.();
    silently(() => store.setState(tableFiltersToState(next)));
  };
  useLayoutEffect(() => { if (signature(filters) !== shown.current) seed(filters); });

  useEffect(() => {
    const emit = (state: TableFilterState) => {
      const next = stateToTableFilters(state, unreadIds.current);
      if (!validFilters(next)) { toast.error(latest.current.t($ => $.trajectory_graph.filter_invalid)); return; }
      shown.current = signature(next);
      latest.current.onChange(next);
    };
    const unsubscribe = store.subscribe((state, previous) => {
      const next = filterState(state), before = filterState(previous);
      if (seeding.current || signature(next) === signature(before)) return;
      if (!next.unreadFilter) {
        if (before.unreadFilter) { unread.generation++; latest.current.cancelUnread?.(); unreadIds.current = undefined; }
        emit(next);
        return;
      }
      if (unreadIds.current) { emit(next); return; }
      // Unread waits for the viewer's membership; edits made meanwhile apply with it.
      if (before.unreadFilter) return;
      const revert = () => silently(() => store.setState({ unreadFilter: false }));
      const load = latest.current.loadUnread;
      if (!load) { revert(); return; }
      const generation = ++unread.generation;
      load().then(ids => {
        if (generation !== unread.generation) return;
        unreadIds.current = ids;
        emit(filterState(store.getState()));
      }, () => {
        if (generation !== unread.generation) return;
        revert();
        toast.error(latest.current.t($ => $.unread_filter_error.title));
      });
    });
    return () => { unsubscribe(); unread.generation++; latest.current.cancelUnread?.(); };
  }, [store, unread]);

  const handledRequest = useRef(openRequest);
  useEffect(() => {
    if (openRequest === handledRequest.current) return;
    handledRequest.current = openRequest;
    show();
  }, [openRequest, show]);

  const count = getActiveFilterCount(current);
  const active = count > 0;
  const restorable = signature(filters) !== signature(initial);
  const trigger = <Button variant={active ? "default" : "outline"} size="sm" className={cn("gap-1", active && "bg-brand text-white hover:bg-brand/90")}>
    <Filter className="size-3.5" />
    {active ? t($ => $.filters.active_count, { count }) : t($ => $.filters.tooltip)}
  </Button>;
  return <div data-readonly-preview="filter" className="min-w-0">
    {!mounted ? cloneElement(trigger, { onClick: show }) : <ViewStoreProvider store={store}>
      <IssueFilterMenu
        align="start"
        open={open}
        onOpenChange={setOpen}
        tooltip={t($ => $.filters.tooltip)}
        dateFilter={current.dateFilter}
        onDateFilterChange={filter => store.getState().setDateFilter(filter)}
        leadingSections={<>
          <DropdownMenuCheckboxItem checked={current.showSubIssues} onCheckedChange={() => store.getState().toggleShowSubIssues()} className={FILTER_ITEM_CLASS}>
            <HoverCheck checked={current.showSubIssues} />
            <ListTree className="size-3.5 text-muted-foreground" />
            {t($ => $.display.show_sub_issues)}
          </DropdownMenuCheckboxItem>
          {restorable && <DropdownMenuItem onClick={() => { seed(initial); onChange(JSON.parse(JSON.stringify(initial))); }}>
            <RotateCcw className="size-3.5" />
            {t($ => $.trajectory_graph.filter_restore)}
          </DropdownMenuItem>}
        </>}
        trigger={trigger}
      />
    </ViewStoreProvider>}
  </div>;
}
