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
import { useArchiveIssue, useUnarchiveIssue } from "@multica/core/issues/mutations";
import { useT } from "../i18n";

export function ArchiveIssueConfirmModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const issueId = (data?.issueId as string) || "";
  const [archiving, setArchiving] = useState(false);
  const archiveIssue = useArchiveIssue();

  const handleArchive = async () => {
    if (!issueId) return;
    setArchiving(true);
    try {
      await archiveIssue.mutateAsync(issueId);
      toast.success(t(($) => $.archive_issue.toast_archived));
      onClose();
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.archive_issue.toast_archive_failed),
      );
      setArchiving(false);
    }
  };

  return (
    <AlertDialog open onOpenChange={(v) => { if (!v && !archiving) onClose(); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.archive_issue.title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.archive_issue.description)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={archiving}>{t(($) => $.archive_issue.cancel)}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleArchive}
            disabled={archiving}
          >
            {archiving ? t(($) => $.archive_issue.archiving) : t(($) => $.archive_issue.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function UnarchiveIssueConfirmModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const issueId = (data?.issueId as string) || "";
  const [unarchiving, setUnarchiving] = useState(false);
  const unarchiveIssue = useUnarchiveIssue();

  const handleUnarchive = async () => {
    if (!issueId) return;
    setUnarchiving(true);
    try {
      await unarchiveIssue.mutateAsync(issueId);
      toast.success(t(($) => $.archive_issue.toast_unarchived));
      onClose();
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.archive_issue.toast_unarchive_failed),
      );
      setUnarchiving(false);
    }
  };

  return (
    <AlertDialog open onOpenChange={(v) => { if (!v && !unarchiving) onClose(); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.archive_issue.unarchive_title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.archive_issue.unarchive_description)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={unarchiving}>{t(($) => $.archive_issue.cancel)}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleUnarchive}
            disabled={unarchiving}
          >
            {unarchiving ? t(($) => $.archive_issue.unarchiving) : t(($) => $.archive_issue.unarchive_confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
