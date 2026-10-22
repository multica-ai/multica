"use client";

import { useEffect, useState } from "react";
import { TriangleAlert } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { GithubRefField, githubRefHasError } from "./github-ref-field";
import { useT } from "../../i18n/use-t";
import { githubShortLabel } from "../../common/github-url";

interface GithubRefDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Repository being configured, so the user knows which row this edits. */
  url: string;
  /** Current ref; empty means the repository's default branch. */
  value: string;
  /** Server-side rejection to show inline. */
  errorMessage?: string;
  saving?: boolean;
  onConfirm: (ref: string) => void;
}

/**
 * Sets the branch an already attached `github_repo` resource works on.
 *
 * A dialog rather than an inline field: unlike renaming a row, this changes
 * where every future task in the project starts AND where it delivers, and
 * that is worth a sentence the row has no space for. The same sentence has to
 * say what it does not do — the ref is read when a task is claimed, so work
 * already underway keeps the branch it started on.
 */
export function GithubRefDialog({
  open,
  onOpenChange,
  url,
  value,
  errorMessage,
  saving = false,
  onConfirm,
}: GithubRefDialogProps) {
  const { t } = useT("projects");
  const [draft, setDraft] = useState(value);

  // Re-sync on reopen, otherwise the previous row's ref is what shows.
  useEffect(() => {
    if (open) setDraft(value);
  }, [open, value]);

  const invalid = githubRefHasError(draft);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.resources.ref_dialog_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.resources.ref_dialog_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="rounded-md bg-muted px-2.5 py-1.5 font-mono text-micro text-muted-foreground break-all">
          {githubShortLabel(url)}
        </div>

        <GithubRefField
          id="github-ref"
          value={draft}
          onChange={setDraft}
          onSubmit={() => {
            if (!invalid && !saving) onConfirm(draft.trim());
          }}
          autoFocus
        />

        {errorMessage && (
          <div className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-caption text-destructive">
            <TriangleAlert className="size-3.5 mt-0.5 shrink-0" />
            <span>{errorMessage}</span>
          </div>
        )}

        <DialogFooter>
          <Button
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={saving}
          >
            {t(($) => $.resources.ref_cancel)}
          </Button>
          <Button onClick={() => onConfirm(draft.trim())} disabled={saving || invalid}>
            {t(($) => $.resources.ref_save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
