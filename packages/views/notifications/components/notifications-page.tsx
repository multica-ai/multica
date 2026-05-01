"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bell, CheckCheck, Trash2 } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { notificationsListOptions } from "@multica/core/notifications";
import {
  useArchiveAllNotifications,
  useArchiveNotification,
  useMarkAllNotificationsRead,
  useMarkNotificationRead,
} from "@multica/core/notifications";
import type { InboxItem, InboxItemType } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useNavigation } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { StatusIcon } from "../../issues/components";
import { timeAgo } from "../../inbox/components/inbox-list-item";
import { InboxDetailLabel } from "../../inbox/components/inbox-detail-label";

const EMPTY: InboxItem[] = [];

type FilterId =
  | "all"
  | "unread"
  | "mentions"
  | "comments"
  | "status_priority"
  | "reactions";

const FILTERS: { id: FilterId; label: string }[] = [
  { id: "all", label: "All" },
  { id: "unread", label: "Unread" },
  { id: "mentions", label: "Mentions" },
  { id: "comments", label: "Comments" },
  { id: "status_priority", label: "Status & priority" },
  { id: "reactions", label: "Reactions" },
];

const STATUS_PRIORITY_TYPES = new Set<InboxItemType>([
  "status_changed",
  "priority_changed",
  "due_date_changed",
  "assignee_changed",
  "unassigned",
]);

function matchesFilter(item: InboxItem, filter: FilterId): boolean {
  switch (filter) {
    case "all":
      return true;
    case "unread":
      return !item.read;
    case "mentions":
      return item.type === "mentioned";
    case "comments":
      return item.type === "new_comment";
    case "status_priority":
      return STATUS_PRIORITY_TYPES.has(item.type);
    case "reactions":
      return item.type === "reaction_added";
  }
}

