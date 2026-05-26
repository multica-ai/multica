"use client";

import { useState, useCallback, useMemo, useEffect, useRef } from "react";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  useDroppable,
  pointerWithin,
  closestCenter,
  type CollisionDetection,
  type DragStartEvent,
  type DragEndEvent,
  type DragOverEvent,
} from "@dnd-kit/core";
import { SortableContext, useSortable, verticalListSortingStrategy, arrayMove } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { ChevronRight, EyeOff, GripVertical, MoreHorizontal, Pencil, Plus } from "lucide-react";
import type { Issue, IssueStatus } from "@multica/core/types";
import type { UpdateIssueRequest } from "@multica/core/types";
import { useViewStore, useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import { useWorkspacePaths } from "@multica/core/paths";
import { useLoadMoreByStatus } from "@multica/core/issues/mutations";
import type { IssueSortParam, MyIssuesFilter } from "@multica/core/issues/queries";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { sortIssues } from "../utils/sort";
import { BOARD_STATUSES, STATUS_CONFIG } from "@multica/core/issues/config";
import { useModalStore } from "@multica/core/modals";
import { DraggableBoardCard, BoardCardContent } from "./board-card";
import { StatusIcon } from "./status-icon";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Button } from "@multica/ui/components/ui/button";
import { StatusHeading } from "./status-heading";
import { HiddenColumnsPanel, HiddenColumnRow } from "./hidden-columns-panel";
import { InfiniteScrollSentinel } from "./infinite-scroll-sentinel";
import { AppLink } from "../../navigation";
import type { ChildProgress } from "./list-row";
import { useT } from "../../i18n";

const COLUMN_WIDTH = 280;
const COLUMN_GAP = 16;

type SwimLaneMoveUpdates = Pick<
  UpdateIssueRequest,
  "parent_issue_id" | "status" | "position"
>;

function makeSwimLaneCollision(cellIds: Set<string>): CollisionDetection {
  return (args) => {
    const activeId = args.active.id as string;
    const isLaneDrag = activeId.startsWith("lane:");

    const pointer = pointerWithin(args);
    if (pointer.length > 0) {
      let filtered = pointer;
      if (isLaneDrag) {
        // Lane dragging: only consider other lane headers
        filtered = pointer.filter((c) => (c.id as string).startsWith("lane:"));
      } else {
        // Card dragging: ignore parent lane headers entirely
        filtered = pointer.filter((c) => !(c.id as string).startsWith("lane:"));
      }

      if (filtered.length > 0) {
        const cards = filtered.filter((c) => !cellIds.has(c.id as string));
        if (cards.length > 0) return cards;
        return filtered;
      }
    }

    const closest = closestCenter(args);
    let filteredClosest = closest;
    if (isLaneDrag) {
      filteredClosest = closest.filter((c) => (c.id as string).startsWith("lane:"));
    } else {
      filteredClosest = closest.filter((c) => !(c.id as string).startsWith("lane:"));
    }

    return filteredClosest;
  };
}

function parseCellId(id: string): { parentKey: string; status: string } | null {
  if (!id.startsWith("swim:")) return null;
  const rest = id.slice(5);
  const lastColon = rest.lastIndexOf(":");
  if (lastColon === -1) return null;
  return {
    parentKey: rest.slice(0, lastColon),
    status: rest.slice(lastColon + 1),
  };
}

function findCellIn(
  data: Record<string, Record<string, string[]>>,
  cellIds: Set<string>,
  id: string,
): { parentKey: string; status: string } | null {
  if (cellIds.has(id)) return parseCellId(id);
  for (const [pk, statusMap] of Object.entries(data)) {
    for (const [status, ids] of Object.entries(statusMap)) {
      if (ids.includes(id)) return { parentKey: pk, status };
    }
  }
  return null;
}

function cellId(parentKey: string, status: IssueStatus): string {
  return `swim:${parentKey}:${status}`;
}

const LANE_ID_PREFIX = "lane:";

/** Sentinel key for the "Other parents" fallback lane. */
const ORPHAN_LANE_KEY = "parent:__orphans__";

/** Sortable id for a draggable swimlane header (parents only). */
function laneId(parentIssueId: string): string {
  return `${LANE_ID_PREFIX}${parentIssueId}`;
}

function parseLaneId(id: string): string | null {
  if (!id.startsWith(LANE_ID_PREFIX)) return null;
  return id.slice(LANE_ID_PREFIX.length);
}

