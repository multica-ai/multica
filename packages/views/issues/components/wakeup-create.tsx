"use client";

import { useId, useMemo, useRef, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Bot,
  ChevronDown,
  CircleDashed,
  CircleDot,
  Clock3,
  GitPullRequest,
  Info,
  Link2,
  ListChecks,
  MessageSquare,
  Plus,
  RefreshCw,
  Zap,
  type LucideIcon,
} from "lucide-react";
import { childIssuesOptions, issueDetailOptions, useCreateIssueWakeup } from "@multica/core/issues";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { labelListOptions } from "@multica/core/labels/queries";
import { propertyListOptions } from "@multica/core/properties/queries";
import { isAgentRuntimeBound } from "@multica/core/agents";
import { ApiError } from "@multica/core/api";
import { agentListOptions, memberListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { shortcutMatchesEvent, useShortcut } from "@multica/core/shortcuts";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { DialogTitle } from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { ShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useViewingTimezone } from "../../common/use-viewing-timezone";
import { useT } from "../../i18n";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { PickerEmpty, PickerItem, PickerSection, PropertyPicker } from "./pickers/property-picker";
import { IssuePickerModal } from "../../modals/issue-picker-modal";
import { useStatusLabel } from "../utils/status-label";
import { useWakeupText } from "./wakeup-presentation";
import {
  WAKEUP_EVENT_TYPES,
  WAKEUP_MAX_FIRES,
  WAKEUP_WAIT_DAYS,
  buildWakeupInput,
  emptyWakeupDraft,
  isEventCondition,
  wakesAssigneeOnComments,
  type WakeupAtPreset,
  type WakeupCondition,
  type WakeupDraft,
  type WakeupDraftError,
  type WakeupField,
  type WakeupRecurrence,
} from "./wakeup-draft";

const pillClass =
  "inline-flex h-7 max-w-full min-w-0 items-center gap-1.5 rounded-md border border-surface-border bg-surface px-2 text-label outline-none hover:bg-accent focus-visible:ring-3 focus-visible:ring-ring/50 data-popup-open:bg-accent [&_svg]:shrink-0";

/**
 * Lets a member add a wakeup from the issue sidebar. It creates the same rule
 * an agent creates with `multica issue wakeup create`.
 */
export function WakeupCreate({
  workspaceId,
  issueId,
  defaultAgentId,
}: {
  workspaceId: string;
  issueId: string;
  defaultAgentId?: string;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
  const busy = useRef(false);
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        if (!busy.current) setOpen(next);
      }}
    >
      <PopoverTrigger
        render={
          <Button
            variant="ghost"
            size="icon-xs"
            className="text-muted-foreground"
            aria-label={t(($) => $.wakeups.create.open)}
          />
        }
      >
        <Plus aria-hidden="true" />
      </PopoverTrigger>
      <PopoverContent
        side="left"
        align="start"
        sideOffset={12}
        className="w-[420px] max-w-[calc(100vw-2rem)] gap-0 p-4"
      >
        {open && (
          <WakeupCreateForm
            workspaceId={workspaceId}
            issueId={issueId}
            defaultAgentId={defaultAgentId ?? ""}
            onBusy={(value) => (busy.current = value)}
            onClose={() => setOpen(false)}
          />
        )}
      </PopoverContent>
    </Popover>
  );
}

