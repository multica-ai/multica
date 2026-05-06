"use client";

import { useCallback, useMemo, useRef, useState } from "react";
import { ChevronRight, Plus } from "lucide-react";
import { Accordion } from "@base-ui/react/accordion";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useDroppable,
  useSensor,
  useSensors,
  closestCenter,
  pointerWithin,
  type CollisionDetection,
  type DragStartEvent,
  type DragEndEvent,
} from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Button } from "@multica/ui/components/ui/button";
import type { Issue, IssueStatus } from "@multica/core/types";
import { useLoadMoreByStatus } from "@multica/core/issues/mutations";
import type { MyIssuesFilter } from "@multica/core/issues/queries";
import { useModalStore } from "@multica/core/modals";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { useIssueSelectionStore } from "@multica/core/issues/stores/selection-store";
import { ALL_STATUSES } from "@multica/core/issues/config";
import { sortIssues } from "../utils/sort";
import { StatusHeading } from "./status-heading";
import { ListRow, type ChildProgress } from "./list-row";
import { InfiniteScrollSentinel } from "./infinite-scroll-sentinel";

const EMPTY_PROGRESS_MAP = new Map<string, ChildProgress>();

const STATUS_DROPPABLE_PREFIX = "list-status:";

const STATUS_DROPPABLE_IDS = new Set<string>(
  ALL_STATUSES.map((s) => `${STATUS_DROPPABLE_PREFIX}${s}`),
);

/**
 * Drop targets are either status-section droppables (used when dropping into
 * an empty / collapsed status, or onto its header) or top-level row
 * sortables (used when dropping between siblings). Prefer the row over
 * the section so the user can drop precisely between rows even when their
 * pointer is technically inside both targets.
 */
const listCollision: CollisionDetection = (args) => {
  const pointer = pointerWithin(args);
  if (pointer.length > 0) {
    const rows = pointer.filter((c) => !STATUS_DROPPABLE_IDS.has(c.id as string));
    if (rows.length > 0) return rows;
  }
  return closestCenter(args);
};

/** Compute float position based on drop neighbors, mirroring board-view. */
function computePosition(
  ids: string[],
  activeId: string,
  issueMap: Map<string, Issue>,
): number {
  const idx = ids.indexOf(activeId);
  if (idx === -1) return 0;
  const getPos = (id: string) => issueMap.get(id)?.position ?? 0;
  if (ids.length === 1) return issueMap.get(activeId)?.position ?? 0;
  if (idx === 0) return getPos(ids[1]!) - 1;
  if (idx === ids.length - 1) return getPos(ids[idx - 1]!) + 1;
  return (getPos(ids[idx - 1]!) + getPos(ids[idx + 1]!)) / 2;
}

