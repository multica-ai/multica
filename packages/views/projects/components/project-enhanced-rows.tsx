"use client";

import { useCallback, type MouseEvent } from "react";
import { useUpdateProject } from "@multica/core/projects";
import { useWorkspacePaths } from "@multica/core/paths";
import { useRowLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import type { Project, UpdateProjectRequest } from "@multica/core/types";
import { ProjectIcon } from "./project-icon";
import { useFormatRelativeDate } from "./labels";
import { ProjectStatusBadge } from "./project-badge";
import { ProjectLeadPicker } from "./project-lead-picker";
import { ProgressRing, ProjectRowActions } from "./projects-page";
import { ProjectResourceChips } from "./project-resource-chips";
import { LIST_GRID_BOTTOM_CLEARANCE } from "@multica/ui/components/ui/list-grid";
import { PAGE_GUTTER } from "../../layout/page-header";
import { cn } from "@multica/ui/lib/utils";

const stopRowNavigation = (e: MouseEvent) => e.stopPropagation();

// Enhanced row — a wider card-row hybrid showing project icon, title +
// description, status badge, progress ring, lead avatar, created date,
// resource chips on a second line, and a kebab actions menu. Rows use
// whole-row navigation.
function ProjectEnhancedRow({
  project,
  pinned,
  canDelete,
}: {
  project: Project;
  pinned: boolean;
  canDelete: boolean;
}) {
  const wsPaths = useWorkspacePaths();
  const rowLink = useRowLink();
  const formatRelativeDate = useFormatRelativeDate();
  const updateProject = useUpdateProject();
  const handleUpdate = useCallback(
    (data: UpdateProjectRequest) => updateProject.mutate({ id: project.id, ...data }),
    [project.id, updateProject],
  );

  const rowHref = wsPaths.projectDetail(project.id);

  return (
    <div
      className={cn(
        "group/row cursor-pointer rounded-lg border border-surface-border bg-surface px-5 py-3.5 transition-colors hover:border-primary/50",
      )}
      {...rowLink(rowHref, project.title)}
    >
      <div className="grid grid-cols-[32px_minmax(200px,1fr)_auto_auto_auto_auto] items-center gap-4">
        {/* Icon */}
        <ProjectIcon project={project} size="md" />

        {/* Title + description + resource chips */}
        <div className="min-w-0">
          <span className="block truncate text-body font-semibold">
            {project.title}
          </span>
          {project.description && (
            <span className="block truncate text-caption text-muted-foreground">
              {project.description}
            </span>
          )}
          <div className="mt-1">
            <ProjectResourceChips
              projectId={project.id}
              resourceCount={project.resource_count}
            />
          </div>
        </div>

        {/* Status badge */}
        <div>
          <ProjectStatusBadge
            project={project}
            handleUpdate={handleUpdate}
            align="start"
          />
        </div>

        {/* Progress ring */}
        <ProgressRing project={project} />

        {/* Lead + created date (de-emphasized) */}
        <div className="flex items-center gap-3">
          <ProjectLeadPicker
            project={project}
            handleUpdate={handleUpdate}
            renderTrigger={(leadName) => (
              <button type="button" className="flex items-center gap-1.5 rounded-xs px-1 py-0.5 transition-colors hover:bg-accent/60" onClick={stopRowNavigation} onAuxClick={stopRowNavigation}>
                {project.lead_type && project.lead_id ? (
                  <ActorAvatar actorType={project.lead_type} actorId={project.lead_id} size="sm" enableHoverCard />
                ) : (
                  <span className="inline-flex h-5 w-5 rounded-full border border-dashed border-muted-foreground/30" />
                )}
                <span className="max-w-[80px] truncate text-caption text-muted-foreground">
                  {leadName ?? "—"}
                </span>
              </button>
            )}
          />
          <span className="whitespace-nowrap font-mono text-micro tabular-nums text-faint-foreground">
            {formatRelativeDate(project.created_at)}
          </span>
        </div>

        {/* Actions menu */}
        <div className="flex items-center" onClick={stopRowNavigation} onAuxClick={stopRowNavigation}>
          <ProjectRowActions project={project} pinned={pinned} canDelete={canDelete} />
        </div>
      </div>
    </div>
  );
}

// Enhanced rows view — vertical stack of enhanced project rows.
export function ProjectEnhancedRows({
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
        className="space-y-2"
        style={{ paddingBottom: LIST_GRID_BOTTOM_CLEARANCE }}
      >
        {projects.map((project) => (
          <ProjectEnhancedRow
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
