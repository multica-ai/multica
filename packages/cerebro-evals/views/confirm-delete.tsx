"use client";

import type { ReactElement } from "react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@multica/ui/components/ui/alert-dialog";

// ConfirmDelete is the one confirmation used by every irreversible action in the
// eval catalog and detail screens — deleting an eval and retiring a version.
// Those used to call the browser's native confirm(), which cannot be themed,
// ignores the design tokens, and renders as an OS-level dialog in the Electron
// desktop app. AlertDialog is the pattern the rest of the workspace uses.
export function ConfirmDelete({ trigger, title, description, actionLabel, onConfirm, pending }: {
  trigger: ReactElement;
  title: string;
  description: string;
  actionLabel: string;
  onConfirm: () => void;
  pending?: boolean;
}) {
  return (
    <AlertDialog>
      <AlertDialogTrigger render={trigger} />
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction onClick={onConfirm} disabled={pending} className="bg-destructive text-destructive-foreground hover:bg-destructive/90">
            {pending ? "Working…" : actionLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
