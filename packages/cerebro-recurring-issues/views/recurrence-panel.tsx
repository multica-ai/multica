"use client";

import { useEffect, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { CalendarClock, Check, CircleCheck, Repeat } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import {
  Drawer,
  DrawerContent,
  DrawerHeader,
  DrawerTitle,
  DrawerTrigger,
} from "@multica/ui/components/ui/drawer";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { cn } from "@multica/ui/lib/utils";

import {
  issueRecurrenceOptions,
  useDeleteIssueRecurrence,
  useUpsertIssueRecurrence,
} from "../core/queries";
import { statusLabel, summarizeRecurrencePlan } from "../core/summary";
import {
  DATE_FORMATS,
  DATE_PLACEHOLDER,
  FREQUENCIES,
  ISSUE_STATUSES,
  WEEKDAYS,
  type Anchor,
  type DateFormat,
  type Frequency,
  type IssueRecurrence,
  type RecurrenceWriteInput,
  type StaleHandling,
  type TriggerType,
} from "../core/types";

interface Props {
  workspaceId: string;
  issueId: string;
}

const FREQUENCY_LABELS: Record<Frequency, string> = {
  daily: "Daily",
  weekly: "Weekly",
  monthly: "Monthly",
  yearly: "Yearly",
  days_after: "Days after",
  custom: "Custom",
  every_weekday: "Every weekday",
};

function defaultForm(): RecurrenceWriteInput {
  return {
    frequency: "weekly",
    interval_count: 1,
    weekdays: [],
    days_after: 7,
    trigger_status: "done",
    anchor: "completion",
    create_new_issue: true,
    new_status: "todo",
    recur_forever: true,
    enabled: true,
    trigger_type: "completion",
    stale_handling: "leave_open",
    stale_status: "cancelled",
    date_format: "iso",
  };
}

function formFromExisting(r: IssueRecurrence): RecurrenceWriteInput {
  return {
    frequency: r.frequency,
    interval_count: r.interval_count,
    weekdays: r.weekdays,
    days_after: r.days_after,
    trigger_status: r.trigger_status,
    anchor: r.anchor,
    create_new_issue: r.create_new_issue,
    new_status: r.new_status,
    recur_forever: r.recur_forever,
    end_date: r.end_date,
    max_occurrences: r.max_occurrences,
    enabled: r.enabled,
    trigger_type: r.trigger_type,
    stale_handling: r.stale_handling,
    stale_status: r.stale_status,
    date_format: r.date_format,
  };
}

function summarize(r: IssueRecurrence): string {
  const base = FREQUENCY_LABELS[r.frequency] ?? r.frequency;
  const label = r.trigger_type === "schedule" ? `${base} (scheduled)` : base;
  if (!r.enabled) return `${label} (paused)`;
  return label;
}

/** A large, tappable card used for the mode choice and the either/or rows. */
function ChoiceCard({
  selected,
  onSelect,
  icon,
  title,
  description,
  children,
}: {
  selected: boolean;
  onSelect: () => void;
  icon?: ReactNode;
  title: ReactNode;
  description: ReactNode;
  children?: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={selected}
      className={cn(
        "flex min-h-12 w-full items-start gap-2.5 rounded-lg border px-3 py-2.5 text-left transition-colors",
        selected
          ? "border-primary bg-primary/5 ring-1 ring-primary/30"
          : "border-border bg-card hover:bg-accent/30"
      )}
    >
      {icon && (
        <span className={cn("mt-0.5 shrink-0", selected ? "text-primary" : "text-muted-foreground")}>
          {icon}
        </span>
      )}
      <span className="grid gap-0.5">
        <span className="flex items-center gap-1.5 text-sm font-medium">{title}</span>
        <span className="text-xs leading-snug text-muted-foreground">{description}</span>
        {children}
      </span>
      <Check
        className={cn(
          "ml-auto mt-0.5 h-4 w-4 shrink-0 text-primary transition-opacity",
          selected ? "opacity-100" : "opacity-0"
        )}
      />
    </button>
  );
}

/**
 * RecurrenceForm is the single-column body shared by both surfaces — it drops
 * into the desktop popover and the mobile bottom sheet unchanged (FIR-1682).
 */
function RecurrenceForm({
  form,
  setForm,
  issueTitlePreview,
}: {
  form: RecurrenceWriteInput;
  setForm: (next: RecurrenceWriteInput) => void;
  issueTitlePreview: string;
}) {
  const isSchedule = form.trigger_type === "schedule";
  // days_after is completion-relative ("N days after it was closed"), so it is
  // not offered for schedule rules.
  const frequencyOptions = isSchedule
    ? FREQUENCIES.filter((f) => f !== "days_after")
    : FREQUENCIES;
  const showWeekdays = form.frequency === "weekly" || form.frequency === "custom";
  const showDaysAfter = !isSchedule && form.frequency === "days_after";
  const showInterval = form.frequency !== "every_weekday";

  function selectTriggerType(v: TriggerType) {
    setForm({
      ...form,
      trigger_type: v,
      // Schedule rules always spawn an independent new issue and cannot use the
      // completion-only days_after frequency.
      create_new_issue: v === "schedule" ? true : form.create_new_issue,
      frequency: v === "schedule" && form.frequency === "days_after" ? "weekly" : form.frequency,
    });
  }

  function toggleWeekday(day: number) {
    const set = new Set(form.weekdays ?? []);
    if (set.has(day)) set.delete(day);
    else set.add(day);
    setForm({ ...form, weekdays: Array.from(set).sort((a, b) => a - b) });
  }

  const datePreview = DATE_FORMATS.find((d) => d.value === (form.date_format ?? "iso"))?.label;

  return (
    <div className="grid gap-4">
      {/* Mode cards */}
      <div className="grid gap-1.5">
        <Label className="text-xs text-muted-foreground">How should it repeat?</Label>
        <div className="grid gap-2">
          <ChoiceCard
            selected={!isSchedule}
            onSelect={() => selectTriggerType("completion")}
            icon={<CircleCheck className="h-5 w-5" />}
            title="When I finish it"
            description="The next task appears only after I mark this one done. For step-by-step chores."
          />
          <ChoiceCard
            selected={isSchedule}
            onSelect={() => selectTriggerType("schedule")}
            icon={<CalendarClock className="h-5 w-5" />}
            title="On a fixed date"
            description="A new task appears on every date no matter what. For calendar duties."
          />
        </div>
      </div>

      {/* Frequency + interval */}
      <div className={showInterval ? "grid grid-cols-2 gap-2" : "grid gap-2"}>
        <div className="grid gap-1.5">
          <Label htmlFor="rec-freq" className="text-xs">Frequency</Label>
          <Select
            value={form.frequency}
            onValueChange={(v) => setForm({ ...form, frequency: v as Frequency })}
          >
            <SelectTrigger id="rec-freq" className="min-h-11">
              <SelectValue>
                {() =>
                  form.frequency ? FREQUENCY_LABELS[form.frequency] ?? form.frequency : null
                }
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {frequencyOptions.map((f) => (
                <SelectItem key={f} value={f}>
                  {FREQUENCY_LABELS[f]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {showInterval && (showDaysAfter ? (
          <div className="grid gap-1.5">
            <Label htmlFor="rec-days" className="text-xs">Days after</Label>
            <Input
              id="rec-days"
              type="number"
              min={0}
              className="min-h-11"
              value={form.days_after ?? 0}
              onChange={(e) => setForm({ ...form, days_after: Number(e.target.value) })}
            />
          </div>
        ) : (
          <div className="grid gap-1.5">
            <Label htmlFor="rec-interval" className="text-xs">Every</Label>
            <Input
              id="rec-interval"
              type="number"
              min={1}
              className="min-h-11"
              value={form.interval_count ?? 1}
              onChange={(e) => setForm({ ...form, interval_count: Number(e.target.value) })}
            />
          </div>
        ))}
      </div>

      {/* Weekday picker (weekly / custom) */}
      {showWeekdays && (
        <div className="grid gap-1.5">
          <Label className="text-xs">On weekdays</Label>
          <div className="flex gap-1">
            {WEEKDAYS.map((d) => {
              const active = (form.weekdays ?? []).includes(d.value);
              return (
                <button
                  key={d.value}
                  type="button"
                  onClick={() => toggleWeekday(d.value)}
                  className={cn(
                    "h-11 flex-1 rounded text-xs transition-colors",
                    active
                      ? "bg-primary text-primary-foreground"
                      : "bg-muted text-muted-foreground hover:bg-accent"
                  )}
                >
                  {d.label}
                </button>
              );
            })}
          </div>
        </div>
      )}

      {/* Schedule-based: first-occurrence hint */}
      {isSchedule && (
        <p className="text-xs leading-snug text-muted-foreground">
          First task lands on this issue&apos;s due date, then advances by the schedule.
        </p>
      )}

      {/* Completion-based: trigger status */}
      {!isSchedule && (
        <div className="grid gap-1.5">
          <Label htmlFor="rec-trigger" className="text-xs">
            Create the next task when I move this one to
          </Label>
          <Select
            value={form.trigger_status}
            onValueChange={(v) => setForm({ ...form, trigger_status: v ?? "" })}
          >
            <SelectTrigger id="rec-trigger" className="min-h-11">
              <SelectValue>{() => statusLabel(form.trigger_status)}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {ISSUE_STATUSES.map((s) => (
                <SelectItem key={s} value={s}>
                  {statusLabel(s)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-xs leading-snug text-muted-foreground">
            This status is the trigger — move the task here and Multica spins up the next one.
          </p>
        </div>
      )}

      {/* Completion-based: anchor */}
      {!isSchedule && (
        <div className="grid gap-1.5">
          <Label className="text-xs text-muted-foreground">Count the next date from</Label>
          <div className="grid gap-2">
            <ChoiceCard
              selected={form.anchor !== "due_date"}
              onSelect={() => setForm({ ...form, anchor: "completion" as Anchor })}
              title="The day I actually finish it"
              description="Finish 3 days late and the whole rhythm shifts 3 days."
            />
            <ChoiceCard
              selected={form.anchor === "due_date"}
              onSelect={() => setForm({ ...form, anchor: "due_date" as Anchor })}
              title="Its due date"
              description="Stays glued to the calendar — a Monday task stays Monday. For rent, reports, payments."
            />
          </div>
        </div>
      )}

      {/* Completion-based: create-new vs reopen-same */}
      {!isSchedule && (
        <div className="grid gap-1.5">
          <Label className="text-xs text-muted-foreground">The next task should be…</Label>
          <div className="grid gap-2">
            <ChoiceCard
              selected={form.create_new_issue ?? true}
              onSelect={() => setForm({ ...form, create_new_issue: true })}
              title="A fresh copy each time"
              description="A separate task per repeat, each with its own history. Copies title, priority, assignee, labels & files."
            />
            <ChoiceCard
              selected={!(form.create_new_issue ?? true)}
              onSelect={() => setForm({ ...form, create_new_issue: false })}
              title="This same task, reopened"
              description="One task that loops — status resets, date moves forward. No history trail."
            />
          </div>
        </div>
      )}

      {/* Schedule-based: stale handling */}
      {isSchedule && (
        <div className="grid gap-1.5">
          <Label className="text-xs text-muted-foreground">
            If the last one is still open when the next is due
          </Label>
          <div className="grid gap-2">
            <ChoiceCard
              selected={(form.stale_handling ?? "leave_open") !== "auto_close"}
              onSelect={() => setForm({ ...form, stale_handling: "leave_open" as StaleHandling })}
              title="Leave it open"
              description="Both tasks live side by side."
            />
            <ChoiceCard
              selected={form.stale_handling === "auto_close"}
              onSelect={() => setForm({ ...form, stale_handling: "auto_close" as StaleHandling })}
              title="Close it automatically"
              description="The unfinished one is closed for you."
            >
              {form.stale_handling === "auto_close" && (
                <span className="mt-1.5 block" onClick={(e) => e.stopPropagation()}>
                  <Select
                    value={form.stale_status ?? "cancelled"}
                    onValueChange={(v) => setForm({ ...form, stale_status: v ?? "" })}
                  >
                    <SelectTrigger className="min-h-11 w-full">
                      <SelectValue>
                        {() => `Close it as ${statusLabel(form.stale_status ?? "cancelled")}`}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {ISSUE_STATUSES.map((s) => (
                        <SelectItem key={s} value={s}>
                          {statusLabel(s)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </span>
              )}
            </ChoiceCard>
          </div>
        </div>
      )}

      {/* Schedule-based: date stamp */}
      {isSchedule && (
        <div className="grid gap-1.5">
          <Label htmlFor="rec-date-format" className="text-xs">Date stamp in the title</Label>
          <div className="rounded-md border border-border bg-muted/40 px-2.5 py-2 text-xs text-muted-foreground">
            <span className="font-medium text-foreground">{issueTitlePreview}</span>
          </div>
          <Select
            value={form.date_format ?? "iso"}
            onValueChange={(v) => setForm({ ...form, date_format: (v ?? "iso") as DateFormat })}
          >
            <SelectTrigger id="rec-date-format" className="min-h-11">
              <SelectValue>{() => datePreview}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {DATE_FORMATS.map((d) => (
                <SelectItem key={d.value} value={d.value}>
                  {d.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-xs leading-snug text-muted-foreground">
            Type <code>{DATE_PLACEHOLDER}</code> in the title — each new task swaps it for its
            own date.
          </p>
        </div>
      )}

      {/* It starts as */}
      <div className="grid gap-1.5">
        <Label htmlFor="rec-new-status" className="text-xs">It starts as</Label>
        <Select
          value={form.new_status}
          onValueChange={(v) => setForm({ ...form, new_status: v ?? "" })}
        >
          <SelectTrigger id="rec-new-status" className="min-h-11">
            <SelectValue>{() => statusLabel(form.new_status)}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {ISSUE_STATUSES.map((s) => (
              <SelectItem key={s} value={s}>
                {statusLabel(s)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {/* Keep repeating */}
      <label className="flex min-h-11 items-center gap-2.5 text-sm">
        <input
          type="checkbox"
          className="h-4 w-4 accent-primary"
          checked={form.recur_forever ?? true}
          onChange={(e) => setForm({ ...form, recur_forever: e.target.checked })}
        />
        Keep repeating forever
      </label>

      {/* What will happen */}
      <div className="grid gap-1">
        <Label className="text-xs text-muted-foreground">What will happen</Label>
        <p className="rounded-md border border-primary/20 bg-primary/5 px-3 py-2.5 text-xs leading-relaxed text-foreground">
          {summarizeRecurrencePlan(form)}
        </p>
      </div>
    </div>
  );
}

export function RecurrencePanel({ workspaceId, issueId }: Props) {
  const query = useQuery(issueRecurrenceOptions(workspaceId, issueId));
  const existing = query.data ?? null;
  const upsert = useUpsertIssueRecurrence(workspaceId, issueId);
  const remove = useDeleteIssueRecurrence(workspaceId, issueId);
  const isMobile = useIsMobile();

  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<RecurrenceWriteInput>(defaultForm());

  // Re-seed the form whenever the panel opens, so it reflects the latest saved
  // state (or defaults for a not-yet-recurring issue).
  useEffect(() => {
    if (open) setForm(existing ? formFromExisting(existing) : defaultForm());
  }, [open, existing]);

  function save() {
    upsert.mutate(form, {
      onSuccess: () => {
        setOpen(false);
        toast.success(existing ? "Recurrence updated" : "Issue set to recurring");
      },
      onError: () => toast.error("Failed to save recurrence"),
    });
  }

  function stop() {
    remove.mutate(undefined, {
      onSuccess: () => {
        setOpen(false);
        toast.success("Recurrence removed");
      },
      onError: () => toast.error("Failed to remove recurrence"),
    });
  }

  const triggerContent = (
    <>
      <Repeat className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      <span className="truncate">
        {existing ? summarize(existing) : <span className="text-muted-foreground">Not recurring</span>}
      </span>
    </>
  );

  const triggerClassName =
    "flex cursor-pointer items-center gap-1.5 overflow-hidden rounded -mx-1 px-1 transition-colors hover:bg-accent/30";

  const footer = (
    <div className="flex items-center justify-between gap-2">
      {existing ? (
        <Button variant="ghost" size="sm" onClick={stop} disabled={remove.isPending}>
          Stop repeating
        </Button>
      ) : (
        <span />
      )}
      <Button size="sm" onClick={save} disabled={upsert.isPending}>
        <Check className="mr-1 h-3.5 w-3.5" />
        Save
      </Button>
    </div>
  );

  const body = (
    <RecurrenceForm form={form} setForm={setForm} issueTitlePreview={`This task — ${DATE_PLACEHOLDER}`} />
  );

  if (isMobile) {
    return (
      <Drawer open={open} onOpenChange={setOpen}>
        <DrawerTrigger className={triggerClassName}>{triggerContent}</DrawerTrigger>
        <DrawerContent>
          <DrawerHeader className="pb-2">
            <DrawerTitle>Repeat this task</DrawerTitle>
          </DrawerHeader>
          <div className="overflow-y-auto px-4 pb-2">{body}</div>
          <div className="sticky bottom-0 border-t bg-popover p-4">{footer}</div>
        </DrawerContent>
      </Drawer>
    );
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger className={triggerClassName}>{triggerContent}</PopoverTrigger>
      <PopoverContent align="start" className="w-80 p-0">
        <div className="grid gap-3 p-3">
          <div className="text-sm font-medium">Repeat this task</div>
          <div className="max-h-[60vh] overflow-y-auto pr-0.5">{body}</div>
          {footer}
        </div>
      </PopoverContent>
    </Popover>
  );
}
