"use client";

import { useId, useState, type ReactElement } from "react";
import {
  inboxSnoozeOptions,
  parseInboxSnoozeInput,
  type InboxSnoozePreset,
} from "@multica/core/inbox/snooze";
import { CalendarClock, Clock, Undo2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import { useLocale, useT } from "../../i18n";
import { formatSnoozeTime } from "./inbox-snooze-time";

function useSnoozePresetLabel(): (preset: InboxSnoozePreset) => string {
  const { t } = useT("inbox");
  return (preset) =>
    preset === "in_one_hour"
      ? t(($) => $.snooze.in_one_hour)
      : preset === "later_today"
        ? t(($) => $.snooze.later_today)
        : preset === "tomorrow"
          ? t(($) => $.snooze.tomorrow)
          : t(($) => $.snooze.next_week);
}

/**
 * The snooze choices behind a trigger: a few times worth one click, and a
 * dialog for any other.
 *
 * Controlled, so the page's shortcut can open it for the row that is open.
 * `snoozed` adds the way back for an issue that is already put away.
 */
export function InboxSnoozeMenu({
  open,
  onOpenChange,
  trigger,
  snoozed = false,
  onSnooze,
  onUnsnooze,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  trigger: ReactElement;
  snoozed?: boolean;
  onSnooze: (until: Date) => void;
  onUnsnooze?: () => void;
}) {
  const { t } = useT("inbox");
  const locale = useLocale();
  const presetLabel = useSnoozePresetLabel();
  const [picking, setPicking] = useState(false);
  // Read when the menu opens, so the times it lists are the ones it snoozes to.
  const [now, setNow] = useState(() => new Date());

  return (
    <>
      <DropdownMenu
        open={open}
        onOpenChange={(next) => {
          if (next) setNow(new Date());
          onOpenChange(next);
        }}
      >
        <DropdownMenuTrigger render={trigger} />
        <DropdownMenuContent align="end" className="w-64">
          {snoozed && onUnsnooze && (
            <>
              <DropdownMenuItem onClick={onUnsnooze}>
                <Undo2 className="h-4 w-4" />
                {t(($) => $.snooze.unsnooze)}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
            </>
          )}
          {inboxSnoozeOptions(now).map((option) => (
            <DropdownMenuItem key={option.preset} onClick={() => onSnooze(option.until)}>
              <Clock className="h-4 w-4" />
              {presetLabel(option.preset)}
              <DropdownMenuShortcut className="tracking-normal tabular-nums">
                {formatSnoozeTime(option.until, now, locale)}
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => setPicking(true)}>
            <CalendarClock className="h-4 w-4" />
            {t(($) => $.snooze.pick_time)}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <p className="px-2 py-1 text-caption text-muted-foreground">{t(($) => $.snooze.hint)}</p>
        </DropdownMenuContent>
      </DropdownMenu>
      {picking && (
        <SnoozeTimeDialog
          onClose={() => setPicking(false)}
          onSnooze={(until) => {
            setPicking(false);
            onSnooze(until);
          }}
        />
      )}
    </>
  );
}

function pad(value: number): string {
  return String(value).padStart(2, "0");
}

/** A `datetime-local` value for a local time. */
function toLocalInput(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function SnoozeTimeDialog({
  onClose,
  onSnooze,
}: {
  onClose: () => void;
  onSnooze: (until: Date) => void;
}) {
  const { t } = useT("inbox");
  const { t: tIssues } = useT("issues");
  const { t: tCommon } = useT("common");
  const errorId = useId();
  // Tomorrow morning: the choice the field most often ends up near.
  const [value, setValue] = useState(() => {
    const tomorrow = inboxSnoozeOptions(new Date()).find((option) => option.preset === "tomorrow");
    return tomorrow ? toLocalInput(tomorrow.until) : "";
  });
  const [invalid, setInvalid] = useState(false);

  const submit = () => {
    const until = parseInboxSnoozeInput(value, new Date());
    if (!until) {
      setInvalid(true);
      return;
    }
    onSnooze(until);
  };

  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>{t(($) => $.snooze.custom_title)}</DialogTitle>
        </DialogHeader>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            submit();
          }}
        >
          <Input
            type="datetime-local"
            autoFocus
            aria-label={tIssues(($) => $.wakeups.local_time, {
              timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
            })}
            aria-invalid={invalid}
            aria-describedby={invalid ? errorId : undefined}
            value={value}
            onChange={(event) => {
              setValue(event.target.value);
              setInvalid(false);
            }}
          />
          {invalid && (
            <p id={errorId} role="alert" className="mt-2 text-caption text-destructive">
              {t(($) => $.snooze.custom_invalid)}
            </p>
          )}
          <DialogFooter className="mt-4">
            <DialogClose render={<Button type="button" variant="outline" />}>
              {tCommon(($) => $.cancel)}
            </DialogClose>
            <Button type="submit">{t(($) => $.snooze.action)}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
