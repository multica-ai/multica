"use client";

import { useState } from "react";
import { CalendarClock } from "lucide-react";
import type { UpdateIssueRequest } from "@multica/core/types";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../../i18n";

function toLocalDateTimeValue(date: Date) {
  const offsetMs = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offsetMs).toISOString().slice(0, 16);
}

function hasExplicitTime(date: Date) {
  return date.getHours() !== 0 || date.getMinutes() !== 0 || date.getSeconds() !== 0 || date.getMilliseconds() !== 0;
}

function formatStartDateLabel(date: Date) {
  const dateLabel = date.toLocaleDateString("en-US", { month: "short", day: "numeric" });
  if (!hasExplicitTime(date)) return dateLabel;
  const timeLabel = date.toLocaleTimeString("en-US", {
    hour: "numeric",
    minute: "2-digit",
  });
  return `${dateLabel}, ${timeLabel}`;
}

export function StartDatePicker({
  startDate,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  align = "start",
  defaultOpen = false,
}: {
  startDate: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  align?: "start" | "center" | "end";
  /** Open the popover on first mount. Used by progressive-disclosure
   *  sidebars so a newly-added field immediately enters edit state. */
  defaultOpen?: boolean;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(defaultOpen);
  const date = startDate ? new Date(startDate) : undefined;
  const minValue = toLocalDateTimeValue(new Date());

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        className={triggerRender ? undefined : "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors"}
        render={triggerRender}
      >
        {customTrigger ?? (
          <>
            <CalendarClock className="h-3.5 w-3.5 text-muted-foreground" />
            {date ? (
              <span>
                {formatStartDateLabel(date)}
              </span>
            ) : (
              <span className="text-muted-foreground">{t(($) => $.pickers.start_date.trigger_label)}</span>
            )}
          </>
        )}
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align={align}>
        <div className="flex flex-col gap-3 p-3">
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-muted-foreground">{t(($) => $.pickers.start_date.trigger_label)}</span>
            <input
              aria-label={t(($) => $.pickers.start_date.trigger_label)}
              type="datetime-local"
              className="h-9 rounded-md border border-input bg-background px-3 text-sm shadow-sm outline-none ring-offset-background focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
              min={minValue}
              value={date ? toLocalDateTimeValue(date) : ""}
              onChange={(e) => {
                const value = e.target.value;
                onUpdate({ start_date: value ? new Date(value).toISOString() : null });
              }}
            />
          </label>
          <div className="flex justify-end">
            <Button
              variant="ghost"
              size="xs"
              onClick={() => {
                onUpdate({ start_date: null });
                setOpen(false);
              }}
              className="text-muted-foreground hover:text-foreground"
            >
              {t(($) => $.pickers.start_date.clear_action)}
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
