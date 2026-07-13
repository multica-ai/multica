"use client";

import { useState } from "react";
import { CalendarDays } from "lucide-react";
import type { UpdateProjectRequest } from "@multica/core/types";
import {
  toDateOnly,
  dateOnlyToLocalDate,
  formatDateOnly,
  isPastDateOnly,
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
 * Project due-date picker. Mirrors the issue DueDatePicker
 * (packages/views/issues/components/pickers/due-date-picker.tsx), including the
 * overdue `text-destructive` styling, but typed to UpdateProjectRequest and
 * scoped to the "projects" i18n namespace. See ProjectStartDatePicker for the
 * shared-component rationale.
 */
export function ProjectDueDatePicker({
  dueDate,
  onUpdate,
  triggerRender,
  align = "start",
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
}: {
  dueDate: string | null;
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
  const date = dateOnlyToLocalDate(dueDate);
  const isOverdue = isPastDateOnly(dueDate);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        className={triggerRender ? undefined : "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors"}
        render={triggerRender}
      >
        <CalendarDays className="h-3.5 w-3.5 text-muted-foreground" />
        {date ? (
          <span className={isOverdue ? "text-destructive" : ""}>
            {formatDateOnly(dueDate, { month: "short", day: "numeric" }, "en-US")}
          </span>
        ) : (
          <span className="text-muted-foreground">{t(($) => $.detail.prop_due_date)}</span>
        )}
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align={align}>
        <Calendar
          mode="single"
          selected={date}
          onSelect={(d: Date | undefined) => {
            onUpdate({ due_date: d ? toDateOnly(d) : null });
            setOpen(false);
          }}
        />
        {date && (
          <div className="border-t px-3 py-2">
            <Button
              variant="ghost"
              size="xs"
              onClick={() => {
                onUpdate({ due_date: null });
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
