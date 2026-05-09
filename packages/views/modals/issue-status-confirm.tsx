"use client";

import { useEffect, useState } from "react";
import type { IssueStatus } from "@multica/core/types";
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
import { useT } from "../i18n";

type ConfirmCallback = () => void | Promise<void>;

function isConfirmableStatus(value: unknown): value is Extract<IssueStatus, "cancelled" | "archive"> {
  return value === "cancelled" || value === "archive";
}

export function IssueStatusConfirmModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const status = data?.status;
  const count = typeof data?.count === "number" ? data.count : 1;
  const onConfirm = data?.onConfirm;
  const [confirming, setConfirming] = useState(false);
  const valid = isConfirmableStatus(status) && typeof onConfirm === "function";

  useEffect(() => {
    if (!valid) onClose();
  }, [onClose, valid]);

  if (!valid) return null;

  const handleConfirm = async () => {
    setConfirming(true);
    try {
      await (onConfirm as ConfirmCallback)();
      onClose();
    } catch {
      setConfirming(false);
    }
  };

  const description =
    count > 1
      ? status === "cancelled"
        ? t(($) => $.issue_status_confirm.description_cancelled_batch, { count })
        : t(($) => $.issue_status_confirm.description_archive_batch, { count })
      : status === "cancelled"
        ? t(($) => $.issue_status_confirm.description_cancelled)
        : t(($) => $.issue_status_confirm.description_archive);

  return (
    <AlertDialog open onOpenChange={(v) => { if (!v && !confirming) onClose(); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.issue_status_confirm.title)}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={confirming}>
            {t(($) => $.issue_status_confirm.cancel)}
          </AlertDialogCancel>
          <AlertDialogAction onClick={handleConfirm} disabled={confirming}>
            {confirming
              ? t(($) => $.issue_status_confirm.confirming)
              : t(($) => $.issue_status_confirm.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
