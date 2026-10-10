"use client";

import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { useT } from "../i18n";

/**
 * Rename an issue from any surface.
 *
 * The issue detail page lets you click the title itself, but list, board,
 * gantt and sub-issue rows only offer the 3-dot / right-click menu — with no
 * way to change the title. This modal is that missing entry point: the actions
 * menu opens it with the issue's current title, and it writes through the same
 * `useUpdateIssue` mutation every other field edit uses (so lists, board and
 * the detail projection all reconcile from one code path).
 */
export function RenameIssueModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const issueId = typeof data?.issueId === "string" ? data.issueId : "";
  const seedTitle = typeof data?.title === "string" ? data.title : "";
  const seedIdentifier =
    typeof data?.identifier === "string" ? data.identifier : "";
  const [title, setTitle] = useState(seedTitle);
  const inputRef = useRef<HTMLInputElement>(null);
  const updateIssue = useUpdateIssue();
  const pending = updateIssue.isPending;

  // Focus + select on open so the current name is replaced by the first
  // keystroke — the shape every rename affordance has.
  useEffect(() => {
    const input = inputRef.current;
    if (!input) return;
    input.focus();
    input.select();
  }, []);

  const trimmed = title.trim();
  const unchanged = trimmed === seedTitle.trim();
  // Deliberately not gated on "nothing changed" or "empty": both cases route
  // through handleSubmit, which either closes or explains itself. A disabled
  // button would leave the user clicking a dead control with no reason why.
  const canSubmit = !!issueId && !pending;

  const handleSubmit = async () => {
    if (pending || !issueId) return;
    // An empty name is rejected rather than silently treated as a no-op: the
    // user cleared the field on purpose and needs to know it was not applied.
    if (!trimmed) {
      toast.error(t(($) => $.rename_issue.title_required));
      return;
    }
    if (unchanged) {
      onClose();
      return;
    }
    try {
      await updateIssue.mutateAsync({ id: issueId, title: trimmed });
      toast.success(t(($) => $.rename_issue.toast_renamed));
      onClose();
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.rename_issue.toast_failed),
      );
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(v) => {
        // Never dismiss mid-write: the mutation has already been sent, and
        // closing here would leave the user with no feedback either way.
        if (!v && !pending) onClose();
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.rename_issue.title)}</DialogTitle>
        </DialogHeader>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void handleSubmit();
          }}
        >
          <Input
            ref={inputRef}
            value={title}
            disabled={pending}
            placeholder={t(($) => $.rename_issue.placeholder)}
            aria-label={
              seedIdentifier
                ? t(($) => $.rename_issue.input_aria_label, {
                    identifier: seedIdentifier,
                  })
                : t(($) => $.rename_issue.title)
            }
            onChange={(event) => setTitle(event.target.value)}
          />
        </form>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={pending}
            onClick={onClose}
          >
            {t(($) => $.common.cancel)}
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={!canSubmit}
            onClick={() => void handleSubmit()}
          >
            {pending
              ? t(($) => $.rename_issue.saving)
              : t(($) => $.rename_issue.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
