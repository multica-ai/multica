"use client";

import * as React from "react";
import { Check, Folder as FolderIcon, Search } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import type { ArtifactFolder } from "@multica/core/types";

// FIR-4163: "Move to" used to be a flat, unsearchable list of every folder,
// with no indication of nesting — two folders called "2026" were
// indistinguishable. This picker shows each folder with its full path, is
// filterable by name or path, and is shared by Documents and Notes so both
// surfaces move a file the same way.

export interface FolderChoice {
  folder: ArtifactFolder;
  depth: number;
  /** Full path including the folder itself, e.g. "Finance / 2026 / Q1". */
  path: string;
}

/**
 * Folders as a flat, depth-ordered list with a readable path on each entry.
 */
export function buildFolderChoices(folders: ArtifactFolder[]): FolderChoice[] {
  const out: FolderChoice[] = [];
  const walk = (parentId: string | null, depth: number, prefix: string) => {
    folders
      .filter((f) => (f.parent_id ?? null) === parentId)
      .sort((a, b) => a.name.localeCompare(b.name))
      .forEach((f) => {
        const path = prefix ? `${prefix} / ${f.name}` : f.name;
        out.push({ folder: f, depth, path });
        walk(f.id, depth + 1, path);
      });
  };
  walk(null, 0, "");
  return out;
}

export interface FolderMoveDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  folders: ArtifactFolder[];
  /** What is being moved, e.g. a document title or "3 items". */
  subject: string;
  /** Folder the subject sits in today, so it can be marked. */
  currentFolderId?: string | null;
  /** Label for the root destination, e.g. "All documents". */
  rootLabel: string;
  onMove: (folderId: string | null) => void;
}

export function FolderMoveDialog({
  open,
  onOpenChange,
  folders,
  subject,
  currentFolderId = null,
  rootLabel,
  onMove,
}: FolderMoveDialogProps) {
  const [filter, setFilter] = React.useState("");
  const [pending, setPending] = React.useState<string | null | undefined>(
    undefined,
  );

  React.useEffect(() => {
    if (!open) {
      setFilter("");
      setPending(undefined);
    }
  }, [open]);

  const choices = React.useMemo(() => buildFolderChoices(folders), [folders]);
  const needle = filter.trim().toLowerCase();
  const matches = React.useMemo(
    () =>
      needle
        ? choices.filter((c) => c.path.toLowerCase().includes(needle))
        : choices,
    [choices, needle],
  );
  const rootMatches = !needle || rootLabel.toLowerCase().includes(needle);

  // undefined = nothing picked yet, so the current folder is the selection.
  const selected = pending === undefined ? currentFolderId : pending;

  const confirm = () => {
    if (selected === currentFolderId) {
      onOpenChange(false);
      return;
    }
    onMove(selected ?? null);
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Move</DialogTitle>
          <DialogDescription className="truncate">{subject}</DialogDescription>
        </DialogHeader>
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            autoFocus
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Search folders…"
            className="h-9 pl-8"
          />
        </div>
        <div
          role="listbox"
          aria-label="Destination folder"
          className="max-h-72 overflow-y-auto rounded-md border border-border"
        >
          {rootMatches && (
            <FolderRow
              label={rootLabel}
              depth={0}
              selected={selected === null}
              onSelect={() => setPending(null)}
            />
          )}
          {matches.map((c) => (
            <FolderRow
              key={c.folder.id}
              label={c.folder.name}
              hint={c.depth > 0 ? c.path : undefined}
              depth={c.depth + 1}
              selected={selected === c.folder.id}
              onSelect={() => setPending(c.folder.id)}
            />
          ))}
          {!rootMatches && matches.length === 0 && (
            <div className="px-3 py-6 text-center text-sm text-muted-foreground">
              No folder matches “{filter.trim()}”.
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={confirm} disabled={selected === currentFolderId}>
            Move here
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function FolderRow({
  label,
  hint,
  depth,
  selected,
  onSelect,
}: {
  label: string;
  hint?: string;
  depth: number;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      role="option"
      aria-selected={selected}
      onClick={onSelect}
      onDoubleClick={onSelect}
      className={cn(
        // 44px min height keeps the row a comfortable touch target on mobile.
        "flex min-h-11 w-full items-center gap-2 border-b border-border/50 px-3 py-2 text-left text-sm last:border-b-0 hover:bg-accent/50",
        selected && "bg-accent",
      )}
    >
      <span style={{ paddingLeft: depth * 12 }} className="flex shrink-0">
        <FolderIcon className="size-4 text-muted-foreground" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate font-medium">{label}</span>
        {hint && (
          <span
            className={cn(
              "block truncate text-xs",
              // muted-foreground on the accent fill measures 4.39:1 — just under
              // the 4.5:1 AA floor for text this size. On the selected row the
              // path switches to a dimmed accent-foreground (8.8:1) instead.
              selected
                ? "text-accent-foreground/80"
                : "text-muted-foreground",
            )}
          >
            {hint}
          </span>
        )}
      </span>
      {selected && <Check className="size-4 shrink-0 text-primary" />}
    </button>
  );
}
