"use client";

import { Fragment, useEffect, useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { channelMessagesOptions } from "@multica/core/channels";
import { MessageRow } from "./message-row";

// Two messages are part of the same "group" — same author, in close
// succession — when the second one came in within this window. Slack
// uses ~5 minutes; tighter feels twitchy on slow conversations and
// looser starts merging unrelated bursts. 5 minutes is the right knob.
const GROUP_CONTINUATION_MS = 5 * 60 * 1000;

interface ChannelMessageListProps {
  channelId: string;
  enabled: boolean;
  onOpenThread?: (parentMessageId: string) => void;
  /**
   * Frozen-on-mount last_read_message_id for the active channel. The list
   * renders an "unread" divider before the first message newer than this
   * cursor, and on the first render with messages it scrolls that divider
   * into view. Null = no divider (everything has been read OR there are
   * unread messages but we have no anchor to bisect on).
   */
  initialUnreadCursor?: string | null;
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
export function ChannelMessageList({
  channelId,
  enabled,
  onOpenThread,
  initialUnreadCursor,
}: ChannelMessageListProps) {
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

  // Locate the divider position: we render the "New messages" divider
  // immediately BEFORE the first message that's newer than the cursor.
  // If the cursor isn't found (older message that's been retention-trimmed,
  // or this is a brand-new channel for the user) and there are messages,
  // the divider goes at the very top so every visible message reads as
  // unread. If the cursor matches the latest message we render no divider.
  const dividerBeforeIndex = useMemo<number | null>(() => {
    if (initialUnreadCursor === undefined) return null; // not yet hydrated
    if (initialUnreadCursor === null) {
      // Never read at all. Show divider at top only if there's actually
      // content; otherwise nothing to anchor on.
      return messages.length > 0 ? 0 : null;
    }
    const idx = messages.findIndex((m) => m.id === initialUnreadCursor);
    if (idx === -1) {
      // Cursor older than the loaded window — treat everything as unread.
      return 0;
    }
    if (idx === messages.length - 1) {
      // Cursor is the latest message; nothing unread.
      return null;
    }
    return idx + 1;
  }, [messages, initialUnreadCursor]);

  const containerRef = useRef<HTMLDivElement | null>(null);
  const dividerRef = useRef<HTMLDivElement | null>(null);
  const initialScrollDoneRef = useRef<{ channelId: string | null }>({ channelId: null });
  const prevCountRef = useRef(messages.length);

  // Channel switch resets initial-scroll bookkeeping so the new channel
  // gets its own anchor decision (divider into view OR scroll to bottom).
  useEffect(() => {
    initialScrollDoneRef.current = { channelId: null };
    prevCountRef.current = 0;
  }, [channelId]);

  // First render with messages: anchor to divider if we have one, else
  // scroll to bottom. Subsequent message arrivals fall through to the
  // "tail" effect below.
  useEffect(() => {
    if (initialScrollDoneRef.current.channelId === channelId) return;
    if (messages.length === 0) return;
    const el = containerRef.current;
    if (!el) return;
    const divider = dividerRef.current;
    if (divider) {
      divider.scrollIntoView({ block: "center", behavior: "auto" });
    } else {
      el.scrollTop = el.scrollHeight;
    }
    initialScrollDoneRef.current = { channelId };
    prevCountRef.current = messages.length;
  }, [channelId, messages.length, dividerBeforeIndex]);

  // Tail behavior: on new messages after initial scroll, jump to bottom.
  // (Phase 2 polish would gate this on "user was already at bottom" so a
  // user reading older history isn't yanked down. Keeping it simple now.)
  useEffect(() => {
    if (initialScrollDoneRef.current.channelId !== channelId) return;
    if (messages.length > prevCountRef.current) {
      const el = containerRef.current;
      if (el) el.scrollTop = el.scrollHeight;
    }
    prevCountRef.current = messages.length;
  }, [channelId, messages.length]);

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
    <div ref={containerRef} className="flex-1 overflow-y-auto py-1">
      {messages.map((m, i) => {
        const prev = i > 0 ? messages[i - 1] : null;
        // The unread divider visually breaks a group, so a continuation
        // immediately after the divider should re-introduce the avatar
        // header — feels weird otherwise. Hence the `dividerBeforeIndex`
        // check inside the predicate.
        const isContinuation =
          !!prev &&
          dividerBeforeIndex !== i &&
          prev.author_type === m.author_type &&
          prev.author_id === m.author_id &&
          new Date(m.created_at).getTime() - new Date(prev.created_at).getTime() <
            GROUP_CONTINUATION_MS;
        return (
          <Fragment key={m.id}>
            {dividerBeforeIndex === i ? <UnreadDivider ref={dividerRef} /> : null}
            <MessageRow
              message={m}
              channelId={channelId}
              member={m.author_type === "member" ? memberById.get(m.author_id) : undefined}
              agent={m.author_type === "agent" ? agentById.get(m.author_id) : undefined}
              onOpenThread={onOpenThread}
              isGroupContinuation={isContinuation}
            />
          </Fragment>
        );
      })}
    </div>
  );
}

const UnreadDivider = ({ ref }: { ref?: React.Ref<HTMLDivElement> }) => (
  <div
    ref={ref}
    className="my-2 flex items-center gap-3 px-4 text-[11px] font-semibold uppercase tracking-wide text-primary"
    aria-label="New messages"
  >
    <span className="h-px flex-1 bg-primary/40" />
    <span>New messages</span>
    <span className="h-px flex-1 bg-primary/40" />
  </div>
);
