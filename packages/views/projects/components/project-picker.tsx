"use client";

import { useState } from "react";
import { Check, FolderKanban, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { projectListOptions } from "@multica/core/projects/queries";
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
  disabled = false,
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
  /** Read-only lock. When true the trigger, the menu, and the inline clear
   *  button are all disabled and out of the tab order, so no project-context
   *  mutation can fire — pointer OR keyboard. Callers that must freeze the
   *  selection during a transient window (an in-flight chat send) pass this;
   *  every other caller keeps the default hover/keyboard clear behavior since
   *  it defaults to false. */
  disabled?: boolean;
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
  // A disabled picker can never be open, and no interaction may reopen it.
  const open = disabled ? false : controlledOpen ?? internalOpen;
  const setOpen = disabled ? () => {} : onOpenChange ?? setInternalOpen;

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <div className="group/project relative inline-flex min-w-0">
        <DropdownMenuTrigger
          disabled={disabled}
          className={
            triggerRender
              ? undefined
              : cn(
                  "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden",
                  current && "pr-5",
                )
          }
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
            disabled={disabled}
            aria-label={t(($) => $.picker.remove)}
            onClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
              onUpdate({ project_id: null });
            }}
            className="pointer-events-none absolute right-1 top-1/2 flex size-3.5 -translate-y-1/2 items-center justify-center rounded-sm text-muted-foreground opacity-0 transition-[background-color,color,opacity] hover:bg-muted-foreground/20 hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none group-hover/project:pointer-events-auto group-hover/project:opacity-100 focus-visible:pointer-events-auto focus-visible:opacity-100 disabled:pointer-events-none disabled:opacity-0 disabled:group-hover/project:opacity-0"
          >
            <X className="size-2.5" />
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
