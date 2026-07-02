"use client";

import { memo, type Ref } from "react";
import { useSortable, defaultAnimateLayoutChanges } from "@dnd-kit/sortable";
import type { AnimateLayoutChanges } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { AppLink } from "../../navigation";
import type { Issue, Project } from "@multica/core/types";
import { formatDateOnly } from "@multica/core/issues/date";
import { ActorAvatar } from "../../common/actor-avatar";
import { useWorkspacePaths } from "@multica/core/paths";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { ProjectIcon } from "../../projects/components/project-icon";
import { PriorityIcon } from "./priority-icon";
import { ProgressRing } from "./progress-ring";
import { IssueActionsContextMenu } from "../actions";
import { LabelChip } from "../../labels/label-chip";
import { IssueAgentActivityIndicator } from "./issue-agent-activity-indicator";
import { useIssueSurfaceSelection } from "../surface/selection-context";

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
  project,
  isDragging,
  containerRef,
  containerStyle,
  containerProps,
  checkboxProps,
}: {
  issue: Issue;
  childProgress?: ChildProgress;
  project?: Project;
  isDragging?: boolean;
  containerRef?: Ref<HTMLDivElement>;
  containerStyle?: React.CSSProperties;
  containerProps?: Record<string, unknown>;
  checkboxProps?: Pick<React.HTMLAttributes<HTMLDivElement>, "onClick" | "onMouseDown" | "onPointerDown">;
}) {
  const selection = useIssueSurfaceSelection();
  const selected = selection.selectedIds.has(issue.id);
  const toggle = selection.toggle;
  const p = useWorkspacePaths();
  const storeProperties = useViewStore((s) => s.cardProperties);
  const labels = issue.labels ?? [];

  const showProject = storeProperties.project && project;
  const showChildProgress = storeProperties.childProgress && childProgress;
  const showAssignee = storeProperties.assignee && issue.assignee_type && issue.assignee_id;
  const showStartDate = storeProperties.startDate && issue.start_date;
  const showDueDate = storeProperties.dueDate && issue.due_date;
  const showLabels = storeProperties.labels && labels.length > 0;

  return (
    <IssueActionsContextMenu issue={issue}>
      <div
        ref={containerRef}
        style={containerStyle}
        {...containerProps}
        className={`group/row flex h-9 items-center gap-2 px-4 text-sm transition-colors hover:not-data-[popup-open]:bg-accent/60 data-[popup-open]:bg-accent ${
          selected ? "bg-accent/30" : ""
        } ${isDragging ? "opacity-30" : ""}`}
      >
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
  project,
}: {
  issue: Issue;
  childProgress?: ChildProgress;
  project?: Project;
}) {
  return (
    <ListRowContent
      issue={issue}
      childProgress={childProgress}
      project={project}
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
  project,
  disableSorting,
}: {
  issue: Issue;
  childProgress?: ChildProgress;
  project?: Project;
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
      project={project}
      isDragging={isDragging}
      containerRef={setNodeRef}
      containerStyle={style}
      containerProps={{ ...attributes, ...listeners }}
      checkboxProps={{ onClick: stopDrag, onMouseDown: stopDrag, onPointerDown: stopDrag }}
    />
  );
});
