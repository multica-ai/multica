"use client";

import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  Filter,
  X,
} from "lucide-react";
import type { Autopilot } from "@multica/core/types";
import {
  AUTOPILOT_SCOPES,
  type AutopilotColumnKey,
  type AutopilotListFilters,
  type AutopilotScope,
  type AutopilotSortDirection,
  type AutopilotSortField,
} from "@multica/core/autopilots/stores";
import { useActorName } from "@multica/core/workspace/hooks";
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
import { ActorAvatar } from "../../common/actor-avatar";
import { FILTER_ITEM_CLASS, HoverCheck } from "../../common/hover-check";
import { useT } from "../../i18n";

// Composite "type:id" value for polymorphic actor filter dimensions, so the
// string[] filter store can hold agent/squad/member references alike.
export function actorFilterValue(type: string, id: string): string {
  return `${type}:${id}`;
}

const COLUMN_KEYS: AutopilotColumnKey[] = [
  "assignee",
  "trigger",
  "lastRun",
  "nextRun",
  "mode",
  "creator",
  "created",
];

const SORT_FIELDS: AutopilotSortField[] = [
  "name",
  "lastRun",
  "nextRun",
  "created",
];

const MODES = ["create_issue", "run_only"] as const;
const TRIGGER_KINDS = ["schedule", "webhook", "api"] as const;

export function countActiveFilterDimensions(
  filters: AutopilotListFilters,
): number {
  let count = 0;
  if (filters.assignees.length > 0) count++;
  if (filters.modes.length > 0) count++;
  if (filters.triggerKinds.length > 0) count++;
  if (filters.creators.length > 0) count++;
  return count;
}

