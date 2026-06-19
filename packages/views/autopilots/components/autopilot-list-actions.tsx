"use client";

import { useState } from "react";
import {
  Loader2,
  MoreHorizontal,
  Pause,
  Play,
  Trash2,
  X,
} from "lucide-react";
import { toast } from "sonner";
import type { Autopilot } from "@multica/core/types";
import {
  useDeleteAutopilot,
  useUpdateAutopilot,
} from "@multica/core/autopilots";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../../i18n";

// ---------------------------------------------------------------------------
// Delete dialog — single row (kebab) and batch share one implementation.
// ---------------------------------------------------------------------------

export function DeleteAutopilotsDialog({
  rows,
  open,
  onOpenChange,
  onDeleted,
}: {
  rows: Autopilot[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDeleted?: () => void;
}) {
  const { t } = useT("autopilots");
  const deleteAutopilot = useDeleteAutopilot();
  const [deleting, setDeleting] = useState(false);

  const handleDelete = async () => {
    setDeleting(true);
    try {
      for (const row of rows) {
        await deleteAutopilot.mutateAsync(row.id);
      }
      onOpenChange(false);
      onDeleted?.();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err));
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {t(($) => $.actions.delete_dialog.title)}
          </DialogTitle>
          <DialogDescription>
            {t(($) => $.actions.delete_dialog.description, {
              count: rows.length,
              name: rows[0]?.title ?? "",
            })}
          </DialogDescription>
        </DialogHeader>
        <p className="text-xs text-muted-foreground">
          {t(($) => $.actions.delete_dialog.warning)}
        </p>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={deleting}
            onClick={() => onOpenChange(false)}
          >
            {t(($) => $.actions.delete_dialog.cancel)}
          </Button>
          <Button
            type="button"
            variant="destructive"
            size="sm"
            disabled={deleting}
            onClick={handleDelete}
          >
            {deleting ? (
              <>
                <Loader2 className="mr-1 size-3.5 animate-spin" />
                {t(($) => $.actions.delete_dialog.deleting)}
              </>
            ) : (
              t(($) => $.actions.delete_dialog.confirm)
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Status helpers shared by kebab and batch toolbar.
// ---------------------------------------------------------------------------

function useSetStatus() {
  const updateAutopilot = useUpdateAutopilot();
  return async (rows: Autopilot[], status: "active" | "paused") => {
    try {
      for (const row of rows) {
        if (row.status === status) continue;
        await updateAutopilot.mutateAsync({ id: row.id, status });
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err));
    }
  };
}

// ---------------------------------------------------------------------------
// Row kebab — the row is a plain `<div>` whose whole-row navigation is a mouse
// `onClick` (see `useRowLink`), not an ancestor `<a>`. The wrapper span stops
// click propagation so opening this menu never navigates the row — just
// stopPropagation, no preventDefault (same pattern as the skills list).
// ---------------------------------------------------------------------------

export function AutopilotRowActions({ row }: { row: Autopilot }) {
  const { t } = useT("autopilots");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const setStatus = useSetStatus();

  return (
    <span
      onClick={(e) => e.stopPropagation()}
      className="flex items-center"
    >
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              aria-label={t(($) => $.actions.row_menu)}
              className="flex size-7 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-accent-foreground group-hover/row:opacity-100 data-popup-open:bg-accent data-popup-open:opacity-100 data-popup-open:text-accent-foreground"
            >
              <MoreHorizontal className="size-4" />
            </button>
          }
        />
        <DropdownMenuContent align="end" className="w-48">
          {row.status === "active" && (
            <DropdownMenuItem onClick={() => setStatus([row], "paused")}>
              <Pause className="size-3.5" />
              {t(($) => $.actions.pause)}
            </DropdownMenuItem>
          )}
          {row.status === "paused" && (
            <DropdownMenuItem onClick={() => setStatus([row], "active")}>
              <Play className="size-3.5" />
              {t(($) => $.actions.resume)}
            </DropdownMenuItem>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem
            variant="destructive"
            onClick={() => setDeleteOpen(true)}
          >
            <Trash2 className="size-3.5" />
            {t(($) => $.actions.delete)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <DeleteAutopilotsDialog
        rows={[row]}
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
      />
    </span>
  );
}

// ---------------------------------------------------------------------------
// Batch toolbar
// ---------------------------------------------------------------------------

export function AutopilotBatchToolbar({
  rows,
  onClear,
}: {
  rows: Autopilot[];
  onClear: () => void;
}) {
  const { t } = useT("autopilots");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const setStatus = useSetStatus();

  if (rows.length === 0) return null;

  const anyActive = rows.some((r) => r.status === "active");
  const anyPaused = rows.some((r) => r.status === "paused");

  return (
    <>
      {/* Anchored to the page root (relative), NOT the viewport — see the
          skills batch toolbar for the rationale. */}
      <div className="absolute bottom-6 left-1/2 z-50 flex -translate-x-1/2 items-center gap-1 rounded-lg border bg-background px-2 py-1.5 shadow-lg">
        <div className="mr-1 flex items-center gap-1.5 border-r pl-1 pr-2">
          <span className="text-sm font-medium">
            {t(($) => $.actions.selected, { count: rows.length })}
          </span>
          <button
            type="button"
            aria-label={t(($) => $.actions.clear_selection)}
            onClick={onClear}
            className="rounded p-0.5 transition-colors hover:bg-accent"
          >
            <X className="size-3.5 text-muted-foreground" />
          </button>
        </div>

        {anyActive && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setStatus(rows, "paused")}
          >
            <Pause className="mr-1 size-3.5" />
            {t(($) => $.actions.pause)}
          </Button>
        )}
        {anyPaused && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setStatus(rows, "active")}
          >
            <Play className="mr-1 size-3.5" />
            {t(($) => $.actions.resume)}
          </Button>
        )}
        <Button
          variant="ghost"
          size="sm"
          className="text-destructive hover:text-destructive"
          onClick={() => setDeleteOpen(true)}
        >
          <Trash2 className="mr-1 size-3.5" />
          {t(($) => $.actions.delete)}
        </Button>
      </div>

      <DeleteAutopilotsDialog
        rows={rows}
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        onDeleted={onClear}
      />
    </>
  );
}
