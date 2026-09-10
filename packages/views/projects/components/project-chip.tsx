"use client";

import { useQuery } from "@tanstack/react-query";
import { projectListOptions, projectDetailOptions } from "@multica/core/projects/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { ProjectIcon } from "./project-icon";
import { useT } from "../../i18n";

/**
 * Compact presentational representation of a project —
 * `<emoji> <title>`, bordered, truncating once it hits its width cap. Mirror of
 * IssueChip, including the `min(18rem, 100%)` cap — see that file for why the
 * content limit and the container limit are both needed. The two chips share
 * one rendering contract and must not drift.
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
  "project-chip inline-flex min-w-0 max-w-[min(18rem,100%)] items-center gap-1.5 rounded-md border mx-0.5 px-2 py-0.5 text-caption";

export function ProjectChip({
  projectId,
  fallbackLabel,
  className,
}: ProjectChipProps) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
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
        <span className="min-w-0 truncate text-muted-foreground">
          {fallbackLabel ?? t(($) => $.chip.fallback_label)}
        </span>
      </span>
    );
  }

  return (
    <span className={cls}>
      <ProjectIcon project={project} size="md" />
      <span className="min-w-0 truncate text-foreground">{project.title}</span>
    </span>
  );
}
