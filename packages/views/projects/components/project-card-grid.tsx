"use client";

import { useCallback } from "react";
import { useUpdateProject } from "@multica/core/projects";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import type { Project, UpdateProjectRequest } from "@multica/core/types";
import { ProjectIcon } from "./project-icon";
import { useT } from "../../i18n";
import { useFormatRelativeDate } from "./labels";
import { ProjectStatusBadge, ProjectPriorityBadge } from "./project-badge";
import { ProjectLeadPicker } from "./project-lead-picker";
import { ProjectRowActions, ProgressRing } from "./projects-page";
import { ProjectResourceChips } from "./project-resource-chips";
import { LIST_GRID_BOTTOM_CLEARANCE } from "@multica/ui/components/ui/list-grid";
import { PAGE_GUTTER } from "../../layout/page-header";
import { cn } from "@multica/ui/lib/utils";

// Card (comfortable) view — enhanced with resource chips. Each card shows
// the project icon + title header, resource chips, progress ring, status,
// lead picker, priority badge, and created date.
function ProjectCard({
  project,
  pinned,
  canDelete,
}: {
  project: Project;
  pinned: boolean;
  canDelete: boolean;
}) {
  const { t } = useT("projects");
  const wsPaths = useWorkspacePaths();
  const formatRelativeDate = useFormatRelativeDate();
  const updateProject = useUpdateProject();
  const handleUpdate = useCallback(
    (data: UpdateProjectRequest) => updateProject.mutate({ id: project.id, ...data }),
    [project.id, updateProject],
  );

  return (
    <div className="group/card group/row flex flex-col rounded-xl border border-surface-border bg-surface p-5 shadow-[var(--surface-shadow)] transition-colors hover:border-primary/50">
      <div className="flex items-center gap-2">
        <AppLink
          href={wsPaths.projectDetail(project.id)}
          className="flex min-w-0 flex-1 items-center gap-2"
        >
          <ProjectIcon project={project} size="sm" />
          <h3 className="truncate text-title-sm font-semibold">{project.title}</h3>
        </AppLink>
        <ProjectRowActions project={project} pinned={pinned} canDelete={canDelete} />
        <ProjectStatusBadge project={project} handleUpdate={handleUpdate} triggerClassName="shrink-0" />
      </div>

      {/* Resource chips */}
      <div className="mt-2">
        <ProjectResourceChips
          projectId={project.id}
          resourceCount={project.resource_count}
        />
      </div>

      {/* Progress */}
      <div className="mt-3">
        <ProgressRing project={project} />
      </div>

      {/* Footer */}
      <div className="mt-auto flex items-center justify-between border-t border-surface-border pt-3">
        <ProjectLeadPicker
          project={project}
          handleUpdate={handleUpdate}
          renderTrigger={(leadName) => (
            <button type="button" className="-mx-1.5 flex items-center gap-1.5 rounded-xs px-1.5 py-0.5 transition-colors hover:bg-accent/60">
              {project.lead_type && project.lead_id ? (
                <ActorAvatar actorType={project.lead_type} actorId={project.lead_id} size="sm" enableHoverCard />
              ) : (
                <span className="inline-flex h-5 w-5 rounded-full border border-dashed border-muted-foreground/30" />
              )}
              <span className="max-w-[60px] truncate text-micro text-muted-foreground">
                {leadName ?? t(($) => $.lead.no_lead)}
              </span>
            </button>
          )}
        />
        <div className="flex items-center gap-2">
          <ProjectPriorityBadge project={project} handleUpdate={handleUpdate} align="start" />
          <span className="text-micro text-muted-foreground">
            {formatRelativeDate(project.created_at)}
          </span>
        </div>
      </div>
    </div>
  );
}

// Responsive card grid wrapper — 1/2/3/4 columns by breakpoint.
export function ProjectCardGrid({
  projects,
  pinnedIds,
  canDelete,
}: {
  projects: Project[];
  pinnedIds: Set<string>;
  canDelete: boolean;
}) {
  return (
    <div className={cn("min-h-0 flex-1 overflow-y-auto pt-4", PAGE_GUTTER)}>
      <div
        className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4"
        style={{ paddingBottom: LIST_GRID_BOTTOM_CLEARANCE }}
      >
        {projects.map((project) => (
          <ProjectCard
            key={project.id}
            project={project}
            pinned={pinnedIds.has(project.id)}
            canDelete={canDelete}
          />
        ))}
      </div>
    </div>
  );
}
