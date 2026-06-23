"use client";

import { memo, useCallback, useState, useEffect, useRef, type Ref } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSortable, defaultAnimateLayoutChanges } from "@dnd-kit/sortable";
import type { AnimateLayoutChanges } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { ChevronRight, CornerDownRight } from "lucide-react";
import { AppLink } from "../../navigation";
import type { Issue } from "@multica/core/types";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import { formatDateOnly } from "@multica/core/issues/date";
import { ActorAvatar } from "../../common/actor-avatar";
import { useIssueSelectionStore } from "@multica/core/issues/stores/selection-store";
import { useWorkspacePaths } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { projectListOptions } from "@multica/core/projects/queries";
import { ProjectIcon } from "../../projects/components/project-icon";
import { PriorityIcon } from "./priority-icon";
import { ProgressRing } from "./progress-ring";
import { IssueActionsContextMenu } from "../actions";
import { LabelChip } from "../../labels/label-chip";
import { IssueAgentActivityIndicator } from "./issue-agent-activity-indicator";
import type { ParentInfo, CrossStatusChild } from "../utils/hierarchy";

export interface ChildProgress {
  done: number;
  total: number;
}

function formatDate(date: string): string {
  return formatDateOnly(date, { month: "short", day: "numeric" }, "en-US");
}

const STATUS_CN: Record<string, string> = {
  todo: "待办",
  in_progress: "进行中",
  in_review: "审核中",
  done: "已完成",
  backlog: "待规划",
  cancelled: "已取消",
  blocked: "阻塞中",
};

function getStatusColor(status: string): string {
  return STATUS_CONFIG[status as keyof typeof STATUS_CONFIG]?.iconColor ?? "text-muted-foreground";
}

function formatStatusLabel(status: string): string {
  return STATUS_CN[status] ?? status.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
}

