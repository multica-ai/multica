"use client";

import { useEffect, useMemo } from "react";
import { CircleDot, Filter, Mail, RotateCcw, SignalHigh, UserRound } from "lucide-react";
import { PRIORITY_DISPLAY_ORDER } from "@multica/core/issues/config";
import {
  filterInboxItems,
  inboxActorKey,
  inboxActorKeyParts,
  inboxFiltersForPrioritySupport,
  inboxFilterCount,
  type InboxPriorityFilterSupport,
  useInboxFilters,
  useInboxFilterStore,
} from "@multica/core/inbox/filter-store";
import { useActorName } from "@multica/core/workspace/hooks";
import type { InboxItem } from "@multica/core/types";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
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

function actorCounts(items: InboxItem[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const item of items) {
    const key = inboxActorKey(item);
    if (key == null) continue;
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  return counts;
}

/** Faceted filters for the deduplicated list currently on screen. */
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
  const toggleActor = useInboxFilterStore((state) => state.toggleActorFilter);
  const toggleUnreadOnly = useInboxFilterStore(
    (state) => state.toggleUnreadOnly,
  );
  const { getActorName, getActorInitials, getActorAvatarUrl } = useActorName();
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

  // Counts are faceted: every count respects the other active dimensions while
  // ignoring its own, so each number says how many rows selecting that value
  // can actually reveal.
  const statusFacetItems = useMemo(
    () => filterInboxItems(items, { ...effectiveFilters, statuses: [] }),
    [items, effectiveFilters],
  );
  const priorityFacetItems = useMemo(
    () => filterInboxItems(items, { ...effectiveFilters, priorities: [] }),
    [items, effectiveFilters],
  );
  const actorFacetItems = useMemo(
    () => filterInboxItems(items, { ...effectiveFilters, actors: [] }),
    [items, effectiveFilters],
  );
  const unreadCount = useMemo(
    () =>
      filterInboxItems(items, { ...effectiveFilters, unreadOnly: false }).filter(
        (item) => item.read !== true,
      ).length,
    [items, effectiveFilters],
  );
  const statuses = useMemo(
    () => statusCounts(statusFacetItems),
    [statusFacetItems],
  );
  const priorities = useMemo(
    () => priorityCounts(priorityFacetItems),
    [priorityFacetItems],
  );
  const actors = useMemo(
    () => actorCounts(actorFacetItems),
    [actorFacetItems],
  );
  // The universe of actors comes from every row in the view rather than the
  // faceted subset: picking one actor must not remove the others from the menu
  // that offers them. Sorted by name so the list does not reshuffle as counts
  // change under other selections.
  const actorOptions = useMemo(() => {
    const keys = new Set<string>();
    for (const item of items) {
      const key = inboxActorKey(item);
      if (key != null) keys.add(key);
    }
    return [...keys]
      .map((key) => {
        const { type, id } = inboxActorKeyParts(key);
        return { key, type, id, name: getActorName(type, id) };
      })
      .sort((a, b) => a.name.localeCompare(b.name));
  }, [items, getActorName]);
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
        <DropdownMenuCheckboxItem
          checked={effectiveFilters.unreadOnly}
          onCheckedChange={() => toggleUnreadOnly(wsId)}
        >
          <Mail className="size-3.5" />
          <span className="flex-1">{t(($) => $.filters.unread_only)}</span>
          {unreadCount > 0 && (
            <span className="text-caption text-muted-foreground">
              {t(($) => $.filters.notification_count, { count: unreadCount })}
            </span>
          )}
        </DropdownMenuCheckboxItem>
        <DropdownMenuSeparator />

        {actorOptions.length > 0 && (
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <UserRound className="size-3.5" />
              <span className="flex-1">{t(($) => $.filters.from)}</span>
              {effectiveFilters.actors.length > 0 && (
                <span className="text-caption font-medium text-primary">
                  {effectiveFilters.actors.length}
                </span>
              )}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="w-auto min-w-48">
              {actorOptions.map((option) => {
                const checked = effectiveFilters.actors.includes(option.key);
                const count = actors.get(option.key) ?? 0;
                return (
                  <DropdownMenuCheckboxItem
                    key={option.key}
                    checked={checked}
                    onCheckedChange={() => toggleActor(wsId, option.key)}
                  >
                    <ActorAvatar
                      size="xs"
                      name={option.name}
                      initials={getActorInitials(option.type, option.id)}
                      avatarUrl={getActorAvatarUrl(option.type, option.id)}
                      isAgent={option.type === "agent"}
                      isSquad={option.type === "squad"}
                      isSystem={option.type === "system"}
                    />
                    <span className="flex-1">{option.name}</span>
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
        )}

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
