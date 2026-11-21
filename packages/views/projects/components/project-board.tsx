"use client";

import { useState, type ReactNode } from "react";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  pointerWithin,
  rectIntersection,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type CollisionDetection,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import { Calendar } from "@multica/ui/icons";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspacePaths } from "@multica/core/paths";
import type { Project, UpdateProjectRequest } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { CardControl } from "../../common/card-control";
import { PAGE_GUTTER } from "../../layout/page-header";
import { useT } from "../../i18n";
import { useRestoredScrollRef } from "../../platform/scroll-restoration";
import { ProjectStatusBadge, ProjectPriorityBadge } from "./project-badge";
import { ProjectIcon } from "./project-icon";
import { ProjectLeadPicker } from "./project-lead-picker";
import { ProjectRowActions } from "./project-row-actions";
import { ProjectDate, projectDueState } from "./project-date";

const COLUMN_WIDTH = 280;
const CARD_WIDTH = COLUMN_WIDTH - 16 - 8;

export interface ProjectBoardColumn {
  key: string;
  label: string;
  icon?: ReactNode;
  projects: Project[];
}

interface DragData {
  project: Project;
  columnKey: string;
}

// Hits the column under the pointer; between columns, the nearest one.
const collisionDetection: CollisionDetection = (args) => {
  const hits = pointerWithin(args);
  return hits.length > 0 ? hits : rectIntersection(args);
};

/**
 * Projects as columns of one field (status, priority or lead). Dropping a card
 * on another column sets that field; within a column cards follow the page's
 * sort, so there is no manual order to keep. Status, priority and lead can also
 * be set from the card itself, for values with no column on screen.
 */
export function ProjectBoard({
  columns,
  pinnedIds,
  mayDelete,
  onMove,
  onUpdate,
}: {
  columns: readonly ProjectBoardColumn[];
  pinnedIds: ReadonlySet<string>;
  mayDelete: (project: Project) => boolean;
  onMove: (project: Project, columnKey: string) => void;
  onUpdate: (project: Project, data: UpdateProjectRequest) => void;
}) {
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );
  const [active, setActive] = useState<Project | null>(null);
  const restoreScrollRef = useRestoredScrollRef("project-board");

  const handleDragStart = (event: DragStartEvent) => {
    setActive((event.active.data.current as DragData | undefined)?.project ?? null);
  };
  const handleDragEnd = (event: DragEndEvent) => {
    setActive(null);
    const data = event.active.data.current as DragData | undefined;
    const target = event.over?.id;
    if (!data || typeof target !== "string" || target === data.columnKey) return;
    onMove(data.project, target);
  };

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={collisionDetection}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
      onDragCancel={() => setActive(null)}
    >
      <div
        ref={restoreScrollRef}
        data-board-scroller=""
        data-tab-scroll-root="project-board"
        className={cn("flex min-h-0 flex-1 gap-3 overflow-x-auto pt-2 pb-4", PAGE_GUTTER)}
      >
        {columns.map((column) => (
          <ProjectBoardColumnView key={column.key} column={column}>
            {column.projects.map((project) => (
              <ProjectBoardCard
                key={project.id}
                project={project}
                columnKey={column.key}
                pinned={pinnedIds.has(project.id)}
                canDelete={mayDelete(project)}
                onUpdate={(data) => onUpdate(project, data)}
              />
            ))}
          </ProjectBoardColumnView>
        ))}
      </div>
      <DragOverlay dropAnimation={null}>
        {active ? (
          <div
            style={{ width: CARD_WIDTH }}
            className="rotate-1 cursor-grabbing opacity-90 shadow-lg shadow-black/10"
          >
            <ProjectBoardCardContent project={active} onUpdate={() => {}} />
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}

function ProjectBoardColumnView({
  column,
  children,
}: {
  column: ProjectBoardColumn;
  children: ReactNode;
}) {
  const { t } = useT("projects");
  const { setNodeRef, isOver, active } = useDroppable({ id: column.key });
  const isTarget =
    isOver && (active?.data.current as DragData | undefined)?.columnKey !== column.key;

  return (
    <section
      data-mc="board-column"
      aria-label={column.label}
      style={{ width: COLUMN_WIDTH }}
      className="flex min-h-0 shrink-0 flex-col rounded-board-column bg-board-column p-2"
    >
      <div className="mb-2 flex min-w-0 items-center gap-2 px-1.5">
        {column.icon}
        <span className="min-w-0 truncate text-body font-medium">{column.label}</span>
        <span className="shrink-0 rounded-full bg-background px-1.5 py-0.5 text-micro font-medium tabular-nums text-muted-foreground">
          {column.projects.length}
        </span>
      </div>
      <div
        ref={setNodeRef}
        className={cn(
          "min-h-[200px] flex-1 overflow-y-auto rounded-lg p-1 transition-colors",
          isTarget && "bg-accent/60",
        )}
      >
        {column.projects.length > 0 ? (
          <div className="flex flex-col gap-[var(--board-card-gap,var(--space-xs,0.5rem))]">
            {children}
          </div>
        ) : (
          <p className="py-8 text-center text-caption text-muted-foreground">
            {t(($) => $.page.board_column_empty)}
          </p>
        )}
      </div>
    </section>
  );
}

function ProjectBoardCard({
  project,
  columnKey,
  pinned,
  canDelete,
  onUpdate,
}: {
  project: Project;
  columnKey: string;
  pinned: boolean;
  canDelete: boolean;
  onUpdate: (data: UpdateProjectRequest) => void;
}) {
  const paths = useWorkspacePaths();
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: project.id,
    data: { project, columnKey } satisfies DragData,
  });

  return (
    <div
      ref={setNodeRef}
      data-board-card=""
      {...attributes}
      {...listeners}
      className={cn("group/card group/row relative", isDragging && "opacity-30")}
    >
      <AppLink
        href={paths.projectDetail(project.id)}
        newTabTitle={project.title}
        className={cn("block", isDragging && "pointer-events-none")}
      >
        <ProjectBoardCardContent project={project} onUpdate={onUpdate} />
      </AppLink>
      <div
        className="absolute top-2 right-2"
        onPointerDown={(event) => event.stopPropagation()}
      >
        <ProjectRowActions project={project} pinned={pinned} canDelete={canDelete} />
      </div>
    </div>
  );
}

