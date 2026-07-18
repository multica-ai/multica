"use client";

import { CalendarDays } from "lucide-react";
import type { UpdateProjectRequest } from "@multica/core/types";
import { DateOnlyPicker } from "../../common/date-only-picker";
import { useT } from "../../i18n";

/**
 * Project due-date pill. Thin wrapper over the shared DateOnlyPicker (see
 * ProjectStartDatePicker); `highlightOverdue` paints a past due date red, the
 * same as the issue due-date pill.
 */
export function ProjectDueDatePicker({
  dueDate,
  onUpdate,
  triggerRender,
  align = "start",
  open,
  onOpenChange,
}: {
  dueDate: string | null;
  onUpdate: (updates: Partial<UpdateProjectRequest>) => void;
  /** Custom trigger element (e.g. a pill button in the create modal). */
  triggerRender?: React.ReactElement<Record<string, unknown>>;
  align?: "start" | "center" | "end";
  /** Controlled open state — lets a ⋯ overflow menu reveal + open the pill. */
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
}) {
  const { t } = useT("projects");
  return (
    <DateOnlyPicker
      value={dueDate}
      onChange={(v) => onUpdate({ due_date: v })}
      icon={<CalendarDays className="h-3.5 w-3.5 text-muted-foreground" />}
      placeholder={t(($) => $.detail.prop_due_date)}
      clearLabel={t(($) => $.detail.clear_date)}
      highlightOverdue
      triggerRender={triggerRender}
      align={align}
      open={open}
      onOpenChange={onOpenChange}
    />
  );
}
