"use client";

import { useState } from "react";
import { toast } from "sonner";
import { useT } from "@multica/i18n/react";
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

export function DeleteIssueConfirmModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const t = useT("modals");
  const c = useT("common");
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
      toast.success(t("toast_deleted"));
      onClose();
      if (navigateTo) navigation.push(navigateTo);
    } catch {
      toast.error(t("toast_failed_delete"));
      setDeleting(false);
    }
  };

  return (
    <AlertDialog open onOpenChange={(v) => { if (!v && !deleting) onClose(); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("delete_title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("delete_description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={deleting}>{c("cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleDelete}
            disabled={deleting}
            className="bg-destructive text-white hover:bg-destructive/90"
          >
            {deleting ? t("deleting") : c("delete")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
