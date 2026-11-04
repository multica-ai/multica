"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown } from "@multica/ui/icons";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectListOptions } from "@multica/core/projects/queries";
import type { IssueScope } from "@multica/core/issues/surface/scope";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../../i18n";
import { IssueSurface } from "../../issues/surface/issue-surface";
import { ProjectIcon } from "../../projects/components/project-icon";

/**
 * The columns share the dialog's width instead of keeping the board's fixed
 * 280px each, so every status shows without a sideways scroll and only the
 * cards scroll, down. The overrides win over the column's inline width.
 */
const FIT_COLUMNS = "[&_.group\\/column]:w-0! [&_.group\\/column]:min-w-0! [&_.group\\/column]:flex-1!";

/**
 * The kanban board in a dialog, for one project or for every issue. It opens on
 * the project the company page is on, and the picker switches it.
 */
export function CompanyBoardDialog({
  open,
  initialProjectId,
  onClose,
}: {
  open: boolean;
  /** The project the company page is scoped to, if any. */
  initialProjectId?: string;
  onClose: () => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const [projectId, setProjectId] = useState<string | null>(initialProjectId ?? null);
  const current = projects.find((project) => project.id === projectId);
  const scope: IssueScope = projectId ? { type: "project", projectId } : { type: "workspace" };

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="flex h-[92dvh] flex-col gap-0 overflow-hidden p-0 sm:max-w-[min(96vw,120rem)]">
        <header className="flex shrink-0 items-center gap-3 border-b px-4 py-3 pr-12">
          <DialogTitle className="text-heading">{t(($) => $.company.board_title)}</DialogTitle>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={<Button type="button" variant="outline" size="sm" aria-label={t(($) => $.company.board_project)} />}
            >
              {current ? <ProjectIcon project={current} size="sm" /> : null}
              <span className="max-w-60 truncate">{current?.title ?? t(($) => $.company.board_all_projects)}</span>
              <ChevronDown className="size-3.5 text-muted-foreground" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="max-h-80 min-w-56 overflow-y-auto">
              <DropdownMenuItem onClick={() => setProjectId(null)}>{t(($) => $.company.board_all_projects)}</DropdownMenuItem>
              {projects.map((project) => (
                <DropdownMenuItem key={project.id} onClick={() => setProjectId(project.id)}>
                  <ProjectIcon project={project} size="sm" />
                  <span className="min-w-0 flex-1 truncate">{project.title}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        </header>
        <div className={`flex min-h-0 flex-1 flex-col ${FIT_COLUMNS}`}>
          {/* A new key gives each project its own surface and data, rather than a mix of both. */}
          <IssueSurface
            key={projectId ?? "all"}
            scope={scope}
            modes={["board"]}
            surfaceKey="company-board"
            savedViews={false}
            batchToolbar="never"
          />
        </div>
      </DialogContent>
    </Dialog>
  );
}
