"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { CalendarDays, TriangleAlert } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { LazyCalendar } from "@multica/ui/components/ui/lazy-calendar";
import { TimeInput } from "@multica/ui/components/ui/time-input";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { useRescheduleChatWakeup } from "@multica/core/chat/mutations";
import type { ChatWakeup } from "@multica/core/types";
import { useLocale, useT } from "../../i18n";
import { formatSnoozeTime } from "../../inbox/components/inbox-snooze-time";
import {
  INTERVAL_PRESETS_S,
  KEEP,
  WAKEUP_MAX_CHECKS,
  WAKEUP_MAX_REMINDER_MS,
  WAKEUP_MAX_WAIT_MS,
  checkTiming,
  deadlinePresets,
  reminderPresets,
  timingChange,
  type TimingChoice,
} from "./chat-wakeup-timing";

const HOUR_MS = 3_600_000;

function intervalLabel(locale: string, seconds: number): string {
  const [value, unit] = seconds % 3600 === 0 ? [seconds / 3600, "hour"] : [Math.round(seconds / 60), "minute"];
  return new Intl.NumberFormat(locale, { style: "unit", unit, unitDisplay: "long" }).format(value);
}

function inLabel(locale: string, ms: number): string {
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "always" });
  return ms % (24 * HOUR_MS) === 0 ? rtf.format(ms / (24 * HOUR_MS), "day") : rtf.format(ms / HOUR_MS, "hour");
}

function startOfDay(at: number): Date {
  const d = new Date(at);
  return new Date(d.getFullYear(), d.getMonth(), d.getDate());
}

