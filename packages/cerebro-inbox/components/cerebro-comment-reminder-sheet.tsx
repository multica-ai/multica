"use client";

// FIR-2643 — "Remind me" on a specific comment. The whole comment-reminder
// surface lives in the cerebro zone; the shared comment menu only gets a small
// marked hook (see comment-card.tsx). This reuses the inbox reminder time
// picker (presets + CustomReminderPicker on desktop / CerebroReminderWheel on
// mobile) and adds an editable, auto-suggested reminder text.

import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import {
  Drawer,
  DrawerContent,
  DrawerHeader,
  DrawerTitle,
} from "@multica/ui/components/ui/drawer";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { CerebroReminderWheel } from "./cerebro-reminder-wheel";
import { CustomReminderPicker } from "./cerebro-inbox-row-actions";
import { useCreateInboxReminder } from "../mutations";
import {
  addHours,
  fromDateTimeLocalValue,
  nextBusinessDayNineAm,
  nextMondayNineAm,
  toDateTimeLocalValue,
} from "../mute-time";
import { useCerebroInboxStrings } from "../strings";

/** Bound matching the backend's reminderTextMaxRunes (handler.go) so the UI
 * suggestion and the server fallback stay in sync. */
const REMINDER_TEXT_MAX = 140;

/**
 * Mirror of the Go `suggestReminderText`: collapse whitespace, trim, and
 * truncate to REMINDER_TEXT_MAX with an ellipsis. The user can edit the result
 * before saving; an empty result lets the caller fall back to a default phrase.
 */
export function suggestReminderText(content: string): string {
  const snippet = content.split(/\s+/).filter(Boolean).join(" ");
  if (!snippet) return "";
  const runes = Array.from(snippet);
  if (runes.length > REMINDER_TEXT_MAX) {
    return runes.slice(0, REMINDER_TEXT_MAX).join("").trimEnd() + "…";
  }
  return snippet;
}

export function CerebroCommentReminderSheet({
  open,
  onOpenChange,
  issueId,
  commentId,
  commentContent,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  issueId: string;
  commentId: string;
  commentContent: string;
}) {
  const isMobile = useIsMobile();
  const strings = useCerebroInboxStrings();
  const { i18n } = useTranslation();
  const createReminder = useCreateInboxReminder();

  const [text, setText] = useState("");
  const [customReminder, setCustomReminder] = useState(() =>
    toDateTimeLocalValue(nextBusinessDayNineAm()),
  );

  // Re-seed the editable text + default time each time the sheet opens for a
  // (possibly different) comment, so a stale draft from a previous comment
  // never leaks in.
  useEffect(() => {
    if (open) {
      setText(suggestReminderText(commentContent) || strings.comment_remind_default_text);
      setCustomReminder(toDateTimeLocalValue(nextBusinessDayNineAm()));
    }
  }, [open, commentId, commentContent, strings.comment_remind_default_text]);

  const now = new Date();
  const presets = [
    { label: strings.remind_one_hour, date: () => addHours(1) },
    { label: strings.remind_three_hours, date: () => addHours(3) },
    { label: strings.remind_tomorrow_morning, date: () => nextBusinessDayNineAm() },
    { label: strings.remind_monday, date: () => nextMondayNineAm() },
  ];

  const planned = fromDateTimeLocalValue(customReminder);
  const canSave =
    planned !== null && planned.getTime() > now.getTime() && !createReminder.isPending;

  const schedule = (plannedAt: Date) => {
    createReminder.mutate(
      { text: text.trim(), plannedAt, issueId, commentId },
      {
        onSuccess: () => {
          onOpenChange(false);
          toast.success(strings.comment_remind_created);
        },
        onError: () => toast.error(strings.comment_remind_failed),
      },
    );
  };

  const body = (
    <div className="space-y-4">
      {/* Editable, auto-suggested reminder text. */}
      <div className="space-y-1.5">
        <Label htmlFor="comment-reminder-text">{strings.comment_remind_text_label}</Label>
        <Textarea
          id="comment-reminder-text"
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={strings.comment_remind_text_placeholder}
          rows={2}
        />
      </div>

      {/* Quick presets — apply immediately on tap, carrying the current text. */}
      <ul className="divide-y divide-border border-t border-border">
        {presets.map((preset) => (
          <li key={preset.label}>
            <button
              type="button"
              disabled={createReminder.isPending}
              className="w-full py-3 text-left text-base text-foreground/90 hover:text-foreground disabled:opacity-50"
              onClick={() => schedule(preset.date())}
            >
              {preset.label}
            </button>
          </li>
        ))}
      </ul>

      {/* Custom date + time — scroll wheel on mobile, Calendar + columns on desktop. */}
      <div className="space-y-3 border-t border-border pt-3">
        <p className="text-sm font-medium text-muted-foreground">
          {strings.remind_custom_datetime}
        </p>
        {isMobile ? (
          <CerebroReminderWheel
            value={planned ?? nextBusinessDayNineAm()}
            todayLabel={strings.remind_today}
            tomorrowLabel={strings.remind_tomorrow}
            locale={i18n.language}
            onChange={(date) => setCustomReminder(toDateTimeLocalValue(date))}
          />
        ) : (
          <CustomReminderPicker value={customReminder} min={now} onChange={setCustomReminder} />
        )}
        <Button
          type="button"
          className="w-full"
          disabled={!canSave}
          onClick={() => {
            if (planned && planned.getTime() > Date.now()) schedule(planned);
          }}
        >
          {strings.remind_set}
        </Button>
      </div>
    </div>
  );

  if (isMobile) {
    return (
      <Drawer open={open} onOpenChange={onOpenChange}>
        <DrawerContent>
          <DrawerHeader>
            <DrawerTitle>{strings.remind_title}</DrawerTitle>
          </DrawerHeader>
          <div className="overflow-y-auto px-4 pb-6">{body}</div>
        </DrawerContent>
      </Drawer>
    );
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{strings.remind_title}</DialogTitle>
        </DialogHeader>
        {body}
      </DialogContent>
    </Dialog>
  );
}
