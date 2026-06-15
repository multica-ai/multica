// TECH-3413 — renders one merged-inbox entry inside a dynamic section, reusing
// the upstream row components so rows look identical to the classic inbox.
"use client";

import {
  InboxListItem,
  ChannelListItem,
  useTimeAgo,
  AgentRunPip,
  type AgentRunState,
} from "@multica/views/inbox/components/inbox-list-item";
import { ActorAvatar } from "@multica/views/common/actor-avatar";
import { useActorName } from "@multica/core/workspace/hooks";
import { CerebroChatSessionRowActions } from "@multica/cerebro-chat/views";
import { Star } from "lucide-react";
import type { ChatSession } from "@multica/core/types";
import type { DynInboxEntry } from "../section-filter";

export interface DynamicInboxRowProps {
  entry: DynInboxEntry;
  isSelected: boolean;
  mentioned?: boolean;
  agentRunState?: AgentRunState;
  /** TECH-3541 #3 — archived view: rows offer "unarchive" instead of archive. */
  isArchivedView?: boolean;
  /** TECH-3579 — show the favorite star overlay on the row avatar. */
  favoritesEnabled?: boolean;
  /** TECH-3579 — is this row's conversation currently a favorite? */
  isFavorite?: boolean;
  /** TECH-3579 — star / unstar this row's conversation. */
  onToggleFavorite?: (entry: DynInboxEntry) => void;
  onSelect: (entry: DynInboxEntry) => void;
  onArchive: (entry: DynInboxEntry) => void;
  /** TECH-3541 #3 — required when isArchivedView; brings the row back. */
  onUnarchive?: (entry: DynInboxEntry) => void;
}

export function DynamicInboxRow({
  entry,
  isSelected,
  mentioned,
  agentRunState,
  isArchivedView,
  favoritesEnabled,
  isFavorite,
  onToggleFavorite,
  onSelect,
  onArchive,
  onUnarchive,
}: DynamicInboxRowProps) {
  let row: React.ReactNode;
  if (entry.kind === "notif") {
    row = (
      <InboxListItem
        item={entry.item}
        isSelected={isSelected}
        agentRunState={agentRunState}
        onClick={() => onSelect(entry)}
        onArchive={() => onArchive(entry)}
        onUnarchive={isArchivedView && onUnarchive ? () => onUnarchive(entry) : undefined}
      />
    );
  } else if (entry.kind === "channel") {
    row = (
      <ChannelListItem
        channel={entry.channel}
        mentioned={mentioned}
        isSelected={isSelected}
        onClick={() => onSelect(entry)}
        onArchive={() => onArchive(entry)}
        onUnarchive={isArchivedView && onUnarchive ? () => onUnarchive(entry) : undefined}
      />
    );
  } else {
    // Agent chat session — same row chrome as the classic inbox chat row
    // (brand-blue unread stripe, agent avatar, title + relative time, run pip,
    // swipe-archive) so it reads as the same kind of message as before.
    row = (
      <ChatSessionRow
        session={entry.session}
        agentRunState={agentRunState}
        isSelected={isSelected}
        isArchivedView={isArchivedView}
        onSelect={() => onSelect(entry)}
        onArchive={() => onArchive(entry)}
      />
    );
  }

  // TECH-3579 — the favorite toggle. Rather than touch the shared upstream row
  // components, the star sits as an overlay over the leading avatar (all three
  // row kinds put a size-7 avatar at `px-4`, vertically centred). It is hidden
  // until the row is hovered/focused, so the avatar shows normally; a starred
  // row keeps the gold star visible in place of the avatar.
  if (!favoritesEnabled || isArchivedView) return row;
  return (
    <div className="group/fav relative">
      {row}
      <FavoriteStar active={!!isFavorite} onToggle={() => onToggleFavorite?.(entry)} />
    </div>
  );
}

function FavoriteStar({ active, onToggle }: { active: boolean; onToggle: () => void }) {
  return (
    <button
      type="button"
      aria-label={active ? "Remove from favorites" : "Add to favorites"}
      aria-pressed={active}
      title={active ? "Remove from favorites" : "Add to favorites"}
      onClick={(e) => {
        // Don't let the star toggle bubble up into the row's select handler.
        e.preventDefault();
        e.stopPropagation();
        onToggle();
      }}
      className={`absolute left-4 top-1/2 z-10 flex size-7 -translate-y-1/2 items-center justify-center rounded-full transition-opacity ${
        active
          ? "bg-warning/15 opacity-100"
          : "bg-background/90 opacity-0 group-hover/fav:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
      }`}
    >
      <Star className={`size-4 ${active ? "fill-warning text-warning" : "text-muted-foreground"}`} />
    </button>
  );
}

function ChatSessionRow({
  session,
  agentRunState,
  isSelected,
  isArchivedView,
  onSelect,
  onArchive,
}: {
  session: ChatSession;
  agentRunState?: AgentRunState;
  isSelected: boolean;
  isArchivedView?: boolean;
  onSelect: () => void;
  onArchive: () => void;
}) {
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const agentName = getActorName("agent", session.agent_id);
  const unread = session.has_unread;

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onSelect}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSelect();
        }
      }}
      className={`group relative flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors ${
        isSelected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      {unread && <span aria-hidden className="absolute inset-y-0 left-0 w-1 bg-brand" />}
      <ActorAvatar actorType="agent" actorId={session.agent_id} size={28} enableHoverCard />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-2">
          <span className={`min-w-0 flex-1 truncate text-sm ${unread ? "font-semibold" : "text-muted-foreground"}`}>
            {session.title || agentName || "Chat"}
          </span>
          <span className={`shrink-0 text-xs ${unread ? "text-muted-foreground" : "text-muted-foreground/60"}`}>
            {timeAgo(session.updated_at)}
          </span>
        </div>
        <p
          className={`mt-0.5 flex items-center gap-1.5 text-xs leading-snug ${
            unread ? "text-foreground" : "text-muted-foreground/70"
          }`}
        >
          {agentRunState && <AgentRunPip state={agentRunState} />}
          <span className="truncate">{agentName}</span>
        </p>
      </div>
      {/* TECH-3489 — full 3-dot menu (mark read / rename / convert / archive /
          delete) + mobile swipe, matching issue and channel rows. The dynamic
          inbox only lists active chats, so this is always the archive variant. */}
      <CerebroChatSessionRowActions
        session={session}
        isArchivedView={isArchivedView}
        onCleared={onArchive}
      />
    </div>
  );
}
