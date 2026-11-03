"use client";

import { useState, type FormEvent } from "react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { useCreateKnowledgeEvalCase } from "@multica/core/knowledge";
import { useT } from "../i18n";

/**
 * "This should have found it": turns a search result into an eval case. The
 * question defaults to the search, and the expected document is the result.
 */
export function AddEvalCaseButton({
  wsId,
  question,
  documentId,
  documentTitle,
}: {
  wsId: string;
  question: string;
  documentId: string;
  documentTitle: string;
}) {
  const { t } = useT("knowledge");
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button type="button" variant="ghost" size="sm" onClick={() => setOpen(true)}>
        {t(($) => $.taxonomy.add_eval.button)}
      </Button>
      <AddEvalCaseDialog
        wsId={wsId}
        open={open}
        onOpenChange={setOpen}
        question={question}
        documentId={documentId}
        documentTitle={documentTitle}
      />
    </>
  );
}

function AddEvalCaseDialog({
  wsId,
  open,
  onOpenChange,
  question,
  documentId,
  documentTitle,
}: {
  wsId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  question: string;
  documentId: string;
  documentTitle: string;
}) {
  const { t } = useT("knowledge");
  const create = useCreateKnowledgeEvalCase(wsId);
  const [draft, setDraft] = useState(question);

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const text = draft.trim();
    if (!text) return;
    create.mutate(
      { question: text, expected_document_id: documentId },
      {
        // Only a saved case closes the dialog; a failure keeps the question.
        onSuccess: () => {
          toast.success(t(($) => $.taxonomy.add_eval.toast_added));
          onOpenChange(false);
        },
        onError: () => toast.error(t(($) => $.taxonomy.add_eval.toast_failed)),
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>{t(($) => $.taxonomy.add_eval.title)}</DialogTitle>
            <DialogDescription>{t(($) => $.taxonomy.add_eval.description)}</DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="add-eval-question">{t(($) => $.taxonomy.add_eval.question)}</Label>
            <Input
              id="add-eval-question"
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              maxLength={500}
              required
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <span className="text-caption text-muted-foreground">
              {t(($) => $.taxonomy.add_eval.document)}
            </span>
            <span className="text-body">{documentTitle}</span>
          </div>
          <DialogFooter>
            <DialogClose render={<Button variant="outline" type="button" />}>
              {t(($) => $.cancel)}
            </DialogClose>
            <Button type="submit" disabled={create.isPending || draft.trim() === ""} aria-busy={create.isPending}>
              {t(($) => $.taxonomy.add_eval.submit)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