function computePosition(ids: string[], activeId: string, issueMap: Map<string, Issue>): number {
  const idx = ids.indexOf(activeId);
  if (idx === -1) return 0;
  const getPos = (id: string) => issueMap.get(id)?.position ?? 0;
  if (ids.length === 1) return issueMap.get(activeId)?.position ?? 0;
  if (idx === 0) return getPos(ids[1]!) - 1;
  if (idx === ids.length - 1) return getPos(ids[idx - 1]!) + 1;
  return (getPos(ids[idx - 1]!) + getPos(ids[idx + 1]!)) / 2;
}

interface ParentGroup {
  key: string;
  parentIssueId: string | null;
  identifier: string;
  title: string;
  /** Full Issue object for the parent — available for parent lanes, null for "No parent". */
  issue: Issue | null;
}

const EMPTY_PROGRESS_MAP = new Map<string, ChildProgress>();

export function SwimLaneView({
  issues,
  unfilteredIssues,
  visibleStatuses = BOARD_STATUSES,
  hiddenStatuses = [],
  onMoveIssue,
  childProgressMap = EMPTY_PROGRESS_MAP,
  myIssuesScope,
  myIssuesFilter,
  sort,
  projectId,
}: {
  issues: Issue[];
  /**
   * Status-unfiltered companion set used for parent metadata lookup and
   * status totals. Lane discovery still drives off the visible `issues`
   * set so that parents whose children are all in hidden statuses don't
   * produce empty rows, but header chrome (identifier, title, issue ref
   * for the Open-parent link) and hidden-column counts read from here so
   * a parent in a hidden status still surfaces its label correctly.
   */
  unfilteredIssues?: Issue[];
  visibleStatuses?: IssueStatus[];
  hiddenStatuses?: IssueStatus[];
  onMoveIssue: (issueId: string, updates: SwimLaneMoveUpdates) => void;
  childProgressMap?: Map<string, ChildProgress>;
  myIssuesScope?: string;
  myIssuesFilter?: MyIssuesFilter;
  /** Must match the sort the page queried with — embedded in the cache key. */
  sort?: IssueSortParam;
  /** Pre-fills `project_id` on the create form for the in-cell "+" button. */
  projectId?: string;
}) {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const viewStoreApi = useViewStoreApi();
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const swimlaneOrder = useViewStore((s) => s.swimlaneOrder);

  const laneSourceIssues = unfilteredIssues ?? issues;

  const myIssuesOpts = useMemo(
    () =>
      myIssuesScope
        ? { scope: myIssuesScope, filter: myIssuesFilter ?? {} }
        : undefined,
    [myIssuesScope, myIssuesFilter],
  );

  const sortedStatuses = useMemo(
    () => BOARD_STATUSES.filter((s) => visibleStatuses.includes(s)),
    [visibleStatuses],
  );

  const parentGroups = useMemo<ParentGroup[]>(() => {
    // Metadata lookup spans the broader unfiltered set so a parent whose
    // status is currently hidden still surfaces its identifier / title
    // when one of its children is visible.
    const metadataMap = new Map<string, Issue>();
    for (const issue of laneSourceIssues) {
      metadataMap.set(issue.id, issue);
    }

    // Lane discovery drives off the visible `issues` set — a lane is
    // only rendered when at least one visible card belongs in it. This
    // avoids a stack of empty rows for parents whose children all live
    // in currently-filtered-out statuses.
    const seen = new Map<string, ParentGroup>();
    let hasOrphan = false;
    for (const issue of issues) {
      if (issue.parent_issue_id === null) continue;
      const parent = metadataMap.get(issue.parent_issue_id);
      if (!parent) {
        hasOrphan = true;
        continue;
      }
      const key = `parent:${issue.parent_issue_id}`;
      if (!seen.has(key)) {
        seen.set(key, {
          key,
          parentIssueId: issue.parent_issue_id,
          identifier: parent.identifier,
          title: parent.title,
          issue: parent,
        });
      }
    }

    // Apply user-defined lane order: stored entries first (in the order they
    // were saved), then any newly-introduced parents that aren't in the
    // stored order yet (in natural data order, so they don't randomly
    // shuffle around). "No parent" is always pinned at the very top.
    const orderIndex = new Map<string, number>();
    swimlaneOrder.forEach((parentId, idx) => {
      orderIndex.set(`parent:${parentId}`, idx);
    });
    const ordered = Array.from(seen.values()).sort((a, b) => {
      const ai = orderIndex.get(a.key);
      const bi = orderIndex.get(b.key);
      if (ai !== undefined && bi !== undefined) return ai - bi;
      if (ai !== undefined) return -1; // a is stored, b isn't → a first
      if (bi !== undefined) return 1;
      return 0; // both unstored → preserve insertion order
    });

    const groups: ParentGroup[] = [
      {
        key: "parent:none",
        parentIssueId: null,
        identifier: "",
        title: t(($) => $.swimlane.no_parent),
        issue: null,
      },
    ];
    if (hasOrphan) {
      groups.push({
        key: ORPHAN_LANE_KEY,
        parentIssueId: null,
        identifier: "",
        title: t(($) => $.swimlane.other_parents),
        issue: null,
      });
    }
    groups.push(...ordered);
    return groups;
  }, [issues, laneSourceIssues, t, swimlaneOrder]);

  // Issues that act as swimlane headers (they have children in the loaded
  // set) should not also appear as cards in the "No parent" lane — that
  // would be redundant noise since the lane header already represents them.
  const parentIssueIds = useMemo(
    () => new Set(parentGroups.map((g) => g.parentIssueId).filter(Boolean)),
    [parentGroups],
  );

  const cells = useMemo(() => {
    const result: Record<string, Record<string, string[]>> = {};
    for (const parent of parentGroups) {
      const cellMap: Record<string, string[]> = {};
      for (const status of sortedStatuses) {
        cellMap[status] = [];
      }
      const parentIssues = sortIssues(
        issues.filter((issue) => {
          // Issues that are themselves lane headers are rendered by the
          // header, not as a card — avoids a double render for multi-level
          // nesting (a parent that also has a grandparent).
          if (parentIssueIds.has(issue.id)) return false;
          if (parent.key === ORPHAN_LANE_KEY) {
            // Fallback bucket: children whose parent isn't in the loaded
            // set (deleted, or filtered out by a server-side scope like
            // "assigned to me"). Catches them so they don't silently
            // vanish from Swimlane while still visible in Board/List.
            return (
              issue.parent_issue_id !== null &&
              !parentIssueIds.has(issue.parent_issue_id)
            );
          }
          if (parent.parentIssueId === null) {
            return issue.parent_issue_id === null;
          }
          return issue.parent_issue_id === parent.parentIssueId;
        }),
        sortBy,
        sortDirection,
      );
      for (const issue of parentIssues) {
        const s = issue.status;
        if (cellMap[s]) {
          cellMap[s].push(issue.id);
        }
      }
      result[parent.key] = cellMap;
    }
    return result;
  }, [issues, parentGroups, sortedStatuses, sortBy, sortDirection, parentIssueIds]);

  const cellSet = useMemo(() => {
    const ids = new Set<string>();
    for (const parent of parentGroups) {
      for (const status of sortedStatuses) {
        ids.add(cellId(parent.key, status));
      }
    }
    return ids;
  }, [parentGroups, sortedStatuses]);

  // Drives both visible status-header counts AND hidden-column panel rows.
  // Known limitation: `parentIssueIds` is derived from lanes that exist
  // in the current render (only parents with ≥1 visible child). A parent
  // whose children are all hidden doesn't get a lane, so it counts as a
  // card here. When the user un-hides that status the parent gets
  // promoted to a lane header and the count for that status drops by 1.
  // The visible-column total and the "would-see-if-unhidden" total are
  // therefore subtly different aggregates — fixing this requires a
  // second parentIssueIds set built from laneSourceIssues rather than
  // the rendered parentGroups. Tracked as a follow-up.
  const statusTotals = useMemo(() => {
    const totals = new Map<IssueStatus, number>();
    for (const issue of laneSourceIssues) {
      if (parentIssueIds.has(issue.id)) continue;
      totals.set(issue.status, (totals.get(issue.status) ?? 0) + 1);
    }
    return totals;
  }, [laneSourceIssues, parentIssueIds]);

  // Collapsed swimlanes — persisted per-workspace via the view store.
  // The store keys are raw parent issue ids (or "none" for the "No parent"
  // lane), but lane keys in this component are namespaced as
  // `parent:<id>` / `parent:none`.  Convert on read/write.
  const collapsedSwimlanes = useViewStore((s) => s.collapsedSwimlanes);
  const collapsedLanes = useMemo(() => {
    const set = new Set<string>();
    for (const id of collapsedSwimlanes) {
      set.add(id === "none" ? "parent:none" : `parent:${id}`);
    }
    return set;
  }, [collapsedSwimlanes]);
  const toggleLane = useCallback(
    (laneKey: string) => {
      const storeKey = laneKey === "parent:none" ? "none" : laneKey.slice("parent:".length);
      viewStoreApi.getState().toggleSwimlaneCollapsed(storeKey);
    },
    [viewStoreApi],
  );

  const [activeIssue, setActiveIssue] = useState<Issue | null>(null);
  const isDraggingRef = useRef(false);

  const issueMap = useMemo(() => {
    const map = new Map<string, Issue>();
    for (const issue of issues) map.set(issue.id, issue);
    return map;
  }, [issues]);

  const issueMapRef = useRef(issueMap);
  if (!isDraggingRef.current) {
    issueMapRef.current = issueMap;
  }

  const [localCells, setLocalCells] = useState(cells);
  const localCellsRef = useRef(localCells);
  localCellsRef.current = localCells;

  useEffect(() => {
    if (!isDraggingRef.current) {
      setLocalCells(cells);
    }
  }, [cells]);

  const recentlyMovedRef = useRef(false);
  useEffect(() => {
    const id = requestAnimationFrame(() => {
      recentlyMovedRef.current = false;
    });
    return () => cancelAnimationFrame(id);
  }, [localCells]);

  const collisionDetection = useMemo(
    () => makeSwimLaneCollision(cellSet),
    [cellSet],
  );

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 5 },
    }),
  );

  const handleDragStart = useCallback((event: DragStartEvent) => {
    isDraggingRef.current = true;
    const activeId = event.active.id as string;
    // Lane drags don't carry an Issue payload — clear the card overlay so
    // we don't show a stale card during a lane reorder.
    if (parseLaneId(activeId)) {
      setActiveIssue(null);
      return;
    }
    const issue = issueMapRef.current.get(activeId) ?? null;
    setActiveIssue(issue);
  }, []);

  const handleDragOver = useCallback(
    (event: DragOverEvent) => {
      const { active, over } = event;
      if (!over || recentlyMovedRef.current) return;

      const activeId = active.id as string;
      const overId = over.id as string;

      setLocalCells((prev) => {
        const activeCell = findCellIn(prev, cellSet, activeId);
        const overCell = findCellIn(prev, cellSet, overId);
        if (!activeCell || !overCell) return prev;
        if (
          activeCell.parentKey === overCell.parentKey &&
          activeCell.status === overCell.status
        ) {
          return prev;
        }
        // The "Other parents" lane is display-only: never let a card
        // enter or leave it via drag. The lane represents children whose
        // canonical parent isn't loaded, so any cross-lane move would
        // either lose that parent (when leaving) or invent a new one
        // (when entering).
        if (
          activeCell.parentKey === ORPHAN_LANE_KEY ||
          overCell.parentKey === ORPHAN_LANE_KEY
        ) {
          return prev;
        }

        recentlyMovedRef.current = true;

        if (activeCell.parentKey === overCell.parentKey) {
          // Same parent row, different status column
          const row = prev[activeCell.parentKey] ?? {};
          const sourceIds = (row[activeCell.status] ?? []).filter((id) => id !== activeId);
          const targetIds = (row[overCell.status] ?? []).filter((id) => id !== activeId);

          const overIndex = targetIds.indexOf(overId);
          const insertIndex = overIndex >= 0 ? overIndex : targetIds.length;
          targetIds.splice(insertIndex, 0, activeId);

          return {
            ...prev,
            [activeCell.parentKey]: {
              ...row,
              [activeCell.status]: sourceIds,
              [overCell.status]: targetIds,
            },
          };
        } else {
          // Different parent rows
          const sourceRow = prev[activeCell.parentKey] ?? {};
          const targetRow = prev[overCell.parentKey] ?? {};

          const sourceIds = (sourceRow[activeCell.status] ?? []).filter((id) => id !== activeId);
          const targetIds = (targetRow[overCell.status] ?? []).filter((id) => id !== activeId);

          const overIndex = targetIds.indexOf(overId);
          const insertIndex = overIndex >= 0 ? overIndex : targetIds.length;
          targetIds.splice(insertIndex, 0, activeId);

          return {
            ...prev,
            [activeCell.parentKey]: {
              ...sourceRow,
              [activeCell.status]: sourceIds,
            },
            [overCell.parentKey]: {
              ...targetRow,
              [overCell.status]: targetIds,
            },
          };
        }
      });
    },
    [cellSet],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      isDraggingRef.current = false;
      setActiveIssue(null);

      const reset = () => setLocalCells(cells);

      if (!over) {
        reset();
        return;
      }

      const activeId = active.id as string;
      const overId = over.id as string;

      // Lane reorder runs before the card-move logic because lane ids
      // don't resolve to any cell.
      const activeParentId = parseLaneId(activeId);
      const overParentId = parseLaneId(overId);
      if (activeParentId && overParentId && activeParentId !== overParentId) {
        const visibleOrder = parentGroups
          .filter((g) => g.parentIssueId !== null)
          .map((g) => g.parentIssueId!);
        const fromIdx = visibleOrder.indexOf(activeParentId);
        const toIdx = visibleOrder.indexOf(overParentId);
        if (fromIdx === -1 || toIdx === -1 || fromIdx === toIdx) return;
        const visibleNext = arrayMove(visibleOrder, fromIdx, toIdx);

        // Merge into the persisted order without clobbering entries that
        // aren't currently visible (e.g. hidden by a status filter).
        // Walk stored, overwriting each visible slot with the next id
        // from `visibleNext`; non-visible entries pass through verbatim.
        // Any remaining `visibleNext` ids (visible parents that weren't
        // in stored at all) get appended at the end.
        const stored = viewStoreApi.getState().swimlaneOrder;
        const visibleSet = new Set(visibleOrder);
        let cursor = 0;
        const merged = stored.map((id) =>
          visibleSet.has(id) ? visibleNext[cursor++]! : id,
        );
        for (const id of visibleNext.slice(cursor)) merged.push(id);

        viewStoreApi.getState().setSwimlaneOrder(merged);
        return;
      }
      if (activeParentId || overParentId) return;

      const cols = localCellsRef.current;

      const activeCell = findCellIn(cols, cellSet, activeId);
      const overCell = findCellIn(cols, cellSet, overId);
      if (!activeCell || !overCell) {
        reset();
        return;
      }

      // The "Other parents" lane is display-only. Refuse any drop where
      // either the source or the original target cell belongs to it —
      // no re-parenting (we don't know the canonical parent), no
      // position write (siblings here belong to different parents).
      if (
        activeCell.parentKey === ORPHAN_LANE_KEY ||
        overCell.parentKey === ORPHAN_LANE_KEY
      ) {
        reset();
        return;
      }

      let finalCells = cols;
      // Handle reordering within the same target cell upon drop.
      if (
        activeCell.parentKey === overCell.parentKey &&
        activeCell.status === overCell.status
      ) {
        const ids = cols[activeCell.parentKey]?.[activeCell.status];
        if (ids) {
          const oldIndex = ids.indexOf(activeId);
          const newIndex = ids.indexOf(overId);
          if (oldIndex !== -1 && newIndex !== -1 && oldIndex !== newIndex) {
            const reordered = arrayMove(ids, oldIndex, newIndex);
            finalCells = {
              ...cols,
              [activeCell.parentKey]: {
                ...cols[activeCell.parentKey],
                [activeCell.status]: reordered,
              },
            };
            setLocalCells(finalCells);
          }
        }
      }

      const finalOverCell = findCellIn(finalCells, cellSet, activeId);
      if (!finalOverCell) {
        reset();
        return;
      }

      const finalIds = finalCells[finalOverCell.parentKey]?.[finalOverCell.status] ?? [];
      const newPosition = computePosition(finalIds, activeId, issueMapRef.current);
      const currentIssue = issueMapRef.current.get(activeId);

      const expectedParent =
        finalOverCell.parentKey === "parent:none"
          ? null
          : finalOverCell.parentKey.replace("parent:", "");

      if (
        currentIssue &&
        currentIssue.parent_issue_id === expectedParent &&
        currentIssue.status === (finalOverCell.status as IssueStatus) &&
        currentIssue.position === newPosition
      ) {
        return;
      }

      onMoveIssue(activeId, {
        parent_issue_id: expectedParent,
        status: finalOverCell.status as IssueStatus,
        position: newPosition,
      });
    },
    [cells, cellSet, onMoveIssue, parentGroups, viewStoreApi],
  );

  // Grid template: one column per status, fixed width COLUMN_WIDTH, gap COLUMN_GAP.
  const trackWidth = sortedStatuses.length * COLUMN_WIDTH + Math.max(0, sortedStatuses.length - 1) * COLUMN_GAP;
  const gridStyle = {
    display: "grid",
    gridTemplateColumns: `repeat(${sortedStatuses.length}, ${COLUMN_WIDTH}px)`,
    columnGap: `${COLUMN_GAP}px`,
    width: `${trackWidth}px`,
  } as const;

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={collisionDetection}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
    >
      <div className="flex flex-1 min-h-0 gap-4 overflow-auto p-4">
        <div className="flex shrink-0 flex-col" style={{ width: `${trackWidth}px` }}>
        {/* Sticky status header row — visually matches the top of a BoardColumn */}
        <div className="sticky top-0 z-10 mb-2 bg-background/95 pb-2 backdrop-blur supports-[backdrop-filter]:bg-background/75">
          <div style={gridStyle}>
            {sortedStatuses.map((status) => {
              const cfg = STATUS_CONFIG[status];
              const total = statusTotals.get(status) ?? 0;
              return (
                <div
                  key={status}
                  className={`flex items-center justify-between rounded-xl ${cfg?.columnBg ?? "bg-muted/40"} px-3 py-2`}
                >
                  <StatusHeading status={status} count={total} />
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-sm"
                          aria-label={t(($) => $.board.hide_column)}
                          className="rounded-full text-muted-foreground"
                        >
                          <MoreHorizontal className="size-3.5" />
                        </Button>
                      }
                    />
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem
                        onClick={() => viewStoreApi.getState().hideStatus(status)}
                      >
                        <EyeOff className="size-3.5" />
                        {t(($) => $.board.hide_column)}
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              );
            })}
          </div>
        </div>

        {/* Parent rows. "No parent" is pinned at top and non-draggable;
            the rest are wrapped in a SortableContext so users can reorder
            lanes by dragging the grip handle. */}
        <div className="flex flex-col gap-4">
          {parentGroups
            .filter((p) => p.parentIssueId === null)
            .map((parent) => (
              <DraggableSwimLane
                key={parent.key}
                parent={parent}
                isCollapsed={collapsedLanes.has(parent.key)}
                onToggleCollapse={() => toggleLane(parent.key)}
                localCells={localCells}
                sortedStatuses={sortedStatuses}
                issueMap={issueMapRef.current}
                childProgressMap={childProgressMap}
                gridStyle={gridStyle}
                paths={paths}
                projectId={projectId}
              />
            ))}
          <SortableContext
            items={parentGroups
              .filter((p) => p.parentIssueId !== null)
              .map((p) => laneId(p.parentIssueId!))}
            strategy={verticalListSortingStrategy}
          >
            {parentGroups
              .filter((p) => p.parentIssueId !== null)
              .map((parent) => (
                <DraggableSwimLane
                  key={parent.key}
                  parent={parent}
                  isCollapsed={collapsedLanes.has(parent.key)}
                  onToggleCollapse={() => toggleLane(parent.key)}
                  localCells={localCells}
                  sortedStatuses={sortedStatuses}
                  issueMap={issueMapRef.current}
                  childProgressMap={childProgressMap}
                  gridStyle={gridStyle}
                  paths={paths}
                  projectId={projectId}
                />
              ))}
          </SortableContext>

          {/* Per-status load-more sentinels — same bucketed cache as Board. */}
          <SwimLaneLoadMoreRow
            sortedStatuses={sortedStatuses}
            gridStyle={gridStyle}
            myIssuesOpts={myIssuesOpts}
            sort={sort}
          />
        </div>
        </div>

        {hiddenStatuses.length > 0 && (
          <SwimLaneHiddenColumnsPanel
            hiddenStatuses={hiddenStatuses}
            statusTotals={statusTotals}
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

/**
 * Renders a single swimlane (lane header + cells row).
 *
 * Lanes with a real parent are made draggable via `useSortable` so users can
 * reorder them.  The "No parent" lane passes through with `disabled: true`
 * so it stays pinned and unclickable for drag — useSortable must still be
 * called unconditionally to satisfy the rules of hooks.
 *
 * Click vs drag: PointerSensor has `activationConstraint: { distance: 5 }`,
 * so taps on the header still toggle collapse while a >=5px drag starts the
 * sortable interaction. The "Open parent" pencil link stops pointer events
 * so users can click it without inadvertently starting a drag.
 */
function DraggableSwimLane({
  parent,
  isCollapsed,
  onToggleCollapse,
  localCells,
  sortedStatuses,
  issueMap,
  childProgressMap,
  gridStyle,
  paths,
  projectId,
}: {
  parent: ParentGroup;
  isCollapsed: boolean;
  onToggleCollapse: () => void;
  localCells: Record<string, Record<string, string[]>>;
  sortedStatuses: IssueStatus[];
  issueMap: Map<string, Issue>;
  childProgressMap: Map<string, ChildProgress>;
  gridStyle: React.CSSProperties;
  paths: ReturnType<typeof useWorkspacePaths>;
  projectId?: string;
}) {
  const { t } = useT("issues");
  const isNoParent = parent.parentIssueId === null;
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    // Always provide a valid id (rules of hooks) — the "No parent" lane is
    // disabled, so its id is never used as a sortable target.
    id: isNoParent ? "lane:__no_parent__" : laneId(parent.parentIssueId!),
    disabled: isNoParent,
  });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  const laneTotal = sortedStatuses.reduce(
    (sum, s) => sum + (localCells[parent.key]?.[s]?.length ?? 0),
    0,
  );

  return (
    <div ref={setNodeRef} style={style} className={`flex flex-col ${isDragging ? "opacity-50" : ""}`}>
      {/* Non-interactive container — the inner collapse button and the
          open-parent link are independent controls so we don't nest an
          <a> inside a <button>. The drag listeners attach here so the
          whole header row is the drag surface. */}
      <div
        className="mb-2 flex w-full items-center gap-2 rounded-md px-1 py-1"
        {...attributes}
        {...listeners}
      >
        {!isNoParent && (
          <GripVertical
            className="!size-3 shrink-0 cursor-grab text-muted-foreground/60"
            aria-hidden
          />
        )}
        <button
          type="button"
          onClick={onToggleCollapse}
          aria-label={t(($) => $.swimlane.toggle_collapse)}
          className="flex min-w-0 flex-1 items-center gap-2 rounded-md text-left transition-colors hover:bg-accent/70"
        >
          <ChevronRight
            className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${isCollapsed ? "" : "rotate-90"}`}
          />
          {parent.issue && (
            <StatusIcon status={parent.issue.status} className="size-3.5" />
          )}
          <span className="truncate text-sm font-semibold">{parent.title}</span>
          {parent.identifier && (
            <span className="shrink-0 rounded-full bg-muted px-1.5 py-0.5 text-[11px] font-medium tabular-nums text-muted-foreground">
              {parent.identifier}
            </span>
          )}
          <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
            {laneTotal}
          </span>
        </button>
        {parent.parentIssueId && (
          <Tooltip>
            <TooltipTrigger
              render={
                <AppLink
                  href={paths.issueDetail(parent.parentIssueId)}
                  aria-label={t(($) => $.swimlane.open_parent)}
                  className="inline-flex size-5 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
                >
                  <Pencil className="size-3" />
                </AppLink>
              }
            />
            <TooltipContent>{t(($) => $.swimlane.open_parent)}</TooltipContent>
          </Tooltip>
        )}
      </div>
      {/* Cells row — each cell mirrors a BoardColumn body */}
      {!isCollapsed && (
        <div style={gridStyle}>
          {sortedStatuses.map((status) => {
            const cId = cellId(parent.key, status);
            const issueIds = localCells[parent.key]?.[status] ?? [];
            return (
              <SwimLaneCell
                key={cId}
                cellId={cId}
                issueIds={issueIds}
                issueMap={issueMap}
                childProgressMap={childProgressMap}
                status={status}
                parentGroup={parent}
                projectId={projectId}
                readOnly={parent.key === ORPHAN_LANE_KEY}
              />
            );
          })}
        </div>
      )}
    </div>
  );
}

function SwimLaneCell({
  cellId: cId,
  issueIds,
  issueMap,
  childProgressMap,
  status,
  parentGroup,
  projectId,
  readOnly = false,
}: {
  cellId: string;
  issueIds: string[];
  issueMap: Map<string, Issue>;
  childProgressMap: Map<string, ChildProgress>;
  status: IssueStatus;
  parentGroup: ParentGroup;
  projectId?: string;
  /**
   * Display-only cell — the create affordance is suppressed and drag-end
   * upstream refuses to honour drops that would re-anchor a card to this
   * lane. Used by the "Other parents" fallback lane whose contents
   * belong to parents we don't have loaded.
   */
  readOnly?: boolean;
}) {
  // The orphan cell stays enabled in the collision graph so that drops
  // onto its whitespace area are absorbed here instead of falling through
  // to the nearest real cell. The ORPHAN_LANE_KEY guards in
  // handleDragOver / handleDragEnd reject the actual move.
  const { setNodeRef, isOver: droppableIsOver } = useDroppable({ id: cId });
  // Never show the hover highlight on a readOnly cell — the guards will
  // reject the drop, so visual confirmation would be misleading.
  const isOver = readOnly ? false : droppableIsOver;
  const { t } = useT("issues");
  const cfg = STATUS_CONFIG[status];

  const resolvedIssues = useMemo(
    () =>
      issueIds.flatMap((id) => {
        const issue = issueMap.get(id);
        return issue ? [issue] : [];
      }),
    [issueIds, issueMap],
  );

  const handleAdd = useCallback(() => {
    const data: Record<string, unknown> = { status };
    if (parentGroup.parentIssueId) data.parent_issue_id = parentGroup.parentIssueId;
    if (projectId) data.project_id = projectId;
    useModalStore.getState().open("create-issue", data);
  }, [status, parentGroup, projectId]);

  return (
    <div className={`flex min-h-[120px] flex-col rounded-xl ${cfg?.columnBg ?? "bg-muted/40"} p-2`}>
      <div
        ref={setNodeRef}
        className={`flex-1 space-y-2 rounded-lg p-1 transition-colors ${
          isOver ? "bg-accent/60" : ""
        }`}
      >
        <SortableContext items={issueIds} strategy={verticalListSortingStrategy}>
          {resolvedIssues.map((issue) => (
            <DraggableBoardCard
              key={issue.id}
              issue={issue}
              childProgress={childProgressMap.get(issue.id)}
            />
          ))}
        </SortableContext>
        {issueIds.length === 0 && (
          <p className="py-6 text-center text-xs text-muted-foreground">
            &mdash;
          </p>
        )}
      </div>
      {!readOnly && (
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={t(($) => $.board.add_issue_tooltip)}
                className="mt-1 w-full rounded-md text-muted-foreground hover:text-foreground"
                onClick={handleAdd}
              >
                <Plus className="size-3.5" />
              </Button>
            }
          />
          <TooltipContent>{t(($) => $.board.add_issue_tooltip)}</TooltipContent>
        </Tooltip>
      )}
    </div>
  );
}

function SwimLaneHiddenColumnsPanel({
  hiddenStatuses,
  statusTotals,
}: {
  hiddenStatuses: IssueStatus[];
  statusTotals: Map<IssueStatus, number>;
}) {
  return (
    <HiddenColumnsPanel
      hiddenStatuses={hiddenStatuses}
      renderRow={(status) => (
        <HiddenColumnRow
          key={status}
          status={status}
          total={statusTotals.get(status) ?? 0}
        />
      )}
    />
  );
}

function SwimLaneLoadMoreRow({
  sortedStatuses,
  gridStyle,
  myIssuesOpts,
  sort,
}: {
  sortedStatuses: IssueStatus[];
  gridStyle: React.CSSProperties;
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
  sort?: IssueSortParam;
}) {
  return (
    <div style={gridStyle}>
      {sortedStatuses.map((status) => (
        <SwimLaneLoadMoreCell
          key={status}
          status={status}
          myIssuesOpts={myIssuesOpts}
          sort={sort}
        />
      ))}
    </div>
  );
}

function SwimLaneLoadMoreCell({
  status,
  myIssuesOpts,
  sort,
}: {
  status: IssueStatus;
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
  sort?: IssueSortParam;
}) {
  const { loadMore, hasMore, isLoading } = useLoadMoreByStatus(status, myIssuesOpts, sort);
  if (!hasMore) return <div />;
  return <InfiniteScrollSentinel onVisible={loadMore} loading={isLoading} />;
}
