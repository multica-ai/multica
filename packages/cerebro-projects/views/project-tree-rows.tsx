"use client";

import type { ReactNode } from "react";
import { CalendarRange, ChevronDown, ChevronRight } from "lucide-react";

import type { WorkspaceSprint } from "@multica/cerebro-sprints/core";
import type {
  ProjectColumnKey,
  ProjectListFilters,
  ProjectSortDirection,
  ProjectSortField,
} from "@multica/core/projects";
import type { ProjectTreeItem } from "@multica/core/types";
import { ListGridCell, ListGridRow } from "@multica/ui/components/ui/list-grid";

import { useProjectsTreeStore } from "../core/store";
import { buildProjectTreeRows } from "../core/tree";

export interface ProjectRowRenderInput {
  project: ProjectTreeItem;
  namePrefix: ReactNode;
  pathLabel?: string;
  contextOnly: boolean;
}

interface CerebroProjectTreeRowsProps {
  workspaceId: string;
  tree: ProjectTreeItem[];
  sprints: WorkspaceSprint[];
  search: string;
  filters: ProjectListFilters;
  sortField: ProjectSortField;
  sortDirection: ProjectSortDirection;
  isColVisible: (key: ProjectColumnKey) => boolean;
  renderProjectRow: (input: ProjectRowRenderInput) => ReactNode;
  renderSprintLink: (sprint: WorkspaceSprint, children: ReactNode) => ReactNode;
}

const EMPTY_EXPANDED_PROJECTS: Record<string, boolean> = {};
const EMPTY_COMPLETED_SPRINTS: Record<string, boolean> = {};

function formatDate(value: string): string {
  return new Intl.DateTimeFormat("en", { month: "short", day: "numeric" }).format(
    new Date(`${value}T00:00:00Z`),
  );
}

function SprintProgress({ sprint }: { sprint: WorkspaceSprint }) {
  if (sprint.issue_count === 0) {
    return <span className="text-xs text-muted-foreground/40">—</span>;
  }
  const percentage = Math.round((sprint.done_count / sprint.issue_count) * 100);
  return (
    <span className="flex items-center gap-1.5">
      <span className="relative size-3.5">
        <svg className="size-3.5 -rotate-90" viewBox="0 0 16 16" aria-hidden="true">
          <circle className="text-muted" strokeWidth="2" stroke="currentColor" fill="none" r="6" cx="8" cy="8" />
          <circle
            className="text-success"
            strokeWidth="2"
            stroke="currentColor"
            fill="none"
            r="6"
            cx="8"
            cy="8"
            strokeDasharray={`${percentage * 0.377} 37.7`}
            strokeLinecap="round"
          />
        </svg>
      </span>
      <span className="text-xs tabular-nums text-muted-foreground">
        {sprint.done_count}/{sprint.issue_count}
      </span>
    </span>
  );
}

function ProjectPrefix({
  depth,
  hasChildren,
  expanded,
  onToggle,
}: {
  depth: number;
  hasChildren: boolean;
  expanded: boolean;
  onToggle: () => void;
}) {
  const Icon = expanded ? ChevronDown : ChevronRight;
  return (
    <span className="flex shrink-0 items-center" style={{ marginLeft: `${depth * 26}px` }}>
      {hasChildren ? (
        <button
          type="button"
          aria-label={expanded ? "Collapse project" : "Expand project"}
          aria-expanded={expanded}
          onClick={onToggle}
          className="flex size-5 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground"
        >
          <Icon className="size-3.5" />
        </button>
      ) : (
        <span className="size-5" />
      )}
    </span>
  );
}

