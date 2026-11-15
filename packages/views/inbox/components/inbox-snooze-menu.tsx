"use client";

import { useState, type ReactElement } from "react";
import {
  INBOX_SNOOZE_MAX_DAYS,
  INBOX_SNOOZE_MORNING,
  inboxSnoozeOptions,
  inboxSnoozePickedTime,
  type InboxSnoozePreset,
} from "@multica/core/inbox/snooze";
import { AlarmClock, ArrowLeft, CalendarDays, ChevronRight, Clock, Undo2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { LazyCalendar } from "@multica/ui/components/ui/lazy-calendar";
import { TimeInput } from "@multica/ui/components/ui/time-input";
import { useLocale, useT } from "../../i18n";
import { formatSnoozeTime } from "./inbox-snooze-time";

function useSnoozePresetLabel(): (preset: InboxSnoozePreset) => string {
  const { t } = useT("inbox");
  return (preset) => {
    switch (preset) {
      case "hours_1":
        return t(($) => $.snooze.hours_1);
      case "hours_3":
        return t(($) => $.snooze.hours_3);
      case "days_1":
        return t(($) => $.snooze.days_1);
      case "days_3":
        return t(($) => $.snooze.days_3);
      case "weeks_1":
        return t(($) => $.snooze.weeks_1);
    }
  };
}

/**
 * The snooze choices behind a trigger: a few durations worth one click, and a
 * date and time picker for any other time, which takes the menu's place.
 *
 * Controlled, so a shortcut can open it. `snoozed` adds the way back for an
 * issue that is already snoozed.
 */
export function InboxSnoozeMenu({
  open,
  onOpenChange,
  trigger,
  snoozed = false,
  onSnooze,
  onUnsnooze,
  side = "bottom",
  align = "end",
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  trigger: ReactElement;
  snoozed?: boolean;
  onSnooze: (until: Date) => void;
  onUnsnooze?: () => void;
  side?: "bottom" | "right";
  align?: "start" | "end";
}) {
  const { t } = useT("inbox");
  const locale = useLocale();
  const presetLabel = useSnoozePresetLabel();
  const [picking, setPicking] = useState(false);
  // Read whenever the menu opens, from its trigger or from the parent (`H`, a
  // hover card), so the times it lists are the ones it snoozes to.
  const [now, setNow] = useState(() => new Date());
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) {
      setNow(new Date());
      setPicking(false);
    }
  }

  return (
    <DropdownMenu open={open} onOpenChange={onOpenChange}>
      <DropdownMenuTrigger render={trigger} />
      <DropdownMenuContent side={side} align={align} className={picking ? "w-auto" : "w-64"}>
        {picking ? (
          <SnoozeTimePicker
            now={now}
            onBack={() => setPicking(false)}
            onSnooze={(until) => {
              onSnooze(until);
              onOpenChange(false);
            }}
          />
        ) : (
          <>
            {snoozed && onUnsnooze && (
              <>
                <DropdownMenuItem onClick={onUnsnooze}>
                  <Undo2 className="size-4" />
                  {t(($) => $.snooze.unsnooze)}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
              </>
            )}
            <DropdownMenuGroup>
              <DropdownMenuLabel>
                {snoozed ? t(($) => $.snooze.change_time) : t(($) => $.snooze.menu_title)}
              </DropdownMenuLabel>
              {inboxSnoozeOptions(now).map((option) => (
                <DropdownMenuItem key={option.preset} onClick={() => onSnooze(option.until)}>
                  <Clock className="size-4" />
                  {presetLabel(option.preset)}
                  <DropdownMenuShortcut className="tracking-normal tabular-nums">
                    {formatSnoozeTime(option.until, now, locale)}
                  </DropdownMenuShortcut>
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuItem closeOnClick={false} onClick={() => setPicking(true)}>
              <CalendarDays className="size-4" />
              {t(($) => $.snooze.pick_time)}
              <ChevronRight className="ml-auto size-4 text-muted-foreground" />
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <p className="px-2 py-1 text-caption text-muted-foreground">{t(($) => $.snooze.hint)}</p>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function startOfDay(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate());
}

function SnoozeTimePicker({
  now,
  onBack,
  onSnooze,
}: {
  now: Date;
  onBack: () => void;
  onSnooze: (until: Date) => void;
}) {
  const { t } = useT("inbox");
  const locale = useLocale();
  const today = startOfDay(now);
  const lastDay = new Date(today.getFullYear(), today.getMonth(), today.getDate() + INBOX_SNOOZE_MAX_DAYS - 1);
  const [day, setDay] = useState(() => new Date(today.getFullYear(), today.getMonth(), today.getDate() + 1));
  const [time, setTime] = useState(INBOX_SNOOZE_MORNING);
  const until = inboxSnoozePickedTime(day, time, now);

  return (
    // The menu navigates and type-selects from the keys pressed inside it, and
    // would take the calendar's arrows and the time's digits for its own.
    <div
      className="flex flex-col"
      onKeyDown={(event) => {
        if (event.key !== "Escape") event.stopPropagation();
      }}
    >
      <div className="flex items-center gap-1 px-1 pt-1">
        <Button size="icon-xs" variant="ghost" aria-label={t(($) => $.snooze.picker_back)} onClick={onBack}>
          <ArrowLeft />
        </Button>
        <span className="text-label font-medium">{t(($) => $.snooze.picker_title)}</span>
      </div>
      <LazyCalendar
        mode="single"
        required
        autoFocus
        selected={day}
        defaultMonth={day}
        onSelect={(next: Date) => setDay(next)}
        startMonth={today}
        endMonth={lastDay}
        disabled={[{ before: today }, { after: lastDay }]}
        className="bg-transparent"
      />
      <div className="flex items-center justify-between gap-3 border-t px-2 py-2">
        <span className="text-label text-muted-foreground">{t(($) => $.snooze.picker_time)}</span>
        <TimeInput
          value={time}
          onChange={setTime}
          hourLabel={t(($) => $.snooze.picker_hour)}
          minuteLabel={t(($) => $.snooze.picker_minute)}
        />
      </div>
      <div className="px-1 pb-1">
        <Button className="w-full" disabled={!until} onClick={() => until && onSnooze(until)}>
          <AlarmClock />
          {until
            ? t(($) => $.snooze.picker_submit, { time: formatSnoozeTime(until, now, locale) })
            : t(($) => $.snooze.custom_invalid)}
        </Button>
      </div>
    </div>
  );
}
