// FIR-1854 — pure builder for the per-thread inbox rows. Extracted from
// use-dynamic-inbox-data.ts so the channel/DM thread-split logic is unit
// testable on its own (the hook around it only wires queries together).
import { inboxItemSortTime } from "@multica/core/inbox/queries";
import type { Channel, InboxItem } from "@multica/core/types";
import type { DynInboxEntry } from "./section-filter";

// Build one inbox row per channel/DM thread that has unread replies. A reply
// that lands inside a thread carries details.thread_root_id (set server-side by
// cerebroChannelThreadDetails); grouping those replies by their root surfaces
// the thread as its own row instead of burying it in the single channel row.
// Only threads with an unread reply get a row, so the inbox stays clean once
// the thread has been read. The most recent unread reply backs the row
// (preview, sort time, deep-link target). Channels and DMs share this path —
// the only difference is channelKind, read off the backing Channel.
export function buildChannelThreadEntries(
  rawItems: InboxItem[],
  channelMap: Map<string, Channel>,
): DynInboxEntry[] {
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
  const out: DynInboxEntry[] = [];
  for (const [rootId, group] of byThread) {
    const unread = group.filter((i) => !i.read);
    if (unread.length === 0) continue;
    unread.sort((a, b) => inboxItemSortTime(b) - inboxItemSortTime(a));
    const representative = unread[0];
    if (!representative?.issue_id) continue;
    const channel = channelMap.get(representative.issue_id);
    if (!channel) continue;
    out.push({
      kind: "thread",
      id: `thread:${rootId}`,
      time: inboxItemSortTime(representative),
      item: representative,
      channelId: representative.issue_id,
      channelKind: channel.kind === "dm" ? "dm" : "channel",
      threadRootId: rootId,
    });
  }
  return out;
}