export function ListView({
  issues,
  visibleStatuses,
  onMoveIssue,
  childProgressMap = EMPTY_PROGRESS_MAP,
  myIssuesScope,
  myIssuesFilter,
}: {
  issues: Issue[];
  visibleStatuses: IssueStatus[];
  /**
   * Optional drag-to-move handler. When set, top-level rows become
   * draggable; dropping into another status changes `status`, dropping
   * within the same status sets a fractional `position`. When omitted
   * (e.g. read-only contexts), drag is disabled.
   */
  onMoveIssue?: (
    issueId: string,
    newStatus: IssueStatus,
    newPosition?: number,
  ) => void;
  childProgressMap?: Map<string, ChildProgress>;
  /** When set, per-status load-more targets the scoped cache instead of the workspace one. */
  myIssuesScope?: string;
  myIssuesFilter?: MyIssuesFilter;
}) {
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const listCollapsedStatuses = useViewStore(
    (s) => s.listCollapsedStatuses
  );
  const toggleListCollapsed = useViewStore(
    (s) => s.toggleListCollapsed
  );
  const collapsedParentIds = useViewStore((s) => s.collapsedParentIds);
  const toggleParentCollapsed = useViewStore((s) => s.toggleParentCollapsed);

  // Build a map from parent_issue_id → child issues, scoped to the issues
  // currently visible (post-filter, post-scope). A child whose parent is
  // also in `issues` is treated as nested; a child whose parent has been
  // filtered out becomes an "orphan" and renders at the top level so it
  // isn't silently hidden when a status filter excludes the parent.
  const { issuesByStatus, childrenByParent, orphanedChildIds } = useMemo(() => {
    const parentIds = new Set<string>();
    for (const i of issues) parentIds.add(i.id);

    const childrenMap = new Map<string, Issue[]>();
    const orphans = new Set<string>();
    for (const i of issues) {
      if (i.parent_issue_id && parentIds.has(i.parent_issue_id)) {
        const arr = childrenMap.get(i.parent_issue_id);
        if (arr) arr.push(i);
        else childrenMap.set(i.parent_issue_id, [i]);
      } else if (i.parent_issue_id) {
        orphans.add(i.id);
      }
    }

    // Sort children by the same view-level sort (so order is consistent
    // with siblings at the top level).
    for (const [pid, arr] of childrenMap) {
      childrenMap.set(pid, sortIssues(arr, sortBy, sortDirection));
    }

    // Top-level issues per status: any issue with no parent OR with a
    // parent that isn't in the visible set (orphans surface here).
    const byStatus = new Map<IssueStatus, Issue[]>();
    for (const status of visibleStatuses) {
      const filtered = issues.filter(
        (i) =>
          i.status === status &&
          (!i.parent_issue_id || !parentIds.has(i.parent_issue_id)),
      );
      byStatus.set(status, sortIssues(filtered, sortBy, sortDirection));
    }

    return {
      issuesByStatus: byStatus,
      childrenByParent: childrenMap,
      orphanedChildIds: orphans,
    };
  }, [issues, visibleStatuses, sortBy, sortDirection]);

  const collapsedSet = useMemo(
    () => new Set(collapsedParentIds),
    [collapsedParentIds],
  );

  const expandedStatuses = useMemo(
    () =>
      visibleStatuses.filter(
        (s) => !listCollapsedStatuses.includes(s)
      ),
    [visibleStatuses, listCollapsedStatuses]
  );

  const myIssuesOpts = myIssuesScope
    ? { scope: myIssuesScope, filter: myIssuesFilter ?? {} }
    : undefined;

  // ----- drag state -----
  const dragEnabled = !!onMoveIssue;
  const [activeIssue, setActiveIssue] = useState<Issue | null>(null);

  // Frozen during drag so child sortables stay referentially stable
  // even if a TQ refetch lands mid-drag.
  const issueMap = useMemo(() => {
    const m = new Map<string, Issue>();
    for (const i of issues) m.set(i.id, i);
    return m;
  }, [issues]);
  const issueMapRef = useRef(issueMap);
  if (!activeIssue) issueMapRef.current = issueMap;

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );

  const handleDragStart = useCallback(
    (e: DragStartEvent) => {
      const issue = issueMapRef.current.get(e.active.id as string);
      if (issue) setActiveIssue(issue);
    },
    [],
  );

  const handleDragCancel = useCallback(() => setActiveIssue(null), []);

  const handleDragEnd = useCallback(
    (e: DragEndEvent) => {
      setActiveIssue(null);
      const { active, over } = e;
      if (!over || !onMoveIssue) return;

      const activeId = active.id as string;
      const overId = over.id as string;

      // Resolve the target status. Dropped on a status section header
      // → that status. Dropped on a row → that row's status.
      let targetStatus: IssueStatus | null = null;
      if (overId.startsWith(STATUS_DROPPABLE_PREFIX)) {
        targetStatus = overId.slice(STATUS_DROPPABLE_PREFIX.length) as IssueStatus;
      } else {
        const overIssue = issueMapRef.current.get(overId);
        if (overIssue) targetStatus = overIssue.status as IssueStatus;
      }
      if (!targetStatus) return;

      const activeIssueObj = issueMapRef.current.get(activeId);
      if (!activeIssueObj) return;

      // Build the post-drop ID array for the target status, with `activeId`
      // inserted at the drop position. Used purely to compute the
      // fractional position from neighbors — server is the source of truth.
      const sourceIds = (issuesByStatus.get(activeIssueObj.status as IssueStatus) ?? [])
        .filter((i) => i.id !== activeId)
        .map((i) => i.id);
      const destBase =
        targetStatus === activeIssueObj.status
          ? sourceIds
          : (issuesByStatus.get(targetStatus) ?? []).map((i) => i.id);

      let insertIndex = destBase.length;
      if (!overId.startsWith(STATUS_DROPPABLE_PREFIX)) {
        const overIdx = destBase.indexOf(overId);
        if (overIdx !== -1) insertIndex = overIdx;
      }
      const finalIds = [...destBase];
      finalIds.splice(insertIndex, 0, activeId);

      const newPosition = computePosition(finalIds, activeId, issueMapRef.current);

      // No-op guard: same status + same effective position.
      if (
        activeIssueObj.status === targetStatus &&
        activeIssueObj.position === newPosition
      ) {
        return;
      }

      onMoveIssue(activeId, targetStatus, newPosition);
    },
    [onMoveIssue, issuesByStatus],
  );

  const accordion = (
    <Accordion.Root
      multiple
      className="space-y-1"
      value={expandedStatuses}
      onValueChange={(value: string[]) => {
        for (const status of visibleStatuses) {
          const wasExpanded = expandedStatuses.includes(status);
          const isExpanded = value.includes(status);
          if (wasExpanded !== isExpanded) {
            toggleListCollapsed(status as IssueStatus);
          }
        }
      }}
    >
      {visibleStatuses.map((status) => (
        <StatusAccordionItem
          key={status}
          status={status}
          issues={issuesByStatus.get(status) ?? []}
          childrenByParent={childrenByParent}
          collapsedParentIds={collapsedSet}
          onToggleParent={toggleParentCollapsed}
          childProgressMap={childProgressMap}
          orphanedChildIds={orphanedChildIds}
          dragEnabled={dragEnabled}
          myIssuesOpts={myIssuesOpts}
        />
      ))}
    </Accordion.Root>
  );

  return (
    <div className="flex-1 min-h-0 overflow-y-auto p-2">
      {dragEnabled ? (
        <DndContext
          sensors={sensors}
          collisionDetection={listCollision}
          onDragStart={handleDragStart}
          onDragEnd={handleDragEnd}
          onDragCancel={handleDragCancel}
        >
          {accordion}
          <DragOverlay dropAnimation={null}>
            {activeIssue ? (
              <div className="rounded-md bg-card shadow-lg shadow-black/10 ring-1 ring-border/60 opacity-95">
                <ListRow
                  issue={activeIssue}
                  childProgress={childProgressMap.get(activeIssue.id)}
                  isDragOverlay
                />
              </div>
            ) : null}
          </DragOverlay>
        </DndContext>
      ) : (
        accordion
      )}
    </div>
  );
}

