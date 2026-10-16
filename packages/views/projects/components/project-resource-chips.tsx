"use client";

import { useQuery } from "@tanstack/react-query";
import { Folder, FolderGit2 } from "lucide-react";
import { projectResourcesOptions } from "@multica/core/projects";
import { useWorkspaceId } from "@multica/core/hooks";
import type { ProjectResource } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";

// Derive a display label for a resource: prefer the user-set label, then
// fall back to the repo basename (url → last path segment) for github_repo
// or the directory basename (local_path → last segment) for local_directory.
function resourceLabel(r: ProjectResource): string {
  if (r.label) return r.label;

  if (r.resource_type === "github_repo") {
    const ref = r.resource_ref as { url?: string };
    if (ref?.url) {
      const parts = ref.url.replace(/\.git$/, "").split("/");
      return parts[parts.length - 1] || ref.url;
    }
    return "repo";
  }

  if (r.resource_type === "local_directory") {
    const ref = r.resource_ref as { local_path?: string };
    if (ref?.local_path) {
      const parts = ref.local_path.replace(/[/\\]+$/, "").split(/[/\\]/);
      return parts[parts.length - 1] || ref.local_path;
    }
    return "directory";
  }

  return "resource";
}

function ResourceIcon({ type }: { type: string }) {
  if (type === "github_repo") return <FolderGit2 className="size-3 shrink-0" />;
  if (type === "local_directory") return <Folder className="size-3 shrink-0" />;
  return <Folder className="size-3 shrink-0" />;
}

const MAX_CHIPS = 3;

// Renders a compact row of resource chips for a project. Each chip shows an
// icon + label. Shows up to MAX_CHIPS resources + a "+N" overflow badge.
// Returns null when resourceCount is 0 so callers don't render an empty row.
export function ProjectResourceChips({
  projectId,
  resourceCount,
}: {
  projectId: string;
  resourceCount: number;
}) {
  const wsId = useWorkspaceId();
  const { data: resources = [] } = useQuery(
    projectResourcesOptions(wsId, projectId),
  );

  if (resourceCount === 0 || resources.length === 0) return null;

  const visible = resources.slice(0, MAX_CHIPS);
  const overflow = resources.length - MAX_CHIPS;

  return (
    <div className="flex flex-wrap items-center gap-1">
      {visible.map((r) => (
        <span
          key={r.id}
          className={cn(
            "inline-flex items-center gap-1 rounded-xs border border-surface-border bg-page-canvas px-1.5 py-0.5",
          )}
        >
          <ResourceIcon type={r.resource_type} />
          <span className="max-w-[120px] truncate text-micro text-muted-foreground">
            {resourceLabel(r)}
          </span>
        </span>
      ))}
      {overflow > 0 && (
        <span className="inline-flex items-center rounded-xs px-1.5 py-0.5 text-micro text-muted-foreground">
          +{overflow}
        </span>
      )}
    </div>
  );
}