function SprintRow({
  sprint,
  depth,
  isColVisible,
  renderSprintLink,
}: {
  sprint: WorkspaceSprint;
  depth: number;
  isColVisible: (key: ProjectColumnKey) => boolean;
  renderSprintLink: CerebroProjectTreeRowsProps["renderSprintLink"];
}) {
  const statusLabel = sprint.status === "active" ? "Active" : sprint.status === "planned" ? "Planned" : "Completed";
  return (
    <ListGridRow className="h-10 bg-muted/25">
      <ListGridCell className="px-0" />
      <ListGridCell className="gap-2" style={{ paddingLeft: `${depth * 26 + 20}px` }}>
        <CalendarRange className="size-3.5 shrink-0 text-primary" />
        {renderSprintLink(
          sprint,
          <span className="flex min-w-0 items-baseline gap-2">
            <span className="truncate text-sm font-medium">{sprint.name}</span>
            <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
              {formatDate(sprint.start_date)} – {formatDate(sprint.end_date)}
            </span>
          </span>,
        )}
      </ListGridCell>
      <ListGridCell>
        <span
          className={
            sprint.status === "active"
              ? "rounded-full bg-success/10 px-2 py-0.5 text-xs font-medium text-success"
              : "rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground"
          }
        >
          {statusLabel}
        </span>
      </ListGridCell>
      {isColVisible("priority") ? <ListGridCell className="hidden @2xl:flex" /> : <ListGridCell className="hidden px-0 @2xl:flex" />}
      {isColVisible("progress") ? <ListGridCell className="hidden @2xl:flex"><SprintProgress sprint={sprint} /></ListGridCell> : <ListGridCell className="hidden px-0 @2xl:flex" />}
      {isColVisible("lead") ? <ListGridCell className="hidden @2xl:flex" /> : <ListGridCell className="hidden px-0 @2xl:flex" />}
      {isColVisible("issues") ? <ListGridCell className="hidden justify-end font-mono text-xs tabular-nums text-muted-foreground @2xl:flex">{sprint.issue_count}</ListGridCell> : <ListGridCell className="hidden px-0 @2xl:flex" />}
      {isColVisible("created") ? <ListGridCell className="hidden whitespace-nowrap text-xs tabular-nums text-muted-foreground @2xl:flex">ends {formatDate(sprint.end_date)}</ListGridCell> : <ListGridCell className="hidden px-0 @2xl:flex" />}
      <ListGridCell className="px-0" />
    </ListGridRow>
  );
}

function CompletedToggleRow({
  projectId,
  depth,
  count,
  expanded,
  onToggle,
}: {
  projectId: string;
  depth: number;
  count: number;
  expanded: boolean;
  onToggle: (projectId: string) => void;
}) {
  return (
    <ListGridRow className="h-9 bg-muted/15">
      <ListGridCell className="px-0" />
      <ListGridCell style={{ paddingLeft: `${depth * 26 + 20}px` }}>
        <button
          type="button"
          aria-expanded={expanded}
          onClick={() => onToggle(projectId)}
          className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
        >
          {expanded ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
          {expanded ? "Hide completed" : `Show ${count} completed`}
        </button>
      </ListGridCell>
      <ListGridCell />
      <ListGridCell className="hidden @2xl:flex" />
      <ListGridCell className="hidden @2xl:flex" />
      <ListGridCell className="hidden @2xl:flex" />
      <ListGridCell className="hidden @2xl:flex" />
      <ListGridCell className="hidden @2xl:flex" />
      <ListGridCell className="px-0" />
    </ListGridRow>
  );
}

export function CerebroProjectTreeRows(props: CerebroProjectTreeRowsProps) {
  const expandedProjects = useProjectsTreeStore(
    (state) => state.expandedProjectsByWorkspace[props.workspaceId] ?? EMPTY_EXPANDED_PROJECTS,
  );
  const showCompletedSprints = useProjectsTreeStore(
    (state) => state.showCompletedSprintsByWorkspace[props.workspaceId] ?? EMPTY_COMPLETED_SPRINTS,
  );
  const toggleProject = useProjectsTreeStore((state) => state.toggleProject);
  const toggleCompletedSprints = useProjectsTreeStore((state) => state.toggleCompletedSprints);
  const rows = buildProjectTreeRows({
    tree: props.tree,
    sprints: props.sprints,
    expandedProjects,
    showCompletedSprints,
    search: props.search,
    filters: props.filters,
    sortField: props.sortField,
    sortDirection: props.sortDirection,
  });

  return rows.map((row) => {
    if (row.kind === "project") {
      return props.renderProjectRow({
        project: row.project,
        namePrefix: (
          <ProjectPrefix
            depth={row.depth}
            hasChildren={row.hasChildren}
            expanded={row.expanded}
            onToggle={() => toggleProject(props.workspaceId, row.project.id)}
          />
        ),
        pathLabel: row.path.length > 0 ? row.path.join(" / ") : undefined,
        contextOnly: row.contextOnly,
      });
    }
    if (row.kind === "sprint") {
      return (
        <SprintRow
          key={`sprint-${row.sprint.id}`}
          sprint={row.sprint}
          depth={row.depth}
          isColVisible={props.isColVisible}
          renderSprintLink={props.renderSprintLink}
        />
      );
    }
    return (
      <CompletedToggleRow
        key={`completed-${row.projectId}`}
        projectId={row.projectId}
        depth={row.depth}
        count={row.count}
        expanded={row.expanded}
        onToggle={(projectId) => toggleCompletedSprints(props.workspaceId, projectId)}
      />
    );
  });
}
