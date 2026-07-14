"use client";

import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  Download,
  Filter,
  HardDrive,
  Pencil,
  Search,
  X,
} from "lucide-react";
import type { Agent, MemberWithUser } from "@multica/core/types";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { FILTER_ITEM_CLASS, HoverCheck } from "../../common/hover-check";
import {
  type SkillColumnKey,
  type SkillListFilters,
  type SkillOriginType,
  type SkillSortDirection,
  type SkillSortField,
} from "@multica/core/skills/stores";
import { useT } from "../../i18n";
import type { SkillRow } from "./skills-page";

export type OriginType = SkillOriginType;

const COLUMN_KEYS: SkillColumnKey[] = [
  "usedBy",
  "source",
  "creator",
  "updated",
  "created",
];

const SORT_FIELDS: SkillSortField[] = ["name", "usedBy", "updated", "created"];

export function countActiveFilterDimensions(
  filters: SkillListFilters,
): number {
  let count = 0;
  if (filters.usage.length > 0) count++;
  if (filters.origins.length > 0) count++;
  if (filters.agents.length > 0) count++;
  if (filters.creators.length > 0) count++;
  return count;
}

const ORIGIN_TYPES: OriginType[] = [
  "manual",
  "runtime_local",
  "clawhub",
  "skills_sh",
  "github",
];

function originIcon(type: OriginType) {
  if (type === "manual") return <Pencil className="size-3.5" />;
  if (type === "runtime_local") return <HardDrive className="size-3.5" />;
  return <Download className="size-3.5" />;
}

