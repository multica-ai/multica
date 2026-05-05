"use client";

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
import { RestrictedDot } from "../../common/restricted-dot";

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
        {current?.access === "restricted" && <RestrictedDot className="mr-1" />}
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
            {proj.access === "restricted" && <RestrictedDot className="mr-1" />}
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
