"use client";

import { Check, FolderKanban, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { projectListOptions } from "@multica/core/projects/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import type { UpdateIssueRequest } from "@multica/core/types";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";

export function ProjectPicker({
  projectId,
  onUpdate,
}: {
  projectId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
}) {
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const current = projects.find((p) => p.id === projectId);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger className="flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden">
        <FolderKanban className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate">{current ? current.title : "No project"}</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-52">
        {projects.map((p) => (
          <DropdownMenuItem key={p.id} onClick={() => onUpdate({ project_id: p.id })}>
            <span className="mr-1">{p.icon || "📁"}</span>
            <span className="truncate">{p.title}</span>
            {p.id === projectId && <Check className="ml-auto h-3.5 w-3.5 shrink-0" />}
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