function hhmm(at: number): string {
  const d = new Date(at);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

/** The picked day and time, or null when it is not within (now, now + max]. */
function pickedTime(day: Date, time: string, now: number, maxMs: number): number | null {
  const [hours, minutes] = time.split(":").map(Number);
  if (!Number.isInteger(hours) || !Number.isInteger(minutes)) return null;
  const at = new Date(day.getFullYear(), day.getMonth(), day.getDate(), hours, minutes).getTime();
  return at > now && at <= now + maxMs ? at : null;
}

function Choice({
  selected,
  onClick,
  children,
}: {
  selected: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <Button type="button" size="xs" variant={selected ? "brand" : "outline"} aria-pressed={selected} onClick={onClick}>
      {children}
    </Button>
  );
}

function Line({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div role="group" aria-label={label} className="grid grid-cols-[auto_1fr] items-start gap-x-2">
      <span aria-hidden className="min-w-7 text-caption leading-6 text-muted-foreground">{label}</span>
      <div className="flex min-w-0 flex-wrap gap-1">{children}</div>
    </div>
  );
}

/**
 * Change an active wakeup's timing in place, under its row in the Wakeups
 * panel: one-click choices, a date and time for anything else, and the same
 * limits the server applies, so a choice that would break one is adjusted
 * before it is saved rather than refused after.
 */
export function WakeupTimingEditor({
  wakeup,
  sessionId,
  onClose,
}: {
  wakeup: ChatWakeup;
  sessionId: string;
  onClose: () => void;
}) {
  const { t } = useT("chat");
  const { t: common } = useT("common");
  const { t: inbox } = useT("inbox");
  const locale = useLocale();
  const reschedule = useRescheduleChatWakeup();
  const [now] = useState(() => Date.now());
  const isReminder = wakeup.kind === "at";
  const current = Date.parse((isReminder ? wakeup.fire_at : wakeup.expires_at) ?? "");
  const maxMs = isReminder ? WAKEUP_MAX_REMINDER_MS : WAKEUP_MAX_WAIT_MS;

  const [interval, setIntervalChoice] = useState<number | null>(null);
  const [preset, setPreset] = useState<TimingChoice>(KEEP);
  const [custom, setCustom] = useState<{ day: Date; time: string } | null>(null);

  const customAt = custom ? pickedTime(custom.day, custom.time, now, maxMs) : null;
  const choice: TimingChoice | null = custom ? (customAt === null ? null : { kind: "at", at: customAt }) : preset;
  const check = wakeup.kind === "check" && choice ? checkTiming(wakeup, interval, choice, now) : null;
  const change = choice
    ? timingChange(wakeup, { interval, deadline: isReminder ? KEEP : choice, reminder: isReminder ? choice : KEEP }, now)
    : null;

  // The panel is short: opening the calendar brings its time field and Save
  // into view.
  const footerRef = useRef<HTMLDivElement>(null);
  const customOpen = custom !== null;
  useEffect(() => {
    if (customOpen) footerRef.current?.scrollIntoView?.({ block: "nearest" });
  }, [customOpen]);

  const pick = (next: TimingChoice) => {
    setCustom(null);
    setPreset(next);
  };
  const openCustom = () => {
    const start = Number.isNaN(current) || current <= now ? now + HOUR_MS : current;
    setCustom({ day: startOfDay(start), time: hhmm(start) });
  };
  const save = () => {
    if (!change || reschedule.isPending) return;
    reschedule.mutateAsync({ sessionId, wakeupId: wakeup.id, timing: change }).then(onClose, () => {
      toast.error(t(($) => $.wakeup.timing_failed));
    });
  };

  const intervals: number[] = [...INTERVAL_PRESETS_S];
  if (wakeup.interval_seconds && !intervals.includes(wakeup.interval_seconds)) {
    intervals.unshift(wakeup.interval_seconds);
  }
  const effectiveInterval = interval ?? wakeup.interval_seconds;
  const presets = isReminder ? reminderPresets(now) : deadlinePresets(now);
  const limited = check?.limited ?? false;
  const today = startOfDay(now);
  const lastDay = startOfDay(now + maxMs);

  return (
    <div data-slot="chat-wakeup-timing" className="flex flex-col gap-2 pt-1 pb-1 pl-[22px]">
      {wakeup.kind === "check" && (
        <Line label={t(($) => $.wakeup.timing_interval)}>
          {intervals.map((seconds) => (
            <Choice
              key={seconds}
              selected={effectiveInterval === seconds}
              onClick={() => setIntervalChoice(seconds === wakeup.interval_seconds ? null : seconds)}
            >
              {intervalLabel(locale, seconds)}
            </Choice>
          ))}
        </Line>
      )}
      <Line label={isReminder ? t(($) => $.wakeup.timing_reminder) : t(($) => $.wakeup.timing_deadline)}>
        {!isReminder && !limited && (
          <Choice selected={!custom && preset.kind === "keep"} onClick={() => pick(KEEP)}>
            {t(($) => $.wakeup.timing_keep)}
          </Choice>
        )}
        {limited && check && (
          <Tooltip>
            <TooltipTrigger
              render={<Button type="button" size="xs" variant="brand" aria-pressed />}
            >
              <TriangleAlert aria-hidden />
              {formatSnoozeTime(new Date(check.deadline), new Date(now), locale)}
            </TooltipTrigger>
            <TooltipContent side="bottom">{t(($) => $.wakeup.timing_check_limit, { max: WAKEUP_MAX_CHECKS })}</TooltipContent>
          </Tooltip>
        )}
        {presets.map(({ at, inMs }) => (
          <Choice
            key={at}
            selected={!limited && !custom && preset.kind === "at" && preset.at === at}
            onClick={() => pick({ kind: "at", at })}
          >
            {inMs ? inLabel(locale, inMs) : formatSnoozeTime(new Date(at), new Date(now), locale)}
          </Choice>
        ))}
        <Choice selected={!!custom && !limited} onClick={openCustom}>
          <CalendarDays aria-hidden />
          {t(($) => $.wakeup.timing_custom)}
        </Choice>
      </Line>
      {custom && (
        <div className="flex flex-col rounded-md border border-surface-border bg-surface">
          <LazyCalendar
            mode="single"
            required
            selected={custom.day}
            defaultMonth={custom.day}
            onSelect={(day: Date) => setCustom({ ...custom, day })}
            startMonth={today}
            endMonth={lastDay}
            disabled={[{ before: today }, { after: lastDay }]}
            className="bg-transparent"
          />
          <div className="flex items-center justify-end border-t border-surface-border px-2 py-1.5">
            <TimeInput
              value={custom.time}
              onChange={(time) => setCustom({ ...custom, time })}
              hourLabel={inbox(($) => $.snooze.picker_hour)}
              minuteLabel={inbox(($) => $.snooze.picker_minute)}
            />
          </div>
        </div>
      )}
      <div ref={footerRef} className="flex justify-end gap-1.5">
        <Button type="button" size="xs" variant="ghost" onClick={onClose}>
          {common(($) => $.cancel)}
        </Button>
        <Button
          type="button"
          size="xs"
          disabled={!change || reschedule.isPending}
          aria-busy={reschedule.isPending}
          onClick={save}
        >
          {common(($) => $.save)}
        </Button>
      </div>
    </div>
  );
}
