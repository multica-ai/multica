"use client";

import { useId, useState } from "react";
import { ListChecks, Pencil } from "lucide-react";
import { toast } from "sonner";
import { useUpdateIssueSystemWakeup } from "@multica/core/issues";
import type { SystemWakeup } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { useT } from "../../i18n";
import { useWakeupText } from "./wakeup-presentation";

const MAX_INSTRUCTION_BYTES = 4000;

/** The platform's child-done wakeup, shown beside the rules people created. */
export function SystemWakeupRow({
  rule,
  workspaceId,
  issueId,
}: {
  rule: SystemWakeup;
  workspaceId: string;
  issueId: string;
}) {
  const { t } = useT("issues");
  const text = useWakeupText();
  const update = useUpdateIssueSystemWakeup(workspaceId, issueId);
  const title =
    rule.staged && rule.stage !== null
      ? t(($) => $.wakeups.system.title_stage, { stage: rule.stage })
      : t(($) => $.wakeups.system.title_all);
  const blocked = {
    "": null,
    backlog: t(($) => $.wakeups.system.blocked_backlog),
    member_assignee: t(($) => $.wakeups.system.blocked_member),
    no_assignee: t(($) => $.wakeups.system.blocked_none),
  }[rule.blocked];
  const paused = rule.paused_reason
    ? t(($) => $.wakeups.system.paused, {
        reason: text.pausedReason({ paused_reason: rule.paused_reason, max_fires: null, fire_count: 0 }) ?? "",
      })
    : null;
  const target =
    rule.target?.type === "member"
      ? t(($) => $.wakeups.system.notify_member, { name: rule.target.name })
      : t(($) => $.wakeups.system.wake_assignee, { name: rule.target?.name ?? "" });
  const summary =
    paused ??
    (!rule.enabled
      ? t(($) => $.wakeups.system.turned_off)
      : rule.blocked && rule.blocked !== "member_assignee"
        ? blocked
        : [target, t(($) => $.wakeups.system.remaining, { count: rule.remaining })].join(" · "));
  const save = (input: { enabled: boolean; instruction: string }) =>
    update.mutateAsync({ rule: rule.rule, ...input });
  const toggle = (enabled: boolean) =>
    void save({ enabled, instruction: rule.instruction }).catch(() =>
      toast.error(t(($) => $.wakeups.system.save_error)),
    );
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_auto]" aria-busy={update.isPending}>
      <Popover>
        <PopoverTrigger
          render={
            <button
              type="button"
              className="col-span-2 col-start-1 row-start-1 grid min-w-0 grid-cols-subgrid rounded-md py-1.5 text-left text-caption hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring"
            />
          }
        >
          <span className="flex min-h-8 min-w-0 items-start gap-2 py-1 pl-2 pr-1">
            <ListChecks className="mt-px size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
            <span className="line-clamp-2 min-w-0 break-words font-medium">{title}</span>
            <SystemBadge />
          </span>
          <span className={`col-span-2 min-w-0 break-words pl-7.5 pr-2 ${paused ? "text-warning" : "text-muted-foreground"}`}>
            {summary}
          </span>
        </PopoverTrigger>
        <PopoverContent align="end" className="max-h-[70dvh] w-80 max-w-[calc(100vw-2rem)] overflow-y-auto" keepMounted>
          <div className="flex items-start gap-2">
            <PopoverTitle className="flex-1">{title}</PopoverTitle>
            <SystemBadge />
          </div>
          <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-2 text-caption">
            <dt className="text-muted-foreground">{t(($) => $.wakeups.system.target_title)}</dt>
            <dd>
              {rule.target
                ? t(($) => $.wakeups.system.target_current, { name: rule.target.name })
                : t(($) => $.wakeups.system.target_none)}
            </dd>
            <dt className="text-muted-foreground">{t(($) => $.wakeups.system.progress_title)}</dt>
            <dd>
              {t(($) => $.wakeups.system.progress, { done: rule.total - rule.remaining, total: rule.total })}
              {rule.waiting.length > 0 &&
                ` · ${t(($) => $.wakeups.system.waiting_on, { ids: rule.waiting.join(", ") })}`}
            </dd>
            <dt className="text-muted-foreground">{t(($) => $.wakeups.system.skip_title)}</dt>
            <dd>{t(($) => $.wakeups.system.skip)}</dd>
            <dt className="text-muted-foreground">{t(($) => $.wakeups.source_title)}</dt>
            <dd>{t(($) => $.wakeups.system.source)}</dd>
          </dl>
          <InstructionEditor rule={rule} pending={update.isPending} onSave={save} />
          <div className="flex items-center gap-2 border-t border-border pt-2.5">
            <span className="flex-1 text-caption">{t(($) => $.wakeups.system.enable)}</span>
            <Switch
              checked={rule.enabled}
              disabled={update.isPending}
              aria-label={t(($) => $.wakeups.system.toggle)}
              onCheckedChange={toggle}
            />
          </div>
          <p className="text-caption text-muted-foreground">
            {rule.workspace_default ? t(($) => $.wakeups.system.default_on) : t(($) => $.wakeups.system.default_off)}
          </p>
        </PopoverContent>
      </Popover>
      <div className="z-10 col-start-2 row-start-1 flex min-h-11 items-center self-start px-2">
        <Switch
          checked={rule.enabled}
          disabled={update.isPending}
          aria-label={t(($) => $.wakeups.system.toggle)}
          onCheckedChange={toggle}
        />
      </div>
    </div>
  );
}

