"use client";

import { useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bell, BellOff, Pencil } from "@multica/ui/icons";
import { toast } from "sonner";
import { issueSystemWakeupsOptions, useUpdateIssueSystemWakeup } from "@multica/core/issues";
import { useWorkspacePaths } from "@multica/core/paths";
import type { SystemWakeup } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { TruncatedTooltip } from "@multica/ui/components/common/truncated-tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";
import { wakeupsHref } from "../../autopilots/components/wakeups-tab";
import { useWakeupText } from "./wakeup-presentation";

const MAX_INSTRUCTION_BYTES = 4000;

/**
 * The platform's sub-issue wakeups, as chips beside the sub-issue heading.
 * A turned-off rule keeps a quiet chip so it can be turned back on.
 */
export function SystemWakeupChips({
  workspaceId,
  issueId,
}: {
  workspaceId: string;
  issueId: string;
}) {
  const { data: rules = [] } = useQuery(issueSystemWakeupsOptions(workspaceId, issueId));
  if (rules.length === 0) return null;
  return (
    <>
      {rules.map((rule) => (
        <SystemWakeupChip key={rule.rule} rule={rule} workspaceId={workspaceId} issueId={issueId} />
      ))}
    </>
  );
}

function SystemWakeupChip({
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
  const paths = useWorkspacePaths();
  const update = useUpdateIssueSystemWakeup(workspaceId, issueId);
  const title =
    rule.staged && rule.stage !== null
      ? t(($) => $.wakeups.system.title_stage, { stage: rule.stage })
      : t(($) => $.wakeups.system.title_all);
  const paused = rule.paused_reason
    ? t(($) => $.wakeups.system.paused, {
        reason: text.pausedReason({ paused_reason: rule.paused_reason, max_fires: null, fire_count: 0 }) ?? "",
      })
    : null;
  // Why an enabled rule will not wake anyone right now. A member assignee is
  // already named on the chip ("Notifies …").
  const blocked = rule.enabled && !paused
    ? rule.blocked === "backlog"
      ? t(($) => $.wakeups.system.blocked_backlog)
      : rule.blocked === "no_assignee"
        ? t(($) => $.wakeups.system.blocked_none)
        : null
    : null;
  const label = !rule.enabled
    ? t(($) => $.wakeups.system.chip_off)
    : !rule.target
      ? t(($) => $.wakeups.system.chip_assignee)
      : rule.target.type === "member"
        ? t(($) => $.wakeups.system.chip_notify, { name: rule.target.name })
        : t(($) => $.wakeups.system.chip_wake, { name: rule.target.name });
  const save = (input: { enabled: boolean; instruction: string }) =>
    update.mutateAsync({ rule: rule.rule, ...input });
  const toggle = (enabled: boolean) => {
    void save({ enabled, instruction: rule.instruction }).catch(() =>
      toast.error(t(($) => $.wakeups.system.save_error)),
    );
  };
  return (
    <Popover>
      <PopoverTrigger
        render={
          <button
            type="button"
            aria-busy={update.isPending}
            className={cn(
              "inline-flex h-6 min-w-0 shrink items-center gap-1 rounded-md px-1.5 text-caption transition-colors hover:bg-accent/50 focus-visible:outline-2 focus-visible:outline-ring data-popup-open:bg-accent",
              paused ? "text-warning" : "text-muted-foreground",
            )}
          />
        }
      >
        {rule.enabled ? (
          <Bell className="size-3.5 shrink-0" aria-hidden="true" />
        ) : (
          <BellOff className="size-3.5 shrink-0 text-faint-foreground" aria-hidden="true" />
        )}
        <span className="truncate">{label}</span>
      </PopoverTrigger>
      <PopoverContent align="start" className="max-h-[70dvh] w-80 max-w-[calc(100vw-2rem)] overflow-y-auto" keepMounted>
        <div className="flex items-start gap-2">
          <PopoverTitle className="flex-1">{title}</PopoverTitle>
          <SystemBadge />
        </div>
        {paused && <p className="break-words text-caption text-warning">{paused}</p>}
        {blocked && <p className="break-words text-caption text-muted-foreground">{blocked}</p>}
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
          {rule.workspace_default ? t(($) => $.wakeups.system.default_on) : t(($) => $.wakeups.system.default_off)}{" "}
          <AppLink
            href={wakeupsHref(paths.autopilots())}
            className="rounded-xs font-medium text-brand hover:underline focus-visible:outline-2 focus-visible:outline-ring"
          >
            {t(($) => $.wakeups.system.default_change)}
          </AppLink>
        </p>
      </PopoverContent>
    </Popover>
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
          <IconButton
            variant="ghost"
            size="icon-xs"
            className="text-muted-foreground"
            label={t(($) => $.wakeups.system.instruction_edit)}
            onClick={() => {
              setValue(rule.instruction);
              setError("");
              setEditing(true);
            }}
          >
            <Pencil aria-hidden="true" />
          </IconButton>
        </div>
        {rule.instruction ? (
          <p className="whitespace-pre-wrap break-words text-caption">{rule.instruction}</p>
        ) : (
          <div className="text-caption text-muted-foreground">
            <p>{t(($) => $.wakeups.system.instruction_default)}</p>
            <TruncatedTooltip content={rule.default_instruction} focusable>
              <p className="mt-1 line-clamp-4 whitespace-pre-wrap break-words">
                {rule.default_instruction}
              </p>
            </TruncatedTooltip>
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