export function NotificationsPage() {
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { push } = useNavigation();

  const [filter, setFilter] = useState<FilterId>("all");

  const { data: items = EMPTY } = useQuery(notificationsListOptions(wsId));
  const markRead = useMarkNotificationRead();
  const archive = useArchiveNotification();
  const markAllRead = useMarkAllNotificationsRead();
  const archiveAll = useArchiveAllNotifications();

  const visible = useMemo(
    () => items.filter((i) => !i.archived),
    [items],
  );
  const unreadCount = useMemo(
    () => visible.filter((i) => !i.read).length,
    [visible],
  );

  const filtered = useMemo(
    () => visible.filter((i) => matchesFilter(i, filter)),
    [visible, filter],
  );

  const groups = useMemo(() => groupByDay(filtered), [filtered]);

  const handleRowClick = (item: InboxItem) => {
    if (!item.read) markRead.mutate(item.id);
    if (item.issue_id) {
      push(p.issueDetail(item.issue_id));
    }
  };

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <header className="flex items-center justify-between border-b px-6 py-4">
        <div className="flex items-center gap-2">
          <Bell className="h-5 w-5 text-muted-foreground" />
          <h1 className="text-base font-semibold">Notifications</h1>
          {visible.length > 0 && (
            <span
              data-testid="notifications-count"
              className="rounded-full bg-muted px-2 text-xs font-semibold text-muted-foreground"
            >
              {visible.length}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            variant="ghost"
            disabled={unreadCount === 0 || markAllRead.isPending}
            onClick={() => markAllRead.mutate()}
          >
            <CheckCheck className="h-3.5 w-3.5" />
            Mark all read
          </Button>
          <Button
            size="sm"
            variant="outline"
            data-testid="notifications-clear-all"
            disabled={visible.length === 0 || archiveAll.isPending}
            onClick={() => archiveAll.mutate()}
          >
            <Trash2 className="h-3.5 w-3.5" />
            Clear
          </Button>
        </div>
      </header>

      {visible.length > 0 && (
        <nav
          aria-label="Notification filters"
          className="flex items-center gap-1 border-b bg-muted/30 px-4 py-2"
        >
          {FILTERS.map((f) => {
            const count = visible.filter((i) => matchesFilter(i, f.id)).length;
            const active = filter === f.id;
            return (
              <button
                key={f.id}
                type="button"
                aria-pressed={active}
                data-testid={`notifications-filter-${f.id}`}
                onClick={() => setFilter(f.id)}
                className={cn(
                  "flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium transition-colors",
                  active
                    ? "bg-accent text-foreground"
                    : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
                )}
              >
                {f.label}
                {count > 0 && (
                  <span
                    className={cn(
                      "rounded px-1 text-[10px]",
                      active
                        ? "bg-background text-muted-foreground"
                        : "bg-muted text-muted-foreground",
                    )}
                  >
                    {count}
                  </span>
                )}
              </button>
            );
          })}
        </nav>
      )}

      <div className="flex-1 min-h-0 overflow-y-auto">
        {visible.length === 0 ? (
          <EmptyState />
        ) : filtered.length === 0 ? (
          <FilteredEmptyState onClearFilter={() => setFilter("all")} />
        ) : (
          groups.map(({ label, items: groupItems }) => (
            <div key={label}>
              <div className="px-6 py-2 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
                {label}
              </div>
              {groupItems.map((item) => (
                <NotificationRow
                  key={item.id}
                  item={item}
                  onClick={() => handleRowClick(item)}
                  onMarkRead={() => markRead.mutate(item.id)}
                  onArchive={() => archive.mutate(item.id)}
                />
              ))}
            </div>
          ))
        )}
      </div>
    </div>
  );
}

interface RowProps {
  item: InboxItem;
  onClick: () => void;
  onMarkRead: () => void;
  onArchive: () => void;
}

function NotificationRow({ item, onClick, onMarkRead, onArchive }: RowProps) {
  const isCritical = item.severity === "action_required";
  return (
    <div
      role="button"
      tabIndex={0}
      data-testid="notification-row"
      data-notification-type={item.type}
      data-read={item.read ? "true" : "false"}
      onClick={onClick}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onClick();
        }
      }}
      className="group relative flex cursor-pointer items-start gap-3 border-b px-6 py-3 transition-colors hover:bg-muted/60"
    >
      {isCritical && (
        <span className="absolute left-0 top-3 bottom-3 w-[3px] rounded-r bg-brand" />
      )}
      <ActorAvatar
        actorType={item.actor_type ?? item.recipient_type}
        actorId={item.actor_id ?? item.recipient_id}
        size={28}
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          {!item.read && (
            <span className="size-1.5 shrink-0 rounded-full bg-brand" />
          )}
          <span
            className={`truncate text-sm ${item.read ? "text-muted-foreground" : "font-medium"}`}
          >
            {item.title}
          </span>
        </div>
        <div className="mt-0.5 truncate text-xs text-muted-foreground">
          <InboxDetailLabel item={item} />
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-2 text-xs text-muted-foreground">
        <span>{timeAgo(item.created_at)}</span>
        {item.issue_status && (
          <StatusIcon status={item.issue_status} className="h-3.5 w-3.5" />
        )}
        <div className="flex gap-1 opacity-0 transition-opacity group-hover:opacity-100">
          {!item.read && (
            <button
              type="button"
              title="Mark read"
              className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
              onClick={(e) => {
                e.stopPropagation();
                onMarkRead();
              }}
            >
              <CheckCheck className="h-3.5 w-3.5" />
            </button>
          )}
          <button
            type="button"
            title="Dismiss"
            className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
            onClick={(e) => {
              e.stopPropagation();
              onArchive();
            }}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>
    </div>
  );
}

function EmptyState() {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 p-12 text-muted-foreground">
      <Bell className="h-10 w-10 opacity-30" />
      <div className="text-sm">You&apos;re all caught up.</div>
      <div className="text-xs">
        Notifications you&apos;ve routed here will show up in this list.
      </div>
    </div>
  );
}

function FilteredEmptyState({ onClearFilter }: { onClearFilter: () => void }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-2 p-12 text-muted-foreground">
      <div className="text-sm">No notifications match this filter.</div>
      <button
        type="button"
        className="text-xs underline hover:text-foreground"
        onClick={onClearFilter}
      >
        Show all
      </button>
    </div>
  );
}

function groupByDay(items: InboxItem[]): { label: string; items: InboxItem[] }[] {
  if (items.length === 0) return [];
  const today = startOfDay(new Date());
  const yesterday = startOfDay(addDays(today, -1));

  const buckets = new Map<string, InboxItem[]>();
  for (const item of items) {
    const day = startOfDay(new Date(item.created_at));
    let label: string;
    if (day.getTime() === today.getTime()) label = "Today";
    else if (day.getTime() === yesterday.getTime()) label = "Yesterday";
    else
      label = day.toLocaleDateString(undefined, {
        weekday: "short",
        month: "short",
        day: "numeric",
      });
    const list = buckets.get(label) ?? [];
    list.push(item);
    buckets.set(label, list);
  }
  return Array.from(buckets.entries()).map(([label, items]) => ({ label, items }));
}

function startOfDay(d: Date): Date {
  const out = new Date(d);
  out.setHours(0, 0, 0, 0);
  return out;
}

function addDays(d: Date, n: number): Date {
  const out = new Date(d);
  out.setDate(out.getDate() + n);
  return out;
}
