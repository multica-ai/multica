"use client";

import { ArrowDown, ArrowUp, ChevronDown } from "@multica/ui/icons";
import type { Autopilot } from "@multica/core/types";
import type {
  AutopilotColumnKey,
  AutopilotListFilters,
  AutopilotSortDirection,
  AutopilotSortField,
} from "@multica/core/autopilots/stores";
import { useActorName } from "@multica/core/workspace/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
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
import { ActorAvatar } from "../../common/actor-avatar";
import { FILTER_ITEM_CLASS, HoverCheck } from "../../common/hover-check";
import { useT } from "../../i18n";
import {
  CollectionDisplayButton,
  CollectionFilterButton,
} from "../../layout/collection-page";

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

/**
 * The header's filter icon and its menu. Status is the title's scope, so it
 * never appears here.
 */
export function AutopilotFilterMenu({
  filters,
  onToggleFilter,
  allRows,
}: {
  filters: AutopilotListFilters;
  onToggleFilter: (key: keyof AutopilotListFilters, value: string) => void;
  /** Rows within the current scope, unfiltered — option lists and counts
   *  derive from this set. */
  allRows: Autopilot[];
}) {
  const { t } = useT("autopilots");
  const { getActorName } = useActorName();

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

  const countBadge = (n: number) => (
    <span className="ml-auto pl-3 text-caption text-muted-foreground">{n}</span>
  );

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <CollectionFilterButton count={countActiveFilterDimensions(filters)} />
        }
      />
      <DropdownMenuContent align="start" className="w-auto">
        {/* Assignee */}
        <DropdownMenuSub>
          <DropdownMenuSubTrigger>
            <span className="flex-1">
              {t(($) => $.toolbar.section_assignee)}
            </span>
            {filters.assignees.length > 0 && (
              <span className="text-caption font-medium text-primary">
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
                  onCheckedChange={() => onToggleFilter("assignees", value)}
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={filters.assignees.includes(value)} />
                  <ActorAvatar actorType={type} actorId={id} size="sm" />
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
              <span className="text-caption font-medium text-primary">
                {filters.triggerKinds.length}
              </span>
            )}
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="w-auto min-w-44">
            {TRIGGER_KINDS.filter((kind) => triggerKindCounts.has(kind)).map(
              (kind) => (
                <DropdownMenuCheckboxItem
                  key={kind}
                  checked={filters.triggerKinds.includes(kind)}
                  onCheckedChange={() => onToggleFilter("triggerKinds", kind)}
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={filters.triggerKinds.includes(kind)} />
                  {t(($) => $.trigger_kind[kind])}
                  {countBadge(triggerKindCounts.get(kind) ?? 0)}
                </DropdownMenuCheckboxItem>
              ),
            )}
          </DropdownMenuSubContent>
        </DropdownMenuSub>

        {/* Mode */}
        <DropdownMenuSub>
          <DropdownMenuSubTrigger>
            <span className="flex-1">{t(($) => $.toolbar.section_mode)}</span>
            {filters.modes.length > 0 && (
              <span className="text-caption font-medium text-primary">
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
              <span className="text-caption font-medium text-primary">
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
                  <ActorAvatar actorType={type} actorId={id} size="sm" />
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
  );
}

/**
 * Display settings — same paradigm as skills: sort select + direction toggle +
 * column switches, mutating the same store the list header sort buttons use.
 */
export function AutopilotDisplayMenu({
  sortField,
  sortDirection,
  onSortFieldChange,
  onSortDirectionChange,
  hiddenColumns,
  onToggleColumn,
}: {
  sortField: AutopilotSortField;
  sortDirection: AutopilotSortDirection;
  onSortFieldChange: (field: AutopilotSortField) => void;
  onSortDirectionChange: (direction: AutopilotSortDirection) => void;
  hiddenColumns: AutopilotColumnKey[];
  onToggleColumn: (key: AutopilotColumnKey) => void;
}) {
  const { t } = useT("autopilots");

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

  return (
    <Popover>
      <PopoverTrigger render={<CollectionDisplayButton />} />
      <PopoverContent align="end" className="w-64 p-0">
        <div className="border-b px-3 py-2.5">
          <span className="text-caption font-medium text-muted-foreground">
            {t(($) => $.toolbar.sort_by)}
          </span>
          <div className="mt-2 flex items-center gap-1.5">
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="outline"
                    size="sm"
                    className="flex-1 justify-between text-caption"
                  >
                    {SORT_LABELS[sortField]}
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
            <IconButton
              variant="outline"
              size="icon-sm"
              onClick={() =>
                onSortDirectionChange(sortDirection === "asc" ? "desc" : "asc")
              }
              label={
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
            </IconButton>
          </div>
        </div>

        <div className="px-3 py-2.5">
          <span className="text-caption font-medium text-muted-foreground">
            {t(($) => $.toolbar.section_columns)}
          </span>
          <div className="mt-2 space-y-2">
            {COLUMN_KEYS.map((key) => (
              <label
                key={key}
                className="flex cursor-pointer items-center justify-between"
              >
                <span className="text-body">{COLUMN_LABELS[key]}</span>
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
  );
}
