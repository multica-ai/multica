"use client";

import { useMemo, useState } from "react";
import { ArrowRight, ChevronRight } from "lucide-react";
import { useMyPlate } from "@multica/core/home";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import {
  addDaysDateOnly,
  dateOnlyToLocalDate,
  todayDateOnly,
} from "@multica/core/issues/date";
import { useWorkspacePaths } from "@multica/core/paths";
import type { Issue } from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { StatusIcon } from "../../issues/components/status-icon";
import { AppLink } from "../../navigation";
import { useLocale, useT } from "../../i18n";

/** Last day of the current Monday-start week, as "YYYY-MM-DD". */
function endOfWeekDateOnly(): string {
  const day = new Date().getDay();
  return addDaysDateOnly(day === 0 ? 0 : 7 - day);
}

export function MyPlateRail() {
  const { t } = useT("home");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { issues, isLoading } = useMyPlate(wsId);
  const { categoryOf } = useIssueStatuses(wsId);

  const { dueNow, dueThisWeek, inProgress } = useMemo(() => {
    const today = todayDateOnly();
    const weekEnd = endOfWeekDateOnly();
    const dueNow: Issue[] = [];
    const dueThisWeek: Issue[] = [];
    const inProgress: Issue[] = [];
    for (const issue of issues) {
      // Date-only strings compare correctly as text.
      if (issue.due_date && issue.due_date <= today) dueNow.push(issue);
      else if (issue.due_date && issue.due_date <= weekEnd) dueThisWeek.push(issue);
      if (categoryOf(issue.status) === "started") inProgress.push(issue);
    }
    return { dueNow, dueThisWeek, inProgress };
  }, [issues, categoryOf]);

  return (
    <section aria-labelledby="home-plate-title" className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <h2 id="home-plate-title" className="text-title-sm font-semibold">
          {t(($) => $.plate.title)}
        </h2>
        <AppLink
          href={paths.myIssues()}
          className="ml-auto flex items-center gap-1 rounded-sm text-caption text-muted-foreground outline-none hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring"
        >
          {t(($) => $.plate.my_issues)}
          <ArrowRight className="size-3.5" />
        </AppLink>
      </div>

      {isLoading ? (
        <div className="space-y-2">
          <Skeleton className="h-4 w-24" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-5/6" />
        </div>
      ) : (
        <div className="flex flex-col">
          <PlateGroup
            label={t(($) => $.plate.due_today)}
            issues={dueNow}
            defaultOpen
            emptyLabel={t(($) => $.plate.empty_today)}
            trailing={(issue) => <DueNowLabel issue={issue} />}
          />
          <PlateGroup
            label={t(($) => $.plate.this_week)}
            issues={dueThisWeek}
            trailing={(issue) => <WeekdayLabel dueDate={issue.due_date} />}
          />
          <PlateGroup
            label={t(($) => $.plate.in_progress)}
            issues={inProgress}
            trailing={(issue) => (
              <span className="text-caption text-muted-foreground">{issue.identifier}</span>
            )}
          />
        </div>
      )}
    </section>
  );
}

function PlateGroup({
  label,
  issues,
  defaultOpen = false,
  emptyLabel,
  trailing,
}: {
  label: string;
  issues: Issue[];
  defaultOpen?: boolean;
  emptyLabel?: string;
  trailing: (issue: Issue) => React.ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  const expandable = issues.length > 0;

  return (
    <div className="border-b py-1.5 last:border-b-0">
      <button
        type="button"
        aria-expanded={open}
        disabled={!expandable}
        onClick={() => setOpen((value) => !value)}
        className="flex w-full items-center gap-1.5 rounded-sm py-1 text-left text-label text-muted-foreground outline-none enabled:hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring"
      >
        <span>{label}</span>
        <span className="tabular-nums">{issues.length}</span>
        {expandable && (
          <ChevronRight
            className={cn("ml-auto size-3.5 transition-transform", open && "rotate-90")}
          />
        )}
      </button>
      {open && issues.length > 0 && (
        <ul className="mt-0.5">
          {issues.map((issue) => (
            <li key={issue.id}>
              <PlateRow issue={issue} trailing={trailing(issue)} />
            </li>
          ))}
        </ul>
      )}
      {open && issues.length === 0 && emptyLabel && (
        <p className="py-1 text-caption text-muted-foreground">{emptyLabel}</p>
      )}
    </div>
  );
}

function PlateRow({ issue, trailing }: { issue: Issue; trailing: React.ReactNode }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(wsId);
  return (
    <AppLink
      href={paths.issueDetail(issue.identifier || issue.id)}
      className="-mx-2 flex min-w-0 items-center gap-2 rounded-md px-2 py-1.5 outline-none transition-colors hover:bg-accent/50 focus-visible:bg-accent/50"
    >
      <PriorityIcon priority={issue.priority} />
      <StatusIcon
        status={issue.status}
        category={categoryOf(issue.status)}
        color={colorOf(issue.status)}
        icon={iconOf(issue.status)}
        className="size-3.5 shrink-0"
      />
      <span className="min-w-0 flex-1 truncate text-body">{issue.title}</span>
      <span className="shrink-0">{trailing}</span>
    </AppLink>
  );
}

function DueNowLabel({ issue }: { issue: Issue }) {
  const { t } = useT("home");
  const overdue = !!issue.due_date && issue.due_date < todayDateOnly();
  return (
    <span className="text-caption font-medium text-destructive">
      {overdue ? t(($) => $.plate.overdue) : t(($) => $.plate.today)}
    </span>
  );
}

function WeekdayLabel({ dueDate }: { dueDate: string | null }) {
  const locale = useLocale();
  const date = dateOnlyToLocalDate(dueDate);
  if (!date) return null;
  return (
    <span className="text-caption text-muted-foreground">
      {new Intl.DateTimeFormat(locale, { weekday: "short" }).format(date)}
    </span>
  );
}
