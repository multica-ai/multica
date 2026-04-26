"use client";

import { AlertTriangle } from "lucide-react";
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
import { StatusIcon } from "./status-icon";
import type { IssueBlocker, IssueStatus } from "@multica/core/types";

/**
 * Soft-warning dialog shown when the user attempts to move an issue to an
 * active status while it has open incoming `blocks` links. The user can
 * proceed past the warning (soft enforcement, per design decision) or cancel.
 *
 * The list of blockers is computed by the parent from `issue.links` (filtered
 * for incoming + blocks + open status), so the dialog is purely presentational.
 */
export function BlockedWarning({
  open,
  onOpenChange,
  blockers,
  onConfirm,
  targetStatusLabel,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  blockers: IssueBlocker[];
  onConfirm: () => void;
  /** Human-readable name of the status the user is trying to move to. */
  targetStatusLabel: string;
}) {
  const count = blockers.length;
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <AlertTriangle className="h-4 w-4 text-amber-500" />
            This issue is blocked
          </AlertDialogTitle>
          <AlertDialogDescription>
            {count === 1
              ? "There is 1 open issue blocking this one."
              : `There are ${count} open issues blocking this one.`}{" "}
            You can still move it to <span className="font-medium">{targetStatusLabel}</span>, but
            the blockers below haven't been resolved yet.
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className="my-2 max-h-48 overflow-y-auto rounded-md border bg-muted/40 p-2">
          <ul className="flex flex-col gap-1">
            {blockers.map((b) => (
              <li
                key={b.blocker_issue_id}
                className="flex min-w-0 items-center gap-1.5 text-xs"
              >
                <StatusIcon status={b.blocker_status as IssueStatus} className="h-3.5 w-3.5 shrink-0" />
                <span className="font-mono text-[11px] text-muted-foreground shrink-0">
                  {b.blocker_identifier}
                </span>
                <span className="truncate">{b.blocker_title}</span>
              </li>
            ))}
          </ul>
        </div>

        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction onClick={onConfirm}>
            Move anyway
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
