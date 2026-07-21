"use client";

import type { ReactNode } from "react";
import { CalendarRange, ChevronDown, ChevronRight } from "lucide-react";

import type { WorkspaceSprint } from "@multica/cerebro-sprints/core";
import type {
  ProjectListFilters,
  ProjectSortDirection,
  ProjectSortField,
} from "@multica/core/projects";
import type { ProjectTreeItem } from "@multica/core/types";

import { useProjectsTreeStore } from "../core/store";
import {
  buildProjectCardGroups,
  type ProjectCardItem,
} from "../core/cards";

export interface ProjectCardRenderInput {
  project: ProjectTreeItem;
  details: ReactNode;
}

interface CerebroProjectCardsProps {
  workspaceId: string;
  tree: ProjectTreeItem[];
  sprints: WorkspaceSprint[];
  visibleProjectIds?: readonly string[];
  search: string;
  filters: ProjectListFilters;
  sortField: ProjectSortField;
  sortDirection: ProjectSortDirection;
  renderProjectCard: (input: ProjectCardRenderInput) => ReactNode;
  renderSprintLink: (sprint: WorkspaceSprint, children: ReactNode) => ReactNode;
}

const EMPTY_EXPANDED_PROJECTS: Record<string, boolean> = {};

function formatEndDate(value: string): string {
  return new Intl.DateTimeFormat("en", { month: "short", day: "numeric" }).format(
    new Date(`${value}T00:00:00Z`),
  );
}

function SprintSummary({
  sprint,
  compact = false,
  renderSprintLink,
}: {
  sprint: WorkspaceSprint;
  compact?: boolean;
  renderSprintLink: CerebroProjectCardsProps["renderSprintLink"];
}) {
  return renderSprintLink(
    sprint,
    <span
      className={
        compact
          ? "inline-flex max-w-full items-center gap-1 rounded-full border bg-background px-2 py-1 text-[11px] text-muted-foreground hover:text-foreground"
          : "flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground hover:text-foreground"
      }
    >
      <CalendarRange className="size-3 shrink-0 text-primary" />
      <span className="truncate font-medium text-foreground">{sprint.name}</span>
      <span className="shrink-0 tabular-nums">
        · {sprint.done_count}/{sprint.issue_count} · ends {formatEndDate(sprint.end_date)}
      </span>
    </span>,
  );
}

function CardDetails({
  card,
  renderSprintLink,
}: {
  card: ProjectCardItem;
  renderSprintLink: CerebroProjectCardsProps["renderSprintLink"];
}) {
  if (card.path.length === 0 && !card.activeSprint) return null;
  return (
    <div className="space-y-1.5 px-3 pb-2">
      {card.path.length > 0 && (
        <p className="truncate text-[10px] text-muted-foreground">
          {card.path.join(" / ")}
        </p>
      )}
      {card.activeSprint && (
        <SprintSummary sprint={card.activeSprint} renderSprintLink={renderSprintLink} />
      )}
    </div>
  );
}

function CardGrid({
  cards,
  renderProjectCard,
  renderSprintLink,
}: {
  cards: ProjectCardItem[];
  renderProjectCard: CerebroProjectCardsProps["renderProjectCard"];
  renderSprintLink: CerebroProjectCardsProps["renderSprintLink"];
}) {
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
      {cards.map((card) => (
        <div key={card.project.id} className="min-w-0">
          {renderProjectCard({
            project: card.project,
            details: <CardDetails card={card} renderSprintLink={renderSprintLink} />,
          })}
        </div>
      ))}
    </div>
  );
}

export function CerebroProjectCards(props: CerebroProjectCardsProps) {
  const expandedProjects = useProjectsTreeStore(
    (state) => state.expandedProjectsByWorkspace[props.workspaceId] ?? EMPTY_EXPANDED_PROJECTS,
  );
  const toggleProject = useProjectsTreeStore((state) => state.toggleProject);
  const groups = buildProjectCardGroups({
    tree: props.tree,
    sprints: props.sprints,
    visibleProjectIds: props.visibleProjectIds,
    search: props.search,
    filters: props.filters,
    sortField: props.sortField,
    sortDirection: props.sortDirection,
  });

  return (
    <div className="space-y-6">
      {groups.sections.map((section) => {
        const expanded = expandedProjects[section.project.id] !== false;
        const ToggleIcon = expanded ? ChevronDown : ChevronRight;
        return (
          <section key={section.project.id} className="space-y-3">
            <div className="flex min-h-9 flex-wrap items-center gap-2 border-b pb-2">
              <button
                type="button"
                aria-label={`${expanded ? "Collapse" : "Expand"} ${section.project.title}`}
                aria-expanded={expanded}
                onClick={() => toggleProject(props.workspaceId, section.project.id)}
                className="flex min-w-0 items-center gap-1.5 rounded px-1 py-0.5 hover:bg-accent"
              >
                <ToggleIcon className="size-4 shrink-0 text-muted-foreground" />
                <h2 className="truncate text-sm font-semibold">{section.project.title}</h2>
                <span className="text-xs tabular-nums text-muted-foreground">
                  {section.cards.length}
                </span>
              </button>
              {section.activeSprint && (
                <div className="ml-auto min-w-0">
                  <SprintSummary
                    sprint={section.activeSprint}
                    compact
                    renderSprintLink={props.renderSprintLink}
                  />
                </div>
              )}
            </div>
            {expanded && (
              <CardGrid
                cards={section.cards}
                renderProjectCard={props.renderProjectCard}
                renderSprintLink={props.renderSprintLink}
              />
            )}
          </section>
        );
      })}

      {groups.otherProjects.length > 0 && (
        <section className="space-y-3">
          <div className="flex min-h-9 items-center gap-1.5 border-b pb-2">
            <h2 className="text-sm font-semibold">Other projects</h2>
            <span className="text-xs tabular-nums text-muted-foreground">
              {groups.otherProjects.length}
            </span>
          </div>
          <CardGrid
            cards={groups.otherProjects}
            renderProjectCard={props.renderProjectCard}
            renderSprintLink={props.renderSprintLink}
          />
        </section>
      )}
    </div>
  );
}
