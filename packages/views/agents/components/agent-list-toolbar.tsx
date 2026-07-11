"use client";

import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  Filter,
  Search,
  X,
} from "lucide-react";
import type { AgentAvailability } from "@multica/core/agents";
import type { MemberWithUser } from "@multica/core/types";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import {
  AGENT_SCOPES,
  type AgentColumnKey,
  type AgentListFilters,
  type AgentsScope,
  type AgentSortDirection,
  type AgentSortField,
} from "@multica/core/agents/stores";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
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
import { availabilityConfig } from "../presence";
import { useT } from "../../i18n";
import type { AgentListRow } from "./agents-page";

const COLUMN_KEYS: AgentColumnKey[] = [
  "status",
  "owner",
  "runtime",
  "lastActive",
  "runs",
  "model",
  "created",
];

const SORT_FIELDS: AgentSortField[] = [
  "lastActive",
  "name",
  "runs",
  "created",
];

const AVAILABILITY_VALUES: AgentAvailability[] = [
  "online",
  "unstable",
  "offline",
];

export function countActiveFilterDimensions(
  filters: AgentListFilters,
): number {
  let count = 0;
  if (filters.availability.length > 0) count++;
  if (filters.runtimes.length > 0) count++;
  if (filters.owners.length > 0) count++;
  if (filters.models.length > 0) count++;
  return count;
}

