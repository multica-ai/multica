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
import { CerebroSwipeArchive } from "@multica/cerebro-inbox";
import type { ChatSession } from "@multica/core/types";
import type { DynInboxEntry } from "../section-filter";

export interface DynamicInboxRowProps {
  entry: DynInboxEntry;
  isSelected: boolean;
  mentioned?: boolean;
  agentRunState?: AgentRunState;
  onSelect: (entry: DynInboxEntry) => void;
  onArchive: (entry: DynInboxEntry) => void;
}

export function DynamicInboxRow({ entry, isSelected, mentioned, agentRunState, onSelect, onArchive }: DynamicInboxRowProps) {
  if (entry.kind === "notif") {
    return (
      <InboxListItem
        item={entry.item}
        isSelected={isSelected}
        agentRunState={agentRunState}
        onClick={() => onSelect(entry)}
        onArchive={() => onArchive(entry)}
      />
    );
  }
  if (entry.kind === "channel") {
    return (
      <ChannelListItem
        channel={entry.channel}
        mentioned={mentioned}
        isSelected={isSelected}
        onClick={() => onSelect(entry)}
        onArchive={() => onArchive(entry)}
      />
    );
  }
  // Agent chat session — same row chrome as the classic inbox chat row
  // (brand-blue unread stripe, agent avatar, title + relative time, run pip,
  // swipe-archive) so it reads as the same kind of message as before.
  return (
    <ChatSessionRow
      session={entry.session}
      agentRunState={agentRunState}
      isSelected={isSelected}
      onSelect={() => onSelect(entry)}
      onArchive={() => onArchive(entry)}
    />
  );
}

function ChatSessionRow({
  session,
  agentRunState,
  isSelected,
  onSelect,
  onArchive,
}: {
  session: ChatSession;
  agentRunState?: AgentRunState;
  isSelected: boolean;
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
      <CerebroSwipeArchive onArchive={onArchive} />
    </div>
  );
}