export function WakeupCreateForm({
  workspaceId,
  issueId,
  defaultAgentId,
  onBusy,
  onClose,
  inDialog = false,
}: {
  workspaceId: string;
  issueId: string;
  defaultAgentId: string;
  onBusy?: (busy: boolean) => void;
  onClose: () => void;
  /** Rendered in a dialog (the workspace list) rather than the sidebar popover. */
  inDialog?: boolean;
}) {
  const Title = inDialog ? DialogTitle : PopoverTitle;
  const { t } = useT("issues");
  const text = useWakeupText();
  const timezone = useViewingTimezone();
  const id = useId();
  const [draft, setDraft] = useState(() => emptyWakeupDraft(defaultAgentId, timezone));
  const [error, setError] = useState("");
  const create = useCreateIssueWakeup(workspaceId, issueId);
  const sendShortcut = useShortcut("send");
  const { data: agents = [] } = useQuery(agentListOptions(workspaceId));
  const { data: properties = [] } = useQuery(propertyListOptions(workspaceId));
  const { data: issue } = useQuery(issueDetailOptions(workspaceId, issueId));
  const update = (patch: Partial<WakeupDraft>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
    setError("");
  };
  const agentName = agents.find((a) => a.id === draft.agentId)?.name;
  const draftErrors: Record<WakeupDraftError, string> = {
    missing_condition: t(($) => $.wakeups.create.missing_condition),
    missing_value: t(($) => $.wakeups.create.missing_value),
    missing_issue: t(($) => $.wakeups.create.missing_issue),
    missing_agent: t(($) => $.wakeups.create.missing_agent),
    missing_events: t(($) => $.wakeups.create.missing_events),
    instruction_invalid: t(($) => $.wakeups.instruction_invalid),
    future_time: t(($) => $.wakeups.future_time),
    until_future: t(($) => $.wakeups.create.until_future),
  };

  const submit = async () => {
    if (create.isPending) return;
    const propertyType =
      draft.field === "property" ? properties.find((p) => p.id === draft.fieldTarget)?.type : undefined;
    const result = buildWakeupInput(draft, new Date(), propertyType);
    if ("error" in result) {
      setError(draftErrors[result.error]);
      return;
    }
    onBusy?.(true);
    try {
      await create.mutateAsync(result.input);
      onClose();
    } catch (err) {
      const code =
        err instanceof ApiError && err.body && typeof err.body === "object" && "code" in err.body
          ? err.body.code
          : undefined;
      setError(
        code === "wakeup_capacity_exceeded"
          ? t(($) => $.wakeups.create.capacity_error)
          : text.error(err, t(($) => $.wakeups.create.error)),
      );
    } finally {
      onBusy?.(false);
    }
  };

  return (
    <form
      aria-labelledby={`${id}-title`}
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
      onKeyDown={(event) => {
        if (sendShortcut && shortcutMatchesEvent(sendShortcut, event.nativeEvent) && !event.nativeEvent.isComposing) {
          event.preventDefault();
          void submit();
        }
      }}
    >
      <Title id={`${id}-title`} className="text-title-sm font-semibold">
        {t(($) => $.wakeups.create.title)}
      </Title>
      <div className="mt-3.5 grid grid-cols-[2.5rem_minmax(0,1fr)] items-center gap-y-2.5">
        <span className="text-label text-muted-foreground">{t(($) => $.wakeups.create.when)}</span>
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          <ConditionMenu value={draft.condition} onChange={(condition) => update({ condition })} />
          <ConditionParams draft={draft} workspaceId={workspaceId} issueId={issueId} update={update} onBusy={onBusy} />
        </div>
        <span className="text-label text-muted-foreground">{t(($) => $.wakeups.create.wake)}</span>
        <div className="flex min-w-0 items-center gap-1.5">
          <AgentChoice
            workspaceId={workspaceId}
            value={draft.agentId}
            onChange={(agentId) => update({ agentId })}
            placeholder={t(($) => $.wakeups.create.choose_agent)}
          />
        </div>
      </div>
      {wakesAssigneeOnComments(draft, issue?.assignee_type === "agent" ? (issue.assignee_id ?? null) : null) && (
        <p className="mt-2.5 rounded-md bg-muted/60 px-2.5 py-2 text-caption leading-5 text-muted-foreground">
          {t(($) => $.wakeups.create.assignee_comment_hint, { name: agentName ?? "" })}
        </p>
      )}
      {draft.condition === "at" && draft.atPreset === "custom" && (
        <Input
          type="datetime-local"
          aria-label={t(($) => $.wakeups.local_time, { timezone: Intl.DateTimeFormat().resolvedOptions().timeZone })}
          className="mt-2.5"
          value={draft.atCustom}
          onChange={(event) => update({ atCustom: event.target.value })}
        />
      )}
      <label htmlFor={`${id}-instruction`} className="mt-4 mb-1.5 block text-caption font-medium">
        {t(($) => $.wakeups.instruction_title)}
      </label>
      <Textarea
        id={`${id}-instruction`}
        rows={3}
        value={draft.instruction}
        placeholder={t(($) => $.wakeups.create.instruction_placeholder)}
        className="max-h-[30dvh] resize-y text-base md:text-label"
        onChange={(event) => update({ instruction: event.target.value })}
      />
      {draft.condition === "recurring" && (
        <div className="mt-4 grid grid-cols-[4.5rem_minmax(0,1fr)] items-center gap-y-2.5">
          <label htmlFor={`${id}-until`} className="text-caption text-muted-foreground">
            {t(($) => $.wakeups.create.until)}
          </label>
          <Input
            id={`${id}-until`}
            type="date"
            className="h-7 w-44"
            value={draft.until}
            onChange={(event) => update({ until: event.target.value })}
          />
        </div>
      )}
      {isEventCondition(draft.condition) && (
        <div className="mt-4 grid grid-cols-[4.5rem_minmax(0,1fr)] items-center gap-y-2.5">
          <span className="text-caption text-muted-foreground">{t(($) => $.wakeups.create.trigger_count)}</span>
          <Segmented
            label={t(($) => $.wakeups.create.trigger_count)}
            value={draft.mode}
            options={[
              { value: "once", label: t(($) => $.wakeups.once) },
              { value: "continuous", label: t(($) => $.wakeups.continuous) },
            ]}
            onChange={(mode) => update({ mode })}
          />
          {draft.mode === "continuous" && (
            <>
              <span className="text-caption text-muted-foreground">{t(($) => $.wakeups.create.max_fires)}</span>
              <div className="flex min-w-0">
                <PillMenu
                  label={t(($) => $.wakeups.create.max_fires)}
                  value={String(draft.maxFires)}
                  options={WAKEUP_MAX_FIRES.map((count) => ({
                    value: String(count),
                    label: t(($) => $.wakeups.create.max_fires_times, { count }),
                  }))}
                  onChange={(value) => update({ maxFires: Number(value) })}
                />
              </div>
            </>
          )}
          <span className="text-caption text-muted-foreground">{t(($) => $.wakeups.create.max_wait)}</span>
          <div className="flex min-w-0">
            <PillMenu
              label={t(($) => $.wakeups.create.max_wait)}
              value={String(draft.waitDays)}
              options={WAKEUP_WAIT_DAYS.map((days) => ({
                value: String(days),
                label: t(($) => $.wakeups.create.wait_days, { count: days }),
              }))}
              onChange={(value) => update({ waitDays: Number(value) })}
            />
          </div>
          <span className="text-caption text-muted-foreground">{t(($) => $.wakeups.create.on_timeout)}</span>
          <div className="flex min-w-0">
            <PillMenu
              label={t(($) => $.wakeups.create.on_timeout)}
              value={draft.onTimeout}
              options={[
                {
                  value: "wake",
                  label: t(($) => $.wakeups.create.timeout_wake, {
                    agent: agentName ?? t(($) => $.wakeups.create.the_agent),
                  }),
                },
                { value: "end", label: t(($) => $.wakeups.create.timeout_end) },
              ]}
              onChange={(value) => update({ onTimeout: value as WakeupDraft["onTimeout"] })}
            />
          </div>
        </div>
      )}
      {error && (
        <p role="alert" className="mt-3 text-caption text-destructive">
          {error}
        </p>
      )}
      <div className="mt-4 flex items-center gap-2 border-t border-border pt-3">
        <span className="flex min-w-0 flex-1 items-center gap-1 text-caption text-muted-foreground">
          <Info className="size-3 shrink-0" aria-hidden="true" />
          {t(($) => $.wakeups.create.on_behalf)}
        </span>
        <Button type="button" variant="ghost" size="sm" disabled={create.isPending} onClick={onClose}>
          {t(($) => $.wakeups.create.cancel)}
        </Button>
        <Button type="submit" size="sm" disabled={create.isPending} aria-busy={create.isPending}>
          {t(($) => (create.isPending ? $.wakeups.create.submitting : $.wakeups.create.submit))}
          {sendShortcut && !create.isPending ? (
            <ShortcutKeycaps
              shortcut={sendShortcut}
              decorative
              className="ml-1 max-sm:hidden"
              keyClassName="border-background/30 bg-background/15 text-primary-foreground shadow-none"
            />
          ) : null}
        </Button>
      </div>
    </form>
  );
}

