"use client";

import { useMemo, useState, type ReactNode } from "react";
import { BookOpen, EyeOff, MoreHorizontal, Plus } from "lucide-react";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { useDroppable } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import type { Issue, IssueStatus, WorkspaceColumnConfig } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { Popover, PopoverTrigger, PopoverContent } from "@multica/ui/components/ui/popover";
import { getStatusConfig } from "@multica/core/issues/config";
import { useModalStore } from "@multica/core/modals";
import { useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import { Markdown } from "../../common/markdown";
import { StatusIcon } from "./status-icon";
import { DraggableBoardCard } from "./board-card";
import type { ChildProgress } from "./list-row";

export function BoardColumn({
  status,
  isTerminal,
  label: labelProp,
  pipelineId,
  issueIds,
  issueMap,
  columnConfig,
  childProgressMap,
  totalCount,
  footer,
}: {
  status: string;
  isTerminal?: boolean;
  label?: string;
  pipelineId?: string | null;
  issueIds: string[];
  issueMap: Map<string, Issue>;
  columnConfig?: WorkspaceColumnConfig;
  childProgressMap?: Map<string, ChildProgress>;
  totalCount?: number;
  footer?: ReactNode;
}) {
  const cfg = getStatusConfig(status);
  const label = labelProp ?? cfg.label;
  const { setNodeRef, isOver } = useDroppable({ id: status });
  const viewStoreApi = useViewStoreApi();
  const [instructionsOpen, setInstructionsOpen] = useState(false);
  const hasInstructions = Boolean(columnConfig?.instructions.trim());

  // Resolve IDs to Issue objects, preserving parent-provided order
  const resolvedIssues = useMemo(
    () =>
      issueIds.flatMap((id) => {
        const issue = issueMap.get(id);
        return issue ? [issue] : [];
      }),
    [issueIds, issueMap],
  );

  return (
    <>
      <div className={`group/column flex w-[280px] shrink-0 flex-col rounded-xl ${cfg.columnBg} p-2`}>
        <div className="mb-2 px-1.5">
          <div className="group/header flex items-start justify-between gap-2">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className={`inline-flex items-center gap-1.5 rounded px-2 py-0.5 text-xs font-semibold ${cfg.badgeBg} ${cfg.badgeText}`}>
                  <StatusIcon status={status} isTerminal={isTerminal} className="h-3 w-3" inheritColor />
                  {label}
                </span>
                <span className="text-xs text-muted-foreground">
                  {totalCount ?? issueIds.length}
                </span>
              </div>

              {(columnConfig?.allowed_transitions.length ?? 0) > 0 && (
                <div className="mt-2 flex flex-wrap gap-1">
                  {columnConfig?.allowed_transitions.map((transition) => (
                    <Badge key={transition} variant="outline" className="px-1.5 py-0 text-[10px] font-medium">
                      {getStatusConfig(transition).label}
                    </Badge>
                  ))}
                </div>
              )}
            </div>

            <div className="flex shrink-0 items-center gap-1">
              {hasInstructions && (
                <Popover open={instructionsOpen} onOpenChange={setInstructionsOpen}>
                  <Tooltip>
                    <PopoverTrigger
                      render={
                        <TooltipTrigger
                          render={
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              className="rounded-full text-muted-foreground"
                            >
                              <BookOpen className="size-3.5" />
                            </Button>
                          }
                        />
                      }
                    />
                    <TooltipContent>View column instructions</TooltipContent>
                  </Tooltip>
                  <PopoverContent align="end" className="w-80 max-w-[calc(100vw-3rem)] p-0">
                    <div className="border-b px-3 py-2.5">
                      <div className="text-sm font-semibold">{label} instructions</div>
                      <div className="text-xs text-muted-foreground">
                        Shared guidance for this board column.
                      </div>
                    </div>
                    <div className="max-h-80 overflow-y-auto p-3">
                      <Markdown mode="full">{columnConfig?.instructions ?? ""}</Markdown>
                    </div>
                  </PopoverContent>
                </Popover>
              )}

              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button variant="ghost" size="icon-sm" className="rounded-full text-muted-foreground">
                      <MoreHorizontal className="size-3.5" />
                    </Button>
                  }
                />
                <DropdownMenuContent align="end">
                  <DropdownMenuItem onClick={() => viewStoreApi.getState().hideStatus(status as IssueStatus)}>
                    <EyeOff className="size-3.5" />
                    Hide column
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>

              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      className="rounded-full text-muted-foreground"
                      onClick={() =>
                        useModalStore.getState().open("create-issue", {
                          status,
                          pipeline_id: pipelineId,
                        })}
                    >
                      <Plus className="size-3.5" />
                    </Button>
                  }
                />
                <TooltipContent>Add issue</TooltipContent>
              </Tooltip>
            </div>
          </div>
        </div>

        <div
          ref={setNodeRef}
          className={`min-h-[200px] flex-1 space-y-2 overflow-y-auto rounded-lg p-1 transition-colors ${
            isOver ? "bg-accent/60" : ""
          }`}
        >
          <SortableContext items={issueIds} strategy={verticalListSortingStrategy}>
            {resolvedIssues.map((issue) => (
              <DraggableBoardCard key={issue.id} issue={issue} childProgress={childProgressMap?.get(issue.id)} />
            ))}
          </SortableContext>
          {issueIds.length === 0 && (
            <p className="py-8 text-center text-xs text-muted-foreground">
              No issues
            </p>
          )}
          {footer}
        </div>
      </div>
    </>
  );
}
