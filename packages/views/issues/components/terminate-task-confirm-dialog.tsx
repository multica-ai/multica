"use client";

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
import { useT } from "../../i18n";

// Reusable confirm step for the two issue-detail surfaces that terminate
// a single agent task — the sticky AgentLiveCard banner and the row
// action inside ExecutionLogSection. Task cancellation is irreversible
// and a misclick on a long-running run is costly, so both entry points
// route through this dialog instead of firing the cancel request on the
// first click.
//
// The dialog is fully controlled by the caller (which already owns the
// confirmCancel state alongside the in-flight cancelling state). When the
// caller signals it is currently terminating, the action button shows a
// disabled state so a second click cannot race the in-flight request.
interface TerminateTaskConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  // Running tasks may take a few seconds to fully halt on the daemon
  // side. Queued tasks cancel immediately, so we omit the note to keep
  // the copy honest.
  showRunningNote?: boolean;
}

export function TerminateTaskConfirmDialog({
  open,
  onOpenChange,
  onConfirm,
  showRunningNote = false,
}: TerminateTaskConfirmDialogProps) {
  const { t } = useT("issues");

  if (!open) return null;

  return (
    <AlertDialog open onOpenChange={onOpenChange}>
      <AlertDialogContent
        // Stop clicks inside the dialog from bubbling to the row /
        // banner underneath (the dialog can render inside a clickable
        // ancestor — e.g. an ExecutionLogSection row).
        onClick={(e) => e.stopPropagation()}
      >
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.terminate_dialog.title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.terminate_dialog.body)}
            {showRunningNote && t(($) => $.terminate_dialog.running_note)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t(($) => $.terminate_dialog.keep)}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={() => {
              onOpenChange(false);
              onConfirm();
            }}
          >
            {t(($) => $.terminate_dialog.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
