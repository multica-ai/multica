"use client";

import { useState, useEffect, useCallback } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import { useInboxStore } from "@/features/inbox";
import { useInboxMutations } from "@/features/inbox/mutations";
import { IssueDetail, StatusIcon, PriorityIcon } from "@/features/issues/components";
import { STATUS_CONFIG, PRIORITY_CONFIG } from "@/features/issues/config";
import { useActorName } from "@/features/workspace";
import { ActorAvatar } from "@/components/common/actor-avatar";
import { toast } from "sonner";
import {
  ChevronLeft,
  MoreHorizontal,
  Inbox,
  CheckCheck,
  Archive,
  BookCheck,
  ListChecks,
} from "lucide-react";
import type { InboxItem, InboxItemType, IssueStatus, IssuePriority } from "@/shared/types";
import { Button } from "@/components/ui/button";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@/components/ui/resizable";
import { Skeleton } from "@/components/ui/skeleton";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import { usePathname, useRouter, useSearchParams } from "@/shared/router";
import { useIsMobile } from "@/hooks/use-mobile";
import { MobileDetailHeader } from "@/features/layout/components/mobile-detail-header";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const typeLabels: Record<InboxItemType, string> = {
  issue_assigned: "Assigned",
  unassigned: "Unassigned",
  assignee_changed: "Assignee changed",
  status_changed: "Status changed",
  priority_changed: "Priority changed",
  due_date_changed: "Due date changed",
  start_date_changed: "Start date changed",
  end_date_changed: "End date changed",
  new_comment: "New comment",
  mentioned: "Mentioned",
  review_requested: "Review requested",
  task_completed: "Task completed",
  task_failed: "Task failed",
  agent_blocked: "Agent blocked",
  agent_completed: "Agent completed",
  reaction_added: "Reacted",
};

function timeAgo(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  return `${days}d`;
}

function shortDate(dateStr: string): string {
  if (!dateStr) return "";
  return new Date(dateStr).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  });
}

