"use client";

import * as React from "react";
import { ChevronDown, ChevronRight, Folder } from "lucide-react";
import type { ArtifactFolder } from "@multica/core/types/artifact";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";

/**
 * The folders from the root down to `folderId`, in that order. Empty when the
 * item sits outside every folder. A folder whose parent has been deleted — or
 * a cycle, which the API should prevent but a stale cache can still show —
 * ends the walk instead of hanging the render.
 */
export function folderPathChain(
  folders: ArtifactFolder[],
  folderId: string | null | undefined,
): ArtifactFolder[] {
  const byId = new Map(folders.map((f) => [f.id, f]));
  const chain: ArtifactFolder[] = [];
  const seen = new Set<string>();
  let current = folderId ? byId.get(folderId) : undefined;
  while (current && !seen.has(current.id)) {
    seen.add(current.id);
    chain.unshift(current);
    current = current.parent_id ? byId.get(current.parent_id) : undefined;
  }
  return chain;
}

/**
 * FIR-4028 slice 8 — the folder as a full path at the top of the note, e.g.
 * "All notes › Firtal › Q3 planning", replacing the single-name badge that only
 * ever showed the innermost folder. Each crumb navigates when the surface can
 * navigate (the Notes page scopes its list; the inbox pane cannot, and then the
 * path is plain text). `children` is the folder tree shown behind the chevron —
 * omit it for a reader who may not move the item.
 */
export function FolderBreadcrumb({
  folders,
  folderId,
  rootLabel,
  onOpenFolder,
  className,
  children,
}: {
  folders: ArtifactFolder[];
  folderId: string | null | undefined;
  /** Label for the "outside every folder" crumb, e.g. "All notes". */
  rootLabel: string;
  onOpenFolder?: (folderId: string | null) => void;
  className?: string;
  /** The folder tree, rendered inside the chevron's dropdown. */
  children?: React.ReactNode;
}) {
  const chain = folderPathChain(folders, folderId);
  const crumbs: Array<{ id: string | null; label: string }> = [
    { id: null, label: rootLabel },
    ...chain.map((f) => ({ id: f.id as string | null, label: f.name })),
  ];

  return (
    <nav
      aria-label="Folder path"
      className={cn(
        "flex min-w-0 items-center gap-0.5 text-xs text-muted-foreground",
        className,
      )}
    >
      <Folder className="mr-1 size-3 shrink-0" aria-hidden="true" />
      {crumbs.map((crumb, index) => (
        <React.Fragment key={crumb.id ?? "__root__"}>
          {index > 0 && (
            <ChevronRight className="size-3 shrink-0" aria-hidden="true" />
          )}
          {onOpenFolder ? (
            <button
              type="button"
              data-testid="folder-crumb"
              onClick={() => onOpenFolder(crumb.id)}
              title={crumb.label}
              className="max-w-[10rem] truncate rounded px-1 py-0.5 hover:bg-accent/40 hover:text-foreground"
            >
              {crumb.label}
            </button>
          ) : (
            <span
              data-testid="folder-crumb"
              title={crumb.label}
              className="max-w-[10rem] truncate px-1 py-0.5"
            >
              {crumb.label}
            </span>
          )}
        </React.Fragment>
      ))}
      {children && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                size="icon-sm"
                variant="ghost"
                aria-label="Change folder"
                className="size-6 shrink-0"
              >
                <ChevronDown className="size-3.5" />
              </Button>
            }
          />
          <DropdownMenuContent align="start" className="w-56">
            {children}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </nav>
  );
}
