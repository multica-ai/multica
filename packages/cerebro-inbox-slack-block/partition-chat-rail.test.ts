import { describe, it, expect } from "vitest";
import {
  applyChatRailLimit,
  partitionChatRail,
  railLimitKind,
  type RailItem,
} from "./partition-chat-rail";

function item(over: Partial<RailItem> & Pick<RailItem, "key">): RailItem {
  return {
    kind: "channel",
    unread: 0,
    starred: false,
    limitKind: "channel",
    ...over,
  };
}

describe("railLimitKind", () => {
  it("maps threads to their parent kind", () => {
    expect(railLimitKind("thread", "channel")).toBe("channel");
    expect(railLimitKind("thread", "dm")).toBe("person");
    expect(railLimitKind("channel")).toBe("channel");
  });
});

describe("applyChatRailLimit (FIR-4649 — unreads never capped out)", () => {
  it("keeps every unread even when the read cap is already full", () => {
    const items = [
      item({ key: "u1", unread: 1, limitKind: "channel" }),
      item({ key: "u2", unread: 2, limitKind: "channel" }),
      item({ key: "r1", limitKind: "channel" }),
      item({ key: "r2", limitKind: "channel" }),
      item({ key: "r3", limitKind: "channel" }),
    ];
    const out = applyChatRailLimit(items, 1, "type", true);
    expect(out.map((i) => i.key)).toEqual(["u1", "u2", "r1"]);
  });

  it("still caps when unread-first is off", () => {
    const items = [
      item({ key: "a", limitKind: "channel" }),
      item({ key: "b", unread: 1, limitKind: "channel" }),
      item({ key: "c", limitKind: "channel" }),
    ];
    expect(applyChatRailLimit(items, 1, "type", false).map((i) => i.key)).toEqual([
      "a",
    ]);
  });
});

describe("partitionChatRail (FIR-4649 — Slack unread-at-top)", () => {
  it("puts every unread first, any type, including starred", () => {
    const items = [
      item({ key: "star-read", starred: true }),
      item({ key: "star-unread", starred: true, unread: 1, kind: "person", limitKind: "person" }),
      item({ key: "ch-unread", unread: 3 }),
      item({ key: "dm-read", kind: "person", limitKind: "person" }),
      item({ key: "thread", kind: "thread", unread: 1, limitKind: "channel" }),
    ];
    const { unread, favorites, rest } = partitionChatRail(items, true);
    expect(unread.map((i) => i.key)).toEqual([
      "star-unread",
      "ch-unread",
      "thread",
    ]);
    expect(favorites.map((i) => i.key)).toEqual(["star-read"]);
    expect(rest.map((i) => i.key)).toEqual(["dm-read"]);
  });

  it("keeps starred in Favorites when unread-first is off", () => {
    const items = [
      item({ key: "star", starred: true, unread: 1 }),
      item({ key: "plain" }),
    ];
    const { unread, favorites, rest } = partitionChatRail(items, false);
    expect(unread).toEqual([]);
    expect(favorites.map((i) => i.key)).toEqual(["star"]);
    expect(rest.map((i) => i.key)).toEqual(["plain"]);
  });
});
