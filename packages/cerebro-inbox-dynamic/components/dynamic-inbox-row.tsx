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
import { useEffect, useState } from "react";
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
  // components, it sits as an overlay over the leading avatar (all three row
  // kinds put a size-7 avatar at `px-4`, vertically centred). The avatar shows
  // by default; clicking it flips (rotates) to reveal a star, and clicking the
  // star sets/clears the favorite. A starred row shows the gold star at rest.
  if (!favoritesEnabled || isArchivedView) return row;
  return (
    <div className="relative">
      {row}
      <FavoriteStar active={!!isFavorite} onToggle={() => onToggleFavorite?.(entry)} />
    </div>
  );
}

/** TECH-3579 — how long the armed star stays before flipping back to the avatar. */
const ARMED_FLIP_BACK_MS = 5000;

/**
 * TECH-3579 — two-step favorite affordance over the row avatar:
 *  1. Default shows the avatar (the overlay's front face is transparent).
 *  2. Clicking the avatar flips the overlay to reveal a star ("armed").
 *  3. Clicking the star — anywhere in the circle — sets the favorite; the gold
 *     star then stays at rest.
 * A click on the avatar never selects/opens the row (it only flips). Once armed,
 * the whole circle is the favorite target, and the star stays put for 5 seconds
 * (Jesper feedback: flip-back-on-pointer-leave made the star almost impossible
 * to hit). If the user doesn't favorite within 5s, it flips back to the avatar.
 */
function FavoriteStar({ active, onToggle }: { active: boolean; onToggle: () => void }) {
  const [armed, setArmed] = useState(false);
  const showStar = active || armed;

  // Armed but not yet favorited → give the user a full 5s to click, then flip
  // back to the avatar. Re-arming resets the timer (effect re-runs on `armed`).
  useEffect(() => {
    if (!armed || active) return;
    const t = setTimeout(() => setArmed(false), ARMED_FLIP_BACK_MS);
    return () => clearTimeout(t);
  }, [armed, active]);

  return (
    <button
      type="button"
      aria-label={active ? "Remove from favorites" : showStar ? "Set as favorite" : "Favorite this conversation"}
      aria-pressed={active}
      title={active ? "Remove from favorites" : showStar ? "Click the star to favorite" : "Click to favorite"}
      onClick={(e) => {
        // The avatar/star overlay must never bubble into the row's open handler.
        e.preventDefault();
        e.stopPropagation();
        if (!showStar) {
          // First click on the avatar: flip to the star — don't favorite yet.
          setArmed(true);
          return;
        }
        // Star is showing: a click anywhere in the circle commits or clears it.
        onToggle();
        if (active) setArmed(false); // removed → flip back to the avatar
      }}
      className="absolute left-4 top-1/2 z-10 size-7 -translate-y-1/2 [perspective:600px]"
    >
      <span
        className={`relative block size-7 transition-transform duration-300 [transform-style:preserve-3d] ${
          showStar ? "[transform:rotateY(180deg)]" : ""
        }`}
      >
        {/* Front face — transparent so the real avatar underneath stays visible;
            this is just the click target that flips to the star. */}
        <span className="absolute inset-0 rounded-full [backface-visibility:hidden]" aria-hidden />
        {/* Back face — the whole circle is the click target; the star fills it so
            it reads as one big button. Opaque so it masks the avatar once flipped. */}
        <span
          className="absolute inset-0 flex items-center justify-center rounded-full bg-card [backface-visibility:hidden] [transform:rotateY(180deg)]"
          aria-hidden
        >
          <Star className={`size-5 ${active ? "fill-warning text-warning" : "text-muted-foreground"}`} />
        </span>
      </span>
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
