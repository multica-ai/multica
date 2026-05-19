"use client";

// CEREBRO-PATCH(autopilot-privacy-section): cerebro modification of upstream file — JEH-1750

import { useState } from "react";
import { Lock } from "lucide-react";
import { Switch } from "@multica/ui/components/ui/switch";
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

/**
 * AutopilotPrivacySection — owner-only autopilot toggle for the autopilot dialog.
 *
 * When ON, only the owner sees the autopilot, its runs, its comments, and any
 * issues it creates. Mentions in private comments do NOT trigger other agents.
 *
 * On edit, flipping ON → OFF surfaces a confirmation dialog because previously
 * hidden output (comments, runs, inherited private issues) becomes visible.
 *
 * `initialIsPrivate` is the persisted value at dialog-open time. The
 * confirmation only fires when the toggle moves from `true` (persisted) to
 * `false` (proposed) — flipping back and forth without leaving private state
 * never triggers the dialog.
 */
export function AutopilotPrivacySection({
  value,
  onChange,
  initialIsPrivate,
}: {
  value: boolean;
  onChange: (next: boolean) => void;
  initialIsPrivate: boolean;
}) {
  const [confirmOpen, setConfirmOpen] = useState(false);

  const handleToggle = (next: boolean) => {
    if (initialIsPrivate && value && !next) {
      setConfirmOpen(true);
      return;
    }
    onChange(next);
  };

  const handleConfirm = () => {
    setConfirmOpen(false);
    onChange(false);
  };

  return (
    <div>
      <div className="text-[11px] font-semibold tracking-[0.08em] text-muted-foreground uppercase mb-2">
        Privacy
      </div>
      <label className="flex items-start gap-3 rounded-md border bg-background px-3 py-2.5 cursor-pointer hover:bg-accent/40 transition-colors">
        <Switch
          checked={value}
          onCheckedChange={handleToggle}
          aria-label="Toggle private"
          data-testid="autopilot-privacy-toggle"
          className="mt-0.5 shrink-0"
        />
        <span className="flex-1 min-w-0">
          <span className="flex items-center gap-1.5 text-sm font-medium">
            <Lock className="size-3 text-muted-foreground" />
            Private
          </span>
          <span className="block text-xs text-muted-foreground mt-0.5">
            Only you see this autopilot and its output. Others cannot see that it exists.
          </span>
        </span>
      </label>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Make this autopilot public?</AlertDialogTitle>
            <AlertDialogDescription>
              Previously hidden runs, comments, and inherited private issues will become
              visible to everyone in this workspace.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Keep private</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleConfirm}
              data-testid="autopilot-privacy-confirm-public"
            >
              Make public
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