export function AgentListToolbar({
  scope,
  onScopeChange,
  scopeCounts,
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
  members,
  visibleCount,
}: {
  scope: AgentsScope;
  onScopeChange: (scope: AgentsScope) => void;
  /** Per-scope totals from the FULL set — scope counts ignore filters. */
  scopeCounts: Record<AgentsScope, number>;
  search: string;
  onSearchChange: (value: string) => void;
  filters: AgentListFilters;
  onToggleFilter: (key: keyof AgentListFilters, value: string) => void;
  onClearFilters: () => void;
  sortField: AgentSortField;
  sortDirection: AgentSortDirection;
  onSortFieldChange: (field: AgentSortField) => void;
  onSortDirectionChange: (direction: AgentSortDirection) => void;
  hiddenColumns: AgentColumnKey[];
  onToggleColumn: (key: AgentColumnKey) => void;
  /** Rows within the current scope, unfiltered — filter option lists and
   *  counts derive from this set. */
  allRows: AgentListRow[];
  members: MemberWithUser[];
  /** Rows surviving the filters — shown as "n / total" when narrowed. */
  visibleCount: number;
}) {
  const { t } = useT("agents");

  const activeCount = countActiveFilterDimensions(filters);
  const hasActiveFilters = activeCount > 0;
  const hasSearch = search.trim().length > 0;

  // Option lists with counts, derived from the scope's unfiltered rows so
  // toggling one dimension doesn't make the others' options vanish.
  const availabilityCounts = new Map<string, number>();
  const runtimeOptions = new Map<string, { name: string; count: number }>();
  for (const row of allRows) {
    if (row.presence) {
      availabilityCounts.set(
        row.presence.availability,
        (availabilityCounts.get(row.presence.availability) ?? 0) + 1,
      );
    }
    const rt = row.runtime;
    if (rt) {
      const entry = runtimeOptions.get(rt.id);
      if (entry) entry.count += 1;
      else runtimeOptions.set(rt.id, { name: rt.name, count: 1 });
    }
  }

  // Owner options: members who own at least one agent in the current scope.
  const memberById = new Map(members.map((m) => [m.user_id, m]));
  const ownerCounts = new Map<string, number>();
  const modelCounts = new Map<string, number>();
  for (const row of allRows) {
    const oid = row.agent.owner_id;
    if (oid) ownerCounts.set(oid, (ownerCounts.get(oid) ?? 0) + 1);
    const model = row.agent.model;
    if (model) modelCounts.set(model, (modelCounts.get(model) ?? 0) + 1);
  }

  const SCOPE_LABELS: Record<AgentsScope, string> = {
    mine: t(($) => $.scope.mine),
    all: t(($) => $.scope.all),
    archived: t(($) => $.scope.archived),
  };

  const SORT_LABELS: Record<AgentSortField, string> = {
    lastActive: t(($) => $.columns.last_active),
    name: t(($) => $.columns.agent),
    runs: t(($) => $.columns.runs),
    created: t(($) => $.columns.created),
  };

  const COLUMN_LABELS: Record<AgentColumnKey, string> = {
    status: t(($) => $.columns.status),
    owner: t(($) => $.columns.owner),
    runtime: t(($) => $.columns.runtime),
    lastActive: t(($) => $.columns.last_active),
    runs: t(($) => $.columns.runs),
    model: t(($) => $.columns.model),
    created: t(($) => $.columns.created),
  };
  const sortLabel = SORT_LABELS[sortField];

  const countBadge = (n: number) => (
    <span className="ml-auto pl-3 text-xs text-muted-foreground">{n}</span>
  );

  return (
    <div className="h-12 shrink-0 overflow-x-auto px-5 [-webkit-overflow-scrolling:touch]">
      <div className="flex h-full w-max min-w-full items-center justify-between gap-2">
        {/* Left: local search + scope buttons + result count. Scope mixes the
          ownership lens (mine/all) with the archived lifecycle stage. Button
          styling and the <md dropdown collapse follow the issues header's
          scope buttons. */}
      <div className="flex min-w-0 items-center gap-2">
        <div className="relative hidden shrink-0 md:block">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            aria-label={t(($) => $.page.search_placeholder)}
            placeholder={t(($) => $.page.search_placeholder)}
            className="h-8 w-56 pl-8 text-sm"
          />
        </div>

        <div className="hidden shrink-0 items-center gap-1 md:flex">
          {AGENT_SCOPES.map((s) => (
            <Button
              key={s}
              variant="outline"
              size="sm"
              className={
                scope === s
                  ? "gap-1.5 bg-accent text-accent-foreground hover:bg-accent/80"
                  : "gap-1.5 text-muted-foreground"
              }
              onClick={() => onScopeChange(s)}
            >
              {SCOPE_LABELS[s]}
              <span className="tabular-nums text-xs text-muted-foreground/70">
                {scopeCounts[s]}
              </span>
            </Button>
          ))}
        </div>

        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="outline"
                size="sm"
                className="shrink-0 gap-1 text-muted-foreground md:hidden"
              >
                <span className="truncate">{SCOPE_LABELS[scope]}</span>
                <ChevronDown className="size-3 text-muted-foreground" />
              </Button>
            }
          />
          <DropdownMenuContent align="start" className="w-auto">
            <DropdownMenuRadioGroup
              value={scope}
              onValueChange={(value) => onScopeChange(value as AgentsScope)}
            >
              {AGENT_SCOPES.map((s) => (
                <DropdownMenuRadioItem key={s} value={s}>
                  {SCOPE_LABELS[s]}
                  <span className="ml-2 tabular-nums text-xs text-muted-foreground/70">
                    {scopeCounts[s]}
                  </span>
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>

        {(hasActiveFilters || hasSearch) && (
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
            {/* Availability */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <span className="flex-1">
                  {t(($) => $.toolbar.section_availability)}
                </span>
                {filters.availability.length > 0 && (
                  <span className="text-xs font-medium text-primary">
                    {filters.availability.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-44">
                {AVAILABILITY_VALUES.map((value) => {
                  const visual = availabilityConfig[value];
                  return (
                    <DropdownMenuCheckboxItem
                      key={value}
                      checked={filters.availability.includes(value)}
                      onCheckedChange={() =>
                        onToggleFilter("availability", value)
                      }
                      className={FILTER_ITEM_CLASS}
                    >
                      <HoverCheck
                        checked={filters.availability.includes(value)}
                      />
                      <span
                        className={`size-1.5 shrink-0 rounded-full ${visual.dotClass}`}
                      />
                      {t(($) => $.availability[value])}
                      {countBadge(availabilityCounts.get(value) ?? 0)}
                    </DropdownMenuCheckboxItem>
                  );
                })}
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Runtime */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <span className="flex-1">
                  {t(($) => $.toolbar.section_runtime)}
                </span>
                {filters.runtimes.length > 0 && (
                  <span className="text-xs font-medium text-primary">
                    {filters.runtimes.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="max-h-72 w-auto min-w-48 overflow-y-auto">
                {[...runtimeOptions.entries()].map(([id, { name, count }]) => (
                  <DropdownMenuCheckboxItem
                    key={id}
                    checked={filters.runtimes.includes(id)}
                    onCheckedChange={() => onToggleFilter("runtimes", id)}
                    className={FILTER_ITEM_CLASS}
                  >
                    <HoverCheck checked={filters.runtimes.includes(id)} />
                    <span className="min-w-0 truncate">{name}</span>
                    {countBadge(count)}
                  </DropdownMenuCheckboxItem>
                ))}
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Owner — the same person-axis as the Mine scope. Picking an
                owner here leaves the clean "mine" view for "all" (store
                rule), so Mine + owner never coexist. */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <span className="flex-1">
                  {t(($) => $.toolbar.section_owner)}
                </span>
                {filters.owners.length > 0 && (
                  <span className="text-xs font-medium text-primary">
                    {filters.owners.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="max-h-72 w-auto min-w-48 overflow-y-auto">
                {[...ownerCounts.entries()].map(([userId, count]) => {
                  const m = memberById.get(userId);
                  return (
                    <DropdownMenuCheckboxItem
                      key={userId}
                      checked={filters.owners.includes(userId)}
                      onCheckedChange={() => onToggleFilter("owners", userId)}
                      className={FILTER_ITEM_CLASS}
                    >
                      <HoverCheck checked={filters.owners.includes(userId)} />
                      <ActorAvatar
                        name={m?.name ?? userId.slice(0, 8)}
                        initials={(m?.name ?? "?").slice(0, 2).toUpperCase()}
                        avatarUrl={resolvePublicFileUrl(m?.avatar_url ?? null)}
                        size="sm"
                      />
                      <span className="min-w-0 truncate">
                        {m?.name ?? userId.slice(0, 8)}
                      </span>
                      {countBadge(count)}
                    </DropdownMenuCheckboxItem>
                  );
                })}
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Model — runtime-native model id (categorical column → filter) */}
            {modelCounts.size > 0 && (
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <span className="flex-1">
                    {t(($) => $.toolbar.section_model)}
                  </span>
                  {filters.models.length > 0 && (
                    <span className="text-xs font-medium text-primary">
                      {filters.models.length}
                    </span>
                  )}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent className="max-h-72 w-auto min-w-44 overflow-y-auto">
                  {[...modelCounts.entries()].map(([model, count]) => (
                    <DropdownMenuCheckboxItem
                      key={model}
                      checked={filters.models.includes(model)}
                      onCheckedChange={() => onToggleFilter("models", model)}
                      className={FILTER_ITEM_CLASS}
                    >
                      <HoverCheck checked={filters.models.includes(model)} />
                      <span className="min-w-0 truncate">{model}</span>
                      {countBadge(count)}
                    </DropdownMenuCheckboxItem>
                  ))}
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            )}
          </DropdownMenuContent>
        </DropdownMenu>

        {/* Display settings */}
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
                        onSortFieldChange(v as AgentSortField)
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
    </div>
  );
}
