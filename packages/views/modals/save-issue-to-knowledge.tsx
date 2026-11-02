"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useWorkspaceId } from "@multica/core/hooks";
import { createSafeId } from "@multica/core/utils";
import {
  knowledgeBasesOptions,
  knowledgeErrorState,
  useCreateKnowledgeDocument,
} from "@multica/core/knowledge";
import { useT } from "../i18n";

/** The base a save goes to unless the person picks another. */
const DEFAULT_KNOWLEDGE_BASE = "engineering";

/**
 * Save an issue's title and description to the company knowledge base. A
 * person's save publishes at once: the person is the curator, so the document
 * is searchable as soon as the save returns. Opened from the issue actions
 * menu, which hides the entry while knowledge is not ready.
 */
export function SaveIssueToKnowledgeModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const { t: tKnowledge } = useT("knowledge");
  const wsId = useWorkspaceId();
  const issueId = typeof data?.issueId === "string" ? data.issueId : "";
  const title = typeof data?.title === "string" ? data.title.trim() : "";
  const description =
    typeof data?.description === "string" ? data.description.trim() : "";

  const bases = useQuery(knowledgeBasesOptions(wsId));
  const choices = bases.data?.knowledge_bases ?? [];
  const [picked, setPicked] = useState<string | null>(null);
  const selectedKey = choices.some((base) => base.key === picked)
    ? picked
    : (choices.find((base) => base.key === DEFAULT_KNOWLEDGE_BASE) ?? choices[0])?.key ?? null;
  const selected = choices.find((base) => base.key === selectedKey) ?? null;

  // One token per open dialog: a second click on the same save is the same
  // write, while the next time the dialog opens is a new document.
  const [attempt] = useState(createSafeId);
  const create = useCreateKnowledgeDocument(wsId);
  const pending = create.isPending;
  const ready = bases.data?.state === "ready";
  const canSubmit =
    ready && !!issueId && !!title && !!description && !!selected && !pending;

  const handleSubmit = async () => {
    if (!canSubmit || !selected) return;
    try {
      await create.mutateAsync({
        kb: selected.key,
        title,
        content: description,
        source_type: "issue",
        source_id: issueId,
        publish: true,
        idempotency_key: `issue:${issueId}:${attempt}:${selected.key}`,
      });
      toast.success(
        t(($) => $.save_issue_to_knowledge.toast_saved, { knowledgeBase: selected.name }),
      );
      onClose();
    } catch (err) {
      const state = knowledgeErrorState(err);
      toast.error(
        state === "offline"
          ? tKnowledge(($) => $.offline.title)
          : t(($) => $.save_issue_to_knowledge.toast_failed),
      );
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        // Never dismiss mid-write: the save has already been sent.
        if (!open && !pending) onClose();
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.save_issue_to_knowledge.title)}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="save-knowledge-base">
              {t(($) => $.save_issue_to_knowledge.knowledge_base)}
            </Label>
            <Select
              items={choices.map((base) => ({ value: base.key, label: base.name }))}
              value={selectedKey}
              onValueChange={(value) => setPicked(value as string)}
              disabled={pending || choices.length === 0}
            >
              <SelectTrigger id="save-knowledge-base" className="w-full">
                <SelectValue>{() => selected?.name ?? ""}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {choices.map((base) => (
                  <SelectItem key={base.key} value={base.key}>
                    {base.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {description ? (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.save_issue_to_knowledge.published_note)}
            </p>
          ) : (
            <p role="status" className="text-caption text-muted-foreground">
              {t(($) => $.save_issue_to_knowledge.empty_description)}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" size="sm" disabled={pending} onClick={onClose}>
            {t(($) => $.common.cancel)}
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={!canSubmit}
            aria-busy={pending}
            onClick={() => void handleSubmit()}
          >
            {pending
              ? t(($) => $.save_issue_to_knowledge.saving)
              : t(($) => $.save_issue_to_knowledge.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
