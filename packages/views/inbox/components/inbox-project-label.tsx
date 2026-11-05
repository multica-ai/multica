"use client";

import { useQuery } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { projectListOptions } from "@multica/core/projects/queries";
import type { InboxItem } from "@multica/core/types";

/**
 * Names the project of a notification's issue, so rows from
 * different projects can be told apart. The inbox list is fetched for the
 * current workspace, so its cached project list resolves the name; nothing
 * renders for items without a project, from another workspace, or before
 * the projects load.
 */
export function InboxProjectLabel({ item }: { item: InboxItem }) {
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const projectId = item.issue_project_id;
  const enabled = wsId !== "" && wsId === item.workspace_id && !!projectId;
  const { data: projects } = useQuery({ ...projectListOptions(wsId), enabled });
  if (!enabled) return null;
  const project = projects?.find((p) => p.id === projectId);
  if (!project) return null;
  return (
    <span
      data-inbox-project
      title={project.title}
      className="inline-flex max-w-32 shrink-0 items-center gap-1 truncate rounded-xs bg-muted px-1.5 text-micro font-medium text-muted-foreground"
    >
      {project.icon && <span aria-hidden>{project.icon}</span>}
      <span className="truncate">{project.title}</span>
    </span>
  );
}
