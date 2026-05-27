"use client";

import { useEffect, useId, useState } from "react";
import { Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../i18n";

const MAX_RETRY_NOTE_LENGTH = 2000;

interface RetryWithNoteDialogProps {
  open: boolean;
  pending?: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (note: string) => Promise<void>;
}

export function RetryWithNoteDialog({
  open,
  pending = false,
  onOpenChange,
  onSubmit,
}: RetryWithNoteDialogProps) {
  const { t } = useT("issues");
  const textareaId = useId();
  const [note, setNote] = useState("");
  const trimmed = note.trim();
  const tooLong = Array.from(trimmed).length > MAX_RETRY_NOTE_LENGTH;
  const canSubmit = trimmed.length > 0 && !tooLong && !pending;

  useEffect(() => {
    if (!open) setNote("");
  }, [open]);

  const handleSubmit = async () => {
    if (!canSubmit) return;
    try {
      await onSubmit(trimmed);
      onOpenChange(false);
    } catch {
      // The caller owns the toast. Keep the dialog open so the note is not lost.
    }
  };

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !pending && onOpenChange(nextOpen)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.retry_with_note.title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.retry_with_note.description)}
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-2">
          <Label htmlFor={textareaId}>{t(($) => $.retry_with_note.label)}</Label>
          <Textarea
            id={textareaId}
            value={note}
            onChange={(event) => setNote(event.target.value)}
            placeholder={t(($) => $.retry_with_note.placeholder)}
            disabled={pending}
            rows={5}
            aria-invalid={tooLong}
          />
          <div className="flex items-center justify-between gap-3 text-xs">
            <span className={tooLong ? "text-destructive" : "text-muted-foreground"}>
              {tooLong
                ? t(($) => $.retry_with_note.too_long, { max: MAX_RETRY_NOTE_LENGTH })
                : t(($) => $.retry_with_note.hint)}
            </span>
            <span className="shrink-0 text-muted-foreground">
              {Array.from(trimmed).length}/{MAX_RETRY_NOTE_LENGTH}
            </span>
          </div>
        </div>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} disabled={pending}>
            {t(($) => $.retry_with_note.cancel)}
          </Button>
          <Button type="button" onClick={handleSubmit} disabled={!canSubmit}>
            {pending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            {t(($) => $.retry_with_note.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
