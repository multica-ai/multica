"use client";

import { useState, useCallback, useMemo, useEffect, useRef } from "react";
import { useT } from "@multica/i18n/react";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  pointerWithin,
  closestCenter,
  type CollisionDetection,
  type DragStartEvent,
  type DragEndEvent,
  type DragOverEvent,
} from "@dnd-kit/core";
import { arrayMove } from "@dnd-kit/sortable";
import { Eye, MoreHorizontal } from "lucide-react";
import type { Issue, IssueStatus } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useLoadMoreByStatus } from "@multica/core/issues/mutations";
import type { MyIssuesFilter } from "@multica/core/issues/queries";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { ALL_STATUSES } from "@multica/core/issues/config";
import { useViewStoreApi, useViewStore } from "@multica/core/issues/stores/view-store-context";
import type { SortField, SortDirection } from "@multica/core/issues/stores/view-store";
import { sortIssues } from "../utils/sort";
import { StatusIcon } from "./status-icon";
import { BoardColumn } from "./board-column";
import { BoardCardContent } from "./board-card";
import { InfiniteScrollSentinel } from "./infinite-scroll-sentinel";
import type { ChildProgress } from "./list-row";

const COLUMN_IDS = new Set<string>(ALL_STATUSES);

const kanbanCollision: CollisionDetection = (args) => {
  const pointer = pointerWithin(args);
  if (pointer.length > 0) {
    // Prefer card collisions over column collisions so that
    // dragging down within a column finds the target card
    // instead of the column droppable.
    const cards = pointer.filter((c) => !COLUMN_IDS.has(c.id as string));
    if (cards.length > 0) return cards;
  }
  // Fallback: closestCenter finds the nearest card even when
  // the pointer is in a gap between cards (common when dragging down).
  return closestCenter(args);
};

/** Build column ID arrays from TQ issue data, respecting current sort. */
function buildColumns(
  issues: Issue[],
  visibleStatuses: IssueStatus[],
  sortBy: SortField,
  sortDirection: SortDirection,
): Record<IssueStatus, string[]> {
  const cols = {} as Record<IssueStatus, string[]>;
  for (const status of visibleStatuses) {
    const sorted = sortIssues(
      issues.filter((i) => i.status === status),
      sortBy,
      sortDirection,
    );
    cols[status] = sorted.map((i) => i.id);
  }
  return cols;
}

/** Compute a float position for `activeId` based on its neighbors in `ids`. */
function computePosition(ids: string[], activeId: string, issueMap: Map<string, Issue>): number {
  const idx = ids.indexOf(activeId);
  if (idx === -1) return 0;
  const getPos = (id: string) => issueMap.get(id)?.position ?? 0;
  if (ids.length === 1) return issueMap.get(activeId)?.position ?? 0;
  if (idx === 0) return getPos(ids[1]!) - 1;
  if (idx === ids.length - 1) return getPos(ids[idx - 1]!) + 1;
  return (getPos(ids[idx - 1]!) + getPos(ids[idx + 1]!)) / 2;
}

/** Find which column (status) contains a given ID (issue or column droppable). */
function findColumn(
  columns: Record<IssueStatus, string[]>,
  id: string,
  visibleStatuses: IssueStatus[],
): IssueStatus | null {
  if (visibleStatuses.includes(id as IssueStatus)) return id as IssueStatus;
  for (const [status, ids] of Object.entries(columns)) {
    if (ids.includes(id)) return status as IssueStatus;
  }
  return null;
}

const EMPTY_PROGRESS_MAP = new Map<string, ChildProgress>();

