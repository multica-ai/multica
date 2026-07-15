"use client";

import { CalendarClock } from "lucide-react";
import type { UpdateProjectRequest } from "@multica/core/types";
import { DateOnlyPicker } from "../../common/date-only-picker";
import { useT } from "../../i18n";

/**
 * Project start-date pill. Thin wrapper over the shared DateOnlyPicker — it only
 * supplies the field name (via onChange), the icon, and the "projects" copy; all
 * calendar/clear/format behaviour lives in the base so it can't drift from the
 * issue pickers. The same component serves both the create-project modal (map
 * the emitted value into local draft state) and the project sidebar.
 */
export function ProjectStartDatePicker({
  startDate,
  onUpdate,
  triggerRender,
  align = "start",
  open,
  onOpenChange,
}: {
  startDate: string | null;
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
      value={startDate}
      onChange={(v) => onUpdate({ start_date: v })}
      icon={<CalendarClock className="h-3.5 w-3.5 text-muted-foreground" />}
      placeholder={t(($) => $.detail.prop_start_date)}
      clearLabel={t(($) => $.detail.clear_date)}
      triggerRender={triggerRender}
      align={align}
      open={open}
      onOpenChange={onOpenChange}
    />
  );
}