function shortDateTime(dateStr: string): string {
  if (!dateStr) return "";
  return new Date(dateStr).toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

// ---------------------------------------------------------------------------
// InboxDetailLabel — renders rich subtitle per notification type
// ---------------------------------------------------------------------------

function InboxDetailLabel({ item }: { item: InboxItem }) {
  const { getActorName } = useActorName();
  const details = item.details ?? {};

  switch (item.type) {
    case "status_changed": {
      if (!details.to) return <span>{typeLabels[item.type]}</span>;
      const label = STATUS_CONFIG[details.to as IssueStatus]?.label ?? details.to;
      return (
        <span className="inline-flex items-center gap-1">
          Set status to
          <StatusIcon status={details.to as IssueStatus} className="h-3 w-3" />
          {label}
        </span>
      );
    }
    case "priority_changed": {
      if (!details.to) return <span>{typeLabels[item.type]}</span>;
      const label = PRIORITY_CONFIG[details.to as IssuePriority]?.label ?? details.to;
      return (
        <span className="inline-flex items-center gap-1">
          Set priority to
          <PriorityIcon priority={details.to as IssuePriority} className="h-3 w-3" />
          {label}
        </span>
      );
    }
    case "issue_assigned": {
      if (details.new_assignee_id) {
        return <span>Assigned to {getActorName(details.new_assignee_type ?? "member", details.new_assignee_id)}</span>;
      }
      return <span>{typeLabels[item.type]}</span>;
    }
    case "unassigned":
      return <span>Removed assignee</span>;
    case "assignee_changed": {
      if (details.new_assignee_id) {
        return <span>Assigned to {getActorName(details.new_assignee_type ?? "member", details.new_assignee_id)}</span>;
      }
      return <span>{typeLabels[item.type]}</span>;
    }
    case "due_date_changed": {
      if (details.to) return <span>Set due date to {shortDate(details.to)}</span>;
      return <span>Removed due date</span>;
    }
    case "start_date_changed": {
      if (details.to) return <span>Set start date to {shortDateTime(details.to)}</span>;
      return <span>Removed start date</span>;
    }
    case "end_date_changed": {
      if (details.to) return <span>Set end date to {shortDateTime(details.to)}</span>;
      return <span>Removed end date</span>;
    }
    case "new_comment": {
      if (item.body) return <span>{item.body}</span>;
      return <span>{typeLabels[item.type]}</span>;
    }
    case "reaction_added": {
      const emoji = details.emoji;
      if (emoji) return <span>Reacted {emoji} to your comment</span>;
      return <span>{typeLabels[item.type]}</span>;
    }
    default:
      return <span>{typeLabels[item.type] ?? item.type}</span>;
  }
}

// ---------------------------------------------------------------------------
// InboxListItem
// ---------------------------------------------------------------------------

function InboxListItem({
  item,
  isSelected,
  onClick,
  onArchive,
}: {
  item: InboxItem;
  isSelected: boolean;
  onClick: () => void;
  onArchive: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className={`group flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors ${
        isSelected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      <ActorAvatar
        actorType={item.actor_type ?? item.recipient_type}
        actorId={item.actor_id ?? item.recipient_id}
        size={28}
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-1.5">
            {!item.read && (
              <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-brand" />
            )}
            <span
              className={`truncate text-sm ${!item.read ? "font-medium" : "text-muted-foreground"}`}
            >
              {item.title}
            </span>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <span
              role="button"
              tabIndex={-1}
              title="Archive"
              onClick={(e) => {
                e.stopPropagation();
                onArchive();
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.stopPropagation();
                  onArchive();
                }
              }}
              className="hidden rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground group-hover:inline-flex"
            >
              <Archive className="h-3.5 w-3.5" />
            </span>
            {item.issue_status && (
              <StatusIcon status={item.issue_status} className="h-3.5 w-3.5 shrink-0" />
            )}
          </div>
        </div>
        <div className="mt-0.5 flex items-center justify-between gap-2">
          <p className={`min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-xs ${item.read ? "text-muted-foreground/60" : "text-muted-foreground"}`}>
            <InboxDetailLabel item={item} />
          </p>
          <span className={`shrink-0 text-xs ${item.read ? "text-muted-foreground/60" : "text-muted-foreground"}`}>
            {timeAgo(item.created_at)}
          </span>
        </div>
      </div>
    </button>
  );
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export default function InboxPage() {
  const isMobile = useIsMobile();
  const pathname = usePathname();
  const router = useRouter();
  const searchParams = useSearchParams();
  const urlIssue = searchParams.get("issue") ?? "";

  const [selectedKey, setSelectedKeyState] = useState(() => urlIssue);
  const notificationsBasePath =
    pathname === "/"
      ? "/"
      : pathname === "/notifications"
        ? "/notifications"
        : "/inbox";

  // Sync from URL when searchParams change (e.g. Next.js navigation)
  useEffect(() => {
    setSelectedKeyState(urlIssue);
  }, [urlIssue]);

  const setSelectedKey = useCallback((key: string) => {
    setSelectedKeyState(key);
    const url = key ? `${notificationsBasePath}?issue=${key}` : notificationsBasePath;
    router.replace(url);
  }, [notificationsBasePath, router]);

  const items = useInboxStore((s) => s.dedupedItems());
  const loading = useInboxStore((s) => s.loading);
  const {
    markRead,
    archive,
    markAllRead,
    archiveAll,
    archiveAllRead,
    archiveCompleted,
  } = useInboxMutations();

  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_inbox_layout",
  });

  const selected = items.find((i) => (i.issue_id ?? i.id) === selectedKey) ?? null;
  const unreadCount = items.filter((i) => !i.read).length;

  // Click-to-read: select + auto-mark-read
  const handleSelect = async (item: InboxItem) => {
    setSelectedKey(item.issue_id ?? item.id);
    if (!item.read) {
      try {
        await markRead(item.id);
      } catch {
        toast.error("Failed to mark as read");
      }
    }
  };

  const handleArchive = async (id: string) => {
    try {
      await archive(id);
      const archived = items.find((i) => i.id === id);
      if (archived && (archived.issue_id ?? archived.id) === selectedKey) setSelectedKey("");
    } catch {
      toast.error("Failed to archive");
    }
  };

  // Batch operations
  const handleMarkAllRead = async () => {
    try {
      await markAllRead();
    } catch {
      toast.error("Failed to mark all as read");
    }
  };

  const handleArchiveAll = async () => {
    try {
      await archiveAll();
      setSelectedKey("");
    } catch {
      toast.error("Failed to archive all");
    }
  };

  const handleArchiveAllRead = async () => {
    try {
      const readKeys = items.filter((i) => i.read).map((i) => i.issue_id ?? i.id);
      await archiveAllRead();
      if (readKeys.includes(selectedKey)) setSelectedKey("");
    } catch {
      toast.error("Failed to archive read items");
    }
  };

  const handleArchiveCompleted = async () => {
    try {
      await archiveCompleted();
      setSelectedKey("");
    } catch {
      toast.error("Failed to archive completed");
    }
  };

  if (loading) {
    return (
      <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0" defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
        <ResizablePanel id="list" defaultSize={320} minSize={240} maxSize={480} groupResizeBehavior="preserve-pixel-size">
          <div className="flex flex-col border-r h-full">
            <div className="flex h-12 shrink-0 items-center border-b px-4">
              <Skeleton className="h-5 w-16" />
            </div>
            <div className="flex-1 min-h-0 overflow-y-auto space-y-1 p-2">
              {Array.from({ length: 5 }).map((_, i) => (
                <div key={i} className="flex items-center gap-3 px-4 py-2.5">
                  <Skeleton className="h-7 w-7 shrink-0 rounded-full" />
                  <div className="flex-1 space-y-2">
                    <Skeleton className="h-4 w-3/4" />
                    <Skeleton className="h-3 w-1/2" />
                  </div>
                </div>
              ))}
            </div>
          </div>
        </ResizablePanel>
        <ResizableHandle />
        <ResizablePanel id="detail" minSize="40%">
          <div className="p-6">
            <Skeleton className="h-6 w-48" />
            <Skeleton className="mt-4 h-4 w-32" />
          </div>
        </ResizablePanel>
      </ResizablePanelGroup>
    );
  }

  if (isMobile) {
    if (selected) {
      return (
        <div className="flex flex-1 min-h-0 flex-col">
          <MobileDetailHeader
            title="Notifications"
            subtitle={selected.title}
            onBack={() => setSelectedKey("")}
          />
          <div className="min-h-0 flex-1 overflow-y-auto">
            {selected.issue_id ? (
              <IssueDetail
                key={selected.id}
                issueId={selected.issue_id}
                defaultSidebarOpen={false}
                layoutId="multica_inbox_issue_detail_layout"
                highlightCommentId={selected.details?.comment_id ?? undefined}
                onDelete={() => {
                  handleArchive(selected.id);
                }}
              />
            ) : (
              <div className="p-4">
                <h2 className="text-lg font-semibold">{selected.title}</h2>
                <p className="mt-1 text-sm text-muted-foreground">
                  {typeLabels[selected.type]} · {timeAgo(selected.created_at)}
                </p>
                {selected.body && (
                  <div className="mt-4 whitespace-pre-wrap text-sm leading-relaxed text-foreground/80">
                    {selected.body}
                  </div>
                )}
                <div className="mt-4">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => handleArchive(selected.id)}
                  >
                    <Archive className="mr-1.5 h-3.5 w-3.5" />
                    Archive
                  </Button>
                </div>
              </div>
            )}
          </div>
        </div>
      );
    }

    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center justify-between border-b px-4">
          <div className="flex items-center gap-2">
            <h1 className="text-sm font-semibold">Notifications</h1>
            {unreadCount > 0 && (
              <span className="text-xs text-muted-foreground">
                {unreadCount}
              </span>
            )}
          </div>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-xs"
                  className="text-muted-foreground"
                />
              }
            >
              <MoreHorizontal className="h-4 w-4" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-auto">
              <DropdownMenuItem onClick={handleMarkAllRead}>
                <CheckCheck className="h-4 w-4" />
                Mark all as read
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={handleArchiveAll}>
                <Archive className="h-4 w-4" />
                Archive all
              </DropdownMenuItem>
              <DropdownMenuItem onClick={handleArchiveAllRead}>
                <BookCheck className="h-4 w-4" />
                Archive all read
              </DropdownMenuItem>
              <DropdownMenuItem onClick={handleArchiveCompleted}>
                <ListChecks className="h-4 w-4" />
                Archive completed
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>

        <div className="flex-1 min-h-0 overflow-y-auto">
          {items.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
              <Inbox className="mb-3 h-8 w-8 text-muted-foreground/50" />
              <p className="text-sm">No notifications</p>
            </div>
          ) : (
            <div>
              {items.map((item) => (
                <InboxListItem
                  key={item.id}
                  item={item}
                  isSelected={false}
                  onClick={() => handleSelect(item)}
                  onArchive={() => handleArchive(item.id)}
                />
              ))}
            </div>
          )}
        </div>
      </div>
    );
  }

  return (
    <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0" defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
      <ResizablePanel id="list" defaultSize={320} minSize={240} maxSize={480} groupResizeBehavior="preserve-pixel-size">
      {/* Left column — inbox list */}
      <div className="flex flex-col border-r h-full">
        <div className="flex h-12 shrink-0 items-center justify-between border-b px-4">
          <div className="flex items-center gap-2">
            <h1 className="text-sm font-semibold">Notifications</h1>
            {unreadCount > 0 && (
              <span className="text-xs text-muted-foreground">
                {unreadCount}
              </span>
            )}
          </div>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-xs"
                  className="text-muted-foreground"
                />
              }
            >
              <MoreHorizontal className="h-4 w-4" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-auto">
              <DropdownMenuItem onClick={handleMarkAllRead}>
                <CheckCheck className="h-4 w-4" />
                Mark all as read
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={handleArchiveAll}>
                <Archive className="h-4 w-4" />
                Archive all
              </DropdownMenuItem>
              <DropdownMenuItem onClick={handleArchiveAllRead}>
                <BookCheck className="h-4 w-4" />
                Archive all read
              </DropdownMenuItem>
              <DropdownMenuItem onClick={handleArchiveCompleted}>
                <ListChecks className="h-4 w-4" />
                Archive completed
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>

        <div className="flex-1 min-h-0 overflow-y-auto">
        {items.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
            <Inbox className="mb-3 h-8 w-8 text-muted-foreground/50" />
            <p className="text-sm">No notifications</p>
          </div>
        ) : (
          <div>
            {items.map((item) => (
              <InboxListItem
                key={item.id}
                item={item}
                isSelected={(item.issue_id ?? item.id) === selectedKey}
                onClick={() => handleSelect(item)}
                onArchive={() => handleArchive(item.id)}
              />
            ))}
          </div>
        )}
        </div>
      </div>
      </ResizablePanel>
      <ResizableHandle />
      <ResizablePanel id="detail" minSize="40%">
      {/* Right column — detail */}
      <div className="flex flex-col min-h-0 h-full">
        {selected?.issue_id ? (
          <IssueDetail
            key={selected.id}
            issueId={selected.issue_id}
            defaultSidebarOpen={false}
            layoutId="multica_inbox_issue_detail_layout"
            highlightCommentId={selected.details?.comment_id ?? undefined}
            onDelete={() => {
              handleArchive(selected.id);
            }}
          />
        ) : selected ? (
          <div className="p-6">
            <h2 className="text-lg font-semibold">{selected.title}</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              {typeLabels[selected.type]} · {timeAgo(selected.created_at)}
            </p>
            {selected.body && (
              <div className="mt-4 whitespace-pre-wrap text-sm leading-relaxed text-foreground/80">
                {selected.body}
              </div>
            )}
            <div className="mt-4">
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleArchive(selected.id)}
              >
                <Archive className="mr-1.5 h-3.5 w-3.5" />
                Archive
              </Button>
            </div>
          </div>
        ) : (
          <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
            <Inbox className="mb-3 h-10 w-10 text-muted-foreground/30" />
            <p className="text-sm">
              {items.length === 0
                ? "Your notifications are empty"
                : "Select a notification to view details"}
            </p>
          </div>
        )}
      </div>
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}
