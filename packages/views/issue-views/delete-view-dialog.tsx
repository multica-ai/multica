"use client";

import { toast } from "sonner";
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
import type { IssueView } from "@multica/core/api/schemas";
import { useT } from "../i18n";

/** Deletes the view only — never the issues it lists. */
export function DeleteViewDialog({
  view,
  onOpenChange,
  onConfirm,
}: {
  view: IssueView | null;
  onOpenChange: (open: boolean) => void;
  onConfirm: (view: IssueView) => Promise<void>;
}) {
  const { t } = useT("issues");
  return (
    <AlertDialog open={!!view} onOpenChange={(open) => !open && onOpenChange(false)}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.views.delete_title)}</AlertDialogTitle>
          <AlertDialogDescription className="break-words">
            {view?.visibility === "workspace"
              ? t(($) => $.views.delete_description_shared, { name: view.name })
              : t(($) => $.views.delete_description, { name: view?.name ?? "" })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t(($) => $.views.cancel)}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={() => {
              const target = view;
              onOpenChange(false);
              if (!target) return;
              void onConfirm(target).then(
                () => toast.success(t(($) => $.views.toast_deleted)),
                () => toast.error(t(($) => $.views.toast_delete_failed)),
              );
            }}
          >
            {t(($) => $.views.delete)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