function ConditionMenu({
  value,
  onChange,
}: {
  value: WakeupCondition | null;
  onChange: (value: WakeupCondition) => void;
}) {
  const { t } = useT("issues");
  const items: Record<WakeupCondition, { icon: LucideIcon; label: string; hint: string }> = {
    at: { icon: Clock3, label: t(($) => $.wakeups.create.cond_at), hint: t(($) => $.wakeups.create.cond_at_hint) },
    recurring: { icon: RefreshCw, label: t(($) => $.wakeups.create.cond_recurring), hint: t(($) => $.wakeups.create.cond_recurring_hint) },
    reply: { icon: MessageSquare, label: t(($) => $.wakeups.create.cond_reply), hint: t(($) => $.wakeups.create.cond_reply_hint) },
    field: { icon: CircleDot, label: t(($) => $.wakeups.create.cond_field), hint: t(($) => $.wakeups.create.cond_field_hint) },
    run_end: { icon: Bot, label: t(($) => $.wakeups.create.cond_run_end), hint: t(($) => $.wakeups.create.cond_run_end_hint) },
    children: { icon: ListChecks, label: t(($) => $.wakeups.create.cond_children), hint: t(($) => $.wakeups.create.cond_children_hint) },
    pull_request: { icon: GitPullRequest, label: t(($) => $.wakeups.create.cond_pr), hint: t(($) => $.wakeups.create.cond_pr_hint) },
    other_issue: { icon: Link2, label: t(($) => $.wakeups.create.cond_issue), hint: t(($) => $.wakeups.create.cond_issue_hint) },
    custom: { icon: Zap, label: t(($) => $.wakeups.create.cond_custom), hint: t(($) => $.wakeups.create.cond_custom_hint, { count: WAKEUP_EVENT_TYPES.length }) },
  };
  const groups: [string, WakeupCondition[]][] = [
    [t(($) => $.wakeups.create.group_time), ["at", "recurring"]],
    [t(($) => $.wakeups.create.group_collaboration), ["reply", "field"]],
    [t(($) => $.wakeups.create.group_runs), ["run_end", "children"]],
    [t(($) => $.wakeups.create.group_linked), ["pull_request", "other_issue"]],
  ];
  const selected = value ? items[value] : null;
  const SelectedIcon = selected?.icon ?? CircleDashed;
  const option = (key: WakeupCondition) => {
    const Icon = items[key].icon;
    return (
      <DropdownMenuItem key={key} onClick={() => onChange(key)} className="gap-2 py-1.5">
        <Icon className="size-3.5 text-muted-foreground" aria-hidden="true" />
        <span>{items[key].label}</span>
        <span className="ml-auto pl-4 text-caption text-muted-foreground">{items[key].hint}</span>
      </DropdownMenuItem>
    );
  };
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<button type="button" className={cn(pillClass, !value && "border-ring")} />}
      >
        <SelectedIcon className="size-3.5 text-muted-foreground" aria-hidden="true" />
        <span className={cn("truncate", !selected && "text-muted-foreground")}>
          {selected?.label ?? t(($) => $.wakeups.create.choose_condition)}
        </span>
        <ChevronDown className="size-3 text-muted-foreground" aria-hidden="true" />
      </DropdownMenuTrigger>
      <DropdownMenuContent className="w-[22rem]">
        {groups.map(([label, keys]) => (
          <DropdownMenuGroup key={label}>
            <DropdownMenuLabel>{label}</DropdownMenuLabel>
            {keys.map(option)}
          </DropdownMenuGroup>
        ))}
        <DropdownMenuSeparator />
        {option("custom")}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function ConditionParams({
  draft,
  workspaceId,
  issueId,
  update,
  onBusy,
}: {
  draft: WakeupDraft;
  workspaceId: string;
  issueId: string;
  update: (patch: Partial<WakeupDraft>) => void;
  onBusy?: (busy: boolean) => void;
}) {
  const { t } = useT("issues");
  const text = useWakeupText();
  switch (draft.condition) {
    case "field":
      return <FieldParams draft={draft} workspaceId={workspaceId} update={update} />;
    case "children":
      return <StageChoice workspaceId={workspaceId} issueId={issueId} value={draft.stage} onChange={(stage) => update({ stage })} />;
    case "pull_request":
      return (
        <PillMenu
          label={t(($) => $.wakeups.create.cond_pr)}
          value={draft.prEvent}
          options={[
            { value: "checks_finished", label: t(($) => $.wakeups.create.pr_checks) },
            { value: "merged", label: t(($) => $.wakeups.create.pr_merged) },
          ]}
          onChange={(prEvent) => update({ prEvent: prEvent as WakeupDraft["prEvent"] })}
        />
      );
    case "other_issue":
      return (
        <>
          <OtherIssueChoice issueId={issueId} value={draft.otherIssue} onChange={(otherIssue) => update({ otherIssue })} onBusy={onBusy} />
          <PillMenu
            label={t(($) => $.wakeups.create.cond_issue)}
            value={draft.otherState}
            options={[
              { value: "done", label: t(($) => $.wakeups.create.other_done) },
              { value: "ended", label: t(($) => $.wakeups.create.other_ended) },
              { value: "in_review", label: t(($) => $.wakeups.create.other_in_review) },
            ]}
            onChange={(otherState) => update({ otherState: otherState as WakeupDraft["otherState"] })}
          />
        </>
      );
    case "at":
      return (
        <PillMenu
          label={t(($) => $.wakeups.create.cond_at)}
          value={draft.atPreset}
          options={[
            { value: "10m", label: t(($) => $.wakeups.create.at_10m) },
            { value: "1h", label: t(($) => $.wakeups.create.at_1h) },
            { value: "tomorrow", label: t(($) => $.wakeups.create.at_tomorrow) },
            { value: "custom", label: t(($) => $.wakeups.create.at_custom) },
          ]}
          onChange={(atPreset) => update({ atPreset: atPreset as WakeupAtPreset })}
        />
      );
    case "recurring":
      return (
        <PillMenu
          label={t(($) => $.wakeups.create.cond_recurring)}
          value={draft.recurrence}
          options={[
            { value: "hourly", label: t(($) => $.wakeups.create.hourly) },
            { value: "daily", label: t(($) => $.wakeups.create.daily) },
            { value: "weekdays", label: t(($) => $.wakeups.create.weekdays) },
          ]}
          onChange={(recurrence) => update({ recurrence: recurrence as WakeupRecurrence })}
        />
      );
    case "reply":
      return <ActorChoice workspaceId={workspaceId} value={draft.replyActor} onChange={(replyActor) => update({ replyActor })} />;
    case "run_end":
      return (
        <AgentChoice
          workspaceId={workspaceId}
          value={draft.runAgentId}
          onChange={(runAgentId) => update({ runAgentId })}
          placeholder={t(($) => $.wakeups.create.any_agent)}
          allowAny
        />
      );
    case "custom":
      return (
        <DropdownMenu>
          <DropdownMenuTrigger render={<button type="button" className={pillClass} />}>
            <span className={cn(draft.events.length === 0 && "text-muted-foreground")}>
              {t(($) => $.wakeups.create.events_selected, { count: draft.events.length })}
            </span>
            <ChevronDown className="size-3 text-muted-foreground" aria-hidden="true" />
          </DropdownMenuTrigger>
          <DropdownMenuContent className="max-h-80 w-80">
            {WAKEUP_EVENT_TYPES.map((event) => (
              <DropdownMenuCheckboxItem
                key={event}
                checked={draft.events.includes(event)}
                closeOnClick={false}
                onCheckedChange={(checked) =>
                  update({
                    events: checked
                      ? WAKEUP_EVENT_TYPES.filter((e) => e === event || draft.events.includes(e))
                      : draft.events.filter((e) => e !== event),
                  })
                }
              >
                {text.eventName(event)}
              </DropdownMenuCheckboxItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      );
    default:
      return null;
  }
}

function PillMenu({
  label,
  value,
  options,
  onChange,
  placeholder,
}: {
  label: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
  /** Shown until a value is chosen. */
  placeholder?: string;
}) {
  const current = options.find((option) => option.value === value);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<button type="button" className={pillClass} aria-label={`${label}: ${current?.label ?? ""}`} />}>
        <span className={cn("truncate", !current && "text-muted-foreground")}>{current?.label ?? placeholder}</span>
        <ChevronDown className="size-3 text-muted-foreground" aria-hidden="true" />
      </DropdownMenuTrigger>
      <DropdownMenuContent className="max-h-80">
        <DropdownMenuRadioGroup value={value} onValueChange={(next) => onChange(String(next))}>
          {options.map((option) => (
            <DropdownMenuRadioItem key={option.value} value={option.value}>
              {option.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function Segmented<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: { value: T; label: string }[];
  onChange: (value: T) => void;
}) {
  return (
    <div role="radiogroup" aria-label={label} className="inline-flex w-fit gap-0.5 rounded-lg bg-muted p-0.5">
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          role="radio"
          aria-checked={value === option.value}
          onClick={() => onChange(option.value)}
          className={cn(
            "h-6 rounded-md px-2.5 text-caption text-muted-foreground outline-none focus-visible:ring-3 focus-visible:ring-ring/50",
            value === option.value && "bg-surface font-medium text-foreground shadow-[var(--surface-shadow)]",
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

function useMatches() {
  const [filter, setFilter] = useState("");
  const query = filter.trim().toLowerCase();
  const matches = (name: string) => !query || name.toLowerCase().includes(query) || matchesPinyin(name, query);
  return { setFilter, matches };
}

function AgentChoice({
  workspaceId,
  value,
  onChange,
  placeholder,
  allowAny = false,
}: {
  workspaceId: string;
  value: string;
  onChange: (id: string) => void;
  placeholder: string;
  allowAny?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const { setFilter, matches } = useMatches();
  const { data = [] } = useQuery(agentListOptions(workspaceId));
  const agents = useMemo(() => data.filter((a) => !a.archived_at), [data]);
  const selected = agents.find((a) => a.id === value);
  const pick = (id: string) => {
    onChange(id);
    setOpen(false);
  };
  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-56"
      align="start"
      searchable
      onSearchChange={setFilter}
      triggerRender={<button type="button" className={pillClass} />}
      trigger={
        <>
          {selected ? (
            <ActorAvatar actorType="agent" actorId={selected.id} size="sm" />
          ) : (
            <Bot className="size-3.5 text-muted-foreground" aria-hidden="true" />
          )}
          <span className={cn("truncate", !selected && "text-muted-foreground")}>{selected?.name ?? placeholder}</span>
          <ChevronDown className="size-3 text-muted-foreground" aria-hidden="true" />
        </>
      }
    >
      {allowAny && (
        <PickerItem selected={!value} onClick={() => pick("")}>
          <Bot className="size-3.5 text-muted-foreground" aria-hidden="true" />
          <span className="truncate">{placeholder}</span>
        </PickerItem>
      )}
      {agents.filter((a) => matches(a.name)).length === 0 && !allowAny ? (
        <PickerEmpty />
      ) : (
        agents
          .filter((a) => matches(a.name))
          .map((a) => (
            <PickerItem
              key={a.id}
              selected={a.id === value}
              disabled={!allowAny && !isAgentRuntimeBound(a)}
              onClick={() => pick(a.id)}
            >
              <ActorAvatar actorType="agent" actorId={a.id} size="sm" showStatusDot />
              <span className="truncate">{a.name}</span>
            </PickerItem>
          ))
      )}
    </PropertyPicker>
  );
}

function ActorChoice({
  workspaceId,
  value,
  onChange,
}: {
  workspaceId: string;
  value: WakeupDraft["replyActor"];
  onChange: (value: WakeupDraft["replyActor"]) => void;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
  const { setFilter, matches } = useMatches();
  const { data: members = [] } = useQuery(memberListOptions(workspaceId));
  const { data: agentRows = [] } = useQuery(agentListOptions(workspaceId));
  const agents = agentRows.filter((a) => !a.archived_at);
  const name =
    value?.type === "member"
      ? members.find((m) => m.user_id === value.id)?.name
      : value?.type === "agent"
        ? agents.find((a) => a.id === value.id)?.name
        : undefined;
  const pick = (next: WakeupDraft["replyActor"]) => {
    onChange(next);
    setOpen(false);
  };
  const section = (label: string, rows: ReactNode[]) =>
    rows.length > 0 && <PickerSection label={label}>{rows}</PickerSection>;
  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-56"
      align="start"
      searchable
      onSearchChange={setFilter}
      triggerRender={<button type="button" className={pillClass} />}
      trigger={
        <>
          {value ? (
            <ActorAvatar actorType={value.type} actorId={value.id} size="sm" />
          ) : null}
          <span className="truncate">{name ?? t(($) => $.wakeups.create.anyone)}</span>
          <ChevronDown className="size-3 text-muted-foreground" aria-hidden="true" />
        </>
      }
    >
      <PickerItem selected={!value} onClick={() => pick(null)}>
        <span className="truncate">{t(($) => $.wakeups.create.anyone)}</span>
      </PickerItem>
      {section(
        t(($) => $.wakeups.create.members),
        members
          .filter((m) => matches(m.name))
          .map((m) => (
            <PickerItem
              key={m.user_id}
              selected={value?.type === "member" && value.id === m.user_id}
              onClick={() => pick({ type: "member", id: m.user_id })}
            >
              <ActorAvatar actorType="member" actorId={m.user_id} size="sm" />
              <span className="truncate">{m.name}</span>
            </PickerItem>
          )),
      )}
      {section(
        t(($) => $.wakeups.create.agents),
        agents
          .filter((a) => matches(a.name))
          .map((a) => (
            <PickerItem
              key={a.id}
              selected={value?.type === "agent" && value.id === a.id}
              onClick={() => pick({ type: "agent", id: a.id })}
            >
              <ActorAvatar actorType="agent" actorId={a.id} size="sm" />
              <span className="truncate">{a.name}</span>
            </PickerItem>
          )),
      )}
    </PropertyPicker>
  );
}

/** Field, then the value it has to reach. */
function FieldParams({
  draft,
  workspaceId,
  update,
}: {
  draft: WakeupDraft;
  workspaceId: string;
  update: (patch: Partial<WakeupDraft>) => void;
}) {
  const { t } = useT("issues");
  const statuses = useIssueStatuses(workspaceId);
  const statusLabel = useStatusLabel(workspaceId);
  const { data: labels = [] } = useQuery(labelListOptions(workspaceId));
  const { data: allProperties = [] } = useQuery(propertyListOptions(workspaceId));
  const properties = allProperties.filter((p) => !p.archived);
  const property = properties.find((p) => p.id === draft.fieldTarget);
  const choose = (field: WakeupField) => update({ field, fieldTarget: "", fieldValue: "", assignee: null });
  const fieldMenu = (
    <PillMenu
      label={t(($) => $.wakeups.create.field_kind)}
      value={draft.field}
      options={[
        { value: "status", label: t(($) => $.wakeups.create.field_status) },
        { value: "assignee", label: t(($) => $.wakeups.create.field_assignee) },
        { value: "label", label: t(($) => $.wakeups.create.field_label) },
        { value: "property", label: t(($) => $.wakeups.create.field_property) },
      ]}
      onChange={(field) => choose(field as WakeupField)}
    />
  );
  let value: ReactNode = null;
  if (draft.field === "status") {
    value = (
      <PillMenu
        label={t(($) => $.wakeups.create.choose_status)}
        placeholder={t(($) => $.wakeups.create.choose_status)}
        value={draft.fieldTarget}
        options={statuses.activeStatuses.map((s) => ({ value: s.key, label: statusLabel(s.key) }))}
        onChange={(fieldTarget) => update({ fieldTarget })}
      />
    );
  } else if (draft.field === "assignee") {
    value = <AssigneeChoice workspaceId={workspaceId} value={draft.assignee} onChange={(assignee) => update({ assignee })} />;
  } else if (draft.field === "label") {
    value = (
      <PillMenu
        label={t(($) => $.wakeups.create.choose_label)}
        placeholder={t(($) => $.wakeups.create.choose_label)}
        value={draft.fieldTarget}
        options={labels.map((l) => ({ value: l.id, label: l.name }))}
        onChange={(fieldTarget) => update({ fieldTarget })}
      />
    );
  } else {
    value = (
      <>
        <PillMenu
          label={t(($) => $.wakeups.create.choose_property)}
          placeholder={t(($) => $.wakeups.create.choose_property)}
          value={draft.fieldTarget}
          options={properties.map((p) => ({ value: p.id, label: p.name }))}
          onChange={(fieldTarget) => update({ fieldTarget, fieldValue: "" })}
        />
        {property &&
          (property.type === "select" || property.type === "multi_select" ? (
            <PillMenu
              label={property.name}
              placeholder={t(($) => $.wakeups.create.value_placeholder)}
              value={draft.fieldValue}
              options={(property.config.options ?? []).map((o) => ({ value: o.id, label: o.name }))}
              onChange={(fieldValue) => update({ fieldValue })}
            />
          ) : property.type === "checkbox" ? (
            <PillMenu
              label={property.name}
              placeholder={t(($) => $.wakeups.create.value_placeholder)}
              value={draft.fieldValue}
              options={[
                { value: "true", label: t(($) => $.wakeups.create.checked) },
                { value: "false", label: t(($) => $.wakeups.create.unchecked) },
              ]}
              onChange={(fieldValue) => update({ fieldValue })}
            />
          ) : (
            <Input
              aria-label={property.name}
              placeholder={t(($) => $.wakeups.create.value_placeholder)}
              type={property.type === "number" ? "number" : "text"}
              className="h-7 w-32 text-base md:text-label"
              value={draft.fieldValue}
              onChange={(event) => update({ fieldValue: event.target.value })}
            />
          ))}
      </>
    );
  }
  return (
    <>
      {fieldMenu}
      {value}
    </>
  );
}

/** Every sub-issue, or one of the stages this issue's sub-issues use. */
function StageChoice({
  workspaceId,
  issueId,
  value,
  onChange,
}: {
  workspaceId: string;
  issueId: string;
  value: number | null;
  onChange: (stage: number | null) => void;
}) {
  const { t } = useT("issues");
  const { data: children = [] } = useQuery(childIssuesOptions(workspaceId, issueId));
  const stages = [...new Set(children.map((c) => c.stage).filter((s): s is number => typeof s === "number"))].sort((a, b) => a - b);
  return (
    <PillMenu
      label={t(($) => $.wakeups.create.cond_children)}
      value={value === null ? "all" : String(value)}
      options={[
        { value: "all", label: t(($) => $.wakeups.create.children_all) },
        ...stages.map((stage) => ({ value: String(stage), label: t(($) => $.wakeups.create.children_stage, { stage }) })),
      ]}
      onChange={(next) => onChange(next === "all" ? null : Number(next))}
    />
  );
}

function OtherIssueChoice({
  issueId,
  value,
  onChange,
  onBusy,
}: {
  issueId: string;
  value: WakeupDraft["otherIssue"];
  onChange: (value: WakeupDraft["otherIssue"]) => void;
  onBusy?: (busy: boolean) => void;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
  // The picker is a dialog over this popover; keep the popover open while it is.
  const toggle = (next: boolean) => {
    onBusy?.(next);
    setOpen(next);
  };
  return (
    <>
      <button type="button" className={pillClass} onClick={() => toggle(true)}>
        <Link2 className="size-3.5 text-muted-foreground" aria-hidden="true" />
        <span className={cn("truncate", !value && "text-muted-foreground")}>
          {value?.identifier ?? t(($) => $.wakeups.create.choose_issue)}
        </span>
      </button>
      <IssuePickerModal
        open={open}
        onOpenChange={toggle}
        title={t(($) => $.wakeups.create.pick_issue_title)}
        description={t(($) => $.wakeups.create.pick_issue_description)}
        excludeIds={[issueId]}
        onSelect={(issue) => {
          onChange({ id: issue.id, identifier: issue.identifier });
          toggle(false);
        }}
      />
    </>
  );
}

function AssigneeChoice({
  workspaceId,
  value,
  onChange,
}: {
  workspaceId: string;
  value: WakeupDraft["assignee"];
  onChange: (value: WakeupDraft["assignee"]) => void;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
  const { setFilter, matches } = useMatches();
  const { data: members = [] } = useQuery(memberListOptions(workspaceId));
  const { data: agentRows = [] } = useQuery(agentListOptions(workspaceId));
  const { data: squads = [] } = useQuery(squadListOptions(workspaceId));
  const agents = agentRows.filter((a) => !a.archived_at);
  const name =
    value?.type === "member"
      ? members.find((m) => m.user_id === value.id)?.name
      : value?.type === "agent"
        ? agents.find((a) => a.id === value.id)?.name
        : value?.type === "squad"
          ? squads.find((s) => s.id === value.id)?.name
          : undefined;
  const pick = (next: WakeupDraft["assignee"]) => {
    onChange(next);
    setOpen(false);
  };
  const section = (label: string, rows: ReactNode[]) =>
    rows.length > 0 && <PickerSection label={label}>{rows}</PickerSection>;
  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-56"
      align="start"
      searchable
      onSearchChange={setFilter}
      triggerRender={<button type="button" className={pillClass} />}
      trigger={
        <>
          {value && value.type !== "squad" ? <ActorAvatar actorType={value.type} actorId={value.id} size="sm" /> : null}
          <span className={cn("truncate", !name && "text-muted-foreground")}>{name ?? t(($) => $.wakeups.create.choose_assignee)}</span>
          <ChevronDown className="size-3 text-muted-foreground" aria-hidden="true" />
        </>
      }
    >
      {section(
        t(($) => $.wakeups.create.members),
        members
          .filter((m) => matches(m.name))
          .map((m) => (
            <PickerItem
              key={m.user_id}
              selected={value?.type === "member" && value.id === m.user_id}
              onClick={() => pick({ type: "member", id: m.user_id })}
            >
              <ActorAvatar actorType="member" actorId={m.user_id} size="sm" />
              <span className="truncate">{m.name}</span>
            </PickerItem>
          )),
      )}
      {section(
        t(($) => $.wakeups.create.agents),
        agents
          .filter((a) => matches(a.name))
          .map((a) => (
            <PickerItem key={a.id} selected={value?.type === "agent" && value.id === a.id} onClick={() => pick({ type: "agent", id: a.id })}>
              <ActorAvatar actorType="agent" actorId={a.id} size="sm" />
              <span className="truncate">{a.name}</span>
            </PickerItem>
          )),
      )}
      {section(
        t(($) => $.wakeups.create.squads),
        squads
          .filter((s) => matches(s.name))
          .map((s) => (
            <PickerItem key={s.id} selected={value?.type === "squad" && value.id === s.id} onClick={() => pick({ type: "squad", id: s.id })}>
              <span className="truncate">{s.name}</span>
            </PickerItem>
          )),
      )}
    </PropertyPicker>
  );
}
