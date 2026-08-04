// FIR-4350 — which conversation types the Inbox currently hides, for this user.
//
// HIDES, not removes: the rows are filtered out of the list the Inbox renders.
// Nothing is deleted, archived or marked read, and turning the type back on in
// the placement settings shows every row again exactly as it was.
//
// Read by both inbox implementations and by the Inbox unread badge, so badge
// and visible list can never disagree.
"use client";

import { useMemo } from "react";
import { useFeatureFlag } from "./index";
import {
  conversationKindOfChannel,
  hiddenFromInbox,
  type ConversationKind,
} from "./chat-placement";
import { useChatPlacement } from "./use-chat-placement";

const NOTHING_HIDDEN: ReadonlySet<ConversationKind> = new Set();

/**
 * Empty whenever the feature is off, so a user who never enables Chat sees the
 * Inbox exactly as it is today.
 */
export function useHiddenConversationKinds(): ReadonlySet<ConversationKind> {
  const enabled = useFeatureFlag("cerebro_chat_page");
  const { placement } = useChatPlacement();
  return useMemo(
    () => (enabled ? hiddenFromInbox(placement) : NOTHING_HIDDEN),
    [enabled, placement],
  );
}

/**
 * Pure predicate over the inbox row kinds. `notif` rows are issue
 * notifications, never conversation, so they are never hidden by placement.
 */
export function isConversationHidden(
  hidden: ReadonlySet<ConversationKind>,
  entry: { kind: string; channel?: { kind?: string }; channelKind?: string },
): boolean {
  if (hidden.size === 0) return false;
  switch (entry.kind) {
    case "channel":
      return hidden.has(conversationKindOfChannel(entry.channel?.kind));
    case "thread":
      // A thread row belongs to its channel and is placed with it (FIR-1854).
      return hidden.has(conversationKindOfChannel(entry.channelKind));
    case "chat":
      return hidden.has("agent_chat");
    default:
      return false;
  }
}
