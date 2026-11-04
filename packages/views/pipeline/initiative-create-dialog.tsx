"use client";

import { useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { errorCode } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { pipelineListOptions, useCreateInitiative } from "@multica/core/pipeline";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../i18n";

// The server's limits: the title in characters, the thesis in bytes.
const TITLE_MAX_CHARS = 200;
const THESIS_MAX_BYTES = 4000;

/**
 * Starts an initiative at its pipeline's first stage. The thesis is optional
 * (spec 3.1); the server bounds its length. Rendered for owners and admins only.
 */
export function InitiativeCreateDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("pipeline");
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.create.title)}</DialogTitle>
        </DialogHeader>
        <CreateInitiativeForm onCreated={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

// The form lives inside the dialog content, so it starts empty each time the
// dialog opens. The dialog closes only once the server has accepted the initiative.
function CreateInitiativeForm({ onCreated }: { onCreated: () => void }) {
  const { t } = useT("pipeline");
  const wsId = useWorkspaceId();
  const pipelines = useQuery(pipelineListOptions(wsId));
  const create = useCreateInitiative(wsId);
  const [pipelineId, setPipelineId] = useState<string | null>(null);
  const [title, setTitle] = useState("");
  const [thesis, setThesis] = useState("");
  const [submitted, setSubmitted] = useState(false);
  const [serverError, setServerError] = useState<string | null>(null);

  const list = pipelines.data ?? [];
  const defaultPipeline = list.find((candidate) => candidate.is_default) ?? list[0] ?? null;
  const selectedId = pipelineId ?? defaultPipeline?.id ?? "";
  const selectedName = list.find((candidate) => candidate.id === selectedId)?.name ?? "";

  const trimmedTitle = title.trim();
  const trimmedThesis = thesis.trim();
  const titleError = !trimmedTitle
    ? t(($) => $.create.title_required)
    : Array.from(trimmedTitle).length > TITLE_MAX_CHARS
      ? t(($) => $.create.title_too_long)
      : null;
  const thesisError =
    new TextEncoder().encode(trimmedThesis).length > THESIS_MAX_BYTES ? t(($) => $.create.thesis_too_long) : null;

  const submit = (event: FormEvent) => {
    event.preventDefault();
    setSubmitted(true);
    if (titleError || thesisError || !defaultPipeline) return;
    setServerError(null);
    create.mutate(
      { pipeline_id: selectedId || undefined, title: trimmedTitle, thesis: trimmedThesis },
      {
        onSuccess: () => {
          toast.success(t(($) => $.toast.created));
          onCreated();
        },
        onError: (err) =>
          setServerError(
            errorCode(err) === "no_pipeline" ? t(($) => $.create.no_pipeline) : t(($) => $.create.failed),
          ),
      },
    );
  };

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <label className="flex flex-col gap-1.5 text-body">
        {t(($) => $.create.title_label)}
        <Input
          value={title}
          aria-invalid={submitted && titleError ? true : undefined}
          onChange={(event) => setTitle(event.target.value)}
        />
      </label>
      {submitted && titleError ? (
        <p role="alert" className="-mt-2 text-caption text-destructive">
          {titleError}
        </p>
      ) : null}
      <label className="flex flex-col gap-1.5 text-body">
        {t(($) => $.create.thesis_label)}
        <Textarea
          value={thesis}
          rows={4}
          aria-invalid={submitted && thesisError ? true : undefined}
          onChange={(event) => setThesis(event.target.value)}
          className="text-body text-foreground"
        />
      </label>
      {submitted && thesisError ? (
        <p role="alert" className="-mt-2 text-caption text-destructive">
          {thesisError}
        </p>
      ) : null}
      {pipelines.isSuccess && list.length === 0 ? (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.create.no_pipeline)}
        </p>
      ) : null}
      {list.length > 0 ? (
        <div className="flex flex-col gap-1.5 text-body">
          <span>{t(($) => $.create.pipeline_label)}</span>
          <Select
            items={list.map((candidate) => ({ value: candidate.id, label: candidate.name }))}
            value={selectedId}
            onValueChange={(value) => setPipelineId(value ?? null)}
          >
            <SelectTrigger aria-label={t(($) => $.create.pipeline_label)} className="w-full">
              <SelectValue>{selectedName}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {list.map((candidate) => (
                <SelectItem key={candidate.id} value={candidate.id}>
                  {candidate.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      ) : null}
      {serverError ? (
        <p role="alert" className="text-caption text-destructive">
          {serverError}
        </p>
      ) : null}
      <DialogFooter>
        <DialogClose render={<Button type="button" variant="outline" />}>{t(($) => $.create.cancel)}</DialogClose>
        <Button
          type="submit"
          disabled={create.isPending || !defaultPipeline}
          aria-busy={create.isPending}
        >
          {t(($) => $.create.submit)}
        </Button>
      </DialogFooter>
    </form>
  );
}
