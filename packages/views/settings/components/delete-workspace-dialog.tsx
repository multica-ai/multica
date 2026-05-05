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
import { useT } from "@multica/i18n/react";

/**
 * Typed-confirmation dialog for workspace deletion — GitHub's repo-delete
 * pattern. The destructive button stays disabled until the user types
 * the workspace name exactly (case-sensitive, no trimming). The friction
 * is deliberate: deleting a workspace cascades into every issue, agent,
 * skill, and run under it, and the backend has no soft-delete.
 *
 * Case-sensitive match matches GitHub's pattern and catches the "I
 * remember the gist of the name but not the casing" misfire. No trim —
 * leading/trailing whitespace indicates a typo, and silently accepting
 * it would weaken the whole point of the gate.
 *
 * Input value resets whenever the dialog closes so reopening doesn't
 * leak the previous attempt (which might have been for a different
 * workspace after a swap).
 */
export function DeleteWorkspaceDialog({
  workspaceName,
  loading = false,
  open,
  onOpenChange,
  onConfirm,
}: {
  workspaceName: string;
  loading?: boolean;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}) {
  const td = useT("delete_workspace");
  const [typed, setTyped] = useState("");
  const matched = typed === workspaceName;

  // Reset on close (so reopening for a different workspace doesn't leak
  // the prior attempt) AND on workspaceName change (if another owner
  // renames the workspace while the dialog is open, the already-typed
  // string stops matching and there'd be no feedback explaining why).
  useEffect(() => {
    setTyped("");
  }, [open, workspaceName]);

  const submit = () => {
    if (!matched || loading) return;
    onConfirm();
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{td("title")}</DialogTitle>
          <DialogDescription>
            {td("description")}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-2">
          <Label htmlFor="delete-workspace-confirm" className="text-xs">
            {td("confirm_label", { workspaceName })}
          </Label>
          <Input
            id="delete-workspace-confirm"
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                submit();
              }
            }}
            placeholder={workspaceName}
            autoFocus
            disabled={loading}
            autoComplete="off"
            autoCorrect="off"
            autoCapitalize="off"
            spellCheck={false}
          />
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={loading}
          >
            {td("cancel")}
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={submit}
            disabled={!matched || loading}
          >
            {loading ? td("deleting") : td("delete")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
