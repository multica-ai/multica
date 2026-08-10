"use client";

// FIR-2382 — the "Inbox" badge must never count more unread than the inbox list
// can show. The upstream badge counts every unread inbox_item (deduplicated by
// issue). But with the thread-split feature on (cerebro_inbox_thread_split,
// default on) a channel/DM reply that lives inside a thread carries
// details.thread_root_id and is deliberately EXCLUDED from the channel's unread
// count server-side (cerebroChannelUnreadCount → CountUnreadInboxForChannel-
// ExcludingThreads) — it surfaces on its own thread row in the dynamic inbox,
// and not at all in the classic inbox. So an unread thread reply would inflate
// the badge while producing no visible channel row: badge > visible rows.
//
// The badge is a count of unread CONVERSATIONS. A thread reply has independent,
// Slack-style read state, so it is not part of the conversation-level unread
// count — exactly mirroring the server's per-channel exclusion. We therefore
// drop thread replies before deduplicating, but only when the same flag that
// drives the server-side exclusion is on, so badge and channel row always agree.
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { deduplicateInboxItems, inboxListOptions } from "@multica/core/inbox/queries";
import type { Channel, InboxItem } from "@multica/core/types";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { roundExcludedIssueIds, useRoundStatuses } from "@multica/cerebro-rounds";
// FIR-4350 — a conversation the user has taken out of the Inbox must not be
// counted here either, or the badge exceeds the rows the Inbox can show.
import { channelListOptions } from "@multica/core/channels";
import {
  conversationKindOfChannel,
  useHiddenConversationKinds,
} from "@multica/cerebro-feature-flags";

/**
 * Count unread inbox conversations the way the inbox list surfaces them.
 *
 * Pure + exported so the behaviour is unit-tested without a query client.
 * When `excludeThreadReplies` is true, inbox items that live inside a thread
 * (`details.thread_root_id` set) are removed BEFORE deduplication — so a channel
 * whose only unread is a buried thread reply drops to zero (matching the hidden
 * classic row), while a channel that also has an unread top-level message still
 * counts once (its top-level item survives the filter).
 */
export function countUnreadInboxConversations(
  items: InboxItem[],
  opts: { excludeThreadReplies: boolean; excludeIssueIds?: ReadonlySet<string> },
): number {
  let base = opts.excludeThreadReplies
    ? items.filter((i) => !i.details?.thread_root_id)
    : items;
  const excludedIssueIds = opts.excludeIssueIds;
  if (excludedIssueIds && excludedIssueIds.size > 0) {
    base = base.filter((item) => !item.issue_id || !excludedIssueIds.has(item.issue_id));
  }
  // FIR-4935 — a muted (snoozed) conversation is hidden from every inbox box
  // until its mute expires (matchesViewFilter drops it from every view except
  // "Muted"), and the server's own badge query already excludes it
  // (CountUnreadInboxItems: `muted_until IS NULL OR muted_until <= NOW()`).
  // Filtered AFTER deduplication, exactly like that query: the row's visibility
  // is decided by the representative item, so an older unread sibling behind a
  // muted representative must not resurrect the count.
  return deduplicateInboxItems(base).filter((i) => !i.read && !isMutedNow(i)).length;
}

function isMutedNow(item: InboxItem): boolean {
  if (!item.muted_until) return false;
  const until = Date.parse(item.muted_until);
  return Number.isFinite(until) && until > Date.now();
}

/**
 * Unread inbox count for the sidebar / inbox-header / desktop-dock badge,
 * aligned with what the inbox list actually renders. Shares the inbox list
 * query cache, so it adds no extra fetch.
 */
export function useCerebroInboxUnreadCount(
  wsId: string | null | undefined,
): number {
  const threadSplit = useFeatureFlag("cerebro_inbox_thread_split");
  const roundsEnabled = useFeatureFlag("cerebro_inbox_rounds");
  const { data: roundStatuses = [] } = useRoundStatuses(wsId ?? "", roundsEnabled && !!wsId);
  const roundExcluded = roundExcludedIssueIds(roundStatuses);
  // FIR-4350 — channels/DMs of a hidden type. Their rows are filtered out of
  // the list, so their unread must leave the badge with them.
  const hiddenKinds = useHiddenConversationKinds();
  const { data: channels = EMPTY_CHANNELS } = useQuery({
    ...channelListOptions(wsId ?? ""),
    enabled: !!wsId && hiddenKinds.size > 0,
  });
  const excludeIssueIds = useMemo(() => {
    if (hiddenKinds.size === 0) return roundExcluded;
    const next = new Set(roundExcluded);
    for (const c of channels) {
      if (hiddenKinds.has(conversationKindOfChannel(c.kind))) next.add(c.id);
    }
    return next;
  }, [roundExcluded, hiddenKinds, channels]);
  const { data } = useQuery({
    ...inboxListOptions(wsId ?? ""),
    enabled: !!wsId,
    select: (items: InboxItem[]) =>
      countUnreadInboxConversations(items, { excludeThreadReplies: threadSplit, excludeIssueIds }),
  });
  return data ?? 0;
}

// Stable empty array — an inline default would change reference each render.
const EMPTY_CHANNELS: Channel[] = [];
