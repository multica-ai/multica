"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Repeat, Check } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";

import {
  issueRecurrenceOptions,
  useDeleteIssueRecurrence,
  useUpsertIssueRecurrence,
} from "../core/queries";
import {
  FREQUENCIES,
  ISSUE_STATUSES,
  WEEKDAYS,
  type Anchor,
  type Frequency,
  type IssueRecurrence,
  type RecurrenceWriteInput,
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
  };
}

function summarize(r: IssueRecurrence): string {
  const base = FREQUENCY_LABELS[r.frequency] ?? r.frequency;
  if (!r.enabled) return `${base} (paused)`;
  return base;
}

/**
 * RecurrencePanel is the issue-sidebar control for the recurring-issues
 * feature (TECH-3064 / FIR-334). It shows the issue's recurrence state and
 * opens a popover with the full config — frequency, weekday picker, trigger
 * status, "create new task", "recur forever", "update status to", and "sync
 * recurrence to due date" — mirroring the FIR-334 mockup.
 */
export function RecurrencePanel({ workspaceId, issueId }: Props) {
  const query = useQuery(issueRecurrenceOptions(workspaceId, issueId));
  const existing = query.data ?? null;
  const upsert = useUpsertIssueRecurrence(workspaceId, issueId);
  const remove = useDeleteIssueRecurrence(workspaceId, issueId);

  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<RecurrenceWriteInput>(defaultForm());

  // Re-seed the form whenever the popover opens, so it reflects the latest
  // saved state (or defaults for a not-yet-recurring issue).
  useEffect(() => {
    if (open) setForm(existing ? formFromExisting(existing) : defaultForm());
  }, [open, existing]);

  const showWeekdays = form.frequency === "weekly" || form.frequency === "custom";
  const showDaysAfter = form.frequency === "days_after";
  const showInterval = form.frequency !== "every_weekday";

  function toggleWeekday(day: number) {
    const set = new Set(form.weekdays ?? []);
    if (set.has(day)) set.delete(day);
    else set.add(day);
    setForm({ ...form, weekdays: Array.from(set).sort((a, b) => a - b) });
  }

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

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger className="flex cursor-pointer items-center gap-1.5 overflow-hidden rounded -mx-1 px-1 transition-colors hover:bg-accent/30">
        <Repeat className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate">
          {existing ? summarize(existing) : <span className="text-muted-foreground">Not recurring</span>}
        </span>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80 p-3">
        <div className="grid gap-3">
          <div className="text-sm font-medium">Recurring</div>

          {/* Frequency + interval */}
          <div className={showInterval ? "grid grid-cols-2 gap-2" : "grid gap-2"}>
            <div className="grid gap-1.5">
              <Label htmlFor="rec-freq" className="text-xs">Frequency</Label>
              <Select
                value={form.frequency}
                onValueChange={(v) => setForm({ ...form, frequency: v as Frequency })}
              >
                <SelectTrigger id="rec-freq">
                  <SelectValue>
                    {() => FREQUENCY_LABELS[form.frequency] ?? form.frequency}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {FREQUENCIES.map((f) => (
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
                      className={
                        "h-7 w-8 rounded text-xs transition-colors " +
                        (active
                          ? "bg-primary text-primary-foreground"
                          : "bg-muted text-muted-foreground hover:bg-accent")
                      }
                    >
                      {d.label}
                    </button>
                  );
                })}
              </div>
            </div>
          )}

          {/* Trigger status */}
          <div className="grid gap-1.5">
            <Label htmlFor="rec-trigger" className="text-xs">On status change to</Label>
            <Select
              value={form.trigger_status}
              onValueChange={(v) => setForm({ ...form, trigger_status: v ?? "" })}
            >
              <SelectTrigger id="rec-trigger">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ISSUE_STATUSES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {s}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Update status to */}
          <div className="grid gap-1.5">
            <Label htmlFor="rec-new-status" className="text-xs">Update status to</Label>
            <Select
              value={form.new_status}
              onValueChange={(v) => setForm({ ...form, new_status: v ?? "" })}
            >
              <SelectTrigger id="rec-new-status">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ISSUE_STATUSES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {s}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Toggles */}
          <label className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={form.create_new_issue ?? true}
              onCheckedChange={(c) => setForm({ ...form, create_new_issue: c === true })}
            />
            Create new task
          </label>
          <label className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={form.recur_forever ?? true}
              onCheckedChange={(c) => setForm({ ...form, recur_forever: c === true })}
            />
            Recur forever
          </label>
          <label className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={form.anchor === "due_date"}
              onCheckedChange={(c) =>
                setForm({ ...form, anchor: (c === true ? "due_date" : "completion") as Anchor })
              }
            />
            Sync recurrence to due date
          </label>

          <div className="flex items-center justify-between pt-1">
            {existing ? (
              <Button variant="ghost" size="sm" onClick={stop} disabled={remove.isPending}>
                Stop recurring
              </Button>
            ) : (
              <span />
            )}
            <Button size="sm" onClick={save} disabled={upsert.isPending}>
              <Check className="mr-1 h-3.5 w-3.5" />
              Save
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
