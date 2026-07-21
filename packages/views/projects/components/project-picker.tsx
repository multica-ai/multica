"use client";

import { useState } from "react";
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
import { ProjectIcon } from "./project-icon";
import { useT } from "../../i18n";

export function ProjectPicker({
  projectId,
  onUpdate,
  triggerRender,
  align = "start",
  defaultOpen = false,
  open: controlledOpen,
  onOpenChange,
}: {
  projectId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  triggerRender?: React.ReactElement;
  align?: "start" | "center" | "end";
  /** Open the dropdown on first mount. Used by progressive-disclosure
   *  sidebars so a newly-added field immediately enters edit state. */
  defaultOpen?: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const current = projects.find((p) => p.id === projectId);
  // Normalize to an always-boolean controlled `open`, matching the other
  // pickers (status/priority/assignee/labels). Base UI's Menu latches a
  // controlled `open={true}` — a later `undefined` does NOT close it — so
  // callers wiring `open={cond ? true : undefined}` (create-issue dialog)
  // would leave the popup stuck open after selecting a project.
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  const open = controlledOpen ?? internalOpen;
  const setOpen = onOpenChange ?? setInternalOpen;

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <div className="group/project relative inline-flex min-w-0">
        <DropdownMenuTrigger
          className={triggerRender ? undefined : "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden"}
          render={triggerRender}
        >
          {current ? (
            <ProjectIcon project={current} size="sm" />
          ) : (
            <FolderKanban className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          )}
          <span className="truncate">{current ? current.title : t(($) => $.picker.no_project)}</span>
        </DropdownMenuTrigger>
        {current && (
          <button
            type="button"
            aria-label={t(($) => $.picker.remove)}
            onClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
              onUpdate({ project_id: null });
            }}
            className="pointer-events-none absolute inset-y-0 right-0 flex w-7 items-center justify-center rounded-r-full bg-background/95 text-muted-foreground opacity-0 transition-opacity hover:text-foreground group-hover/project:pointer-events-auto group-hover/project:opacity-100 focus-visible:pointer-events-auto focus-visible:opacity-100"
          >
            <X className="size-3" />
          </button>
        )}
      </div>
      <DropdownMenuContent align={align} className="w-52">
        {projects.map((p) => (
          <DropdownMenuItem key={p.id} onClick={() => onUpdate({ project_id: p.id })}>
            <ProjectIcon project={p} size="md" className="mr-1" />
            <span className="truncate">{p.title}</span>
            {p.id === projectId && <Check className="ml-auto h-3.5 w-3.5 shrink-0" />}
          </DropdownMenuItem>
        ))}
        {projects.length > 0 && projectId && <DropdownMenuSeparator />}
        {projectId && (
          <DropdownMenuItem onClick={() => onUpdate({ project_id: null })}>
            <X className="h-3.5 w-3.5 text-muted-foreground" />
            {t(($) => $.picker.remove)}
          </DropdownMenuItem>
        )}
        {projects.length === 0 && (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">{t(($) => $.picker.empty)}</div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
