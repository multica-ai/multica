"use client";

// CEREBRO-PATCH(inbox-list-item-cerebro): cerebro modification of upstream file

import { StatusIcon } from "../../issues/components";
import { ActorAvatar } from "../../common/actor-avatar";
import { Hash, MessagesSquare } from "lucide-react";
import { CerebroInboxRowActions, CerebroSwipeArchive, CerebroUnarchiveAction, CerebroInboxTimestamp } from "@multica/cerebro-inbox"; // CEREBRO-PATCH(inbox-row-actions-mount) / CEREBRO-PATCH(inbox-unarchive-mount) / CEREBRO-PATCH(inbox-muted-timestamp)
import { AvatarGroup } from "@multica/ui/components/ui/avatar";
import { useActorName } from "@multica/core/workspace/hooks";
import type { Channel, ChannelMember, InboxItem } from "@multica/core/types";
import { useChannelDisplay } from "../../channels";
import { InboxDetailLabel } from "./inbox-detail-label";
import { getInboxDisplayTitle } from "./inbox-display";
import { useT } from "../../i18n";
import { AgentRunPip, type AgentRunState } from "../../common/agent-run-pip"; // CEREBRO-PATCH(inbox-run-state-pip): active vs queued indicator (JEH-1332)

// Hook returning a localized relative-time formatter — the i18n equivalent
// of the previous static `timeAgo` function. Returning a function (rather
// than a string) keeps call-site usage identical: `timeAgo(dateStr)`.
export function useTimeAgo() {
  const { t } = useT("inbox");
  return (dateStr: string): string => {
    const diff = Date.now() - new Date(dateStr).getTime();
    const minutes = Math.floor(diff / 60000);
    if (minutes < 1) return t(($) => $.list.time.just_now);
    if (minutes < 60) return t(($) => $.list.time.minutes, { count: minutes });
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return t(($) => $.list.time.hours, { count: hours });
    const days = Math.floor(hours / 24);
    return t(($) => $.list.time.days, { count: days });
  };
}

interface InboxListItemBaseProps {
  isSelected: boolean;
  agentRunState?: AgentRunState;
  unread: boolean;
  onClick: () => void;
  onArchive: () => void;
  children: React.ReactNode;
  cerebroItem?: InboxItem; // CEREBRO-PATCH(inbox-row-actions-mount)
  onUnarchive?: () => void; // CEREBRO-PATCH(inbox-unarchive-mount): JEH-1166
}

/**
 * CEREBRO-PATCH(inbox-list-item-cerebro): Shared row chrome — handles the unread brand-blue stripe, hover-only
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
  cerebroItem, // CEREBRO-PATCH(inbox-row-actions-mount)
  onUnarchive, // CEREBRO-PATCH(inbox-unarchive-mount): JEH-1166
}: InboxListItemBaseProps) {
  return (
    // CEREBRO-PATCH(inbox-row-action-click-target): avoid nested native buttons so row actions receive clicks.
    <div
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onClick();
        }
      }}
      className={`group relative flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors ${
        isSelected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      {unread && (
        <span
          aria-hidden
          className="absolute inset-y-0 left-0 w-1 bg-brand"
        />
      )}
      {children}
      {/* CEREBRO-PATCH(inbox-unarchive-mount): JEH-1166 — archived view swaps
          the archive action for an unarchive action (icon + swipe). */}
      {onUnarchive ? (
        <CerebroUnarchiveAction onUnarchive={onUnarchive} />
      ) : cerebroItem ? (
        /* CEREBRO-PATCH(inbox-row-actions-mount): full row-actions surface
           (mute / mark-unread, hover menu, mobile swipe + long-press) for
           issue inbox rows; swipe-archive-only variant for channel/DM rows
           (no per-row mute/unread state, but the archive gesture applies). */
        <CerebroInboxRowActions item={cerebroItem} onArchive={onArchive} />
      ) : (
        <CerebroSwipeArchive onArchive={onArchive} />
      )}
    </div>
  );
}

