"use client";

import { useEffect, useState, type ReactNode } from "react";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../../../i18n";
import { PlanWriteNotice } from "./plan-write-notice";
import { usePlanWriteError } from "./use-plan-write-error";

/**
 * Shared confirmation stage for every plan write that a form's save button
 * should not authorise on its own: delete phase, delete part, delete plan, and
 * the second stage of supersede. Nothing in this slice deletes or rotates a
 * plan version without going through it.
 *
 * Supersede is the one caller that is not destructive — it retains the old
 * version — so it passes `variant="default"`. It still needs this stage
 * because it changes which version is active, and its first stage is a form
 * that only collects fields.
 *
 * Failures render inline and leave the dialog open, same as the form shell: a
 * 409 on a delete means the plan moved under you, and the reader needs to see
 * that rather than a dismissed dialog and an unchanged pane.
 */
export function PlanConfirmDialog({
  open,
  onOpenChange,
  heading,
  description,
  /** What the write actually does to data — stated, never numerically guessed. */
  consequences,
  confirmLabel,
  variant = "destructive",
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  heading: string;
  description: string;
  consequences?: ReactNode;
  confirmLabel: string;
  variant?: "destructive" | "default";
  onConfirm: () => Promise<void>;
}) {
  const { t } = useT("issues");
  const { notice, report, clear } = usePlanWriteError();
  const [submitting, setSubmitting] = useState(false);

  // `clear` is a stable useCallback from usePlanWriteError, so naming it here
  // keeps the effect a once-per-open reset and needs no lint suppression.
  useEffect(() => {
    if (open) {
      setSubmitting(false);
      clear();
    }
  }, [open, clear]);

  const handleOpenChange = (next: boolean) => {
    if (submitting) return;
    onOpenChange(next);
  };

  const handleConfirm = async () => {
    setSubmitting(true);
    clear();
    try {
      await onConfirm();
      onOpenChange(false);
    } catch (err) {
      report(err);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <AlertDialog open={open} onOpenChange={handleOpenChange}>
      <AlertDialogContent
        className="w-[calc(100vw-2rem)] !max-w-[460px] gap-0 overflow-hidden rounded-lg p-0"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="px-5 pb-4 pt-5">
          <AlertDialogTitle className="font-sans text-title-sm font-semibold">
            {heading}
          </AlertDialogTitle>
          <AlertDialogDescription className="mt-1 text-left text-body leading-5 text-pretty">
            {description}
          </AlertDialogDescription>
          {consequences && (
            <div className="mt-3 rounded-md border border-dashed border-border bg-muted/30 px-3 py-2 text-caption text-muted-foreground">
              {consequences}
            </div>
          )}
          <PlanWriteNotice notice={notice} />
        </div>
        <div className="border-t bg-muted/25 px-5 py-3">
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <Button
              type="button"
              variant="outline"
              className="w-full sm:w-auto"
              onClick={() => handleOpenChange(false)}
              disabled={submitting}
            >
              {t(($) => $.plan.authoring.cancel)}
            </Button>
            <Button
              type="button"
              variant={variant}
              className="w-full sm:w-auto"
              onClick={() => void handleConfirm()}
              disabled={submitting}
            >
              {submitting ? t(($) => $.plan.authoring.saving) : confirmLabel}
            </Button>
          </div>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  );
}
