"use client";

import { BellRing } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { InboxItem } from "@multica/core/types";
import { formatPlannedDateTime, isReminderOverdue } from "../mute-time";
import { useCerebroInboxStrings } from "../strings";

/**
 * FIR-2364 — dedicated reminder row, rendered beneath the title + preview on
 * inbox rows that carry a reminder signal.
 *
 * Before: the "Reminder" pill sat inline on the preview line and the long
 * "Overdue since …" timestamp sat on the title's top-right row, squeezing the
 * title until it truncated to a few characters ("Stop …", "Infisc…"). Moving
 * both onto their own bottom row gives the title the full width back while
 * making the reminder state *more* prominent, not less.
 *
 * Returns null for non-reminder rows, so every other inbox row keeps its
 * original two-line height.
 */
export function CerebroInboxReminderRow({ item }: { item: InboxItem }) {
  const { i18n } = useTranslation();
  const strings = useCerebroInboxStrings();

  // A "remind me" on an existing row keeps its original notification type, so
  // muted_until is the reminder signal there; native reminders use planned_at.
  const isReminder =
    item.type === "reminder" || isReminderOverdue(item.muted_until);
  if (!isReminder) return null;

  const plannedAt =
    item.type === "reminder"
      ? (item.details?.planned_at ?? item.muted_until)
      : item.muted_until;
  const planned = formatPlannedDateTime(plannedAt, i18n.language);
  if (!planned) return null;

  const overdue = isReminderOverdue(plannedAt);

  return (
    <div className="mt-1 flex items-center gap-1.5">
      <span className="inline-flex shrink-0 items-center gap-1 rounded-sm bg-destructive/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase leading-none tracking-normal text-destructive">
        <BellRing className="size-3" aria-hidden />
        {strings.reminder_label}
      </span>
      <span
        className={`truncate text-xs ${overdue ? "font-semibold text-destructive" : "text-muted-foreground"}`}
      >
        {`${overdue ? strings.reminder_overdue_prefix : strings.planned_for_prefix} ${planned}`}
      </span>
    </div>
  );
}
