// FIR-4649 — Chat rail layout: same unread rule as the sidebar badge, Slack-
// style "all unread at the top regardless of type", and a row-cap that never
// hides an unread conversation behind Favorites / recency noise.
//
// Pure helpers so the rail partition is unit-tested without React.

export type RailKind = "channel" | "person" | "agent" | "thread";

export type RailItem = {
  key: string;
  kind: RailKind;
  unread: number;
  starred: boolean;
  /** Parent kind for threads — drives the per-kind row cap. */
  limitKind: "channel" | "person" | "agent";
};

export type RailPartition<T extends RailItem> = {
  unread: T[];
  favorites: T[];
  rest: T[];
};

/** Cap key: threads count toward their parent channel/DM kind. */
export function railLimitKind(kind: RailKind, channelKind?: string): RailItem["limitKind"] {
  if (kind === "thread") {
    return channelKind === "channel" ? "channel" : "person";
  }
  if (kind === "channel" || kind === "person" || kind === "agent") return kind;
  return "person";
}

/**
 * Apply the row cap. When unread-first is on, EVERY unread row is kept
 * (Slack: unreads are never truncated out of the sidebar); the cap only
 * trims the read tail — per kind when grouped by type, shared total when flat.
 */
export function applyChatRailLimit<T extends RailItem>(
  items: T[],
  limit: number,
  groupBy: "type" | "none",
  unreadFirst: boolean,
): T[] {
  if (limit <= 0) return items;
  if (!unreadFirst) {
    return capItems(items, limit, groupBy);
  }
  const unread = items.filter((i) => i.unread > 0);
  const read = items.filter((i) => i.unread === 0);
  return [...unread, ...capItems(read, limit, groupBy)];
}

function capItems<T extends RailItem>(
  items: T[],
  limit: number,
  groupBy: "type" | "none",
): T[] {
  if (groupBy === "type") {
    const perKind = new Map<RailItem["limitKind"], number>();
    return items.filter((it) => {
      const taken = perKind.get(it.limitKind) ?? 0;
      if (taken >= limit) return false;
      perKind.set(it.limitKind, taken + 1);
      return true;
    });
  }
  return items.slice(0, limit);
}

/**
 * Slack-style partition for the Chat rail:
 *   1. Unread — every unread conversation, any type (channel / DM / thread /
 *      agent), starred or not. Always first when unreadFirst is on.
 *   2. Favorites — starred conversations that are already read.
 *   3. Rest — everything else (feeds the type groups / flat list).
 *
 * When unreadFirst is off there is no Unread block; starred stay in Favorites
 * and the rest keep their prior order.
 */
export function partitionChatRail<T extends RailItem>(
  items: T[],
  unreadFirst: boolean,
): RailPartition<T> {
  if (!unreadFirst) {
    return {
      unread: [],
      favorites: items.filter((i) => i.starred),
      rest: items.filter((i) => !i.starred),
    };
  }
  const unread = items.filter((i) => i.unread > 0);
  const read = items.filter((i) => i.unread === 0);
  return {
    unread,
    favorites: read.filter((i) => i.starred),
    rest: read.filter((i) => !i.starred),
  };
}
