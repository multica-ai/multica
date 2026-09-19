"use client";

import { useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import type { SecretaryInstructionInput } from "@multica/core/secretary/contract";
import { secretaryKeys } from "@multica/core/secretary/queries";
import type { ArrangedItem } from "@multica/core/secretary/selectors";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../i18n";

type Kind = "plan" | "complete" | "prepare" | "wait" | "later" | "cancel";

export function InstructionForm({
  item,
  revision,
  today,
  onClose,
}: {
  item: ArrangedItem;
  revision: number;
  today: string;
  onClose: () => void;
}) {
  const { t } = useT("issues");
  const workspaceId = useWorkspaceId();
  const client = useQueryClient();
  const [kind, setKind] = useState<Kind>(
    item.reported === "completed"
      ? "complete"
      : item.stage === "ready"
        ? "plan"
        : item.stage === "waiting"
          ? "wait"
          : item.stage === "later"
            ? "later"
            : "prepare",
  );
  const [day, setDay] = useState(
    (item.stage === "waiting" || item.stage === "later"
      ? item.follow_up_on
      : item.scheduled_on) ?? today,
  );
  const [minutes, setMinutes] = useState(
    item.estimate_minutes ? String(item.estimate_minutes) : "",
  );
  const [note, setNote] = useState("");
  const retry = useRef<{ fingerprint: string; requestId: string } | null>(null);
  const mutation = useMutation({
    mutationFn: (input: SecretaryInstructionInput) => api.saveSecretaryInstruction(input),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: secretaryKeys.all(workspaceId) });
      onClose();
    },
  });
  const actionLabels: Record<Kind, string> = {
    plan: t(($) => $.secretary.action_plan),
    complete: t(($) => $.secretary.action_complete),
    prepare: t(($) => $.secretary.action_prepare),
    wait: t(($) => $.secretary.action_wait),
    later: t(($) => $.secretary.action_later),
    cancel: t(($) => $.secretary.action_cancel),
  };
  const dated = kind === "plan" || kind === "wait" || kind === "later";

  function submit(event: React.FormEvent) {
    event.preventDefault();
    const input = {
      item_key: item.key,
      kind,
      note,
      scheduled_on: dated ? day : null,
      estimate_minutes: kind === "plan" && minutes ? Number(minutes) : null,
    };
    const fingerprint = JSON.stringify(input);
    if (retry.current?.fingerprint !== fingerprint) {
      retry.current = { fingerprint, requestId: crypto.randomUUID() };
    }
    mutation.mutate({
      ...input,
      request_id: retry.current.requestId,
      expected_revision: revision,
    });
  }

  const noteLabel =
    kind === "complete"
      ? t(($) => $.secretary.note_complete)
      : kind === "cancel"
        ? t(($) => $.secretary.note_cancel)
        : kind === "wait"
          ? t(($) => $.secretary.note_wait)
          : t(($) => $.secretary.note_default);

  return (
    <form
      onSubmit={submit}
      className="mt-3 space-y-3 rounded-lg bg-muted/40 p-4"
      aria-label={t(($) => $.secretary.form_aria, { title: item.title })}
    >
      <label className="block text-sm">
        {t(($) => $.secretary.handling_method)}
        <select
          className="ml-3 rounded-md border bg-background p-2"
          value={kind}
          disabled={mutation.isPending}
          onChange={(event) => setKind(event.target.value as Kind)}
        >
          {Object.entries(actionLabels).map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </select>
      </label>
      {dated && (
        <div className="flex flex-wrap gap-4">
          <label className="text-sm">
            {kind === "plan"
              ? t(($) => $.secretary.plan_date)
              : t(($) => $.secretary.follow_up_date)}
            <input
              required
              type="date"
              className="ml-2 rounded-md border bg-background p-2"
              value={day}
              onChange={(event) => setDay(event.target.value)}
            />
          </label>
          {kind === "plan" && (
            <label className="text-sm">
              {t(($) => $.secretary.estimated_minutes)}
              <input
                type="number"
                min={1}
                max={1440}
                className="ml-2 w-20 rounded-md border bg-background p-2"
                value={minutes}
                onChange={(event) => setMinutes(event.target.value)}
                placeholder={t(($) => $.secretary.unestimated)}
              />
            </label>
          )}
        </div>
      )}
      {dated && item.due_date && day > item.due_date && (
        <p className="text-sm text-amber-700 dark:text-amber-400">
          {t(($) => $.secretary.after_due_warning)}
        </p>
      )}
      <label className="block text-sm">
        {noteLabel}
        <textarea
          required={kind !== "plan"}
          maxLength={1000}
          value={note}
          onChange={(event) => setNote(event.target.value)}
          className="mt-1 block min-h-20 w-full rounded-md border bg-background p-2"
        />
      </label>
      {kind === "complete" && (
        <p className="text-xs text-muted-foreground">
          {item.closure_mode === "self_report"
            ? t(($) => $.secretary.complete_self_report_hint)
            : t(($) => $.secretary.complete_review_hint)}
        </p>
      )}
      {kind === "plan" && item.stage !== "ready" && (
        <p className="text-xs text-muted-foreground">
          {t(($) => $.secretary.plan_preparing_hint)}
        </p>
      )}
      {mutation.isError && (
        <p role="alert" className="text-sm text-destructive">
          {t(($) => $.secretary.save_error)}
        </p>
      )}
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={mutation.isPending}>
          {mutation.isPending
            ? t(($) => $.secretary.saving)
            : t(($) => $.secretary.save)}
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={mutation.isPending}
          onClick={onClose}
        >
          {t(($) => $.secretary.cancel)}
        </Button>
      </div>
    </form>
  );
}
