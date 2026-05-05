"use client";

import { useMemo } from "react";
import { useStore } from "zustand";
import {
  ArrowDown,
  ArrowUp,
  Check,
  ChevronDown,
  CircleDot,
  Columns3,
  Filter,
  List,
  SignalHigh,
  SlidersHorizontal,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "@multica/i18n/react";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuCheckboxItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  ALL_STATUSES,
  PRIORITY_ORDER,
} from "@multica/core/issues/config";
import { StatusIcon, PriorityIcon } from "../../issues/components";
import {
  SORT_OPTIONS,
  CARD_PROPERTY_OPTIONS,
} from "@multica/core/issues/stores/view-store";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import type { Issue } from "@multica/core/types";
import { myIssuesViewStore, type MyIssuesScope } from "@multica/core/issues/stores/my-issues-view-store";

// ---------------------------------------------------------------------------
// HoverCheck
// ---------------------------------------------------------------------------

const FILTER_ITEM_CLASS =
  "group/fitem pr-1.5! [&>[data-slot=dropdown-menu-checkbox-item-indicator]]:hidden";

function HoverCheck({ checked }: { checked: boolean }) {
  return (
    <div
      className="border-input data-[selected=true]:border-primary data-[selected=true]:bg-primary data-[selected=true]:text-primary-foreground pointer-events-none size-4 shrink-0 rounded-[4px] border transition-all select-none *:[svg]:opacity-0 data-[selected=true]:*:[svg]:opacity-100 opacity-0 group-hover/fitem:opacity-100 group-focus/fitem:opacity-100 data-[selected=true]:opacity-100"
      data-selected={checked}
    >
      <Check className="size-3.5 text-current" />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function getActiveFilterCount(state: {
  statusFilters: string[];
  priorityFilters: string[];
}) {
  let count = 0;
  if (state.statusFilters.length > 0) count++;
  if (state.priorityFilters.length > 0) count++;
  return count;
}

function useIssueCounts(allIssues: Issue[]) {
  return useMemo(() => {
    const status = new Map<string, number>();
    const priority = new Map<string, number>();

    for (const issue of allIssues) {
      status.set(issue.status, (status.get(issue.status) ?? 0) + 1);
      priority.set(issue.priority, (priority.get(issue.priority) ?? 0) + 1);
    }

    return { status, priority };
  }, [allIssues]);
}

// ---------------------------------------------------------------------------
// Scope config (labels come from i18n in the component)
// ---------------------------------------------------------------------------

const SCOPE_KEYS: { value: MyIssuesScope; labelKey: string; descKey: string }[] = [
  { value: "assigned", labelKey: "scope_assigned", descKey: "scope_assigned_desc" },
  { value: "created", labelKey: "scope_created", descKey: "scope_created_desc" },
  { value: "agents", labelKey: "scope_my_agents", descKey: "scope_my_agents_desc" },
];

// ---------------------------------------------------------------------------
// MyIssuesHeader
// ---------------------------------------------------------------------------

export function MyIssuesHeader({ allIssues }: { allIssues: Issue[] }) {
  const t = useT("issues");
  const tStatus = useT("status");
  const tPriority = useT("priority");
  const viewMode = useStore(myIssuesViewStore, (s) => s.viewMode);
  const statusFilters = useStore(myIssuesViewStore, (s) => s.statusFilters);
  const priorityFilters = useStore(myIssuesViewStore, (s) => s.priorityFilters);
  const sortBy = useStore(myIssuesViewStore, (s) => s.sortBy);
  const sortDirection = useStore(myIssuesViewStore, (s) => s.sortDirection);
  const cardProperties = useStore(myIssuesViewStore, (s) => s.cardProperties);
  const scope = useStore(myIssuesViewStore, (s) => s.scope);
  const act = myIssuesViewStore.getState();

  const counts = useIssueCounts(allIssues);

  const hasActiveFilters =
    getActiveFilterCount({ statusFilters, priorityFilters }) > 0;

  const sortLabel =
    SORT_OPTIONS.find((o) => o.value === sortBy)?.label ?? "Manual";

  return (
    <div className="flex h-12 shrink-0 items-center justify-between px-4">
      {/* Left: scope buttons */}
      <div className="flex items-center gap-1">
        {SCOPE_KEYS.map((s) => (
          <Tooltip key={s.value}>
            <TooltipTrigger
              render={
                <Button
                  variant="outline"
                  size="sm"
                  className={
                    scope === s.value
                      ? "bg-accent text-accent-foreground hover:bg-accent/80"
                      : "text-muted-foreground"
                  }
                  onClick={() => act.setScope(s.value)}
                >
                  {t(s.labelKey)}
                </Button>
              }
            />
            <TooltipContent side="bottom">{t(s.descKey)}</TooltipContent>
          </Tooltip>
        ))}
      </div>

      {/* Right: filter + display + view toggle */}
      <div className="flex items-center gap-1">
        {/* Filter */}
        <DropdownMenu>
          <Tooltip>
            <DropdownMenuTrigger
              render={
                <TooltipTrigger
                  render={
                    <Button variant="outline" size="icon-sm" className="relative text-muted-foreground">
                      <Filter className="size-4" />
                      {hasActiveFilters && (
                        <span className="absolute top-0 right-0 size-1.5 rounded-full bg-brand" />
                      )}
                    </Button>
                  }
                />
              }
            />
            <TooltipContent side="bottom">{t("filter_placeholder")}</TooltipContent>
          </Tooltip>
          <DropdownMenuContent align="end" className="w-auto">
            {/* Status */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <CircleDot className="size-3.5" />
                <span className="flex-1">{t("filter_status")}</span>
                {statusFilters.length > 0 && (
                  <span className="text-xs text-primary font-medium">
                    {statusFilters.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-48">
                {ALL_STATUSES.map((s) => {
                  const checked = statusFilters.includes(s);
                  const count = counts.status.get(s) ?? 0;
                  return (
                    <DropdownMenuCheckboxItem
                      key={s}
                      checked={checked}
                      onCheckedChange={() => act.toggleStatusFilter(s)}
                      className={FILTER_ITEM_CLASS}
                    >
                      <HoverCheck checked={checked} />
                      <StatusIcon status={s} className="h-3.5 w-3.5" />
                      {tStatus(s)}
                      {count > 0 && (
                        <span className="ml-auto text-xs text-muted-foreground">
                          {count} {count === 1 ? t("issue_singular") : t("issue_plural")}
                        </span>
                      )}
                    </DropdownMenuCheckboxItem>
                  );
                })}
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Priority */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <SignalHigh className="size-3.5" />
                <span className="flex-1">{t("filter_priority")}</span>
                {priorityFilters.length > 0 && (
                  <span className="text-xs text-primary font-medium">
                    {priorityFilters.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-44">
                {PRIORITY_ORDER.map((p) => {
                  const checked = priorityFilters.includes(p);
                  const count = counts.priority.get(p) ?? 0;
                  return (
                    <DropdownMenuCheckboxItem
                      key={p}
                      checked={checked}
                      onCheckedChange={() => act.togglePriorityFilter(p)}
                      className={FILTER_ITEM_CLASS}
                    >
                      <HoverCheck checked={checked} />
                      <PriorityIcon priority={p} />
                      {tPriority(p)}
                      {count > 0 && (
                        <span className="ml-auto text-xs text-muted-foreground">
                          {count} {count === 1 ? t("issue_singular") : t("issue_plural")}
                        </span>
                      )}
                    </DropdownMenuCheckboxItem>
                  );
                })}
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Reset */}
            {hasActiveFilters && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={act.clearFilters}>
                  {t("reset_all_filters")}
                </DropdownMenuItem>
              </>
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
                    <Button variant="outline" size="icon-sm" className="text-muted-foreground">
                      <SlidersHorizontal className="size-4" />
                    </Button>
                  }
                />
              }
            />
            <TooltipContent side="bottom">{t("display_settings")}</TooltipContent>
          </Tooltip>
          <PopoverContent align="end" className="w-64 p-0">
            <div className="border-b px-3 py-2.5">
              <span className="text-xs font-medium text-muted-foreground">
                {t("ordering")}
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
                    {SORT_OPTIONS.map((opt) => (
                      <DropdownMenuItem
                        key={opt.value}
                        onClick={() => act.setSortBy(opt.value)}
                      >
                        {opt.label}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
                <Button
                  variant="outline"
                  size="icon-sm"
                  onClick={() =>
                    act.setSortDirection(
                      sortDirection === "asc" ? "desc" : "asc",
                    )
                  }
                  title={sortDirection === "asc" ? t("ascending") : t("descending")}
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
                {t("card_properties")}
              </span>
              <div className="mt-2 space-y-2">
                {CARD_PROPERTY_OPTIONS.map((opt) => (
                  <label
                    key={opt.key}
                    className="flex cursor-pointer items-center justify-between"
                  >
                    <span className="text-sm">{opt.label}</span>
                    <Switch
                      size="sm"
                      checked={cardProperties[opt.key]}
                      onCheckedChange={() => act.toggleCardProperty(opt.key)}
                    />
                  </label>
                ))}
              </div>
            </div>
          </PopoverContent>
        </Popover>

        {/* View toggle */}
        <DropdownMenu>
          <Tooltip>
            <DropdownMenuTrigger
              render={
                <TooltipTrigger
                  render={
                    <Button variant="outline" size="icon-sm" className="text-muted-foreground">
                      {viewMode === "board" ? (
                        <Columns3 className="size-4" />
                      ) : (
                        <List className="size-4" />
                      )}
                    </Button>
                  }
                />
              }
            />
            <TooltipContent side="bottom">
              {viewMode === "board" ? t("board_view") : t("list_view")}
            </TooltipContent>
          </Tooltip>
          <DropdownMenuContent align="end" className="w-auto">
            <DropdownMenuGroup>
              <DropdownMenuLabel>{t("view")}</DropdownMenuLabel>
              <DropdownMenuItem onClick={() => act.setViewMode("board")}>
                <Columns3 />
                {t("board")}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => act.setViewMode("list")}>
                <List />
                {t("list")}
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
