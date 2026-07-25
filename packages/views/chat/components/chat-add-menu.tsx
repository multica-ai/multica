"use client";

import { useRef } from "react";
import { Check, FolderKanban, Image as ImageIcon, Plus, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import type { Project } from "@multica/core/types";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useT } from "../../i18n";

interface ChatAddMenuProps {
  /** Called with each selected file — the caller routes it through the
   *  editor's upload extension, same path as paste / drag-drop. */
  onSelectFile?: (file: File) => void;
  projects?: Project[];
  projectId?: string | null;
  onSelectProject?: (projectId: string | null) => void;
  /** Soft warning: the active agent's daemon is too old to receive the
   *  project description. Selection stays enabled; the submenu only appends
   *  an explanatory hint so the user knows before choosing. */
  projectContextUnsupported?: boolean;
  disabled?: boolean;
}

/**
 * The "+" affordance at the bottom-left of the chat composer. Replaces the
 * standalone paperclip button: file upload now lives here as a submenu entry,
 * leaving room for future add-actions (agents, skills, tools) under one entry
 * point without crowding the input bar.
 */
export function ChatAddMenu({
  onSelectFile,
  projects = [],
  projectId,
  onSelectProject,
  projectContextUnsupported,
  disabled,
}: ChatAddMenuProps) {
  const { t } = useT("chat");
  const inputRef = useRef<HTMLInputElement>(null);

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files ?? []);
    if (files.length === 0) return;
    e.target.value = "";
    for (const file of files) onSelectFile?.(file);
  };

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              disabled={disabled}
              aria-label={t(($) => $.input.add_tooltip)}
              title={t(($) => $.input.add_tooltip)}
              className="rounded-full text-muted-foreground"
            >
              <Plus />
            </Button>
          }
        />
        <DropdownMenuContent align="start" side="top" sideOffset={6}>
          {onSelectFile && (
            <DropdownMenuItem onClick={() => inputRef.current?.click()}>
              <ImageIcon />
              {t(($) => $.input.upload_file)}
            </DropdownMenuItem>
          )}
          {onSelectProject && (
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <FolderKanban />
                {t(($) => $.input.project_context)}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="max-h-72 min-w-52 overflow-y-auto">
                {projects.map((project) => (
                  <DropdownMenuItem
                    key={project.id}
                    onClick={() => onSelectProject(project.id)}
                  >
                    <ProjectIcon project={project} size="md" />
                    <span className="min-w-0 flex-1 truncate">{project.title}</span>
                    {project.id === projectId && <Check className="ml-auto" />}
                  </DropdownMenuItem>
                ))}
                {projects.length === 0 && (
                  <div className="px-2 py-1.5 text-xs text-muted-foreground">
                    {t(($) => $.input.no_projects)}
                  </div>
                )}
                {projectId && <DropdownMenuSeparator />}
                {projectId && (
                  <DropdownMenuItem onClick={() => onSelectProject(null)}>
                    <X />
                    {t(($) => $.input.remove_project_context)}
                  </DropdownMenuItem>
                )}
                {projectContextUnsupported && (
                  <>
                    <DropdownMenuSeparator />
                    <div className="max-w-56 px-2 py-1.5 text-xs text-muted-foreground">
                      {t(($) => $.input.project_context_unsupported)}
                    </div>
                  </>
                )}
              </DropdownMenuSubContent>
            </DropdownMenuSub>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      {onSelectFile && (
        <input
          ref={inputRef}
          type="file"
          multiple
          className="hidden"
          onChange={handleChange}
        />
      )}
    </>
  );
}
