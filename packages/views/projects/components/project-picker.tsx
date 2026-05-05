"use client";

// CEREBRO-PATCH: SA3 — L3-marked (cannot wrap cleanly).
// Two intertwined deviations from upstream:
//   1) Replaces ProjectIcon with inline icon/color-dot rendering
//   2) Adds RestrictedLock indicator after icon, in both trigger and items
// No injection slot exists inside DropdownMenuTrigger/Item children — wrapping
// would require duplicating the entire JSX tree, eliminating upstream-tracking
// benefit. Resolution deferred to chunk 11 (likely path-alias shadow with
// cerebro-access/project-picker.tsx as the canonical fork).
import { Check, FolderKanban, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { projectListOptions } from "@multica/core/projects/queries";
import { getProjectColor } from "@multica/core/projects/config";
import { useWorkspaceId } from "@multica/core/hooks";
import type { UpdateIssueRequest } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { RestrictedLock } from "../../common/restricted-lock";

export function ProjectPicker({
  projectId,
  onUpdate,
  triggerRender,
  align = "start",
}: {
  projectId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  triggerRender?: React.ReactElement;
  align?: "start" | "center" | "end";
}) {
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const current = projects.find((p) => p.id === projectId);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        className={triggerRender ? undefined : "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden"}
        render={triggerRender}
      >
        {current?.icon ? (
          <span className="text-sm shrink-0">{current.icon}</span>
        ) : current?.color ? (
          <span className={cn("size-2.5 rounded-full shrink-0", getProjectColor(current.color).dot)} />
        ) : (
          <FolderKanban className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        {current?.access === "restricted" && <RestrictedLock className="mr-1" />}
        <span className="truncate">{current ? current.title : "No project"}</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} className="w-52">
        {projects.map((proj) => (
          <DropdownMenuItem key={proj.id} onClick={() => onUpdate({ project_id: proj.id })}>
            {proj.icon ? (
              <span className="mr-1">{proj.icon}</span>
            ) : (
              <span className={cn("size-2 rounded-full mr-1.5 shrink-0", getProjectColor(proj.color).dot)} />
            )}
            {proj.access === "restricted" && <RestrictedLock className="mr-1" />}
            <span className="truncate">{proj.title}</span>
            {proj.id === projectId && <Check className="ml-auto h-3.5 w-3.5 shrink-0" />}
          </DropdownMenuItem>
        ))}
        {projects.length > 0 && projectId && <DropdownMenuSeparator />}
        {projectId && (
          <DropdownMenuItem onClick={() => onUpdate({ project_id: null })}>
            <X className="h-3.5 w-3.5 text-muted-foreground" />
            Remove from project
          </DropdownMenuItem>
        )}
        {projects.length === 0 && (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">No projects yet</div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
