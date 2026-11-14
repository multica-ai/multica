"use client";

import { useState } from "react";
import type { InboxItem } from "@multica/core/types";
import { useDeleteInbox } from "@multica/core/inbox/mutations";
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
import type { InboxView } from "./inbox-view";

export function InboxDeleteDialog({
  item,
  view,
  onClose,
  onDeleted,
}: {
  item: InboxItem;
  view: InboxView;
  onClose: () => void;
  onDeleted: (id: string) => void;
}) {
  const { t } = useT("inbox");
  const mutation = useDeleteInbox();
  const [failed, setFailed] = useState(false);

  const confirm = async () => {
    setFailed(false);
    try {
      await mutation.mutateAsync(item.id);
      onDeleted(item.id);
      onClose();
    } catch {
      setFailed(true);
    }
  };

  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.delete_dialog.title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {item.issue_id
              ? view === "inbox"
                ? t(($) => $.delete_dialog.active_issue_description)
                : t(($) => $.delete_dialog.issue_description)
              : t(($) => $.delete_dialog.description)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <p className="text-body font-medium wrap-anywhere">{item.title}</p>
        {failed && (
          <p role="alert" className="text-body text-destructive">
            {t(($) => $.errors.delete_failed)}
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={mutation.isPending}>{t(($) => $.delete_dialog.cancel)}</AlertDialogCancel>
          <Button
            variant="destructive"
            disabled={mutation.isPending}
            aria-busy={mutation.isPending}
            onClick={confirm}
          >
            {t(($) => $.context_menu.delete)}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