function ProjectBoardCardContent({
  project,
  onUpdate,
}: {
  project: Project;
  onUpdate: (data: UpdateProjectRequest) => void;
}) {
  const { t } = useT("projects");
  const pct =
    project.issue_count > 0 ? Math.round((project.done_count / project.issue_count) * 100) : 0;

  return (
    <div className="flex flex-col gap-2 rounded-board-card border-[0.5px] border-board-card-border bg-board-card px-[var(--board-card-padding-x,0.625rem)] py-[var(--board-card-padding-y,var(--space-sm,0.75rem))] shadow-[var(--board-card-shadow,var(--surface-shadow))] transition-colors group-hover/card:border-foreground/15 group-hover/card:bg-board-card-hover">
      <div className="flex min-w-0 items-center gap-2 pr-6">
        <ProjectIcon project={project} size="sm" />
        <span className="min-w-0 flex-1 truncate text-body font-medium">{project.title}</span>
      </div>
      {project.issue_count > 0 ? (
        <div className="h-1 overflow-hidden rounded-full bg-muted">
          <div className="h-full rounded-full bg-emerald-500" style={{ width: `${pct}%` }} />
        </div>
      ) : null}
      <div className="flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
        <CardControl className="flex shrink-0">
          <ProjectStatusBadge project={project} handleUpdate={onUpdate} align="start" />
        </CardControl>
        <CardControl className="flex shrink-0">
          <ProjectPriorityBadge
            project={project}
            handleUpdate={onUpdate}
            align="start"
            iconOnly
            triggerClassName="px-1"
          />
        </CardControl>
        <CardControl className="flex shrink-0">
          <ProjectLeadPicker
            project={project}
            handleUpdate={onUpdate}
            renderTrigger={(leadName) => (
              <button
                type="button"
                aria-label={leadName ?? t(($) => $.lead.no_lead)}
                title={leadName ?? t(($) => $.lead.no_lead)}
                className="flex rounded-md transition-colors hover:bg-accent/60"
              >
                {project.lead_type && project.lead_id ? (
                  <ActorAvatar actorType={project.lead_type} actorId={project.lead_id} size="sm" />
                ) : (
                  <span className="inline-flex h-[18px] w-[18px] rounded-full border border-dashed border-muted-foreground/30" />
                )}
              </button>
            )}
          />
        </CardControl>
        {project.issue_count > 0 ? (
          <span className="tabular-nums">
            {project.done_count}/{project.issue_count}
          </span>
        ) : null}
        {project.due_date ? (
          <span className="ml-auto inline-flex shrink-0 items-center gap-1">
            <Calendar className="size-3" />
            <ProjectDate value={project.due_date} dueState={projectDueState(project)} />
          </span>
        ) : null}
      </div>
    </div>
  );
}

export function ProjectBoardLoading() {
  return (
    <div className={cn("flex min-h-0 flex-1 gap-3 overflow-x-hidden pt-2 pb-4", PAGE_GUTTER)}>
      {Array.from({ length: 4 }).map((_, i) => (
        <div
          key={i}
          style={{ width: COLUMN_WIDTH }}
          className="flex shrink-0 flex-col gap-2 rounded-board-column bg-board-column p-2"
        >
          <Skeleton className="mx-1.5 mb-1 h-5 w-24" />
          {Array.from({ length: 3 }).map((__, j) => (
            <Skeleton key={j} className="h-20 rounded-board-card" />
          ))}
        </div>
      ))}
    </div>
  );
}
