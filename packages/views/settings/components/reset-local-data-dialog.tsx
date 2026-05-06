"use client";

import { useEffect, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";

/**
 * Phrase the user must type to enable the destructive action. Case-sensitive,
 * no surrounding whitespace — matches the GitHub repo-delete pattern used by
 * `DeleteWorkspaceDialog`. Exported so tests don't drift from the prod copy.
 */
export const RESET_CONFIRMATION_PHRASE = "reset local data";

/**
 * Typed-confirmation dialog for the local-only "Reset local data" power-user
 * action. Mirrors `DeleteWorkspaceDialog` so the friction feels consistent
 * across destructive flows; this one is even less reversible (the local
 * Postgres cluster has no backup), so the dialog body explicitly enumerates
 * what's deleted vs preserved before asking for the typed confirmation.
 *
 * Input value resets when the dialog closes so reopening doesn't leak a
 * prior partial attempt.
 */
export function ResetLocalDataDialog({
  loading = false,
  open,
  onOpenChange,
  onConfirm,
}: {
  loading?: boolean;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}) {
  const [typed, setTyped] = useState("");
  const matched = typed === RESET_CONFIRMATION_PHRASE;

  useEffect(() => {
    setTyped("");
  }, [open]);

  const submit = () => {
    if (!matched || loading) return;
    onConfirm();
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Reset local data</DialogTitle>
          <DialogDescription>
            This wipes the local Multica database and stack logs. It cannot be
            undone.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div className="space-y-1">
            <p className="text-xs font-medium text-foreground">Will be deleted</p>
            <ul className="list-disc space-y-0.5 pl-5 text-xs text-muted-foreground">
              <li>Local Postgres cluster (issues, agents, runs, settings).</li>
              <li>Postgres, daemon, and app logs.</li>
            </ul>
          </div>
          <div className="space-y-1">
            <p className="text-xs font-medium text-foreground">Will NOT be deleted</p>
            <ul className="list-disc space-y-0.5 pl-5 text-xs text-muted-foreground">
              <li>Your repository checkouts on disk.</li>
              <li>App preferences (theme, sign-in state, window layout).</li>
              <li>OS-level credentials (keychain, secret store).</li>
            </ul>
          </div>

          <div className="space-y-2 pt-2">
            <Label htmlFor="reset-local-data-confirm" className="text-xs">
              To confirm, type{" "}
              <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">
                {RESET_CONFIRMATION_PHRASE}
              </code>{" "}
              below.
            </Label>
            <Input
              id="reset-local-data-confirm"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  submit();
                }
              }}
              placeholder={RESET_CONFIRMATION_PHRASE}
              autoFocus
              disabled={loading}
              autoComplete="off"
              autoCorrect="off"
              autoCapitalize="off"
              spellCheck={false}
            />
          </div>
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={loading}
          >
            Cancel
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={submit}
            disabled={!matched || loading}
          >
            {loading ? "Resetting..." : "Reset local data"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