function ListRowContent({
  issue,
  childProgress,
  isDragging,
  containerRef,
  containerStyle,
  containerProps,
  checkboxProps,
  indent = 0,
  isParent = false,
  isExpanded = false,
  childCount = 0,
  crossStatusChildCount = 0,
  crossStatusChildren = [],
  onToggleChildren,
  parentInfo,
  onHoverParent,
  onScrollToParent,
}: {
  issue: Issue;
  childProgress?: ChildProgress;
  isDragging?: boolean;
  containerRef?: Ref<HTMLDivElement>;
  containerStyle?: React.CSSProperties;
  containerProps?: Record<string, unknown>;
  checkboxProps?: Pick<React.HTMLAttributes<HTMLDivElement>, "onClick" | "onMouseDown" | "onPointerDown">;
  indent?: number;
  isParent?: boolean;
  isExpanded?: boolean;
  childCount?: number;
  crossStatusChildCount?: number;
  crossStatusChildren?: CrossStatusChild[];
  onToggleChildren?: () => void;
  parentInfo?: ParentInfo;
  onHoverParent?: (parentStatus: string | null) => void;
  onScrollToParent?: (parentId: string, parentStatus: string) => void;
}) {
  const selected = useIssueSelectionStore((s) => s.selectedIds.has(issue.id));
  const toggle = useIssueSelectionStore((s) => s.toggle);
  const p = useWorkspacePaths();
  const storeProperties = useViewStore((s) => s.cardProperties);
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery({
    ...projectListOptions(wsId),
    enabled: storeProperties.project && !!issue.project_id,
  });
  const project = issue.project_id ? projects.find((pr) => pr.id === issue.project_id) : undefined;
  const labels = issue.labels ?? [];

  // Dropdown state for cross-status badge.
  const [crossDropdownOpen, setCrossDropdownOpen] = useState(false);
  const crossDropdownRef = useRef<HTMLDivElement>(null);

  // Close dropdown on outside click.
  useEffect(() => {
    if (!crossDropdownOpen) return;
    const handler = (e: MouseEvent) => {
      if (crossDropdownRef.current && !crossDropdownRef.current.contains(e.target as Node)) {
        setCrossDropdownOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [crossDropdownOpen]);

  const showProject = storeProperties.project && project;
  const showChildProgress = storeProperties.childProgress && childProgress;
  const showAssignee = storeProperties.assignee && issue.assignee_type && issue.assignee_id;
  const showStartDate = storeProperties.startDate && issue.start_date;
  const showDueDate = storeProperties.dueDate && issue.due_date;
  const showLabels = storeProperties.labels && labels.length > 0;

  const handleChipClick = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      e.preventDefault();
      if (!parentInfo) return;

      if (e.metaKey || e.ctrlKey) {
        window.open(p.issueDetail(parentInfo.parentId), "_blank");
      } else {
        onScrollToParent?.(parentInfo.parentId, parentInfo.status);
      }
    },
    [parentInfo, p, onScrollToParent],
  );

  const handleChipMouseEnter = useCallback(() => {
    if (parentInfo) onHoverParent?.(parentInfo.status);
  }, [parentInfo, onHoverParent]);

  const handleChipMouseLeave = useCallback(() => {
    onHoverParent?.(null);
  }, [onHoverParent]);

  return (
    <IssueActionsContextMenu issue={issue}>
      <div
        ref={containerRef}
        style={containerStyle}
        {...containerProps}
        data-issue-id={issue.id}
        className={`group/row flex h-9 items-center gap-2 pr-4 text-sm transition-colors hover:not-data-[popup-open]:bg-accent/60 data-[popup-open]:bg-accent ${
          selected ? "bg-accent/30" : ""
        } ${isDragging ? "opacity-30" : ""}`}
        role="row"
      >
        {/* Indent spacer for child rows */}
        {indent > 0 && (
          <div style={{ width: indent * 24 }} className="shrink-0" />
        )}
        {/* Expand / collapse toggle for parent rows (replaces priority icon slot) */}
        {isParent ? (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              e.preventDefault();
              onToggleChildren?.();
            }}
            className="flex shrink-0 items-center justify-center w-4 h-4 rounded-sm hover:bg-muted cursor-pointer transition-transform"
            style={{ transform: isExpanded ? "rotate(90deg)" : undefined }}
            aria-label={isExpanded ? "Collapse sub-issues" : "Expand sub-issues"}
          >
            <ChevronRight className="size-3.5 text-muted-foreground" />
          </button>
        ) : indent > 0 ? (
          /* Branch indicator for child rows */
          <div className="flex shrink-0 items-center justify-center w-4 h-4">
            <CornerDownRight className="size-3.5 text-muted-foreground/50" />
          </div>
        ) : null}
        {/* Priority icon + checkbox */}
        <div
          className="relative flex shrink-0 items-center justify-center w-4 h-4"
          {...checkboxProps}
        >
          <PriorityIcon
            priority={issue.priority}
            className={selected ? "hidden" : "group-hover/row:hidden"}
          />
          <input
            type="checkbox"
            checked={selected}
            onChange={() => toggle(issue.id)}
            className={`absolute inset-0 cursor-pointer accent-primary ${
              selected ? "" : "hidden group-hover/row:block"
            }`}
          />
        </div>
        <AppLink
          href={p.issueDetail(issue.id)}
          className={`flex flex-1 items-center gap-2 min-w-0 ${isDragging ? "pointer-events-none" : ""}`}
        >
          <span className="w-16 shrink-0 text-xs text-muted-foreground">
            {issue.identifier}
          </span>
          <IssueAgentActivityIndicator issueId={issue.id} />

          <span className="flex min-w-0 flex-1 items-center gap-1.5">
            <span className="truncate">{issue.title}</span>
            {isParent && childCount > 0 && (
              <span className="shrink-0 text-[11px] text-muted-foreground tabular-nums">
                ({childCount})
              </span>
            )}
            {isParent && crossStatusChildCount > 0 && (
              <div className="relative shrink-0" ref={crossDropdownRef}>
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    e.preventDefault();
                    setCrossDropdownOpen((v) => !v);
                  }}
                  className="text-[11px] text-muted-foreground/50 tabular-nums hover:text-muted-foreground cursor-pointer transition-colors"
                >
                  {/* eslint-disable-next-line i18next/no-literal-string */}
                  ↓ {crossStatusChildCount} 跨状态
                </button>
                {crossDropdownOpen && crossStatusChildren.length > 0 && (
                  <div className="absolute top-full left-0 z-50 mt-1 bg-popover border border-border rounded-md shadow-md p-1 min-w-[200px] max-h-[240px] overflow-y-auto">
                    {crossStatusChildren.map((child) => (
                      <button
                        key={child.id}
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation();
                          e.preventDefault();
                          setCrossDropdownOpen(false);
                          onScrollToParent?.(child.id, child.status);
                        }}
                        onMouseEnter={() => onHoverParent?.(child.status)}
                        onMouseLeave={() => onHoverParent?.(null)}
                        className="flex w-full items-center gap-1.5 px-2 py-1 text-left text-[11px] rounded-sm hover:bg-accent cursor-pointer transition-colors"
                      >
                        <span className="text-muted-foreground/70 truncate flex-1">
                          {child.identifier}
                        </span>
                        <span className={`shrink-0 ${getStatusColor(child.status)}`}>
                          [{formatStatusLabel(child.status)}]
                        </span>
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )}
            {parentInfo && (
              <button
                type="button"
                onClick={handleChipClick}
                onMouseEnter={handleChipMouseEnter}
                onMouseLeave={handleChipMouseLeave}
                className="shrink-0 inline-flex items-center gap-1 px-1.5 py-0.5 rounded-full bg-muted/60 hover:bg-muted cursor-pointer transition-colors max-w-[200px]"
                title={`← ${parentInfo.identifier} — Click to scroll, Cmd+Click to open`}
              >
                <span className="text-[11px] text-muted-foreground/70 truncate">
                  ← {parentInfo.identifier}
                </span>
                <span className={`text-[10px] truncate ${getStatusColor(parentInfo.status)}`}>
                  [{formatStatusLabel(parentInfo.status)}]
                </span>
              </button>
            )}
            {showChildProgress && (
              <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5">
                <ProgressRing done={childProgress!.done} total={childProgress!.total} size={14} />
                <span className="text-[11px] text-muted-foreground tabular-nums font-medium">
                  {childProgress!.done}/{childProgress!.total}
                </span>
              </span>
            )}
            {showLabels && (
              <span className="ml-1.5 hidden md:inline-flex shrink-0 items-center gap-1 max-w-[260px] overflow-hidden">
                {labels.slice(0, 3).map((label) => (
                  <LabelChip key={label.id} label={label} />
                ))}
                {labels.length > 3 && (
                  <span className="text-[11px] text-muted-foreground">
                    +{labels.length - 3}
                  </span>
                )}
              </span>
            )}
          </span>
          {showProject && (
            <span className="inline-flex shrink-0 items-center gap-1 text-xs text-muted-foreground max-w-[140px]">
              <ProjectIcon project={project} size="sm" />
              <span className="truncate">{project!.title}</span>
            </span>
          )}
          {showStartDate && (
            <span className="shrink-0 text-xs text-muted-foreground">
              {formatDate(issue.start_date!)}
            </span>
          )}
          {showDueDate && (
            <span className="shrink-0 text-xs text-muted-foreground">
              {formatDate(issue.due_date!)}
            </span>
          )}
          {showAssignee && (
            <ActorAvatar
              actorType={issue.assignee_type!}
              actorId={issue.assignee_id!}
              size={20}
              enableHoverCard
            />
          )}
        </AppLink>
      </div>
    </IssueActionsContextMenu>
  );
}

