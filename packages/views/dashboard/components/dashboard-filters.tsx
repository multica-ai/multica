"use client";

import { Bot, CalendarDays, ChevronDown, FolderKanban, Lock, Users } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useT } from "../../i18n";
import {
  ALL_PROJECTS,
  ALL_SQUADS,
  STANDALONE_SQUAD,
  TIME_RANGES,
  type TimeRange,
  type TimeRangeOption,
} from "./dashboard-shared";
import { isRangeAllowed, type UpgradePlan } from "../history-range";

type DashboardProject = { id: string; title: string; icon: string | null };
type DashboardSquad = { id: string; name: string; avatar_url: string | null; archived_at?: string | null };

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
 *
 * Under a plan's history limit (`maxDays`, MUL-9490) the longer ranges stay
 * listed but locked, each marked with the plan that covers it when that is
 * known, and the menu ends with a note and a way to the plans. Admins and
 * members see the same menu. `onViewPlans` is null where billing is off.
 */
export function TimeRangeFilter({
  days,
  onChange,
  maxDays = null,
  planForDays = () => null,
  onViewPlans = null,
  ranges = TIME_RANGES,
}: {
  days: TimeRange;
  onChange: (days: TimeRange) => void;
  maxDays?: number | null;
  planForDays?: (days: TimeRange) => UpgradePlan | null;
  onViewPlans?: (() => void) | null;
  ranges?: readonly TimeRangeOption[];
}) {
  const { t } = useT("usage");
  const current = ranges.find((r) => r.days === days) ?? TIME_RANGES[2];
  const hasLocked = ranges.some((r) => !isRangeAllowed(r.days, maxDays));

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="sm"
            aria-label={t(($) => $.filter.period_label)}
            className="h-7 gap-1.5 px-2 text-body"
          >
            <CalendarDays className="size-3.5 text-muted-foreground" />
            <span className="tabular-nums">{current.label}</span>
            <ChevronDown className="size-3 text-muted-foreground" />
          </Button>
        }
      />
      <DropdownMenuContent align="end" className={hasLocked ? "w-64" : "w-auto min-w-32"}>
        <DropdownMenuRadioGroup
          value={String(days)}
          onValueChange={(value) => {
            const next = Number(value) as TimeRange;
            if (isRangeAllowed(next, maxDays)) onChange(next);
          }}
        >
          {ranges.map((range) => {
            const allowed = isRangeAllowed(range.days, maxDays);
            const plan = allowed ? null : planForDays(range.days);
            return (
              <DropdownMenuRadioItem
                key={range.days}
                value={String(range.days)}
                disabled={!allowed}
                className="tabular-nums"
              >
                <span className="flex-1">{range.label}</span>
                {allowed ? null : (
                  <span className="ml-3 flex shrink-0 items-center gap-1 text-caption text-muted-foreground">
                    <Lock className="size-3" aria-hidden="true" />
                    {plan ? (
                      <span>{t(($) => $.history_limit.plan[plan])}</span>
                    ) : (
                      <span className="sr-only">{t(($) => $.history_limit.locked)}</span>
                    )}
                  </span>
                )}
              </DropdownMenuRadioItem>
            );
          })}
        </DropdownMenuRadioGroup>
        {hasLocked && maxDays !== null ? (
          <>
            <DropdownMenuSeparator />
            <div className="flex flex-col gap-2 px-2 py-1.5">
              <p className="text-caption text-muted-foreground">
                {t(($) => $.history_limit.note, { days: maxDays })}
              </p>
              {onViewPlans ? (
                <Button variant="outline" size="sm" className="w-full" onClick={onViewPlans}>
                  {t(($) => $.history_limit.view_plans)}
                </Button>
              ) : null}
            </div>
          </>
        ) : null}
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
 * a quiet ghost control throughout, showing the selected project's own icon
 * and name once narrowed — the named value is the active-state signal, no
 * filled tier. If a second dimension ships (agent, model, runtime), fold this
 * back into a combined menu in the `IssueDisplayControls` grammar.
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
            variant="ghost"
            size="sm"
            aria-label={t(($) => $.filter.project_label)}
            className="h-7 gap-1.5 px-2 text-body"
          >
            {selected ? (
              <ProjectIcon project={selected} size="sm" />
            ) : (
              <FolderKanban className="size-3.5 text-muted-foreground" />
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

/**
 * Page-scoped attribution filter (all / standalone agents / specific squad).
 * Mirrors ProjectFilter's ghost button grammar: quiet until narrowed, showing
 * the selected squad's own avatar + name when active.
 */
export function SquadFilter({
  squads,
  squadValue,
  onSquadChange,
}: {
  squads: DashboardSquad[];
  squadValue: string;
  onSquadChange: (value: string) => void;
}) {
  const { t } = useT("usage");
  const allLabel = t(($) => $.filter.all_attribution);
  const standaloneLabel = t(($) => $.filter.standalone_only);
  const liveSquads = squads.filter((s) => !s.archived_at);
  const isStandalone = squadValue === STANDALONE_SQUAD;
  const selected = !isStandalone && squadValue !== ALL_SQUADS
    ? squads.find((s) => s.id === squadValue)
    : undefined;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="sm"
            aria-label={t(($) => $.filter.attribution_label)}
            className="h-7 gap-1.5 px-2 text-body"
          >
            {selected ? (
              <ActorAvatarBase
                name={selected.name}
                initials={selected.name.slice(0, 2).toUpperCase()}
                avatarUrl={resolvePublicFileUrl(selected.avatar_url)}
                isSquad
                size="xs"
              />
            ) : isStandalone ? (
              <Bot className="size-3.5 text-muted-foreground" />
            ) : (
              <Users className="size-3.5 text-muted-foreground" />
            )}
            <span className="max-w-40 truncate">
              {selected ? selected.name : isStandalone ? standaloneLabel : allLabel}
            </span>
            <ChevronDown className="size-3 text-muted-foreground" />
          </Button>
        }
      />
      <DropdownMenuContent align="end" className="max-h-72 w-auto min-w-52">
        <DropdownMenuRadioGroup
          value={selected ? selected.id : isStandalone ? STANDALONE_SQUAD : ALL_SQUADS}
          onValueChange={(value) => onSquadChange(value ?? ALL_SQUADS)}
        >
          <DropdownMenuRadioItem value={ALL_SQUADS}>
            <Users className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{allLabel}</span>
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value={STANDALONE_SQUAD}>
            <Bot className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{standaloneLabel}</span>
          </DropdownMenuRadioItem>
          {liveSquads.length > 0 ? (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuLabel className="text-micro font-medium text-muted-foreground">
                  {t(($) => $.filter.squads_group)}
                </DropdownMenuLabel>
                {liveSquads.map((squad) => (
                  <DropdownMenuRadioItem key={squad.id} value={squad.id}>
                    <ActorAvatarBase
                      name={squad.name}
                      initials={squad.name.slice(0, 2).toUpperCase()}
                      avatarUrl={resolvePublicFileUrl(squad.avatar_url)}
                      isSquad
                      size="xs"
                    />
                    <span className="truncate">{squad.name}</span>
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuGroup>
            </>
          ) : null}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
