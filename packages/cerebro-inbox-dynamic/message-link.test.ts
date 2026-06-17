import { describe, it, expect } from "vitest";
import type { InboxItem, Channel } from "@multica/core/types";
import { messageKeyForEntry, findEntryByMessageKey } from "./message-link";
import type { DynInboxEntry } from "./section-filter";

function notifEntry(id: string, over: Partial<InboxItem> = {}): DynInboxEntry {
  const item = { id, issue_id: null, ...over } as InboxItem;
  return { kind: "notif", id, time: 0, item };
}

function channelEntry(id: string): DynInboxEntry {
  return { kind: "channel", id, time: 0, channel: { id } as Channel };
}

function chatEntry(id: string): DynInboxEntry {
  return { kind: "chat", id, time: 0, session: { id } as never };
}

describe("messageKeyForEntry", () => {
  it("keys an issue notification by its issue, not the per-user item id", () => {
    expect(messageKeyForEntry(notifEntry("item-1", { issue_id: "iss-9" }))).toBe("notif:iss-9");
  });

  it("keys an issue-less notification by its own id", () => {
    expect(messageKeyForEntry(notifEntry("item-1"))).toBe("notif:item-1");
  });

  it("keys a channel by its channel id", () => {
    expect(messageKeyForEntry(channelEntry("chan-1"))).toBe("channel:chan-1");
  });

  it("keys a chat by its session id", () => {
    expect(messageKeyForEntry(chatEntry("sess-1"))).toBe("chat:sess-1");
  });
});

describe("findEntryByMessageKey", () => {
  const entries: DynInboxEntry[] = [
    notifEntry("item-1", { issue_id: "iss-9" }),
    notifEntry("item-2"),
    channelEntry("chan-1"),
    chatEntry("sess-1"),
  ];

  it("round-trips every entry through its key", () => {
    for (const entry of entries) {
      expect(findEntryByMessageKey(entries, messageKeyForEntry(entry))).toBe(entry);
    }
  });

  it("matches an issue notification by issue id", () => {
    expect(findEntryByMessageKey(entries, "notif:iss-9")).toBe(entries[0]);
  });

  it("does not cross id spaces of different kinds", () => {
    expect(findEntryByMessageKey(entries, "channel:sess-1")).toBeNull();
    expect(findEntryByMessageKey(entries, "chat:chan-1")).toBeNull();
  });

  it("returns null for an unknown, empty, or malformed key", () => {
    expect(findEntryByMessageKey(entries, "notif:missing")).toBeNull();
    expect(findEntryByMessageKey(entries, "chat:")).toBeNull();
    expect(findEntryByMessageKey(entries, "garbage")).toBeNull();
    expect(findEntryByMessageKey(entries, "")).toBeNull();
  });
});
