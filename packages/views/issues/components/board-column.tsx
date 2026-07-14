"use client";

import { memo, useCallback, useMemo, useState, type ReactNode } from "react";
import { Virtuoso } from "react-virtuoso";
import { EyeOff, MoreHorizontal, Plus, UserMinus } from "lucide-react";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { useDroppable } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import type {
  Issue,
  IssueAssigneeType,
  IssueStatus,
  Project,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import { useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import { StatusHeading } from "./status-heading";
import { DraggableBoardCard } from "./board-card";
import type { ChildProgress } from "./list-row";
import { useT } from "../../i18n";
import { ActorAvatar } from "../../common/actor-avatar";
import type { IssueCreateDefaults } from "../surface/types";

// Insertion-position prediction intentionally omitted. The server's
// ORDER BY uses PostgreSQL's en_US.utf8 collation (glibc), which
// cannot be faithfully replicated in JavaScript (ICU/V8). Showing an
// inaccurate indicator is worse than showing none.

export const BOARD_COL_WIDTH = 280;
export const BOARD_CARD_WIDTH = BOARD_COL_WIDTH - 16 - 8; // col(280) - col p-2(16) - droppable p-1(8)

// Passed to <Virtuoso components> when the column has no footer. Must be a
// STABLE object, never `undefined`: an explicit `undefined` prop overwrites
// react-virtuoso's internal `{}` default and its startup destructure of
// `EmptyPlaceholder`/`Footer` throws (MUL-4474).
const EMPTY_VIRTUOSO_COMPONENTS = {};

export interface BoardColumnGroup {
  id: string;
  title: string;
  status?: IssueStatus;
  assigneeType?: IssueAssigneeType | null;
  assigneeId?: string | null;
  totalCount?: number;
  createData?: IssueCreateDefaults;
}

export const BoardColumn = memo(function BoardColumn({
  group,
  issueIds,
  issueMap,
  childProgressMap,
  projectMap,
  totalCount,
  footer,
  projectId,
  onCreateIssue,
  sortLabel,
}: {
  group: BoardColumnGroup;
  issueIds: string[];
  issueMap: Map<string, Issue>;
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  totalCount?: number;
  footer?: ReactNode;
  /** When set, the per-column "+" pre-fills the project on the create form. */
  projectId?: string;
  onCreateIssue?: (defaults: IssueCreateDefaults) => void;
  sortLabel?: string | null;
}) {
  const status = group.status;
  const cfg = status ? STATUS_CONFIG[status] : null;
  const { setNodeRef, isOver } = useDroppable({ id: group.id });
  const viewStoreApi = useViewStoreApi();
  const { t } = useT("issues");

  // Resolve IDs to Issue objects, preserving parent-provided order
  const resolvedIssues = useMemo(
    () =>
      issueIds.flatMap((id) => {
        const issue = issueMap.get(id);
        return issue ? [issue] : [];
      }),
    [issueIds, issueMap],
  );

  // The column's scroll container is both dnd-kit's droppable and Virtuoso's
  // customScrollParent, so a merged callback ref feeds the element to both.
  // useDroppable's setNodeRef is stable across renders. Keeping the droppable
  // on the always-mounted scroll container (not on individual cards) is what
  // lets cross-column drops survive virtualization — only the cards inside
  // window in/out of the DOM.
  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);
  const mergedRef = useCallback(
    (el: HTMLDivElement | null) => {
      setNodeRef(el);
      setScrollEl(el);
    },
    [setNodeRef],
  );
  // Infinite-scroll sentinel rides Virtuoso's Footer slot so it sits at the
  // real end of the virtualized list and its IntersectionObserver still fires
  // loadMore when scrolled to the bottom.
  const footerComponents = useMemo(
    () => (footer ? { Footer: () => <>{footer}</> } : EMPTY_VIRTUOSO_COMPONENTS),
    [footer],
  );

  return (
    <div style={{ width: BOARD_COL_WIDTH }} className={`flex shrink-0 flex-col rounded-xl ${cfg?.columnBg ?? "bg-muted/40"} p-2`}>
      <div className="mb-2 flex items-center justify-between px-1.5">
        <BoardGroupHeading group={group} count={totalCount ?? issueIds.length} />

        {/* Right: add + menu */}
        <div className="flex items-center gap-1">
          {status && (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button variant="ghost" size="icon-sm" className="rounded-full text-muted-foreground">
                    <MoreHorizontal className="size-3.5" />
                  </Button>
                }
              />
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => viewStoreApi.getState().hideStatus(status)}>
                  <EyeOff className="size-3.5" />
                  {t(($) => $.board.hide_column)}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
          {onCreateIssue && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="rounded-full text-muted-foreground"
                    onClick={() => {
                      const data = {
                        ...(group.createData ?? {}),
                        ...(projectId ? { project_id: projectId } : {}),
                      };
                      onCreateIssue(data);
                    }}
                  >
                    <Plus className="size-3.5" />
                  </Button>
                }
              />
              <TooltipContent>{t(($) => $.board.add_issue_tooltip)}</TooltipContent>
            </Tooltip>
          )}
        </div>
      </div>
      <div className="relative min-h-[200px] flex-1 rounded-lg">
        {isOver && sortLabel && (
          <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-lg bg-background/40">
            <span className="rounded-md bg-popover px-2.5 py-1 text-xs font-medium text-popover-foreground shadow-sm border border-border">
              {sortLabel}
            </span>
          </div>
        )}
        <div
          ref={mergedRef}
          className={`absolute inset-0 overflow-y-auto rounded-lg p-1 transition-colors ${
            isOver && sortLabel
              ? "ring-2 ring-brand/25 bg-accent/15"
              : isOver
                ? "bg-accent/60"
                : ""
          }`}
        >
          {resolvedIssues.length > 0 ? (
            <SortableContext items={issueIds} strategy={verticalListSortingStrategy}>
              {scrollEl && (
                <Virtuoso
                  customScrollParent={scrollEl}
                  data={resolvedIssues}
                  computeItemKey={(_index, issue) => issue.id}
                  increaseViewportBy={{ top: 300, bottom: 300 }}
                  components={footerComponents}
                  itemContent={(index, issue) => (
                    // pt-2 on every card but the first reproduces the previous
                    // `space-y-2` gap; padding (not margin) is inside Virtuoso's
                    // measured item box so its height math stays correct.
                    <div className={index === 0 ? undefined : "pt-2"}>
                      <DraggableBoardCard
                        issue={issue}
                        childProgress={childProgressMap?.get(issue.id)}
                        project={
                          issue.project_id ? projectMap?.get(issue.project_id) : undefined
                        }
                        disableSorting={!!sortLabel}
                      />
                    </div>
                  )}
                />
              )}
            </SortableContext>
          ) : (
            <>
              {issueIds.length === 0 && (
                <p className="py-8 text-center text-xs text-muted-foreground">
                  {t(($) => $.board.empty_column)}
                </p>
              )}
              {footer}
            </>
          )}
        </div>
      </div>
    </div>
  );
});

function BoardGroupHeading({
  group,
  count,
}: {
  group: BoardColumnGroup;
  count: number;
}) {
  if (group.status) {
    return <StatusHeading status={group.status} count={count} />;
  }

  const actorIcon =
    group.assigneeType && group.assigneeId ? (
      <ActorAvatar
        actorType={group.assigneeType}
        actorId={group.assigneeId}
        size="sm"
        showStatusDot={group.assigneeType === "agent"}
      />
    ) : (
      <span className="flex size-[18px] shrink-0 items-center justify-center rounded-full bg-background text-muted-foreground">
        <UserMinus className="size-3.5" />
      </span>
    );

  return (
    <div className="flex min-w-0 items-center gap-2">
      {actorIcon}
      <span className="truncate text-sm font-medium" title={group.title}>
        {group.title}
      </span>
      <span className="shrink-0 rounded-full bg-background px-1.5 py-0.5 text-[11px] font-medium tabular-nums text-muted-foreground">
        {count}
      </span>
    </div>
  );
}