function SystemBadge() {
  const { t } = useT("issues");
  return (
    <span className="mt-px shrink-0 rounded-xs bg-muted px-1.5 text-micro font-medium text-muted-foreground">
      {t(($) => $.wakeups.system.badge)}
    </span>
  );
}

function InstructionEditor({
  rule,
  pending,
  onSave,
}: {
  rule: SystemWakeup;
  pending: boolean;
  onSave: (input: { enabled: boolean; instruction: string }) => Promise<unknown>;
}) {
  const { t } = useT("issues");
  const id = useId();
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(rule.instruction);
  const [error, setError] = useState("");
  const label = t(($) => $.wakeups.system.instruction);
  if (!editing) {
    return (
      <div>
        <div className="flex items-center justify-between gap-2">
          <p className="text-caption font-medium">{label}</p>
          <Button
            variant="ghost"
            size="icon-xs"
            className="text-muted-foreground"
            aria-label={t(($) => $.wakeups.system.instruction_edit)}
            onClick={() => {
              setValue(rule.instruction);
              setError("");
              setEditing(true);
            }}
          >
            <Pencil aria-hidden="true" />
          </Button>
        </div>
        {rule.instruction ? (
          <p className="whitespace-pre-wrap break-words text-caption">{rule.instruction}</p>
        ) : (
          <div className="text-caption text-muted-foreground">
            <p>{t(($) => $.wakeups.system.instruction_default)}</p>
            <p className="mt-1 line-clamp-4 whitespace-pre-wrap break-words" title={rule.default_instruction}>
              {rule.default_instruction}
            </p>
          </div>
        )}
      </div>
    );
  }
  return (
    <form
      className="space-y-2"
      onSubmit={async (event) => {
        event.preventDefault();
        const instruction = value.trim();
        if (new TextEncoder().encode(instruction).length > MAX_INSTRUCTION_BYTES) {
          setError(t(($) => $.wakeups.system.instruction_invalid));
          return;
        }
        try {
          await onSave({ enabled: rule.enabled, instruction });
          setEditing(false);
        } catch {
          setError(t(($) => $.wakeups.system.save_error));
        }
      }}
    >
      <label htmlFor={id} className="text-caption font-medium">
        {label}
      </label>
      <Textarea
        id={id}
        rows={5}
        value={value}
        placeholder={rule.default_instruction || t(($) => $.wakeups.system.instruction_placeholder)}
        className="resize-y text-base md:text-caption"
        disabled={pending}
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
        <Button type="button" variant="ghost" size="sm" disabled={pending} onClick={() => setEditing(false)}>
          {t(($) => $.wakeups.instruction_cancel)}
        </Button>
        <Button type="submit" size="sm" disabled={pending || value.trim() === rule.instruction}>
          {t(($) => (pending ? $.wakeups.instruction_saving : $.wakeups.instruction_save))}
        </Button>
      </div>
    </form>
  );
}
