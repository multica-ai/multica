"use client";

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
  if (!dialog) return null;
  switch (dialog.type) {
    case "cancel-run":
      return <CancelRunDialog dialog={dialog} onClose={onClose} />;
    case "batch-delete":
      return <BatchDeleteDialog dialog={dialog} onClose={onClose} />;
    case "new-autopilot":
      return <AutopilotDialog mode="create" open onOpenChange={(open) => !open && onClose()} />;
  }
}

function CancelRunDialog({
  dialog,
  onClose,
}: {
  dialog: Extract<PaletteHostDialog, { type: "cancel-run" }>;
  onClose: () => void;
}) {
  const { t } = useT("issues");
  const { t: tSearch } = useT("search");
  const cancelRun = useCancelIssueRun(dialog.issueId);
  return (
    <TerminateTaskConfirmDialog
      open
      onOpenChange={(open) => !open && onClose()}
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
  dialog,
  onClose,
}: {
  dialog: Extract<PaletteHostDialog, { type: "batch-delete" }>;
  onClose: () => void;
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
    <AlertDialog open onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.batch.delete_dialog_title, { count })}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.batch.delete_dialog_desc, { count })}
            <span className="mt-2 block text-caption text-muted-foreground">
              {t(($) => $.batch.delete_dialog_warning)}
            </span>
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
