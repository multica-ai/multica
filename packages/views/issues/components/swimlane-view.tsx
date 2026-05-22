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
import { SortableContext, verticalListSortingStrategy, arrayMove } from "@dnd-kit/sortable";
import { Plus } from "lucide-react";
import type { Issue, IssueStatus } from "@multica/core/types";
import type { UpdateIssueRequest } from "@multica/core/types";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { sortIssues } from "../utils/sort";
import { BOARD_STATUSES, STATUS_CONFIG } from "@multica/core/issues/config";
import { useModalStore } from "@multica/core/modals";
import { DraggableBoardCard, BoardCardContent } from "./board-card";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Button } from "@multica/ui/components/ui/button";
import type { ChildProgress } from "./list-row";
import { useT } from "../../i18n";

type SwimLaneMoveUpdates = Pick<
  UpdateIssueRequest,
  "parent_issue_id" | "status" | "position"
>;

function makeSwimLaneCollision(cellIds: Set<string>): CollisionDetection {
  return (args) => {
    const pointer = pointerWithin(args);
    if (pointer.length > 0) {
      const cards = pointer.filter((c) => !cellIds.has(c.id as string));
      if (cards.length > 0) return cards;
    }
    return closestCenter(args);
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
}

const EMPTY_PROGRESS_MAP = new Map<string, ChildProgress>();

export function SwimLaneView({
  issues,
  visibleStatuses = BOARD_STATUSES,
  onMoveIssue,
  childProgressMap = EMPTY_PROGRESS_MAP,
}: {
  issues: Issue[];
  visibleStatuses?: IssueStatus[];
  onMoveIssue: (issueId: string, updates: SwimLaneMoveUpdates) => void;
  childProgressMap?: Map<string, ChildProgress>;
}) {
  const { t } = useT("issues");
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);

  const sortedStatuses = useMemo(
    () => BOARD_STATUSES.filter((s) => visibleStatuses.includes(s)),
    [visibleStatuses],
  );

  const parentGroups = useMemo<ParentGroup[]>(() => {
    const seen = new Map<string, ParentGroup>();
    const issueMap = new Map<string, Issue>();
    for (const issue of issues) {
      issueMap.set(issue.id, issue);
    }

    for (const issue of issues) {
      if (issue.parent_issue_id) {
        const key = `parent:${issue.parent_issue_id}`;
        if (!seen.has(key)) {
          const parent = issueMap.get(issue.parent_issue_id);
          seen.set(key, {
            key,
            parentIssueId: issue.parent_issue_id,
            identifier: parent?.identifier ?? issue.parent_issue_id.slice(0, 8),
            title: parent?.title ?? "(deleted)",
          });
        }
      }
    }

    const noParentGroup: ParentGroup = {
      key: "parent:none",
      parentIssueId: null,
      identifier: "",
      title: t(($) => $.swimlane.no_parent),
    };

    return [noParentGroup, ...seen.values()];
  }, [issues, t]);

  const cells = useMemo(() => {
    const result: Record<string, Record<string, string[]>> = {};
    for (const parent of parentGroups) {
      const cellMap: Record<string, string[]> = {};
      for (const status of sortedStatuses) {
        cellMap[status] = [];
      }
      const parentIssues = sortIssues(
        issues.filter((issue) =>
          parent.parentIssueId === null
            ? issue.parent_issue_id === null
            : issue.parent_issue_id === parent.parentIssueId,
        ),
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
  }, [issues, parentGroups, sortedStatuses, sortBy, sortDirection]);

  const cellSet = useMemo(() => {
    const ids = new Set<string>();
    for (const parent of parentGroups) {
      for (const status of sortedStatuses) {
        ids.add(cellId(parent.key, status));
      }
    }
    return ids;
  }, [parentGroups, sortedStatuses]);

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
    const issue = issueMapRef.current.get(event.active.id as string) ?? null;
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
      const cols = localCellsRef.current;

      const activeCell = findCellIn(cols, cellSet, activeId);
      const overCell = findCellIn(cols, cellSet, overId);
      if (!activeCell || !overCell) {
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
    [cells, cellSet, onMoveIssue],
  );

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={collisionDetection}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
    >
      <div className="flex flex-1 min-h-0 overflow-x-auto">
        <div className="flex flex-col min-w-0" style={{ width: sortedStatuses.length * 280 }}>
          <div className="flex h-10 border-b border-border bg-background sticky top-0 z-10">
            {sortedStatuses.map((status) => {
              const cfg = STATUS_CONFIG[status];
              return (
                <div
                  key={status}
                  className="flex w-[280px] shrink-0 items-center gap-2 px-3"
                >
                  <span className={`h-2 w-2 rounded-full ${cfg?.iconColor ? cfg.iconColor.replace("text-", "bg-") : "bg-muted"}`} />
                  <span className="text-xs font-medium text-muted-foreground">
                    {t(($) => $.status[status])}
                  </span>
                </div>
              );
            })}
          </div>

          <div className="flex-1">
            {parentGroups.map((parent) => (
              <div key={parent.key}>
                <div className="flex items-center gap-2 px-3 py-1.5 border-b border-border/30 bg-muted/20">
                  <span className="text-sm font-semibold text-accent truncate">
                    {parent.title}
                  </span>
                  {parent.identifier && (
                    <span className="text-xs text-muted-foreground tabular-nums truncate">
                      {parent.identifier}
                    </span>
                  )}
                </div>
                <div className="flex border-b border-border/50">
                  {sortedStatuses.map((status) => {
                    const cId = cellId(parent.key, status);
                    const issueIds = localCells[parent.key]?.[status] ?? [];
                    return (
                      <SwimLaneCell
                        key={cId}
                        cellId={cId}
                        issueIds={issueIds}
                        issueMap={issueMapRef.current}
                        childProgressMap={childProgressMap}
                        status={status}
                        parentGroup={parent}
                      />
                    );
                  })}
                </div>
              </div>
            ))}
          </div>
        </div>
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

function SwimLaneCell({
  cellId: cId,
  issueIds,
  issueMap,
  childProgressMap,
  status,
  parentGroup,
}: {
  cellId: string;
  issueIds: string[];
  issueMap: Map<string, Issue>;
  childProgressMap: Map<string, ChildProgress>;
  status: IssueStatus;
  parentGroup: ParentGroup;
}) {
  const { setNodeRef, isOver } = useDroppable({ id: cId });
  const { t } = useT("issues");

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
    if (parentGroup.parentIssueId) {
      data.parent_issue_id = parentGroup.parentIssueId;
    }
    useModalStore.getState().open("create-issue", data);
  }, [status, parentGroup]);

  return (
    <div
      ref={setNodeRef}
      className={`flex w-[280px] shrink-0 flex-col rounded-lg p-1 transition-colors ${
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
        <p className="py-8 text-center text-xs text-muted-foreground">
          &mdash;
        </p>
      )}
      <div className="mt-auto pt-1">
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                className="w-full rounded-md text-muted-foreground hover:text-foreground"
                onClick={handleAdd}
              >
                <Plus className="size-3.5" />
              </Button>
            }
          />
          <TooltipContent>{t(($) => $.board.add_issue_tooltip)}</TooltipContent>
        </Tooltip>
      </div>
    </div>
  );
}
