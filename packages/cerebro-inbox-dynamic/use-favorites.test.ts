import { describe, it, expect } from "vitest";
import type { InboxItem } from "@multica/core/types";
import { favoriteKeyForEntry, readFavorites, toggleFavoriteKey } from "./use-favorites";
import type { DynInboxEntry } from "./section-filter";

function notifEntry(over: Partial<InboxItem> = {}): DynInboxEntry {
  const item = { id: "n1", issue_id: "iss1", ...over } as InboxItem;
  return { kind: "notif", id: item.id, time: 1, item };
}

describe("use-favorites helpers", () => {
  it("keys a notif on its issue, falling back to the notification id", () => {
    expect(favoriteKeyForEntry(notifEntry())).toBe("issue:iss1");
    expect(favoriteKeyForEntry(notifEntry({ issue_id: null }))).toBe("issue:n1");
  });

  it("keys chats and channels on their own id", () => {
    const chat: DynInboxEntry = { kind: "chat", id: "c1", time: 1, session: { id: "c1" } as never };
    const channel: DynInboxEntry = { kind: "channel", id: "ch1", time: 1, channel: { id: "ch1" } as never };
    expect(favoriteKeyForEntry(chat)).toBe("chat:c1");
    expect(favoriteKeyForEntry(channel)).toBe("channel:ch1");
  });

  it("readFavorites keeps only non-empty strings from an untrusted blob", () => {
    expect(readFavorites(["issue:a", "", 3, null, "chat:b"])).toEqual(["issue:a", "chat:b"]);
    expect(readFavorites(undefined)).toEqual([]);
    expect(readFavorites("not-an-array")).toEqual([]);
  });

  it("toggleFavoriteKey adds when absent and removes when present", () => {
    expect(toggleFavoriteKey([], "issue:a")).toEqual(["issue:a"]);
    expect(toggleFavoriteKey(["issue:a"], "issue:a")).toEqual([]);
    expect(toggleFavoriteKey(["issue:a"], "chat:b")).toEqual(["issue:a", "chat:b"]);
    // An empty key is a no-op (returns a copy, never adds "").
    expect(toggleFavoriteKey(["issue:a"], "")).toEqual(["issue:a"]);
  });
});
