"use client";

import { useState, useCallback, useMemo } from "react";
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
} from "@dnd-kit/core";
import { Eye, MoreHorizontal } from "lucide-react";
import type { Issue, IssueStatus } from "@/shared/types";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu";
import { ALL_STATUSES, STATUS_CONFIG } from "@/features/issues/config";
import { useViewStoreApi } from "@/features/issues/stores/view-store-context";
import { StatusIcon } from "./status-icon";
import { BoardColumn } from "./board-column";
import { BoardCardContent } from "./board-card";

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

/** Compute a float position to place an item at `targetIndex` within `siblings`. */
function computePosition(siblings: Issue[], targetIndex: number): number {
  if (siblings.length === 0) return 0;
  if (targetIndex <= 0) return siblings[0]!.position - 1;
  if (targetIndex >= siblings.length)
    return siblings[siblings.length - 1]!.position + 1;
  return (siblings[targetIndex - 1]!.position + siblings[targetIndex]!.position) / 2;
}

export function BoardView({
  issues,
  allIssues,
  visibleStatuses,
  hiddenStatuses,
  onMoveIssue,
}: {
  issues: Issue[];
  allIssues: Issue[];
  visibleStatuses: IssueStatus[];
  hiddenStatuses: IssueStatus[];
  onMoveIssue: (
    issueId: string,
    newStatus: IssueStatus,
    newPosition?: number
  ) => void;
}) {
  const [activeIssue, setActiveIssue] = useState<Issue | null>(null);

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 5 },
    })
  );

  // Pre-sort issues by position per status for position calculations
  const issuesByStatus = useMemo(() => {
    const map: Record<string, Issue[]> = {};
    for (const status of visibleStatuses) {
      map[status] = issues
        .filter((i) => i.status === status)
        .sort((a, b) => a.position - b.position);
    }
    return map;
  }, [issues, visibleStatuses]);

  const handleDragStart = useCallback(
    (event: DragStartEvent) => {
      const issue = issues.find((i) => i.id === event.active.id);
      if (issue) setActiveIssue(issue);
    },
    [issues]
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      setActiveIssue(null);
      const { active, over } = event;
      if (!over || active.id === over.id) return;

      const issueId = active.id as string;
      const currentIssue = issues.find((i) => i.id === issueId);
      if (!currentIssue) return;

      // Determine target status
      let targetStatus: IssueStatus;
      let overIsColumn = false;

      if (visibleStatuses.includes(over.id as IssueStatus)) {
        targetStatus = over.id as IssueStatus;
        overIsColumn = true;
      } else {
        const targetIssue = issues.find((i) => i.id === over.id);
        if (!targetIssue) return;
        targetStatus = targetIssue.status;
      }

      // Get sorted siblings in the target column (excluding the dragged item)
      const siblings = (issuesByStatus[targetStatus] ?? []).filter(
        (i) => i.id !== issueId
      );

      // Compute new position
      let newPosition: number;

      if (overIsColumn) {
        // Dropped on empty area of column → append to end
        newPosition = computePosition(siblings, siblings.length);
      } else {
        // Dropped on a specific card → insert at that card's index
        const overIndex = siblings.findIndex((i) => i.id === over.id);
        if (overIndex === -1) {
          newPosition = computePosition(siblings, siblings.length);
        } else {
          const isSameColumn = currentIssue.status === targetStatus;
          const overIssuePosition = siblings[overIndex]!.position;

          if (isSameColumn && currentIssue.position < overIssuePosition) {
            // Moving down → insert after the over card
            newPosition = computePosition(siblings, overIndex + 1);
          } else {
            // Moving up or cross-column → insert before the over card
            newPosition = computePosition(siblings, overIndex);
          }
        }
      }

      // Skip if nothing changed
      if (
        currentIssue.status === targetStatus &&
        currentIssue.position === newPosition
      ) {
        return;
      }

      onMoveIssue(issueId, targetStatus, newPosition);
    },
    [issues, issuesByStatus, onMoveIssue, visibleStatuses]
  );

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={kanbanCollision}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
    >
      <div className="flex flex-1 min-h-0 gap-4 overflow-x-auto p-4">
        {visibleStatuses.map((status) => (
          <BoardColumn
            key={status}
            status={status}
            issues={issues.filter((i) => i.status === status)}
          />
        ))}

        {hiddenStatuses.length > 0 && (
          <HiddenColumnsPanel
            hiddenStatuses={hiddenStatuses}
            issues={allIssues}
          />
        )}
      </div>

      <DragOverlay>
        {activeIssue ? (
          <div className="w-[280px] rotate-1 cursor-grabbing opacity-95 shadow-md">
            <BoardCardContent issue={activeIssue} />
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}

function HiddenColumnsPanel({
  hiddenStatuses,
  issues,
}: {
  hiddenStatuses: IssueStatus[];
  issues: Issue[];
}) {
  const viewStoreApi = useViewStoreApi();
  return (
    <div className="flex w-[240px] shrink-0 flex-col">
      <div className="mb-2 flex items-center gap-2 px-1">
        <span className="text-sm font-medium text-muted-foreground">
          Hidden columns
        </span>
      </div>
      <div className="flex-1 space-y-0.5">
        {hiddenStatuses.map((status) => {
          const cfg = STATUS_CONFIG[status];
          const count = issues.filter((i) => i.status === status).length;
          return (
            <div
              key={status}
              className="flex items-center justify-between rounded-lg px-2.5 py-2 hover:bg-muted/50"
            >
              <div className="flex items-center gap-2">
                <StatusIcon status={status} className="h-3.5 w-3.5" />
                <span className="text-sm">{cfg.label}</span>
              </div>
              <div className="flex items-center gap-1.5">
                <span className="text-xs text-muted-foreground">{count}</span>
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
                      onClick={() =>
                        viewStoreApi.getState().showStatus(status)
                      }
                    >
                      <Eye className="size-3.5" />
                      Show column
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
