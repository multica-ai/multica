"use client";

import { CalendarDays, ChevronDown, FolderKanban } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useT } from "../../i18n";
import { ALL_PROJECTS, TIME_RANGES, type TimeRange } from "./dashboard-shared";

type DashboardProject = { id: string; title: string; icon: string | null };

/**
 * Page-scoped time range.
 *
 * A button that states the current value plus a single-select menu, rather
 * than five permanently-expanded segments. The five segments were the widest
 * thing in the header and the least informative: the value they encode is
 * already repeated in every KPI label ("Cost · 30D"). Collapsing them costs one
 * click per change and buys the header back.
 *
 * No "clear" entry: the range is a required parameter of every query on the
 * page, so it has no empty value to return to.
 */
export function TimeRangeFilter({
  days,
  onChange,
}: {
  days: TimeRange;
  onChange: (days: TimeRange) => void;
}) {
  const { t } = useT("usage");
  const current = TIME_RANGES.find((r) => r.days === days) ?? TIME_RANGES[2];

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            aria-label={t(($) => $.filter.period_label)}
            className="gap-1 px-2.5"
          >
            <CalendarDays className="size-3.5 text-muted-foreground" />
            <span className="tabular-nums">{current.label}</span>
            <ChevronDown className="size-3 text-muted-foreground" />
          </Button>
        }
      />
      <DropdownMenuContent align="end" className="w-auto min-w-32">
        <DropdownMenuRadioGroup
          value={String(days)}
          onValueChange={(value) => onChange(Number(value) as TimeRange)}
        >
          {TIME_RANGES.map((range) => (
            <DropdownMenuRadioItem
              key={range.days}
              value={String(range.days)}
              className="tabular-nums"
            >
              {range.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * Page-scoped project filter.
 *
 * A single-level dropdown, deliberately not a generic "Filter" menu: with one
 * dimension, a wrapper menu only hides what is filterable behind an extra
 * hover. The trigger states the current value like `TimeRangeFilter` does:
 * a neutral outline throughout, showing the selected project's own icon and
 * name once narrowed — the named value is the active-state signal, no filled
 * tier. If a second dimension ships (agent, model, runtime), fold this back
 * into a combined menu in the `IssueDisplayControls` grammar.
 */
export function ProjectFilter({
  projects,
  projectValue,
  onProjectChange,
}: {
  projects: DashboardProject[];
  projectValue: string;
  onProjectChange: (value: string) => void;
}) {
  const { t } = useT("usage");
  const allLabel = t(($) => $.filter.all_projects);
  // A project id that no longer resolves (deleted project, or a stale id left
  // over from another workspace) counts as no filter — the same reading the
  // page applies when it derives the effective `projectId` for the queries, so
  // the chip cannot claim a filter the data is not actually narrowed by.
  const selected = projects.find((p) => p.id === projectValue);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            aria-label={t(($) => $.filter.project_label)}
            className={selected ? "gap-1 px-2.5" : "gap-1 px-2.5 text-muted-foreground"}
          >
            {selected ? (
              <ProjectIcon project={selected} size="sm" />
            ) : (
              <FolderKanban className="size-3.5" />
            )}
            <span className="max-w-40 truncate">
              {selected ? selected.title : allLabel}
            </span>
            <ChevronDown className="size-3 text-muted-foreground" />
          </Button>
        }
      />
      <DropdownMenuContent align="end" className="max-h-72 w-auto min-w-52">
        <DropdownMenuRadioGroup
          value={projectValue}
          onValueChange={(value) => onProjectChange(value ?? ALL_PROJECTS)}
        >
          <DropdownMenuRadioItem value={ALL_PROJECTS}>
            <FolderKanban className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{allLabel}</span>
          </DropdownMenuRadioItem>
          {projects.map((project) => (
            <DropdownMenuRadioItem key={project.id} value={project.id}>
              <ProjectIcon project={project} size="sm" />
              <span className="truncate">{project.title}</span>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
