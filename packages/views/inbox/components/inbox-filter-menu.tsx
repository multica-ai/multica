"use client";

import { useMemo, useState } from "react";
import { CircleDot, Filter, Mail, RotateCcw, SignalHigh, UserRound } from "lucide-react";
import { PRIORITY_DISPLAY_ORDER } from "@multica/core/issues/config";
import {
  inboxActorKeyParts,
  inboxFilterCount,
  useInboxFilters,
  useInboxFilterStore,
} from "@multica/core/inbox/filter-store";
import { useQuery } from "@tanstack/react-query";
import { archivedInboxFacetsOptions, inboxFacetsOptions } from "@multica/core/inbox/queries";
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

/**
 * Faceted filters for the inbox view on screen. Options and counts come from
 * the server's facets for the whole view, not from the rows loaded so far.
 */
export function InboxFilterMenu({
  wsId,
  items,
  archived = false,
}: {
  wsId: string;
  items: InboxItem[];
  archived?: boolean;
}) {
  const { t } = useT("inbox");
  const { t: tIssues } = useT("issues");
  const filters = useInboxFilters(wsId);
  const [open, setOpen] = useState(false);
  const facetsQuery = useQuery({
    ...(archived ? archivedInboxFacetsOptions(wsId, filters) : inboxFacetsOptions(wsId, filters)),
    enabled: open,
  });
  const facets = facetsQuery.data;
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
  const inboxStatusKeys = useMemo(
    () => [
      ...new Set([
        ...filters.statuses,
        ...Object.keys(facets?.statuses ?? {}),
        ...items.flatMap((item) =>
          item.issue_status == null ? [] : [item.issue_status],
        ),
      ]),
    ],
    [items, facets, filters.statuses],
  );
  const statusOptions = useStatusOptions(wsId, inboxStatusKeys);
  const activeCount = inboxFilterCount(filters);

  // Counts are faceted: every count respects the other active dimensions while
  // ignoring its own, so each number says how many rows selecting that value
  // can actually reveal. The loaded pages cannot answer that, so the server
  // counts the whole view.
  const unreadCount = facets?.unreadCount ?? 0;
  const statuses = new Map(Object.entries(facets?.statuses ?? {}));
  const priorities = new Map(Object.entries(facets?.priorities ?? {}));
  const actors = new Map(Object.entries(facets?.actors ?? {}));
  // The universe of actors comes from the facets rather than the faceted
  // counts: picking one actor must not remove the others from the menu that
  // offers them. Sorted by name so the list does not reshuffle as counts
  // change under other selections.
  const actorOptions = useMemo(() => {
    const keys = new Set<string>([...Object.keys(facets?.actors ?? {}), ...filters.actors]);
    return [...keys]
      .map((key) => {
        const { type, id } = inboxActorKeyParts(key);
        return { key, type, id, name: getActorName(type, id) };
      })
      .sort((a, b) => a.name.localeCompare(b.name));
  }, [getActorName, facets, filters.actors]);
  const triggerLabel =
    activeCount > 0
      ? t(($) => $.filters.active_count, { count: activeCount })
      : t(($) => $.filters.tooltip);

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
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
        {facetsQuery.isLoading && <p role="status" className="px-2 py-1 text-caption text-muted-foreground">{t(($) => $.list.loading_more)}</p>}
        {facetsQuery.isError && <div className="px-2 py-1">
          <p role="alert" className="text-caption text-destructive">{t(($) => $.errors.filters_load_failed)}</p>
          <Button variant="ghost" size="sm" onClick={() => { void facetsQuery.refetch(); }}>{t(($) => $.list.retry)}</Button>
        </div>}
        <DropdownMenuCheckboxItem
          checked={filters.unreadOnly}
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
              {filters.actors.length > 0 && (
                <span className="text-caption font-medium text-primary">
                  {filters.actors.length}
                </span>
              )}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="w-auto min-w-48">
              {actorOptions.map((option) => {
                const checked = filters.actors.includes(option.key);
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
            {filters.statuses.length > 0 && (
              <span className="text-caption font-medium text-primary">
                {filters.statuses.length}
              </span>
            )}
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="w-auto min-w-48">
            {statusOptions.map((option) => {
              const checked = filters.statuses.includes(option.key);
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
                    icon={option.icon}
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

        <DropdownMenuSub>
          <DropdownMenuSubTrigger>
            <SignalHigh className="size-3.5" />
            <span className="flex-1">{t(($) => $.filters.priority)}</span>
            {filters.priorities.length > 0 ? (
              <span className="text-caption font-medium text-primary">
                {filters.priorities.length}
              </span>
            ) : null}
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="w-auto min-w-44">
            {PRIORITY_DISPLAY_ORDER.map((priority) => {
              const checked = filters.priorities.includes(priority);
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
