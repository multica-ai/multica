"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bell, CheckCheck } from "lucide-react";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { InboxItem } from "@multica/core/types";
import { ActorAvatar } from "@multica/views/common/actor-avatar";
import { AppLink } from "@multica/views/navigation";
import { useNavigation } from "@multica/views/navigation";
import { useTimeAgo } from "@multica/views/inbox/components/inbox-list-item";
import {
  notificationsListOptions,
  notificationsUnreadCountOptions,
} from "@multica/cerebro-notifications/core";
import {
  useMarkAllNotificationsRead,
  useMarkNotificationRead,
} from "@multica/cerebro-notifications/core";

const EMPTY_ITEMS: InboxItem[] = [];

const UNREAD_COUNT_POLL_MS = 30_000;
const DROPDOWN_ITEM_LIMIT = 10;

interface NotificationBellProps {
  className?: string;
}

export function NotificationBell({ className }: NotificationBellProps) {
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(false);

  // Polled unread-count drives the badge; light payload so 30s polling stays cheap.
  const { data: countData } = useQuery({
    ...notificationsUnreadCountOptions(wsId),
    enabled: !!wsId,
    refetchInterval: UNREAD_COUNT_POLL_MS,
    refetchIntervalInBackground: false,
  });
  const unreadCount = countData?.count ?? 0;
  const badgeLabel = unreadCount > 99 ? "99+" : String(unreadCount);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        data-testid="notification-bell"
        aria-label={
          unreadCount > 0
            ? `Notifications, ${unreadCount} unread`
            : "Notifications"
        }
        className={cn(
          "relative inline-flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
          className,
        )}
      >
        <Bell className="h-4 w-4" />
        {unreadCount > 0 && (
          <span
            data-testid="notification-bell-badge"
            className="pointer-events-none absolute -top-0.5 -right-0.5 flex h-4 min-w-[16px] items-center justify-center rounded-full bg-brand px-1 text-[10px] font-semibold leading-none text-brand-foreground ring-1 ring-background"
          >
            {badgeLabel}
          </span>
        )}
      </PopoverTrigger>
      <PopoverContent
        side="bottom"
        align="end"
        sideOffset={6}
        className="w-80 p-0"
      >
        <NotificationDropdown
          wsId={wsId}
          unreadCount={unreadCount}
          onClose={() => setOpen(false)}
        />
      </PopoverContent>
    </Popover>
  );
}

interface NotificationDropdownProps {
  wsId: string;
  unreadCount: number;
  onClose: () => void;
}

function NotificationDropdown({
  wsId,
  unreadCount,
  onClose,
}: NotificationDropdownProps) {
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const { data: items = EMPTY_ITEMS } = useQuery({
    ...notificationsListOptions(wsId),
    enabled: !!wsId,
  });
  const markRead = useMarkNotificationRead();
  const markAllRead = useMarkAllNotificationsRead();

  const visible = useMemo(
    () =>
      items
        .filter((item) => !item.archived)
        .slice(0, DROPDOWN_ITEM_LIMIT),
    [items],
  );

  const handleItemClick = (item: InboxItem) => {
    if (!item.read) markRead.mutate(item.id);
    if (item.issue_id) push(p.issueDetail(item.issue_id));
    onClose();
  };

  return (
    <div className="flex flex-col">
      <header className="flex items-center justify-between border-b px-3 py-2">
        <div className="flex items-center gap-2">
          <Bell className="h-3.5 w-3.5 text-muted-foreground" />
          <span className="text-sm font-medium">Notifications</span>
          {unreadCount > 0 && (
            <span className="rounded-full bg-muted px-1.5 text-[10px] font-semibold text-muted-foreground">
              {unreadCount > 99 ? "99+" : unreadCount}
            </span>
          )}
        </div>
        <Button
          size="sm"
          variant="ghost"
          data-testid="notification-bell-mark-all-read"
          disabled={unreadCount === 0 || markAllRead.isPending}
          onClick={() => markAllRead.mutate()}
          className="h-7 px-2 text-xs"
        >
          <CheckCheck className="h-3.5 w-3.5" />
          Mark all read
        </Button>
      </header>

      <div
        className="max-h-96 overflow-y-auto"
        data-testid="notification-bell-list"
      >
        {visible.length === 0 ? (
          <DropdownEmptyState />
        ) : (
          visible.map((item) => (
            <DropdownRow
              key={item.id}
              item={item}
              onClick={() => handleItemClick(item)}
            />
          ))
        )}
      </div>

      <footer className="border-t px-3 py-2">
        <AppLink
          href={p.notifications()}
          data-testid="notification-bell-see-all"
          onClick={onClose}
          className="block text-center text-xs font-medium text-muted-foreground hover:text-foreground"
        >
          See all
        </AppLink>
      </footer>
    </div>
  );
}

function DropdownEmptyState() {
  return (
    <div className="flex flex-col items-center justify-center gap-2 px-3 py-8 text-muted-foreground">
      <Bell className="h-6 w-6 opacity-30" />
      <div className="text-xs">You&apos;re all caught up.</div>
    </div>
  );
}

interface DropdownRowProps {
  item: InboxItem;
  onClick: () => void;
}

function DropdownRow({ item, onClick }: DropdownRowProps) {
  const timeAgo = useTimeAgo();
  // actor_type distinguishes agent vs human events visually via ActorAvatar
  // (CEREBRO-PATCH(actor-avatar-agent-pill) styles agents distinctly).
  const actorType: "member" | "agent" =
    item.actor_type === "agent" || item.actor_type === "system"
      ? "agent"
      : item.actor_type === "member"
        ? "member"
        : item.recipient_type;
  const actorId = item.actor_id ?? item.recipient_id;
  return (
    <button
      type="button"
      data-testid="notification-bell-row"
      data-notification-type={item.type}
      data-actor-type={actorType}
      data-read={item.read ? "true" : "false"}
      onClick={onClick}
      className="flex w-full items-start gap-2.5 border-b px-3 py-2 text-left transition-colors last:border-b-0 hover:bg-muted/60"
    >
      <ActorAvatar
        actorType={actorType}
        actorId={actorId}
        size={24}
        profileLink={false}
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          {!item.read && (
            <span className="size-1.5 shrink-0 rounded-full bg-brand" />
          )}
          <span
            className={cn(
              "truncate text-xs",
              item.read ? "text-muted-foreground" : "font-medium",
            )}
          >
            {item.title}
          </span>
        </div>
        {item.body && (
          <div className="mt-0.5 truncate text-[11px] text-muted-foreground">
            {item.body}
          </div>
        )}
      </div>
      <span className="shrink-0 text-[10px] text-muted-foreground">
        {timeAgo(item.created_at)}
      </span>
    </button>
  );
}
