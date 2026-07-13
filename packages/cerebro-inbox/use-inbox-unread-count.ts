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
import { useQuery } from "@tanstack/react-query";
import { deduplicateInboxItems, inboxListOptions } from "@multica/core/inbox/queries";
import type { InboxItem } from "@multica/core/types";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
// FIR-3114 — issues in the user's rounds notify only inside the round, so the
// badge must not count them (mirrors the server-side count exclusion).
import { roundIssueIdsToExclude, useRoundStatuses } from "@multica/cerebro-rounds";

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
  // FIR-3114 — round-member issues surface inside the Rounds box only; they
  // never contribute to the badge (matches the server count queries).
  const excluded = opts.excludeIssueIds;
  if (excluded && excluded.size > 0) {
    base = base.filter((i) => !i.issue_id || !excluded.has(i.issue_id));
  }
  return deduplicateInboxItems(base).filter((i) => !i.read).length;
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
  const excludeIssueIds = roundIssueIdsToExclude(roundStatuses, roundsEnabled);
  const { data } = useQuery({
    ...inboxListOptions(wsId ?? ""),
    enabled: !!wsId,
    select: (items: InboxItem[]) =>
      countUnreadInboxConversations(items, { excludeThreadReplies: threadSplit, excludeIssueIds }),
  });
  return data ?? 0;
}
