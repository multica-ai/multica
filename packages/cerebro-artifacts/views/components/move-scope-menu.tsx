"use client";

import { useQuery } from "@tanstack/react-query";
import { Move, Folder, Globe, Check } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { projectListOptions } from "@multica/core/projects/queries";
import { useMoveArtifact } from "@multica/cerebro-artifacts/core";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Artifact, UpdateArtifactScopeRequest } from "@multica/core/types";

/**
 * Lets the author/admin move an artifact between scopes:
 *  - workspace (clear both ids)
 *  - any project in the workspace (set project_id)
 *  - leave issue scope as-is (no UI for moving to a specific issue — agents
 *    use MCP for that since picking an issue from a dropdown is rarely the
 *    right UX).
 */
export function MoveScopeMenu({ artifact }: { artifact: Artifact }) {
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const move = useMoveArtifact();

  const moveTo = (data: UpdateArtifactScopeRequest) => {
    move.mutate({ artifact, data });
  };

  const currentProjectId = artifact.project_id ?? null;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" size="sm" className="max-sm:px-2" disabled={move.isPending} />
        }
      >
        <Move className="size-4 sm:mr-1" /><span className="hidden sm:inline">Move</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel>Move to…</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onClick={() => moveTo({})}
          disabled={
            artifact.project_id === null && artifact.issue_id === null
          }
        >
          <Globe className="mr-2 size-4" />
          Workspace
          {artifact.project_id === null && artifact.issue_id === null && (
            <Check className="ml-auto size-4" />
          )}
        </DropdownMenuItem>
        {projects.length > 0 && <DropdownMenuSeparator />}
        {projects.map((p) => {
          const isCurrent = currentProjectId === p.id;
          return (
            <DropdownMenuItem
              key={p.id}
              onClick={() => moveTo({ project_id: p.id })}
              disabled={isCurrent}
            >
              <Folder className="mr-2 size-4" />
              <span className="truncate">{p.title}</span>
              {isCurrent && <Check className="ml-auto size-4" />}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
