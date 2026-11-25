"use client";

import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

/**
 * Confirmation dialog for "Archive all". Archiving everything in the live Inbox
 * clears unread items in one shot; asking once keeps a stray menu click from
 * wiping the queue, while leaving Undo and the Archived tab as recovery.
 */
export function InboxArchiveAllDialog({
  open,
  count,
  onClose,
  onConfirm,
}: {
  open: boolean;
  /** Total number of unfiltered active inbox rows (0 falls back to a count-less description). */
  count: number;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const { t } = useT("inbox");

  return (
    <AlertDialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.archive_all_dialog.title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {count > 0
              ? t(($) => $.archive_all_dialog.description_count, { count })
              : t(($) => $.archive_all_dialog.description)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t(($) => $.archive_all_dialog.cancel)}</AlertDialogCancel>
          <Button
            variant="destructive"
            onClick={() => {
              onClose();
              onConfirm();
            }}
          >
            {t(($) => $.archive_all_dialog.confirm)}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
