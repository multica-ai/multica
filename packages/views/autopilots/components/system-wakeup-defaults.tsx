"use client";

import { useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ArrowRight, ListChecks, Pencil } from "@multica/ui/icons";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { useUpdateWorkspaceSystemWakeup, workspaceSystemWakeupsOptions } from "@multica/core/issues";
import type { WorkspaceSystemWakeup } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";

const MAX_INSTRUCTION_BYTES = 4000;

/**
 * Workspace default of the platform's sub-issue rule, above the rows it
 * governs. Each issue can change its own rule; those stop following it.
 */
export function SystemWakeupDefaults({ onShowCustomized }: { onShowCustomized: () => void }) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const { data: rules = [], isError } = useQuery(workspaceSystemWakeupsOptions(wsId));
  const { role } = useCurrentMember(wsId);
  const canManage = role === "owner" || role === "admin";
  const update = useUpdateWorkspaceSystemWakeup(wsId);
  const id = useId();
  const rule = rules.find((candidate) => candidate.rule === "child_done");
  if (isError) {
    return (
      <p role="alert" className="mx-4 mt-3 text-caption text-destructive">
        {t(($) => $.wakeups.defaults.load_error)}
      </p>
    );
  }
  if (!rule) return null;
  const title = t(($) => $.wakeups.defaults.title);
  return (
    <section
      aria-label={title}
      aria-busy={update.isPending || undefined}
      className="mx-4 mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 rounded-lg border border-border px-3 py-2.5"
    >
      <div className="flex min-w-0 flex-1 basis-80 items-start gap-2.5">
        <ListChecks className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <h2 className="truncate text-body font-medium">{title}</h2>
            <span className="shrink-0 rounded-xs bg-muted px-1.5 text-caption leading-5 text-muted-foreground">
              {t(($) => $.wakeups.defaults.badge)}
            </span>
          </div>
          <p className="mt-0.5 text-caption leading-5 text-muted-foreground">
            {t(($) => $.wakeups.defaults.description)}
            {rule.customized > 0 && (
              <>
                {" "}
                <button
                  type="button"
                  onClick={onShowCustomized}
                  className="inline-flex items-center gap-0.5 rounded-xs font-medium text-brand hover:underline focus-visible:outline-2 focus-visible:outline-ring"
                >
                  {t(($) => $.wakeups.defaults.customized, { count: rule.customized })}
                  <ArrowRight className="size-3 shrink-0" aria-hidden="true" />
                </button>
              </>
            )}
          </p>
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-3">
        <InstructionDialog rule={rule} canManage={canManage} />
        <span
          className="flex items-center gap-2"
          title={canManage ? undefined : t(($) => $.wakeups.defaults.admin_only)}
        >
          <label htmlFor={`${id}-enabled`} className="text-caption text-muted-foreground">
            {t(($) => $.wakeups.defaults.enabled)}
          </label>
          <Switch
            id={`${id}-enabled`}
            checked={rule.enabled}
            disabled={!canManage || update.isPending}
            onCheckedChange={(enabled) =>
              update.mutate(
                { rule: rule.rule, enabled },
                { onError: () => toast.error(t(($) => $.wakeups.defaults.save_error)) },
              )
            }
          />
        </span>
      </div>
    </section>
  );
}

function InstructionDialog({ rule, canManage }: { rule: WorkspaceSystemWakeup; canManage: boolean }) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const update = useUpdateWorkspaceSystemWakeup(wsId);
  const id = useId();
  const [open, setOpen] = useState(false);
  const [instruction, setInstruction] = useState(rule.instruction);
  const [error, setError] = useState("");
  const dirty = instruction.trim() !== rule.instruction;
  const label = canManage
    ? t(($) => $.wakeups.defaults.edit_instruction)
    : t(($) => $.wakeups.defaults.view_instruction);
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (update.isPending) return;
        if (next) {
          setInstruction(rule.instruction);
          setError("");
        }
        setOpen(next);
      }}
    >
      <DialogTrigger render={<Button variant="ghost" size="sm" />}>
        <Pencil aria-hidden="true" />
        {label}
      </DialogTrigger>
      <DialogContent className="sm:max-w-[520px]">
        <form
          className="grid gap-4"
          onSubmit={async (event) => {
            event.preventDefault();
            const next = instruction.trim();
            if (new TextEncoder().encode(next).length > MAX_INSTRUCTION_BYTES) {
              setError(t(($) => $.wakeups.defaults.instruction_invalid));
              return;
            }
            try {
              await update.mutateAsync({ rule: rule.rule, instruction: next });
              setOpen(false);
            } catch {
              setError(t(($) => $.wakeups.defaults.save_error));
            }
          }}
        >
          <DialogHeader>
            <DialogTitle>{t(($) => $.wakeups.defaults.instruction)}</DialogTitle>
            <DialogDescription id={`${id}-hint`}>
              {canManage
                ? t(($) => $.wakeups.defaults.instruction_hint)
                : t(($) => $.wakeups.defaults.admin_only)}
            </DialogDescription>
          </DialogHeader>
          <Textarea
            aria-label={t(($) => $.wakeups.defaults.instruction)}
            rows={4}
            value={instruction}
            placeholder={t(($) => $.wakeups.defaults.instruction_placeholder)}
            disabled={!canManage || update.isPending}
            aria-describedby={error ? `${id}-hint ${id}-error` : `${id}-hint`}
            aria-invalid={!!error}
            className="resize-y text-base md:text-body"
            onChange={(event) => {
              setInstruction(event.target.value);
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
          {rule.builtin_instruction && (
            <div className="rounded-md bg-muted/60 px-3 py-2 text-caption leading-5 text-muted-foreground">
              <p className="font-medium">{t(($) => $.wakeups.defaults.builtin)}</p>
              <p className="mt-0.5 max-h-40 overflow-y-auto whitespace-pre-wrap break-words">
                {rule.builtin_instruction}
              </p>
            </div>
          )}
          {canManage && (
            <DialogFooter>
              <Button type="button" variant="outline" disabled={update.isPending} onClick={() => setOpen(false)}>
                {t(($) => $.wakeups.cancel)}
              </Button>
              <Button type="submit" disabled={!dirty || update.isPending} aria-busy={update.isPending || undefined}>
                {update.isPending ? t(($) => $.wakeups.defaults.saving) : t(($) => $.wakeups.defaults.save)}
              </Button>
            </DialogFooter>
          )}
        </form>
      </DialogContent>
    </Dialog>
  );
}