export function BoardView({
  issues,
  visibleStatuses,
  hiddenStatuses,
  onMoveIssue,
  childProgressMap = EMPTY_PROGRESS_MAP,
  myIssuesScope,
  myIssuesFilter,
}: {
  issues: Issue[];
  visibleStatuses: IssueStatus[];
  hiddenStatuses: IssueStatus[];
  onMoveIssue: (
    issueId: string,
    newStatus: IssueStatus,
    newPosition?: number
  ) => void;
  childProgressMap?: Map<string, ChildProgress>;
  /** When set, per-status load-more targets the scoped cache instead of the workspace one. */
  myIssuesScope?: string;
  myIssuesFilter?: MyIssuesFilter;
}) {
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const myIssuesOpts = myIssuesScope
    ? { scope: myIssuesScope, filter: myIssuesFilter ?? {} }
    : undefined;

  // --- Drag state ---
  const [activeIssue, setActiveIssue] = useState<Issue | null>(null);
  const isDraggingRef = useRef(false);

  // --- Local columns state ---
  // Between drags: follows TQ via useEffect.
  // During drag: local-only, driven by onDragOver/onDragEnd.
  const [columns, setColumns] = useState<Record<IssueStatus, string[]>>(() =>
    buildColumns(issues, visibleStatuses, sortBy, sortDirection),
  );
  const columnsRef = useRef(columns);
  columnsRef.current = columns;

  useEffect(() => {
    if (!isDraggingRef.current) {
      setColumns(buildColumns(issues, visibleStatuses, sortBy, sortDirection));
    }
  }, [issues, visibleStatuses, sortBy, sortDirection]);

  // After a cross-column move, lock for one animation frame so dnd-kit's
  // collision detection can stabilize before processing the next move.
  // Without this, collision oscillates: A→B→A→B… until React bails out.
  const recentlyMovedRef = useRef(false);
  useEffect(() => {
    const id = requestAnimationFrame(() => {
      recentlyMovedRef.current = false;
    });
    return () => cancelAnimationFrame(id);
  }, [columns]);

  // --- Issue map ---
  // Frozen during drag so BoardColumn/DraggableBoardCard props stay
  // referentially stable even if a TQ refetch lands mid-drag.
  const issueMap = useMemo(() => {
    const map = new Map<string, Issue>();
    for (const issue of issues) map.set(issue.id, issue);
    return map;
  }, [issues]);

  const issueMapRef = useRef(issueMap);
  if (!isDraggingRef.current) {
    issueMapRef.current = issueMap;
  }

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 5 },
    })
  );

  const handleDragStart = useCallback(
    (event: DragStartEvent) => {
      isDraggingRef.current = true;
      const issue = issueMapRef.current.get(event.active.id as string) ?? null;
      setActiveIssue(issue);
    },
    [],
  );

  const handleDragOver = useCallback(
    (event: DragOverEvent) => {
      const { active, over } = event;
      if (!over || recentlyMovedRef.current) return;

      const activeId = active.id as string;
      const overId = over.id as string;

      setColumns((prev) => {
        const activeCol = findColumn(prev, activeId, visibleStatuses);
        const overCol = findColumn(prev, overId, visibleStatuses);
        if (!activeCol || !overCol || activeCol === overCol) return prev;

        recentlyMovedRef.current = true;
        const oldIds = prev[activeCol]!.filter((id) => id !== activeId);
        const newIds = [...prev[overCol]!];
        const overIndex = newIds.indexOf(overId);
        const insertIndex = overIndex >= 0 ? overIndex : newIds.length;
        newIds.splice(insertIndex, 0, activeId);
        return { ...prev, [activeCol]: oldIds, [overCol]: newIds };
      });
    },
    [visibleStatuses],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      isDraggingRef.current = false;
      setActiveIssue(null);

      const resetColumns = () =>
        setColumns(buildColumns(issues, visibleStatuses, sortBy, sortDirection));

      if (!over) {
        resetColumns();
        return;
      }

      const activeId = active.id as string;
      const overId = over.id as string;

      const cols = columnsRef.current;
      const activeCol = findColumn(cols, activeId, visibleStatuses);
      const overCol = findColumn(cols, overId, visibleStatuses);
      if (!activeCol || !overCol) {
        resetColumns();
        return;
      }

      // Same-column reorder
      let finalColumns = cols;
      if (activeCol === overCol) {
        const ids = cols[activeCol]!;
        const oldIndex = ids.indexOf(activeId);
        const newIndex = ids.indexOf(overId);
        if (oldIndex !== -1 && newIndex !== -1 && oldIndex !== newIndex) {
          const reordered = arrayMove(ids, oldIndex, newIndex);
          finalColumns = { ...cols, [activeCol]: reordered };
          setColumns(finalColumns);
        }
      }

      const finalCol = findColumn(finalColumns, activeId, visibleStatuses);
      if (!finalCol) {
        resetColumns();
        return;
      }

      const map = issueMapRef.current;
      const finalIds = finalColumns[finalCol]!;
      const newPosition = computePosition(finalIds, activeId, map);
      const currentIssue = map.get(activeId);

      if (
        currentIssue &&
        currentIssue.status === finalCol &&
        currentIssue.position === newPosition
      ) {
        return;
      }

      onMoveIssue(activeId, finalCol, newPosition);
    },
    [issues, visibleStatuses, sortBy, sortDirection, onMoveIssue],
  );

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={kanbanCollision}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
    >
      <div className="flex flex-1 min-h-0 gap-4 overflow-x-auto p-4">
        {visibleStatuses.map((status) => (
          <PaginatedBoardColumn
            key={status}
            status={status}
            issueIds={columns[status] ?? []}
            issueMap={issueMapRef.current}
            childProgressMap={childProgressMap}
            myIssuesOpts={myIssuesOpts}
          />
        ))}

        {hiddenStatuses.length > 0 && (
          <HiddenColumnsPanel
            hiddenStatuses={hiddenStatuses}
            myIssuesOpts={myIssuesOpts}
          />
        )}
      </div>

      <DragOverlay dropAnimation={null}>
        {activeIssue ? (
          <div className="w-[280px] rotate-2 scale-105 cursor-grabbing opacity-90 shadow-lg shadow-black/10">
            <BoardCardContent issue={activeIssue} childProgress={childProgressMap.get(activeIssue.id)} />
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}

function PaginatedBoardColumn({
  status,
  issueIds,
  issueMap,
  childProgressMap,
  myIssuesOpts,
}: {
  status: IssueStatus;
  issueIds: string[];
  issueMap: Map<string, Issue>;
  childProgressMap?: Map<string, ChildProgress>;
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
}) {
  const { loadMore, hasMore, isLoading, total } = useLoadMoreByStatus(
    status,
    myIssuesOpts,
  );
  return (
    <BoardColumn
      status={status}
      issueIds={issueIds}
      issueMap={issueMap}
      childProgressMap={childProgressMap}
      totalCount={total}
      footer={
        hasMore ? (
          <InfiniteScrollSentinel onVisible={loadMore} loading={isLoading} />
        ) : undefined
      }
    />
  );
}

function HiddenColumnsPanel({
  hiddenStatuses,
  myIssuesOpts,
}: {
  hiddenStatuses: IssueStatus[];
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
}) {
  const t = useT("issues");
  return (
    <div className="flex w-[240px] shrink-0 flex-col">
      <div className="mb-2 flex items-center gap-2 px-1">
        <span className="text-sm font-medium text-muted-foreground">
          {t("board_hidden_columns")}
        </span>
      </div>
      <div className="flex-1 space-y-0.5">
        {hiddenStatuses.map((status) => (
          <HiddenColumnRow
            key={status}
            status={status}
            myIssuesOpts={myIssuesOpts}
          />
        ))}
      </div>
    </div>
  );
}

function HiddenColumnRow({
  status,
  myIssuesOpts,
}: {
  status: IssueStatus;
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
}) {
  const t = useT("issues");
  const tStatus = useT("status");
  const viewStoreApi = useViewStoreApi();
  const { total } = useLoadMoreByStatus(status, myIssuesOpts);
  return (
    <div className="flex items-center justify-between rounded-lg px-2.5 py-2 hover:bg-muted/50">
      <div className="flex items-center gap-2">
        <StatusIcon status={status} className="h-3.5 w-3.5" />
        <span className="text-sm">{tStatus(status)}</span>
      </div>
      <div className="flex items-center gap-1.5">
        <span className="text-xs text-muted-foreground">{total}</span>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                className="rounded-full text-muted-foreground"
              >
                <MoreHorizontal className="size-3.5" />
              </Button>
            }
          />
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              onClick={() => viewStoreApi.getState().showStatus(status)}
            >
              <Eye className="size-3.5" />
              {t("board_show_column")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
