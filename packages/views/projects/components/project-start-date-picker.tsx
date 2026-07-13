"use client";

import { useState } from "react";
import { CalendarClock } from "lucide-react";
import type { UpdateProjectRequest } from "@multica/core/types";
import {
  toDateOnly,
  dateOnlyToLocalDate,
  formatDateOnly,
} from "@multica/core/issues/date";
import { Calendar } from "@multica/ui/components/ui/calendar";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

/**
 * Project start-date picker. Mirrors the issue StartDatePicker
 * (packages/views/issues/components/pickers/start-date-picker.tsx) — same
 * calendar-day contract and clear idiom, reusing @multica/core/issues/date —
 * but typed to UpdateProjectRequest and scoped to the "projects" i18n
 * namespace. The same component serves both the create-project modal (map the
 * emitted value into local draft state) and the project sidebar (pass the
 * update mutation straight through).
 */
export function ProjectStartDatePicker({
  startDate,
  onUpdate,
  triggerRender,
  align = "start",
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
}: {
  startDate: string | null;
  onUpdate: (updates: Partial<UpdateProjectRequest>) => void;
  /** Custom trigger element (e.g. a pill button in the create modal). */
  triggerRender?: React.ReactElement;
  align?: "start" | "center" | "end";
  /** Controlled open state — lets a ⋯ overflow menu reveal + open the pill. */
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
}) {
  const { t } = useT("projects");
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const date = dateOnlyToLocalDate(startDate);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        className={triggerRender ? undefined : "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors"}
        render={triggerRender}
      >
        <CalendarClock className="h-3.5 w-3.5 text-muted-foreground" />
        {date ? (
          <span>{formatDateOnly(startDate, { month: "short", day: "numeric" }, "en-US")}</span>
        ) : (
          <span className="text-muted-foreground">{t(($) => $.detail.prop_start_date)}</span>
        )}
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align={align}>
        <Calendar
          mode="single"
          selected={date}
          onSelect={(d: Date | undefined) => {
            onUpdate({ start_date: d ? toDateOnly(d) : null });
            setOpen(false);
          }}
        />
        {date && (
          <div className="border-t px-3 py-2">
            <Button
              variant="ghost"
              size="xs"
              onClick={() => {
                onUpdate({ start_date: null });
                setOpen(false);
              }}
              className="text-muted-foreground hover:text-foreground"
            >
              {t(($) => $.detail.clear_date)}
            </Button>
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}
