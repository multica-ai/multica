// FIR-1854 — pure builder for the per-thread inbox rows. Thin adapter over the
// shared Chat/inbox builder in @multica/cerebro-feature-flags so both surfaces
// stay in lockstep (FIR-4649).
import { buildUnreadChannelThreads } from "@multica/cerebro-feature-flags/channel-thread-entries";
import type { DynInboxEntry } from "./section-filter";
import type { Channel, InboxItem } from "@multica/core/types";

export function buildChannelThreadEntries(
  rawItems: InboxItem[],
  channelMap: Map<string, Channel>,
): DynInboxEntry[] {
  return buildUnreadChannelThreads(rawItems, channelMap).map((t) => ({
    kind: "thread" as const,
    id: `thread:${t.threadRootId}`,
    time: t.time,
    item: t.item,
    channelId: t.channelId,
    channelKind: t.channelKind,
    threadRootId: t.threadRootId,
  }));
}
