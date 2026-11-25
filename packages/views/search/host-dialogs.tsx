"use client";

import { useState } from "react";
import { toast } from "sonner";
import { useBatchDeleteIssues, useCancelIssueRun } from "@multica/core/issues/mutations";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { AutopilotDialog } from "../autopilots/components/autopilot-dialog";
import { TerminateTaskConfirmDialog } from "../issues/components/terminate-task-confirm-dialog";
import { useT } from "../i18n";
import type { PaletteHostDialog } from "./types";

/**
 * Confirms and forms a palette command opens after the palette closes. They
 * live here rather than on the page because the page that would host them
 * may not be the one on screen (a list selection, any route for "New
 * autopilot").
 */
export function PaletteHostDialogs({
  dialog,
  onClose,
}: {
  dialog: PaletteHostDialog | null;
  onClose: () => void;
}) {
  const [retained, setRetained] = useState<{
    dialog: PaletteHostDialog;
    sessionKey: number;
  } | null>(null);

  if (dialog !== null && retained?.dialog !== dialog) {
    setRetained((prev) => ({
      dialog,
      sessionKey: (prev?.sessionKey ?? 0) + 1,
    }));
  }

  const active = dialog !== null
    ? { dialog, sessionKey: retained?.dialog === dialog ? retained.sessionKey : (retained?.sessionKey ?? 0) + 1 }
    : retained;

  if (!active) return null;

  const isOpen = dialog !== null;
  const { dialog: current, sessionKey } = active;
  const handleOpenChangeComplete = (nextOpen: boolean) => {
    if (!nextOpen) {
      setRetained((prev) => (prev?.sessionKey === sessionKey ? null : prev));
    }
  };

  switch (current.type) {
    case "cancel-run":
      return (
        <CancelRunDialog
          key={sessionKey}
          open={isOpen}
          dialog={current}
          onClose={onClose}
          onOpenChangeComplete={handleOpenChangeComplete}
        />
      );
    case "batch-delete":
      return (
        <BatchDeleteDialog
          key={sessionKey}
          open={isOpen}
          dialog={current}
          onClose={onClose}
          onOpenChangeComplete={handleOpenChangeComplete}
        />
      );
    case "new-autopilot":
      return (
        <AutopilotDialog
          key={sessionKey}
          mode="create"
          open={isOpen}
          onOpenChange={(open) => !open && onClose()}
          onOpenChangeComplete={handleOpenChangeComplete}
        />
      );
  }
}

function CancelRunDialog({
  open,
  dialog,
  onClose,
  onOpenChangeComplete,
}: {
  open: boolean;
  dialog: Extract<PaletteHostDialog, { type: "cancel-run" }>;
  onClose: () => void;
  onOpenChangeComplete?: (open: boolean) => void;
}) {
  const { t } = useT("issues");
  const { t: tSearch } = useT("search");
  const cancelRun = useCancelIssueRun(dialog.issueId);
  return (
    <TerminateTaskConfirmDialog
      open={open}
      onOpenChange={(nextOpen) => !nextOpen && onClose()}
      onOpenChangeComplete={onOpenChangeComplete}
      showRunningNote={dialog.running}
      onConfirm={() => {
        onClose();
        cancelRun.mutateAsync(dialog.taskId).then(
          () => toast.success(tSearch(($) => $.toast.run_cancelled)),
          (e: unknown) =>
            toast.error(e instanceof Error ? e.message : t(($) => $.execution_log.cancel_failed)),
        );
      }}
    />
  );
}

function BatchDeleteDialog({
  open,
  dialog,
  onClose,
  onOpenChangeComplete,
}: {
  open: boolean;
  dialog: Extract<PaletteHostDialog, { type: "batch-delete" }>;
  onClose: () => void;
  onOpenChangeComplete?: (open: boolean) => void;
}) {
  const { t } = useT("issues");
  const batchDelete = useBatchDeleteIssues();
  const count = dialog.issues.length;
  const handleDelete = async () => {
    try {
      await batchDelete.mutateAsync(dialog.issues.map((issue) => issue.id));
      dialog.clearSelection();
      toast.success(t(($) => $.batch.delete_success, { count }));
    } catch (err) {
      toast.error(err instanceof Error && err.message ? err.message : t(($) => $.batch.delete_failed));
    } finally {
      onClose();
    }
  };
  return (
    <AlertDialog
      open={open}
      onOpenChange={(nextOpen) => !nextOpen && onClose()}
      onOpenChangeComplete={onOpenChangeComplete}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.batch.delete_dialog_title, { count })}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.batch.delete_dialog_desc, { count })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t(($) => $.batch.cancel)}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleDelete}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {t(($) => $.batch.delete)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
