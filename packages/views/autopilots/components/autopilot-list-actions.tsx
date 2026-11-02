"use client";

import { useState } from "react";
import {
  ExternalLink,
  Loader2,
  Pause,
  Play,
  Trash2,
} from "@multica/ui/icons";
import { toast } from "sonner";
import type { Autopilot } from "@multica/core/types";
import {
  useDeleteAutopilot,
  useUpdateAutopilot,
} from "@multica/core/autopilots";
import { useWorkspacePaths } from "@multica/core/paths";
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
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../../i18n";
import { useIntentNavigate } from "../../navigation";
import {
  CollectionBatchBar,
  CollectionRowMenu,
} from "../../layout/collection-list";

// Delete dialog — single row (kebab) and batch share one implementation.

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
        <p className="text-caption text-muted-foreground">
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

// Status helpers shared by kebab and batch toolbar.

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

// Row menu — the ⋯ button and right-click share these items.

export function AutopilotRowActions({ row }: { row: Autopilot }) {
  const { t } = useT("autopilots");
  const { t: tCommon } = useT("common");
  const wsPaths = useWorkspacePaths();
  const intentNavigate = useIntentNavigate();
  const [deleteOpen, setDeleteOpen] = useState(false);
  const setStatus = useSetStatus();

  // Apart from the navigation entry the kebab only holds write actions
  // (pause/resume/delete), so hide it entirely for members without write
  // access — they still reach a new tab through a modified row click. An
  // absent can_write (older server) keeps the menu visible and lets the
  // backend remain the gate.
  if (row.can_write === false) return null;

  return (
    <>
      <CollectionRowMenu label={t(($) => $.actions.row_menu)}>
        <DropdownMenuItem
          onClick={() =>
            intentNavigate(
              wsPaths.autopilotDetail(row.id),
              "foreground-tab",
              row.title,
            )
          }
        >
          <ExternalLink className="size-3.5" />
          {tCommon(($) => $.navigation.open_in_new_tab)}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
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
      </CollectionRowMenu>
      <DeleteAutopilotsDialog
        rows={[row]}
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
      />
    </>
  );
}

// Batch toolbar

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

  const anyActive = rows.some((r) => r.status === "active");
  const anyPaused = rows.some((r) => r.status === "paused");

  return (
    <>
      <CollectionBatchBar count={rows.length} onClear={onClear}>
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
      </CollectionBatchBar>

      <DeleteAutopilotsDialog
        rows={rows}
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        onDeleted={onClear}
      />
    </>
  );
}
