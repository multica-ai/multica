"use client";

// CEREBRO-PATCH(inbox-list-item-cerebro): cerebro modification of upstream file

import { StatusIcon } from "../../issues/components";
import { ActorAvatar } from "../../common/actor-avatar";
import { Archive, Hash, MessagesSquare } from "lucide-react";
import { AvatarGroup } from "@multica/ui/components/ui/avatar";
import type { Channel, InboxItem } from "@multica/core/types";
import { useChannelDisplay } from "../../channels";
import { InboxDetailLabel } from "./inbox-detail-label";

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

export { timeAgo };

interface InboxListItemBaseProps {
  isSelected: boolean;
  isAgentActive?: boolean;
  unread: boolean;
  onClick: () => void;
  onArchive: () => void;
  children: React.ReactNode;
}

/**
 * Shared row chrome — handles the unread brand-blue stripe, hover-only
 * archive button, selection background, and click target. Variant-specific
 * content is rendered via children so the channel/DM/issue rows stay
 * single-purpose and easy to test.
 */
function InboxListItemShell({
  isSelected,
  unread,
  onClick,
  onArchive,
  children,
}: InboxListItemBaseProps) {
  return (
    <button
      onClick={onClick}
      className={`group relative flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors ${
        isSelected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      {unread && (
        <span
          aria-hidden
          className="absolute inset-y-0 left-0 w-0.5 bg-brand"
        />
      )}
      {children}
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
        className="absolute right-2 top-1/2 -translate-y-1/2 hidden h-7 w-7 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground sm:group-hover:inline-flex"
      >
        <Archive className="h-3.5 w-3.5" />
      </span>
    </button>
  );
}

export function InboxListItem({
  item,
  isSelected,
  isAgentActive,
  onClick,
  onArchive,
}: {
  item: InboxItem;
  isSelected: boolean;
  isAgentActive?: boolean;
  onClick: () => void;
  onArchive: () => void;
}) {
  const unread = !item.read;
  return (
    <InboxListItemShell
      isSelected={isSelected}
      unread={unread}
      onClick={onClick}
      onArchive={onArchive}
    >
      <ActorAvatar
        actorType={item.actor_type ?? item.recipient_type}
        actorId={item.actor_id ?? item.recipient_id}
        size={28}
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-1.5">
            {unread && (
              <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-brand" />
            )}
            <span
              className={`truncate text-sm ${unread ? "font-medium" : "text-muted-foreground"}`}
            >
              {item.title}
            </span>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            {item.issue_status && (
              <StatusIcon status={item.issue_status} className="h-3.5 w-3.5 shrink-0" />
            )}
          </div>
        </div>
        <div className="mt-0.5 flex items-center justify-between gap-2">
          <p
            className={`flex min-w-0 items-center gap-1.5 overflow-hidden text-ellipsis whitespace-nowrap text-xs ${unread ? "text-muted-foreground" : "text-muted-foreground/60"}`}
          >
            {isAgentActive && (
              <span
                title="Agent is working"
                className="relative inline-flex size-2 shrink-0 items-center justify-center"
              >
                <span className="absolute inline-flex size-2 animate-ping rounded-full bg-emerald-500/60" />
                <span className="relative inline-flex size-1.5 rounded-full bg-emerald-500" />
              </span>
            )}
            <span className="truncate">
              <InboxDetailLabel item={item} />
            </span>
          </p>
          <span
            className={`shrink-0 text-xs ${unread ? "text-muted-foreground" : "text-muted-foreground/60"}`}
          >
            {timeAgo(item.created_at)}
          </span>
        </div>
      </div>
    </InboxListItemShell>
  );
}

export function ChannelListItem({
  channel,
  preview,
  isSelected,
  onClick,
  onArchive,
}: {
  channel: Channel;
  /** Last message snippet shown under the title. */
  preview?: { author: string; text: string } | null;
  isSelected: boolean;
  onClick: () => void;
  onArchive: () => void;
}) {
  const display = useChannelDisplay(channel);
  const unread = channel.unread_count > 0;
  // The mockup's "Sara: snippet" shape — compact preview with author name.
  const previewText = preview
    ? `${preview.author}: ${preview.text}`
    : channel.description ?? "";

  return (
    <InboxListItemShell
      isSelected={isSelected}
      unread={unread}
      onClick={onClick}
      onArchive={onArchive}
    >
      <ChannelAvatarStack channel={channel} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-1.5">
            {unread && (
              <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-brand" />
            )}
            {display.isChannel && (
              <Hash className="size-3.5 shrink-0 text-muted-foreground" />
            )}
            <span
              className={`truncate text-sm ${unread ? "font-medium" : "text-muted-foreground"}`}
            >
              {display.title}
            </span>
            {channel.unread_count > 1 && (
              <span className="ml-1 inline-flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full bg-brand/15 px-1 text-[10px] font-medium text-brand">
                {channel.unread_count}
              </span>
            )}
          </div>
        </div>
        <div className="mt-0.5 flex items-center justify-between gap-2">
          <p
            className={`min-w-0 truncate text-xs ${unread ? "text-muted-foreground" : "text-muted-foreground/60"}`}
          >
            {previewText || (display.isChannel ? "No messages yet" : "")}
          </p>
          <span
            className={`shrink-0 text-xs ${unread ? "text-muted-foreground" : "text-muted-foreground/60"}`}
          >
            {timeAgo(channel.updated_at)}
          </span>
        </div>
      </div>
    </InboxListItemShell>
  );
}

function ChannelAvatarStack({ channel }: { channel: Channel }) {
  const display = useChannelDisplay(channel);

  // Channels with names get a single icon avatar instead of the participant
  // stack — same cue Slack uses, and keeps the row from getting cluttered
  // when a channel has many members.
  if (display.isChannel) {
    return (
      <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
        <MessagesSquare className="size-3.5" />
      </span>
    );
  }

  const visible = display.otherParticipants.slice(0, 2);
  if (visible.length === 0) {
    return (
      <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
        <MessagesSquare className="size-3.5" />
      </span>
    );
  }

  if (visible.length === 1 && visible[0]) {
    return (
      <ActorAvatar
        actorType={visible[0].user_type}
        actorId={visible[0].user_id}
        size={28}
      />
    );
  }

  return (
    <AvatarGroup className="shrink-0">
      {visible.map((p) => (
        <ActorAvatar
          key={`${p.user_type}:${p.user_id}`}
          actorType={p.user_type}
          actorId={p.user_id}
          size={20}
        />
      ))}
    </AvatarGroup>
  );
}