export function AutopilotListToolbar({
  scope,
  onScopeChange,
  scopeCounts,
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
  scope: AutopilotScope;
  onScopeChange: (scope: AutopilotScope) => void;
  /** Per-scope totals from the FULL set — scope counts ignore filters. */
  scopeCounts: Record<AutopilotScope, number>;
  filters: AutopilotListFilters;
  onToggleFilter: (key: keyof AutopilotListFilters, value: string) => void;
  onClearFilters: () => void;
  sortField: AutopilotSortField;
  sortDirection: AutopilotSortDirection;
  onSortFieldChange: (field: AutopilotSortField) => void;
  onSortDirectionChange: (direction: AutopilotSortDirection) => void;
  hiddenColumns: AutopilotColumnKey[];
  onToggleColumn: (key: AutopilotColumnKey) => void;
  /** Rows within the current scope, unfiltered — filter option lists and
   *  counts derive from this set. */
  allRows: Autopilot[];
  /** Rows surviving the filters — shown as "n / total" when narrowed. */
  visibleCount: number;
}) {
  const { t } = useT("autopilots");
  const { getActorName } = useActorName();

  const activeCount = countActiveFilterDimensions(filters);
  const hasActiveFilters = activeCount > 0;

  // Option lists with counts, derived from the scope's unfiltered rows so
  // toggling one dimension doesn't make the others' options vanish.
  const assigneeOptions = new Map<
    string,
    { type: string; id: string; count: number }
  >();
  const creatorOptions = new Map<
    string,
    { type: string; id: string; count: number }
  >();
  const modeCounts = new Map<string, number>();
  const triggerKindCounts = new Map<string, number>();
  for (const row of allRows) {
    const aKey = actorFilterValue(row.assignee_type, row.assignee_id);
    const a = assigneeOptions.get(aKey);
    if (a) a.count += 1;
    else
      assigneeOptions.set(aKey, {
        type: row.assignee_type,
        id: row.assignee_id,
        count: 1,
      });
    const cKey = actorFilterValue(row.created_by_type, row.created_by_id);
    const c = creatorOptions.get(cKey);
    if (c) c.count += 1;
    else
      creatorOptions.set(cKey, {
        type: row.created_by_type,
        id: row.created_by_id,
        count: 1,
      });
    modeCounts.set(
      row.execution_mode,
      (modeCounts.get(row.execution_mode) ?? 0) + 1,
    );
    for (const kind of row.trigger_kinds ?? []) {
      triggerKindCounts.set(kind, (triggerKindCounts.get(kind) ?? 0) + 1);
    }
  }

  const SCOPE_LABELS: Record<AutopilotScope, string> = {
    all: t(($) => $.page.scope_all),
    active: t(($) => $.status.active),
    paused: t(($) => $.status.paused),
  };

  const SORT_LABELS: Record<AutopilotSortField, string> = {
    name: t(($) => $.page.table.name),
    lastRun: t(($) => $.page.table.last_run),
    nextRun: t(($) => $.page.table.next_run),
    created: t(($) => $.page.table.created),
  };

  const COLUMN_LABELS: Record<AutopilotColumnKey, string> = {
    assignee: t(($) => $.page.table.assignee),
    trigger: t(($) => $.page.table.trigger),
    lastRun: t(($) => $.page.table.last_run),
    nextRun: t(($) => $.page.table.next_run),
    mode: t(($) => $.page.table.mode),
    creator: t(($) => $.page.table.created_by),
    created: t(($) => $.page.table.created),
  };
  const sortLabel = SORT_LABELS[sortField];

  const countBadge = (n: number) => (
    <span className="ml-auto pl-3 text-xs text-muted-foreground">{n}</span>
  );

  return (
    <div className="flex h-12 shrink-0 items-center justify-between gap-2 px-5">
      {/* Left: scope buttons + result count. Scope is the promoted status
          dimension (it does NOT appear in the filter dropdown). No search
          box: scope buttons already partition the (small) set, so search
          was dropped by product call. The count only appears while filters
          narrow the list. Button styling and the <md dropdown collapse
          follow the issues header's scope buttons. */}
      <div className="flex min-w-0 items-center gap-2">
        <div className="hidden shrink-0 items-center gap-1 md:flex">
          {AUTOPILOT_SCOPES.map((s) => (
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
              onValueChange={(value) =>
                onScopeChange(value as AutopilotScope)
              }
            >
              {AUTOPILOT_SCOPES.map((s) => (
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

        {hasActiveFilters && (
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
            {/* Assignee */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <span className="flex-1">
                  {t(($) => $.toolbar.section_assignee)}
                </span>
                {filters.assignees.length > 0 && (
                  <span className="text-xs font-medium text-primary">
                    {filters.assignees.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="max-h-72 w-auto min-w-48 overflow-y-auto">
                {[...assigneeOptions.entries()].map(
                  ([value, { type, id, count }]) => (
                    <DropdownMenuCheckboxItem
                      key={value}
                      checked={filters.assignees.includes(value)}
                      onCheckedChange={() =>
                        onToggleFilter("assignees", value)
                      }
                      className={FILTER_ITEM_CLASS}
                    >
                      <HoverCheck
                        checked={filters.assignees.includes(value)}
                      />
                      <ActorAvatar actorType={type} actorId={id} size={16} />
                      <span className="min-w-0 truncate">
                        {getActorName(type, id)}
                      </span>
                      {countBadge(count)}
                    </DropdownMenuCheckboxItem>
                  ),
                )}
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Trigger kind */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <span className="flex-1">
                  {t(($) => $.toolbar.section_trigger)}
                </span>
                {filters.triggerKinds.length > 0 && (
                  <span className="text-xs font-medium text-primary">
                    {filters.triggerKinds.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-44">
                {TRIGGER_KINDS.filter((kind) =>
                  triggerKindCounts.has(kind),
                ).map((kind) => (
                  <DropdownMenuCheckboxItem
                    key={kind}
                    checked={filters.triggerKinds.includes(kind)}
                    onCheckedChange={() =>
                      onToggleFilter("triggerKinds", kind)
                    }
                    className={FILTER_ITEM_CLASS}
                  >
                    <HoverCheck
                      checked={filters.triggerKinds.includes(kind)}
                    />
                    {t(($) => $.trigger_kind[kind])}
                    {countBadge(triggerKindCounts.get(kind) ?? 0)}
                  </DropdownMenuCheckboxItem>
                ))}
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Mode */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <span className="flex-1">
                  {t(($) => $.toolbar.section_mode)}
                </span>
                {filters.modes.length > 0 && (
                  <span className="text-xs font-medium text-primary">
                    {filters.modes.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-44">
                {MODES.map((mode) => (
                  <DropdownMenuCheckboxItem
                    key={mode}
                    checked={filters.modes.includes(mode)}
                    onCheckedChange={() => onToggleFilter("modes", mode)}
                    className={FILTER_ITEM_CLASS}
                  >
                    <HoverCheck checked={filters.modes.includes(mode)} />
                    {t(($) => $.execution_mode[mode])}
                    {countBadge(modeCounts.get(mode) ?? 0)}
                  </DropdownMenuCheckboxItem>
                ))}
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Creator */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <span className="flex-1">
                  {t(($) => $.toolbar.section_creator)}
                </span>
                {filters.creators.length > 0 && (
                  <span className="text-xs font-medium text-primary">
                    {filters.creators.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="max-h-72 w-auto min-w-48 overflow-y-auto">
                {[...creatorOptions.entries()].map(
                  ([value, { type, id, count }]) => (
                    <DropdownMenuCheckboxItem
                      key={value}
                      checked={filters.creators.includes(value)}
                      onCheckedChange={() => onToggleFilter("creators", value)}
                      className={FILTER_ITEM_CLASS}
                    >
                      <HoverCheck checked={filters.creators.includes(value)} />
                      <ActorAvatar actorType={type} actorId={id} size={16} />
                      <span className="min-w-0 truncate">
                        {getActorName(type, id)}
                      </span>
                      {countBadge(count)}
                    </DropdownMenuCheckboxItem>
                  ),
                )}
              </DropdownMenuSubContent>
            </DropdownMenuSub>
          </DropdownMenuContent>
        </DropdownMenu>

        {/* Display settings — same paradigm as skills: sort select +
            direction toggle + column switches, mutating the same store the
            list header sort buttons use. */}
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
                        onSortFieldChange(v as AutopilotSortField)
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
