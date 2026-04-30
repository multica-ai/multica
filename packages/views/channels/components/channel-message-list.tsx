"use client";

import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { channelMessagesOptions } from "@multica/core/channels";
import { MessageRow } from "./message-row";

interface ChannelMessageListProps {
  channelId: string;
  enabled: boolean;
  onOpenThread?: (parentMessageId: string) => void;
}

/**
 * ChannelMessageList fetches and renders a channel's most recent messages.
 *
 * Phase 1: simple .map() of the latest 50 (server returns newest-first; we
 * reverse for display). Auto-scrolls to the bottom on new messages —
 * detected by tracking the previous message-count rather than using a
 * MutationObserver, since the only mutation we care about is `length`.
 *
 * Phase 5+: switch to a virtualized list (TanStack Virtual) if channels
 * routinely exceed ~500 visible messages.
 */
export function ChannelMessageList({ channelId, enabled, onOpenThread }: ChannelMessageListProps) {
  const wsId = useWorkspaceId();
  const { data: rawMessages = [], isLoading } = useQuery(
    channelMessagesOptions(channelId, enabled),
  );
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const memberById = new Map(members.map((m) => [m.user_id, m]));
  const agentById = new Map(agents.map((a) => [a.id, a]));

  // Server returns newest-first; reverse so we render oldest at top, newest
  // at bottom (chat convention).
  const messages = [...rawMessages].reverse();

  const containerRef = useRef<HTMLDivElement | null>(null);
  const prevCountRef = useRef(messages.length);
  useEffect(() => {
    if (messages.length > prevCountRef.current) {
      const el = containerRef.current;
      if (el) el.scrollTop = el.scrollHeight;
    }
    prevCountRef.current = messages.length;
  }, [messages.length]);

  if (isLoading && messages.length === 0) {
    return (
      <div className="flex-1 overflow-y-auto px-4 py-6 text-sm text-muted-foreground">
        Loading messages…
      </div>
    );
  }
  if (messages.length === 0) {
    return (
      <div className="flex-1 overflow-y-auto px-4 py-12 text-center text-sm text-muted-foreground">
        No messages yet. Be the first to say hello.
      </div>
    );
  }
  return (
    <div ref={containerRef} className="flex-1 overflow-y-auto py-2">
      {messages.map((m) => (
        <MessageRow
          key={m.id}
          message={m}
          channelId={channelId}
          member={m.author_type === "member" ? memberById.get(m.author_id) : undefined}
          agent={m.author_type === "agent" ? agentById.get(m.author_id) : undefined}
          onOpenThread={onOpenThread}
        />
      ))}
    </div>
  );
}