function StatusAccordionItem({
  status,
  issues,
  childrenByParent,
  collapsedParentIds,
  onToggleParent,
  childProgressMap,
  orphanedChildIds,
  dragEnabled,
  myIssuesOpts,
}: {
  status: IssueStatus;
  issues: Issue[];
  childrenByParent: Map<string, Issue[]>;
  collapsedParentIds: Set<string>;
  onToggleParent: (parentId: string) => void;
  childProgressMap: Map<string, ChildProgress>;
  orphanedChildIds: Set<string>;
  dragEnabled: boolean;
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
}) {
  const selectedIds = useIssueSelectionStore((s) => s.selectedIds);
  const select = useIssueSelectionStore((s) => s.select);
  const deselect = useIssueSelectionStore((s) => s.deselect);
  const { loadMore, hasMore, isLoading, total } = useLoadMoreByStatus(
    status,
    myIssuesOpts,
  );

  // Drop target for the whole status section — used when dropping into
  // an empty status, onto the status header, or below the last row.
  // The id is namespaced (`list-status:`) so it doesn't collide with
  // issue ids in collision detection.
  const droppableId = `${STATUS_DROPPABLE_PREFIX}${status}`;
  const { setNodeRef: setSectionDroppableRef, isOver: sectionIsOver } = useDroppable({
    id: droppableId,
    disabled: !dragEnabled,
  });

  // Selection at the status level covers ALL rows that will render
  // under this status — including currently-visible children — so
  // shift-click and "select all" behave the same with or without nesting.
  const visibleRowIds = useMemo(() => {
    const ids: string[] = [];
    for (const issue of issues) {
      ids.push(issue.id);
      const children = childrenByParent.get(issue.id);
      if (children && !collapsedParentIds.has(issue.id)) {
        for (const c of children) ids.push(c.id);
      }
    }
    return ids;
  }, [issues, childrenByParent, collapsedParentIds]);

  // Only top-level non-orphan issues are draggable. Sub-issues stay
  // anchored to their parent; orphans have a filtered-out parent and
  // moving them across statuses on their own is confusing UX.
  const sortableIds = useMemo(
    () =>
      issues
        .filter((i) => !orphanedChildIds.has(i.id))
        .map((i) => i.id),
    [issues, orphanedChildIds],
  );

  const selectedCount = visibleRowIds.filter((id) => selectedIds.has(id)).length;
  const allSelected = visibleRowIds.length > 0 && selectedCount === visibleRowIds.length;
  const someSelected = selectedCount > 0;

  const sectionContent = (
    <Accordion.Item value={status}>
      <Accordion.Header
        className={`group/header flex h-10 items-center rounded-lg bg-muted/40 transition-colors hover:bg-accent/30 ${
          dragEnabled && sectionIsOver ? "ring-2 ring-primary/40 bg-primary/5" : ""
        }`}
      >
        <div className="pl-3 flex items-center">
          <input
            type="checkbox"
            checked={allSelected}
            ref={(el) => {
              if (el) el.indeterminate = someSelected && !allSelected;
            }}
            onChange={() => {
              if (allSelected) {
                deselect(visibleRowIds);
              } else {
                select(visibleRowIds);
              }
            }}
            className="cursor-pointer accent-primary"
          />
        </div>
        <Accordion.Trigger className="group/trigger flex flex-1 items-center gap-2 px-2 h-full text-left outline-none">
          <ChevronRight className="size-3.5 shrink-0 text-muted-foreground transition-transform group-aria-expanded/trigger:rotate-90" />
          <StatusHeading status={status} count={total} />
        </Accordion.Trigger>
        <div className="pr-2">
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="rounded-full text-muted-foreground opacity-0 group-hover/header:opacity-100 transition-opacity"
                  onClick={() =>
                    useModalStore
                      .getState()
                      .open("create-issue", { status })
                  }
                />
              }
            >
              <Plus className="size-3.5" />
            </TooltipTrigger>
            <TooltipContent>Add issue</TooltipContent>
          </Tooltip>
        </div>
      </Accordion.Header>
      <Accordion.Panel className="pt-1">
        {issues.length > 0 ? (
          <SortableContext items={sortableIds} strategy={verticalListSortingStrategy}>
            {issues.map((issue) => {
              const children = childrenByParent.get(issue.id);
              const hasChildren = !!children && children.length > 0;
              const collapsed = collapsedParentIds.has(issue.id);
              const isOrphan = orphanedChildIds.has(issue.id);
              return (
                <div key={issue.id}>
                  <ListRow
                    issue={issue}
                    childProgress={childProgressMap.get(issue.id)}
                    hasChildren={hasChildren}
                    collapsed={collapsed}
                    onToggleCollapsed={
                      hasChildren ? () => onToggleParent(issue.id) : undefined
                    }
                    isOrphan={isOrphan}
                    isSortable={dragEnabled && !isOrphan}
                  />
                  {hasChildren && !collapsed && (
                    <div role="group" aria-label={`Sub-issues of ${issue.identifier}`}>
                      {children!.map((child) => (
                        <ListRow
                          key={child.id}
                          issue={child}
                          childProgress={childProgressMap.get(child.id)}
                          indentLevel={1}
                        />
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
            {hasMore && (
              <InfiniteScrollSentinel onVisible={loadMore} loading={isLoading} />
            )}
          </SortableContext>
        ) : (
          <p className="py-6 text-center text-xs text-muted-foreground">
            No issues
          </p>
        )}
      </Accordion.Panel>
    </Accordion.Item>
  );

  return dragEnabled ? (
    <div ref={setSectionDroppableRef}>{sectionContent}</div>
  ) : (
    sectionContent
  );
}