export function SkillListToolbar({
  search,
  onSearchChange,
  filters,
  onToggleFilter,
  onClearFilters,
  sortField,
  sortDirection,
  onSortFieldChange,
  onSortDirectionChange,
  hiddenColumns,
  onToggleColumn,
  allRows,
  visibleCount,
}: {
  search: string;
  onSearchChange: (v: string) => void;
  filters: SkillListFilters;
  onToggleFilter: (key: keyof SkillListFilters, value: string) => void;
  onClearFilters: () => void;
  sortField: SkillSortField;
  sortDirection: SkillSortDirection;
  onSortFieldChange: (field: SkillSortField) => void;
  onSortDirectionChange: (direction: SkillSortDirection) => void;
  hiddenColumns: SkillColumnKey[];
  onToggleColumn: (key: SkillColumnKey) => void;
  /** Unfiltered rows — option lists and counts derive from the full set. */
  allRows: SkillRow[];
  /** Rows surviving search + filters — shown as "n / total" when narrowed. */
  visibleCount: number;
}) {
  const { t } = useT("skills");

  const activeCount = countActiveFilterDimensions(filters);
  const hasActiveFilters = activeCount > 0;

  // Option lists with counts, derived from the unfiltered rows so toggling
  // one dimension doesn't make the others' options vanish.
  const usedCount = allRows.filter((r) => r.agents.length > 0).length;
  const unusedCount = allRows.length - usedCount;

  const originCounts = new Map<OriginType, number>();
  const agentOptions = new Map<string, { agent: Agent; count: number }>();
  const creatorOptions = new Map<
    string,
    { member: MemberWithUser; count: number }
  >();
  for (const row of allRows) {
    originCounts.set(row.originType, (originCounts.get(row.originType) ?? 0) + 1);
    for (const agent of row.agents) {
      const entry = agentOptions.get(agent.id);
      if (entry) entry.count += 1;
      else agentOptions.set(agent.id, { agent, count: 1 });
    }
    if (row.creator) {
      const entry = creatorOptions.get(row.creator.user_id);
      if (entry) entry.count += 1;
      else creatorOptions.set(row.creator.user_id, { member: row.creator, count: 1 });
    }
  }

  const ORIGIN_LABELS: Record<OriginType, string> = {
    manual: t(($) => $.table.source_manual),
    runtime_local: t(($) => $.table.source_runtime_unknown),
    clawhub: t(($) => $.table.source_clawhub),
    skills_sh: t(($) => $.table.source_skills_sh),
    github: t(($) => $.table.source_github),
  };

  const COLUMN_LABELS: Record<SkillColumnKey, string> = {
    usedBy: t(($) => $.table.used_by),
    source: t(($) => $.table.source),
    creator: t(($) => $.table.created_by),
    updated: t(($) => $.table.updated),
    created: t(($) => $.table.created),
  };

  const SORT_LABELS: Record<SkillSortField, string> = {
    name: t(($) => $.table.name),
    usedBy: t(($) => $.table.used_by),
    updated: t(($) => $.table.updated),
    created: t(($) => $.table.created),
  };
  const sortLabel = SORT_LABELS[sortField];

  const countBadge = (n: number) => (
    <span className="ml-auto pl-3 text-xs text-muted-foreground">{n}</span>
  );

  return (
    <div className="flex h-12 shrink-0 items-center justify-between gap-2 px-5">
      {/* Left: name search + result count. The count only appears while
          search/filters narrow the list — in the idle state it would just
          duplicate the total already shown in the page header. Below md the
          search (and its count) disappear entirely, following the issues
          header's small-screen treatment. */}
      <div className="flex min-w-0 items-center gap-2">
        <div className="relative hidden md:block">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            aria-label={t(($) => $.page.search_placeholder)}
            placeholder={t(($) => $.page.search_placeholder)}
            className="h-8 w-64 pl-8 text-sm"
          />
        </div>
        {(hasActiveFilters || search.trim().length > 0) && (
          <span
            title={t(($) => $.toolbar.result_count_title)}
            className="hidden shrink-0 text-xs tabular-nums text-muted-foreground md:inline"
          >
            {visibleCount} / {allRows.length}
          </span>
        )}
      </div>

      <div className="flex shrink-0 items-center gap-1">
        {/* Filter */}
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant={hasActiveFilters ? "default" : "outline"}
                size="sm"
                className={
                  hasActiveFilters
                    ? "h-8 w-8 gap-1 bg-brand px-0 text-white hover:bg-brand/90 md:w-auto md:px-2.5"
                    : "h-8 w-8 gap-1 px-0 text-muted-foreground md:w-auto md:px-2.5"
                }
              >
                <Filter className="size-3.5" />
                {hasActiveFilters ? (
                  <>
                    <span className="hidden md:inline">
                      {t(($) => $.toolbar.filter_active_count, {
                        count: activeCount,
                      })}
                    </span>
                    <span className="tabular-nums md:hidden">
                      {activeCount}
                    </span>
                  </>
                ) : (
                  <span className="hidden md:inline">
                    {t(($) => $.toolbar.filter_label)}
                  </span>
                )}
                {hasActiveFilters && (
                  <span
                    role="button"
                    tabIndex={-1}
                    aria-label={t(($) => $.toolbar.clear_filters)}
                    className="-mr-1 ml-0.5 hidden rounded-sm p-0.5 hover:bg-white/20 md:inline-flex"
                    onClick={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      onClearFilters();
                    }}
                    onPointerDown={(e) => e.stopPropagation()}
                  >
                    <X className="size-3" />
                  </span>
                )}
              </Button>
            }
          />
          <DropdownMenuContent align="end" className="w-auto">
            {/* Usage */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <span className="flex-1">
                  {t(($) => $.toolbar.section_usage)}
                </span>
                {filters.usage.length > 0 && (
                  <span className="text-xs font-medium text-primary">
                    {filters.usage.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-44">
                {(["used", "unused"] as const).map((value) => (
                  <DropdownMenuCheckboxItem
                    key={value}
                    checked={filters.usage.includes(value)}
                    onCheckedChange={() => onToggleFilter("usage", value)}
                    className={FILTER_ITEM_CLASS}
                  >
                    <HoverCheck checked={filters.usage.includes(value)} />
                    {value === "used"
                      ? t(($) => $.page.scopes.used.label)
                      : t(($) => $.page.scopes.unused.label)}
                    {countBadge(value === "used" ? usedCount : unusedCount)}
                  </DropdownMenuCheckboxItem>
                ))}
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Source */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <span className="flex-1">{t(($) => $.table.source)}</span>
                {filters.origins.length > 0 && (
                  <span className="text-xs font-medium text-primary">
                    {filters.origins.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-48">
                {ORIGIN_TYPES.filter((type) => originCounts.has(type)).map(
                  (type) => (
                    <DropdownMenuCheckboxItem
                      key={type}
                      checked={filters.origins.includes(type)}
                      onCheckedChange={() => onToggleFilter("origins", type)}
                      className={FILTER_ITEM_CLASS}
                    >
                      <HoverCheck checked={filters.origins.includes(type)} />
                      {originIcon(type)}
                      {ORIGIN_LABELS[type]}
                      {countBadge(originCounts.get(type) ?? 0)}
                    </DropdownMenuCheckboxItem>
                  ),
                )}
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Used by */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <span className="flex-1">{t(($) => $.table.used_by)}</span>
                {filters.agents.length > 0 && (
                  <span className="text-xs font-medium text-primary">
                    {filters.agents.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="max-h-72 w-auto min-w-48 overflow-y-auto">
                {[...agentOptions.values()].map(({ agent, count }) => (
                  <DropdownMenuCheckboxItem
                    key={agent.id}
                    checked={filters.agents.includes(agent.id)}
                    onCheckedChange={() => onToggleFilter("agents", agent.id)}
                    className={FILTER_ITEM_CLASS}
                  >
                    <HoverCheck checked={filters.agents.includes(agent.id)} />
                    <ActorAvatar
                      name={agent.name}
                      initials={agent.name.slice(0, 2).toUpperCase()}
                      avatarUrl={resolvePublicFileUrl(agent.avatar_url)}
                      isAgent
                      size="sm"
                    />
                    <span className="min-w-0 truncate">{agent.name}</span>
                    {countBadge(count)}
                  </DropdownMenuCheckboxItem>
                ))}
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Creator */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <span className="flex-1">{t(($) => $.table.created_by)}</span>
                {filters.creators.length > 0 && (
                  <span className="text-xs font-medium text-primary">
                    {filters.creators.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="max-h-72 w-auto min-w-48 overflow-y-auto">
                {[...creatorOptions.values()].map(({ member, count }) => (
                  <DropdownMenuCheckboxItem
                    key={member.user_id}
                    checked={filters.creators.includes(member.user_id)}
                    onCheckedChange={() =>
                      onToggleFilter("creators", member.user_id)
                    }
                    className={FILTER_ITEM_CLASS}
                  >
                    <HoverCheck
                      checked={filters.creators.includes(member.user_id)}
                    />
                    <ActorAvatar
                      name={member.name}
                      initials={member.name.slice(0, 2).toUpperCase()}
                      avatarUrl={resolvePublicFileUrl(member.avatar_url)}
                      size="sm"
                    />
                    <span className="min-w-0 truncate">{member.name}</span>
                    {countBadge(count)}
                  </DropdownMenuCheckboxItem>
                ))}
              </DropdownMenuSubContent>
            </DropdownMenuSub>
          </DropdownMenuContent>
        </DropdownMenu>

        {/* Display settings — same paradigm as the issues header: Popover
            with bordered sections, trigger shows the active sort (direction
            arrow + field label), sort row is a nested select + a direction
            toggle button, columns are label+Switch rows. Mutates the same
            sort state pair the list header buttons use. */}
        <Popover>
          <Tooltip>
            <PopoverTrigger
              render={
                <TooltipTrigger
                  render={
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-8 w-8 gap-1 px-0 text-muted-foreground md:w-auto md:px-2.5"
                    >
                      {sortDirection === "asc" ? (
                        <ArrowUp className="size-3.5" />
                      ) : (
                        <ArrowDown className="size-3.5" />
                      )}
                      <span className="hidden md:inline">{sortLabel}</span>
                    </Button>
                  }
                />
              }
            />
            <TooltipContent side="bottom">
              {t(($) => $.toolbar.display)}
            </TooltipContent>
          </Tooltip>
          <PopoverContent align="end" className="w-64 p-0">
            <div className="border-b px-3 py-2.5">
              <span className="text-xs font-medium text-muted-foreground">
                {t(($) => $.toolbar.sort_by)}
              </span>
              <div className="mt-2 flex items-center gap-1.5">
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <Button
                        variant="outline"
                        size="sm"
                        className="flex-1 justify-between text-xs"
                      >
                        {sortLabel}
                        <ChevronDown className="size-3 text-muted-foreground" />
                      </Button>
                    }
                  />
                  <DropdownMenuContent align="start" className="w-auto">
                    <DropdownMenuRadioGroup
                      value={sortField}
                      onValueChange={(v) =>
                        onSortFieldChange(v as SkillSortField)
                      }
                    >
                      {SORT_FIELDS.map((field) => (
                        <DropdownMenuRadioItem key={field} value={field}>
                          {SORT_LABELS[field]}
                        </DropdownMenuRadioItem>
                      ))}
                    </DropdownMenuRadioGroup>
                  </DropdownMenuContent>
                </DropdownMenu>
                <Button
                  variant="outline"
                  size="icon-sm"
                  onClick={() =>
                    onSortDirectionChange(
                      sortDirection === "asc" ? "desc" : "asc",
                    )
                  }
                  title={
                    sortDirection === "asc"
                      ? t(($) => $.toolbar.direction_asc)
                      : t(($) => $.toolbar.direction_desc)
                  }
                >
                  {sortDirection === "asc" ? (
                    <ArrowUp className="size-3.5" />
                  ) : (
                    <ArrowDown className="size-3.5" />
                  )}
                </Button>
              </div>
            </div>

            <div className="px-3 py-2.5">
              <span className="text-xs font-medium text-muted-foreground">
                {t(($) => $.toolbar.section_columns)}
              </span>
              <div className="mt-2 space-y-2">
                {COLUMN_KEYS.map((key) => (
                  <label
                    key={key}
                    className="flex cursor-pointer items-center justify-between"
                  >
                    <span className="text-sm">{COLUMN_LABELS[key]}</span>
                    <Switch
                      size="sm"
                      checked={!hiddenColumns.includes(key)}
                      onCheckedChange={() => onToggleColumn(key)}
                    />
                  </label>
                ))}
              </div>
            </div>
          </PopoverContent>
        </Popover>
      </div>
    </div>
  );
}
