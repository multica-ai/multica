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

type PresentedHostDialog = {
  dialog: PaletteHostDialog;
  id: number;
};

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
  // Keep the closing dialog mounted until Base UI finishes its exit animation.
  const [presented, setPresented] = useState<PresentedHostDialog | null>(() =>
    dialog ? { dialog, id: 1 } : null,
  );
  if (dialog && (!presented || presented.dialog !== dialog)) {
    setPresented({ dialog, id: (presented?.id ?? 0) + 1 });
  }

  if (!presented) return null;

  const isOpen = dialog !== null;
  const clearAfterExit = () => {
    setPresented((current) => (current?.id === presented.id && dialog === null ? null : current));
  };

  switch (presented.dialog.type) {
    case "cancel-run":
      return (
        <CancelRunDialog
          key={presented.id}
          open={isOpen}
          dialog={presented.dialog}
          onClose={onClose}
          onExitComplete={clearAfterExit}
        />
      );
    case "batch-delete":
      return (
        <BatchDeleteDialog
          key={presented.id}
          open={isOpen}
          dialog={presented.dialog}
          onClose={onClose}
          onExitComplete={clearAfterExit}
        />
      );
    case "new-autopilot":
      return (
        <AutopilotDialog
          key={presented.id}
          mode="create"
          open={isOpen}
          onOpenChange={(open) => !open && onClose()}
          onOpenChangeComplete={(open) => !open && clearAfterExit()}
        />
      );
  }
}

function CancelRunDialog({
  open,
  dialog,
  onClose,
  onExitComplete,
}: {
  open: boolean;
  dialog: Extract<PaletteHostDialog, { type: "cancel-run" }>;
  onClose: () => void;
  onExitComplete: () => void;
}) {
  const { t } = useT("issues");
  const { t: tSearch } = useT("search");
  const cancelRun = useCancelIssueRun(dialog.issueId);
  return (
    <TerminateTaskConfirmDialog
      open={open}
      onOpenChange={(nextOpen) => !nextOpen && onClose()}
      onOpenChangeComplete={(nextOpen) => !nextOpen && onExitComplete()}
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
  onExitComplete,
}: {
  open: boolean;
  dialog: Extract<PaletteHostDialog, { type: "batch-delete" }>;
  onClose: () => void;
  onExitComplete: () => void;
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
      onOpenChangeComplete={(nextOpen) => !nextOpen && onExitComplete()}
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
