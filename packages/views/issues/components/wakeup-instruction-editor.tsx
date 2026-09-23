"use client";

import { useId, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Pencil } from "lucide-react";
import {
  issueWakeupsOptions,
  useEditWakeupInstruction,
} from "@multica/core/issues/wakeups";
import type { IssueWakeup } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";
import { useWakeupText } from "./wakeup-presentation";

export function WakeupInstructionEditor({
  workspaceId,
  issueId,
  wakeupId,
  disabled = false,
  triggerStyle = "text",
}: {
  workspaceId: string;
  issueId: string;
  wakeupId: string;
  disabled?: boolean;
  triggerStyle?: "text" | "icon";
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const label = t(($) => $.wakeups.edit_instruction);
  const trigger = (
    <DialogTrigger
      render={
        <Button
          variant="ghost"
          size={triggerStyle === "icon" ? "icon" : "sm"}
          className={triggerStyle === "icon" ? "size-11 text-muted-foreground" : undefined}
          disabled={disabled}
          aria-label={label}
        />
      }
    >
      <Pencil className="size-3.5" aria-hidden="true" />
      {triggerStyle === "text" && label}
    </DialogTrigger>
  );
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!saving) setOpen(next);
      }}
    >
      {triggerStyle === "icon" ? (
        <Tooltip>
          <TooltipTrigger render={trigger} />
          <TooltipContent>{label}</TooltipContent>
        </Tooltip>
      ) : trigger}
      <DialogContent className="sm:max-w-xl" showCloseButton={!saving}>
        <DialogHeader>
          <DialogTitle>{t(($) => $.wakeups.edit_instruction)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.wakeups.instruction_effect)}
          </DialogDescription>
        </DialogHeader>
        {open && (
          <InstructionLoader
            workspaceId={workspaceId}
            issueId={issueId}
            wakeupId={wakeupId}
            onSaved={() => setOpen(false)}
            onSaving={setSaving}
            onCancel={() => setOpen(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function InstructionLoader({
  workspaceId, issueId, wakeupId, ...actions
}: {
  workspaceId: string;
  issueId: string;
  wakeupId: string;
  onSaved: () => void;
  onSaving: (value: boolean) => void;
  onCancel: () => void;
}) {
  const { t } = useT("issues");
  const query = useQuery({
    ...issueWakeupsOptions(workspaceId, issueId),
    refetchInterval: false,
  });
  const wakeup = query.data?.find((row) => row.id === wakeupId);
  if (query.isPending) {
    return <p role="status">{t(($) => $.wakeups.instruction_loading)}</p>;
  }
  if (!wakeup) {
    return (
      <Button variant="ghost" onClick={() => void query.refetch()}>
        {t(($) => $.wakeups.retry)}
      </Button>
    );
  }
  return (
    <InstructionForm key={wakeupId} wakeup={wakeup} workspaceId={workspaceId} {...actions} />
  );
}

function InstructionForm({
  wakeup, workspaceId, onSaved, onSaving, onCancel
}: {
  wakeup: IssueWakeup;
  workspaceId: string;
  onSaved: () => void;
  onSaving: (value: boolean) => void;
  onCancel: () => void;
}) {
  const { t } = useT("issues");
  const text = useWakeupText();
  const id = useId();
  // Keep the editing snapshot through query refreshes; a stale save conflicts
  // instead of replacing another person's changes or silently resetting drafts.
  const [original] = useState(wakeup);
  const [value, setValue] = useState(wakeup.instruction);
  const [error, setError] = useState("");
  const busy = useRef(false);
  const mutation = useEditWakeupInstruction(workspaceId, wakeup.issue_id);
  return (
    <form
      className="space-y-3"
      onSubmit={async (event) => {
        event.preventDefault();
        if (busy.current) return;
        const instruction = value.trim();
        if (!instruction || new TextEncoder().encode(instruction).length > 12000) {
          setError(t(($) => $.wakeups.instruction_invalid));
          return;
        }
        busy.current = true;
        onSaving(true);
        setError("");
        try {
          await mutation.mutateAsync({
            id: wakeup.id,
            instruction,
            expected_instruction: original.instruction,
            revision: original.revision ?? 0,
          });
          onSaved();
        } catch (err) {
          setError(text.error(err, t(($) => $.wakeups.instruction_save_error)));
        } finally {
          busy.current = false;
          onSaving(false);
        }
      }}
    >
      <label htmlFor={id} className="text-caption font-medium">
        {t(($) => $.wakeups.instruction_title)}
      </label>
      <Textarea
        id={id}
        value={value}
        rows={8}
        className="max-h-[45dvh] resize-y text-base md:text-body"
        disabled={mutation.isPending}
        aria-invalid={!!error}
        aria-describedby={error ? `${id}-error` : undefined}
        onChange={(event) => {
          setValue(event.target.value);
          setError("");
        }}
        onKeyDown={(event) => {
          if ((event.metaKey || event.ctrlKey) && event.key === "Enter" && !event.nativeEvent.isComposing) {
            event.preventDefault();
            event.currentTarget.form?.requestSubmit();
          }
        }}
      />
      {error && (
        <p id={`${id}-error`} role="alert" className="text-caption text-destructive">
          {error}
        </p>
      )}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" disabled={mutation.isPending} onClick={onCancel}>
          {t(($) => $.wakeups.instruction_cancel)}
        </Button>
        <Button type="submit" disabled={mutation.isPending || value.trim() === original.instruction}>
          {t(($) => mutation.isPending ? $.wakeups.instruction_saving : $.wakeups.instruction_save)}
        </Button>
      </div>
    </form>
  );
}