export function InboxListItem({
  item,
  isSelected,
  agentRunState,
  onClick,
  onArchive,
  onUnarchive, // CEREBRO-PATCH(inbox-unarchive-mount): JEH-1166
}: {
  item: InboxItem;
  isSelected: boolean;
  agentRunState?: AgentRunState;
  onClick: () => void;
  onArchive: () => void;
  onUnarchive?: () => void; // CEREBRO-PATCH(inbox-unarchive-mount): JEH-1166
}) {
  const unread = !item.read;
  const mentioned = item.type === "mentioned" && unread;
  const timeAgo = useTimeAgo();
  const displayTitle = getInboxDisplayTitle(item);

  return (
    <InboxListItemShell
      isSelected={isSelected}
      unread={unread}
      onClick={onClick}
      onArchive={onArchive}
      cerebroItem={item} // CEREBRO-PATCH(inbox-row-actions-mount)
      onUnarchive={onUnarchive} // CEREBRO-PATCH(inbox-unarchive-mount): JEH-1166
    >
      <ActorAvatar
        actorType={item.actor_type ?? item.recipient_type}
        actorId={item.actor_id ?? item.recipient_id}
        size={28}
        enableHoverCard
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-2">
          <span
            className={`min-w-0 flex-1 truncate text-sm ${unread ? "font-semibold" : "text-muted-foreground"}`}
          >
            {displayTitle}
          </span>
          {mentioned && <MentionBadge />}
          {item.issue_status && (
            <StatusIcon status={item.issue_status} className="h-3.5 w-3.5 shrink-0" />
          )}
          <span
            className={`shrink-0 text-xs ${unread ? "text-muted-foreground" : "text-muted-foreground/60"}`}
          >
            {/* CEREBRO-PATCH(inbox-muted-timestamp): JEH-663 — show "Muted til HH:MM" instead of relative time when row is only visible because the Muted filter is active. */}
            <CerebroInboxTimestamp item={item} fallback={timeAgo(item.created_at)} />
          </span>
        </div>
        <p
          className={`mt-0.5 flex items-start gap-1.5 text-xs leading-snug line-clamp-2 ${unread ? "text-foreground" : "text-muted-foreground/70"}`}
        >
          {agentRunState && <AgentRunPip state={agentRunState} className="mt-1" />}
          <span className="line-clamp-2">
            <InboxDetailLabel item={item} />
          </span>
        </p>
      </div>
    </InboxListItemShell>
  );
}

export function ChannelListItem({
  channel,
  preview,
  mentioned = false,
  isSelected,
  onClick,
  onArchive,
  onUnarchive, // CEREBRO-PATCH(inbox-unarchive-mount): JEH-1166
}: {
  channel: Channel;
  /** Last message snippet shown under the title. */
  preview?: { author: string; text: string } | null;
  /** True when at least one unread inbox row for this channel is a mention. */
  mentioned?: boolean;
  isSelected: boolean;
  onClick: () => void;
  onArchive: () => void;
  onUnarchive?: () => void; // CEREBRO-PATCH(inbox-unarchive-mount): JEH-1166
}) {
  const display = useChannelDisplay(channel);
  const unread = channel.unread_count > 0;
  const timeAgo = useTimeAgo();
  const showParticipants =
    display.isChannel && display.otherParticipants.length > 0;

  return (
    <InboxListItemShell
      isSelected={isSelected}
      unread={unread}
      onClick={onClick}
      onArchive={onArchive}
      onUnarchive={onUnarchive} // CEREBRO-PATCH(inbox-unarchive-mount): JEH-1166
    >
      <ChannelAvatarStack channel={channel} />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-2">
          <div className="flex min-w-0 items-center gap-1">
            {display.isChannel && (
              <Hash className="size-3.5 shrink-0 text-muted-foreground" />
            )}
            <span
              className={`truncate text-sm ${unread ? "font-semibold" : "text-muted-foreground"}`}
            >
              {display.title}
            </span>
            {channel.unread_count > 1 && (
              <span className="ml-1 inline-flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full bg-brand/15 px-1 text-[10px] font-medium text-brand">
                {channel.unread_count}
              </span>
            )}
          </div>
          {mentioned && <MentionBadge />}
          <span
            className={`shrink-0 text-xs ${unread ? "text-muted-foreground" : "text-muted-foreground/60"}`}
          >
            {timeAgo(channel.updated_at)}
          </span>
        </div>
        {showParticipants && (
          <ParticipantsLine participants={display.otherParticipants} />
        )}
        <ChannelSnippet
          preview={preview ?? null}
          fallback={channel.description ?? ""}
          isChannel={display.isChannel}
          unread={unread}
        />
      </div>
    </InboxListItemShell>
  );
}

function ParticipantsLine({ participants }: { participants: ChannelMember[] }) {
  const { getActorName } = useActorName();
  // Cap inline names so a 12-member channel doesn't blow the row width.
  const visible = participants.slice(0, 4);
  const overflow = participants.length - visible.length;
  const names = visible
    .map((p) => getActorName(p.user_type, p.user_id))
    .join(", ");
  return (
    <p className="mt-0.5 truncate text-xs text-muted-foreground">
      {names}
      {overflow > 0 ? ` +${overflow}` : ""}
    </p>
  );
}

function ChannelSnippet({
  preview,
  fallback,
  isChannel,
  unread,
}: {
  preview: { author: string; text: string } | null;
  fallback: string;
  isChannel: boolean;
  unread: boolean;
}) {
  const tone = unread ? "text-foreground" : "text-muted-foreground/70";
  if (preview) {
    return (
      <p className={`mt-0.5 line-clamp-2 text-xs leading-snug ${tone}`}>
        <span className="font-semibold">{preview.author}:</span> {preview.text}
      </p>
    );
  }
  if (fallback) {
    return (
      <p className={`mt-0.5 line-clamp-2 text-xs leading-snug ${tone}`}>
        {fallback}
      </p>
    );
  }
  return (
    <p className="mt-0.5 truncate text-xs text-muted-foreground/60">
      {isChannel ? "No messages yet" : ""}
    </p>
  );
}

function MentionBadge() {
  return (
    <span className="shrink-0 text-[10px] font-semibold uppercase tracking-wide text-destructive">
      @ you
    </span>
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
