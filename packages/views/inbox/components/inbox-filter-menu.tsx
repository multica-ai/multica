"use client";

import { useEffect, useMemo } from "react";
import { CircleDot, Filter, RotateCcw, SignalHigh } from "lucide-react";
import { PRIORITY_DISPLAY_ORDER } from "@multica/core/issues/config";
import {
  filterInboxItems,
  inboxFiltersForPrioritySupport,
  inboxFilterCount,
  type InboxPriorityFilterSupport,
  useInboxFilters,
  useInboxFilterStore,
} from "@multica/core/inbox/filter-store";
import type { InboxItem } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { StatusIcon } from "../../issues/components/status-icon";
import { useStatusOptions } from "../../issues/utils/status-options";
import { useT } from "../../i18n";

function statusCounts(items: InboxItem[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const item of items) {
    if (item.issue_status == null) continue;
    counts.set(item.issue_status, (counts.get(item.issue_status) ?? 0) + 1);
  }
  return counts;
}

function priorityCounts(items: InboxItem[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const item of items) {
    if (item.issue_priority == null) continue;
    counts.set(
      item.issue_priority,
      (counts.get(item.issue_priority) ?? 0) + 1,
    );
  }
  return counts;
}

/** Priority/status facets for the deduplicated list currently on screen. */
export function InboxFilterMenu({
  wsId,
  items,
  priorityFilterSupport,
}: {
  wsId: string;
  items: InboxItem[];
  priorityFilterSupport: InboxPriorityFilterSupport;
}) {
  const { t } = useT("inbox");
  const { t: tIssues } = useT("issues");
  const filters = useInboxFilters(wsId);
  const toggleStatus = useInboxFilterStore((state) => state.toggleStatusFilter);
  const togglePriority = useInboxFilterStore(
    (state) => state.togglePriorityFilter,
  );
  const clearFilters = useInboxFilterStore((state) => state.clearFilters);
  const clearPriorityFilters = useInboxFilterStore(
    (state) => state.clearPriorityFilters,
  );
  const inboxStatusKeys = useMemo(
    () => [
      ...new Set(
        items.flatMap((item) =>
          item.issue_status == null ? [] : [item.issue_status],
        ),
      ),
    ],
    [items],
  );
  const statusOptions = useStatusOptions(wsId, inboxStatusKeys);
  const effectiveFilters = useMemo(
    () => inboxFiltersForPrioritySupport(filters, priorityFilterSupport),
    [filters, priorityFilterSupport],
  );
  const activeCount = inboxFilterCount(effectiveFilters);
  const priorityFilteringSupported = priorityFilterSupport === "supported";

  // A workspace can retain filters while its backend changes (Desktop server
  // switch, self-hosted downgrade, or rolling deployment). Remove a priority
  // selection only once incompatibility is confirmed; "unknown" simply keeps
  // it dormant while an empty list gives us no capability evidence.
  useEffect(() => {
    if (
      priorityFilterSupport === "unsupported" &&
      filters.priorities.length > 0
    ) {
      clearPriorityFilters(wsId);
    }
  }, [
    clearPriorityFilters,
    filters.priorities.length,
    priorityFilterSupport,
    wsId,
  ]);

  // Counts are faceted: a status count respects the active priority filter
  // (and vice versa) while ignoring its own dimension, so every number says
  // how many rows selecting that value can actually reveal.
  const statusFacetItems = useMemo(
    () =>
      filterInboxItems(items, {
        statuses: [],
        priorities: effectiveFilters.priorities,
      }),
    [items, effectiveFilters.priorities],
  );
  const priorityFacetItems = useMemo(
    () =>
      filterInboxItems(items, {
        statuses: effectiveFilters.statuses,
        priorities: [],
      }),
    [items, effectiveFilters.statuses],
  );
  const statuses = useMemo(
    () => statusCounts(statusFacetItems),
    [statusFacetItems],
  );
  const priorities = useMemo(
    () => priorityCounts(priorityFacetItems),
    [priorityFacetItems],
  );
  const triggerLabel =
    activeCount > 0
      ? t(($) => $.filters.active_count, { count: activeCount })
      : t(($) => $.filters.tooltip);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant={activeCount > 0 ? "default" : "ghost"}
            size="icon-sm"
            aria-label={triggerLabel}
            title={triggerLabel}
            className={cn(
              "text-muted-foreground",
              activeCount > 0 &&
                "w-auto gap-1 bg-brand px-2 text-white hover:bg-brand/90",
            )}
          />
        }
      >
        <Filter className="size-4" />
        {activeCount > 0 && (
          <span className="text-caption tabular-nums">{activeCount}</span>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-auto min-w-44">
        <DropdownMenuSub>
          <DropdownMenuSubTrigger>
            <CircleDot className="size-3.5" />
            <span className="flex-1">{t(($) => $.filters.status)}</span>
            {effectiveFilters.statuses.length > 0 && (
              <span className="text-caption font-medium text-primary">
                {effectiveFilters.statuses.length}
              </span>
            )}
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="w-auto min-w-48">
            {statusOptions.map((option) => {
              const checked = effectiveFilters.statuses.includes(option.key);
              const count = statuses.get(option.key) ?? 0;
              return (
                <DropdownMenuCheckboxItem
                  key={option.key}
                  checked={checked}
                  onCheckedChange={() => toggleStatus(wsId, option.key)}
                >
                  <StatusIcon
                    status={option.key}
                    category={option.category}
                    color={option.color}
                    className="size-3.5"
                  />
                  <span className="flex-1">{option.label}</span>
                  {count > 0 && (
                    <span className="text-caption text-muted-foreground">
                      {t(($) => $.filters.notification_count, { count })}
                    </span>
                  )}
                </DropdownMenuCheckboxItem>
              );
            })}
          </DropdownMenuSubContent>
        </DropdownMenuSub>

        {priorityFilteringSupported ? (
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <SignalHigh className="size-3.5" />
              <span className="flex-1">{t(($) => $.filters.priority)}</span>
              {effectiveFilters.priorities.length > 0 ? (
                <span className="text-caption font-medium text-primary">
                  {effectiveFilters.priorities.length}
                </span>
              ) : null}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="w-auto min-w-44">
              {PRIORITY_DISPLAY_ORDER.map((priority) => {
                const checked = effectiveFilters.priorities.includes(priority);
                const count = priorities.get(priority) ?? 0;
                return (
                  <DropdownMenuCheckboxItem
                    key={priority}
                    checked={checked}
                    onCheckedChange={() => togglePriority(wsId, priority)}
                  >
                    <PriorityIcon priority={priority} />
                    <span className="flex-1">
                      {tIssues(($) => $.priority[priority])}
                    </span>
                    {count > 0 ? (
                      <span className="text-caption text-muted-foreground">
                        {t(($) => $.filters.notification_count, { count })}
                      </span>
                    ) : null}
                  </DropdownMenuCheckboxItem>
                );
              })}
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        ) : null}

        {activeCount > 0 && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => clearFilters(wsId)}>
              <RotateCcw className="size-3.5" />
              {t(($) => $.filters.clear)}
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
