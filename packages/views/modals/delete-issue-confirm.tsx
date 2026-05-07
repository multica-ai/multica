"use client";

import { useState } from "react";
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
import { useDeleteIssue } from "@multica/core/issues/mutations";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";

export function DeleteIssueConfirmModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const issueId = (data?.issueId as string) || "";
  const navigateTo = (data?.onDeletedNavigateTo as string | undefined) || undefined;
  const [deleting, setDeleting] = useState(false);
  const deleteIssue = useDeleteIssue();
  const navigation = useNavigation();

  const handleDelete = async () => {
    if (!issueId) return;
    setDeleting(true);
    try {
      await deleteIssue.mutateAsync(issueId);
      toast.success(t(($) => $.delete_issue.toast_deleted));
      onClose();
      if (navigateTo) navigation.push(navigateTo);
    } catch {
      toast.error(t(($) => $.delete_issue.toast_delete_failed));
      setDeleting(false);
    }
  };

  return (
    <AlertDialog open onOpenChange={(v) => { if (!v && !deleting) onClose(); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.delete_issue.title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.delete_issue.description)}
            <span className="mt-2 block text-xs text-muted-foreground/80">
              {t(($) => $.delete_issue.hint)}
            </span>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={deleting}>{t(($) => $.delete_issue.cancel)}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleDelete}
            disabled={deleting}
            className="bg-destructive text-white hover:bg-destructive/90"
          >
            {deleting ? t(($) => $.delete_issue.deleting) : t(($) => $.delete_issue.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
