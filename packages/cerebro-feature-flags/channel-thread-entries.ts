// FIR-1854 / FIR-4649 — pure builder for unread channel/DM thread rows.
// Shared by the dynamic inbox and the Chat page rail so a thread reply is not
// invisible when its channel lives only in Chat (default placement).
import { inboxItemSortTime } from "@multica/core/inbox/queries";
import type { Channel, InboxItem } from "@multica/core/types";

export type UnreadChannelThread = {
  threadRootId: string;
  channelId: string;
  channelKind: "channel" | "dm" | "group";
  channel: Channel;
  /** Newest unread reply — preview + deep-link target. */
  item: InboxItem;
  unreadCount: number;
  time: number;
};

/**
 * One row per channel/DM thread that still has unread replies. A reply inside a
 * thread carries details.thread_root_id (server-side); grouping those by root
 * surfaces the thread instead of burying it in the channel row. Read threads
 * produce no row.
 */
export function buildUnreadChannelThreads(
  rawItems: InboxItem[],
  channelMap: Map<string, Channel>,
): UnreadChannelThread[] {
  const byThread = new Map<string, InboxItem[]>();
  for (const item of rawItems) {
    if (item.archived) continue;
    const channelId = item.issue_id;
    if (!channelId || !channelMap.has(channelId)) continue;
    const rootId = item.details?.thread_root_id;
    if (!rootId) continue;
    const group = byThread.get(rootId) ?? [];
    group.push(item);
    byThread.set(rootId, group);
  }
  const out: UnreadChannelThread[] = [];
  for (const [rootId, group] of byThread) {
    const unread = group.filter((i) => !i.read);
    if (unread.length === 0) continue;
    unread.sort((a, b) => inboxItemSortTime(b) - inboxItemSortTime(a));
    const representative = unread[0];
    if (!representative?.issue_id) continue;
    const channel = channelMap.get(representative.issue_id);
    if (!channel) continue;
    out.push({
      threadRootId: rootId,
      channelId: representative.issue_id,
      channelKind:
        channel.kind === "dm" ? "dm" : channel.kind === "group" ? "group" : "channel",
      channel,
      item: representative,
      unreadCount: unread.length,
      time: inboxItemSortTime(representative),
    });
  }
  return out;
}
