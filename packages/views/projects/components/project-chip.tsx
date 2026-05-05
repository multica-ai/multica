"use client";

import { useQuery } from "@tanstack/react-query";
import { projectListOptions, projectDetailOptions } from "@multica/core/projects/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useT } from "@multica/i18n/react";
import { ProjectIcon } from "./project-icon";

/**
 * Compact presentational representation of a project —
 * `<emoji> <title>`, bordered, truncating to max-w-72. Mirror of IssueChip.
 *
 * Not a link / button: callers wrap it in whatever interactive shell they
 * need. Pure UI — data is queried internally so callers can pass just an id.
 */
export interface ProjectChipProps {
  projectId: string;
  /** Shown when the project can't be resolved. */
  fallbackLabel?: string;
  /** Extra classes — callers layer interaction hints here. */
  className?: string;
}

const BASE_CLASS =
  "project-chip inline-flex items-center gap-1.5 rounded-md border mx-0.5 px-2 py-0.5 text-xs max-w-72";

export function ProjectChip({
  projectId,
  fallbackLabel,
  className,
}: ProjectChipProps) {
  const wsId = useWorkspaceId();
  const t = useT("issues");
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const listProject = projects.find((p) => p.id === projectId);

  const { data: detailProject } = useQuery({
    ...projectDetailOptions(wsId, projectId),
    enabled: !listProject,
  });

  const project = listProject ?? detailProject;
  const cls = className ? `${BASE_CLASS} ${className}` : BASE_CLASS;

  if (!project) {
    return (
      <span className={cls}>
        <ProjectIcon size="md" />
        <span className="text-muted-foreground truncate">
          {fallbackLabel ?? t("project_fallback")}
        </span>
      </span>
    );
  }

  return (
    <span className={cls}>
      <ProjectIcon project={project} size="md" />
      <span className="text-foreground truncate">{project.title}</span>
    </span>
  );
}
