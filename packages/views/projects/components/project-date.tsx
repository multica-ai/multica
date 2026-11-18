"use client";

import { addDaysDateOnly, formatDateOnly, todayDateOnly } from "@multica/core/issues/date";
import type { Project } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useLocale } from "../../i18n";
import { CollectionEmptyValue } from "../../layout/collection-list";

const DUE_SOON_DAYS = 7;

export type ProjectDueState = "overdue" | "soon" | null;

/** Closed projects have nothing left to be late for. */
export function projectDueState(project: Project): ProjectDueState {
  const due = project.due_date;
  if (!due || project.status === "completed" || project.status === "cancelled") return null;
  if (due < todayDateOnly()) return "overdue";
  if (due <= addDaysDateOnly(DUE_SOON_DAYS)) return "soon";
  return null;
}

export function ProjectDate({
  value,
  dueState = null,
  className,
}: {
  value: string | null;
  dueState?: ProjectDueState;
  className?: string;
}) {
  const locale = useLocale();
  if (!value) return <CollectionEmptyValue />;
  const options: Intl.DateTimeFormatOptions =
    value.slice(0, 4) === todayDateOnly().slice(0, 4)
      ? { month: "short", day: "numeric" }
      : { year: "numeric", month: "short", day: "numeric" };
  return (
    <span
      className={cn(
        "tabular-nums",
        dueState === "overdue" && "text-destructive",
        dueState === "soon" && "text-warning",
        className,
      )}
    >
      {formatDateOnly(value, options, locale) || value}
    </span>
  );
}