export const ListRow = memo(function ListRow({
  issue,
  childProgress,
  indent,
  isParent,
  isExpanded,
  childCount,
  crossStatusChildCount,
  crossStatusChildren,
  onToggleChildren,
  parentInfo,
  onHoverParent,
  onScrollToParent,
}: {
  issue: Issue;
  childProgress?: ChildProgress;
  indent?: number;
  isParent?: boolean;
  isExpanded?: boolean;
  childCount?: number;
  crossStatusChildCount?: number;
  crossStatusChildren?: CrossStatusChild[];
  onToggleChildren?: () => void;
  parentInfo?: ParentInfo;
  onHoverParent?: (parentStatus: string | null) => void;
  onScrollToParent?: (parentId: string, parentStatus: string) => void;
}) {
  return (
    <ListRowContent
      issue={issue}
      childProgress={childProgress}
      indent={indent}
      isParent={isParent}
      isExpanded={isExpanded}
      childCount={childCount}
      crossStatusChildCount={crossStatusChildCount}
      crossStatusChildren={crossStatusChildren}
      onToggleChildren={onToggleChildren}
      parentInfo={parentInfo}
      onHoverParent={onHoverParent}
      onScrollToParent={onScrollToParent}
    />
  );
});

const animateLayoutChanges: AnimateLayoutChanges = (args) => {
  const { isSorting, wasDragging } = args;
  if (isSorting || wasDragging) return false;
  return defaultAnimateLayoutChanges(args);
};

const stopDrag = (e: React.SyntheticEvent) => {
  e.stopPropagation();
};

export const DraggableListRow = memo(function DraggableListRow({
  issue,
  childProgress,
  disableSorting,
  indent,
  isParent,
  isExpanded,
  childCount,
  crossStatusChildCount,
  crossStatusChildren,
  onToggleChildren,
  parentInfo,
  onHoverParent,
  onScrollToParent,
}: {
  issue: Issue;
  childProgress?: ChildProgress;
  disableSorting?: boolean;
  indent?: number;
  isParent?: boolean;
  isExpanded?: boolean;
  childCount?: number;
  crossStatusChildCount?: number;
  crossStatusChildren?: CrossStatusChild[];
  onToggleChildren?: () => void;
  parentInfo?: ParentInfo;
  onHoverParent?: (parentStatus: string | null) => void;
  onScrollToParent?: (parentId: string, parentStatus: string) => void;
}) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({
    id: issue.id,
    data: { status: issue.status },
    animateLayoutChanges,
    disabled: disableSorting ? { droppable: true } : undefined,
  });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  return (
    <ListRowContent
      issue={issue}
      childProgress={childProgress}
      isDragging={isDragging}
      containerRef={setNodeRef}
      containerStyle={style}
      containerProps={{ ...attributes, ...listeners }}
      checkboxProps={{ onClick: stopDrag, onMouseDown: stopDrag, onPointerDown: stopDrag }}
      indent={indent}
      isParent={isParent}
      isExpanded={isExpanded}
      childCount={childCount}
      crossStatusChildCount={crossStatusChildCount}
      crossStatusChildren={crossStatusChildren}
      onToggleChildren={onToggleChildren}
      parentInfo={parentInfo}
      onHoverParent={onHoverParent}
      onScrollToParent={onScrollToParent}
    />
  );
});
