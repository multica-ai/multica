"use client";

// CEREBRO-PATCH(list-row-cerebro): cerebro modification of upstream file

import { memo, useState, type Ref } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, MoreHorizontal } from "lucide-react";
import { useSortable, defaultAnimateLayoutChanges } from "@dnd-kit/sortable";
import type { AnimateLayoutChanges } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  ListGridCell,
} from "@multica/ui/components/ui/list-grid";
import { AppLink } from "../../navigation";
import type { Issue } from "@multica/core/types";
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
import { IssueActionsContextMenu, IssueActionsDropdown } from "../actions";
import { LabelChip } from "../../labels/label-chip";
import { IssueAgentActivityIndicator } from "./issue-agent-activity-indicator";
// CEREBRO-PATCH(list-row-wakeup-dot): FIR-1521 — orange scheduled-wakeup pip stacked next to the running indicator.
import { CerebroIssueWakeupPip } from "@multica/cerebro-wakeup";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

export interface ChildProgress {
  done: number;
  total: number;
}

function formatDate(date: string): string {
  return formatDateOnly(date, { month: "short", day: "numeric" }, "en-US");
}

function ListRowContent({
  issue,
  childProgress,
  // CEREBRO-PATCH(list-row-tree-expand): inline sub-issue expansion props.
  childIssues,
  indent = false,
  isDragging,
  containerRef,
  containerStyle,
  containerProps,
  checkboxProps,
}: {
  issue: Issue;
  childProgress?: ChildProgress;
  // CEREBRO-PATCH(list-row-tree-expand): inline sub-issue expansion props.
  childIssues?: Issue[];
  indent?: boolean;
  isDragging?: boolean;
  containerRef?: Ref<HTMLDivElement>;
  containerStyle?: React.CSSProperties;
  containerProps?: Record<string, unknown>;
  checkboxProps?: Pick<React.HTMLAttributes<HTMLDivElement>, "onClick" | "onMouseDown" | "onPointerDown">;
}) {
  const [expanded, setExpanded] = useState(false);
  const wakeupDotEnabled = useFeatureFlag("cerebro_activity_wakeup_dot"); // CEREBRO-PATCH(list-row-wakeup-dot): FIR-1521
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

  const showProject = storeProperties.project && project;
  const showChildProgress = storeProperties.childProgress && childProgress;
  const showAssignee = storeProperties.assignee && issue.assignee_type && issue.assignee_id;
  const showStartDate = storeProperties.startDate && issue.start_date;
  const showDueDate = storeProperties.dueDate && issue.due_date;
  const showLabels = storeProperties.labels && labels.length > 0;
  const hasChildren = childIssues && childIssues.length > 0;

  // CEREBRO-PATCH(issues-linear-list-mobile): FIR-2123 — row cells participate in the shared list subgrid.
  return (
    <IssueActionsContextMenu issue={issue}>
      <div className="contents">
        <div
          ref={containerRef}
          style={containerStyle}
          {...containerProps}
          className={`group/row col-span-full grid h-10 grid-cols-subgrid items-center text-sm transition-colors hover:not-data-[popup-open]:bg-accent/60 data-[popup-open]:bg-accent ${
            selected ? "bg-accent/30" : ""
          } ${isDragging ? "opacity-30" : ""}`}
        >
          {/* CEREBRO-PATCH(list-grid-edge-padding): FIR-2172 — edge placeholder spans removed. */}
          <ListGridCell
            className="justify-center px-0"
            {...checkboxProps}
          >
            <span className="relative flex h-4 w-4 items-center justify-center">
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
            </span>
          </ListGridCell>
          <ListGridCell className={`gap-2 ${indent ? "pl-5" : ""}`}>
            {hasChildren && (
              <button
                type="button"
                className="-ml-1 flex h-5 w-5 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground"
                onClick={(event) => {
                  event.stopPropagation();
                  setExpanded(!expanded);
                }}
                onMouseDown={stopDrag}
                onPointerDown={stopDrag}
              >
                <ChevronRight className={`size-3.5 transition-transform ${expanded ? "rotate-90" : ""}`} />
              </button>
            )}
            <span className="w-16 shrink-0 text-xs text-muted-foreground">
              {issue.identifier}
            </span>
            <span className="inline-flex shrink-0 items-center gap-0.5">
              <IssueAgentActivityIndicator issueId={issue.id} />
              {wakeupDotEnabled && <CerebroIssueWakeupPip issueId={issue.id} />}
            </span>

            <AppLink
              href={p.issueDetail(issue.id)}
              className={`flex min-w-0 flex-1 items-center gap-1.5 ${isDragging ? "pointer-events-none" : ""}`}
            >
              <span className="truncate">{issue.title}</span>
              {showChildProgress && (
                <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5">
                  <ProgressRing done={childProgress!.done} total={childProgress!.total} size={14} />
                  <span className="text-[11px] text-muted-foreground tabular-nums font-medium">
                    {childProgress!.done}/{childProgress!.total}
                  </span>
                </span>
              )}
              {showLabels && (
                <span className="ml-1.5 hidden @2xl:inline-flex shrink-0 items-center gap-1 max-w-[260px] overflow-hidden">
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
            </AppLink>
          </ListGridCell>
          {showProject ? (
            <ListGridCell className="hidden @2xl:flex">
              <span className="inline-flex min-w-0 items-center gap-1 text-xs text-muted-foreground">
                <ProjectIcon project={project} size="sm" />
                <span className="truncate">{project!.title}</span>
              </span>
            </ListGridCell>
          ) : (
            <ListGridCell className="hidden px-0 @2xl:flex" />
          )}
          {showStartDate ? (
            <ListGridCell className="hidden whitespace-nowrap text-xs text-muted-foreground @2xl:flex">
              {formatDate(issue.start_date!)}
            </ListGridCell>
          ) : (
            <ListGridCell className="hidden px-0 @2xl:flex" />
          )}
          {showDueDate ? (
            <ListGridCell className="hidden whitespace-nowrap text-xs text-muted-foreground @2xl:flex">
              {formatDate(issue.due_date!)}
            </ListGridCell>
          ) : (
            <ListGridCell className="hidden px-0 @2xl:flex" />
          )}
          <ListGridCell className="hidden justify-center px-0 @2xl:flex">
            {showAssignee && (
              <ActorAvatar
                actorType={issue.assignee_type!}
                actorId={issue.assignee_id!}
                size={20}
                enableHoverCard
              />
            )}
          </ListGridCell>
          <ListGridCell className="justify-end px-0">
            <span
              onClick={(event) => {
                event.preventDefault();
                event.stopPropagation();
              }}
              className="flex items-center opacity-100 @2xl:opacity-0 @2xl:transition-opacity @2xl:group-hover/row:opacity-100"
            >
              <IssueActionsDropdown
                issue={issue}
                trigger={
                  <button
                    type="button"
                    aria-label="Issue actions"
                    className="flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground data-popup-open:bg-accent data-popup-open:text-accent-foreground"
                  >
                    <MoreHorizontal className="size-4" />
                  </button>
                }
              />
            </span>
          </ListGridCell>
        </div>
      {/* CEREBRO-PATCH(list-row-tree-expand): inline child issues when expanded */}
      {hasChildren && expanded && (
        <div className="relative col-span-full ml-[22px]">
          {/* Vertical tree line */}
          <div className="absolute left-0 top-0 bottom-[18px] w-px bg-border" />
          {childIssues!.map((child) => (
            <div key={child.id} className="relative">
              {/* Horizontal branch line */}
              <div className="absolute left-0 top-[18px] w-3 h-px bg-border" />
              <div className="pl-5">
                <ListRow issue={child} indent />
              </div>
            </div>
          ))}
        </div>
      )}
      </div>
    </IssueActionsContextMenu>
  );
}

export const ListRow = memo(function ListRow({
  issue,
  childProgress,
  // CEREBRO-PATCH(list-row-tree-expand): forward sub-issue expansion props.
  childIssues,
  indent,
}: {
  issue: Issue;
  childProgress?: ChildProgress;
  childIssues?: Issue[];
  indent?: boolean;
}) {
  return (
    <ListRowContent
      issue={issue}
      childProgress={childProgress}
      childIssues={childIssues}
      indent={indent}
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
}: {
  issue: Issue;
  childProgress?: ChildProgress;
  disableSorting?: boolean;
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
    />
  );
});
