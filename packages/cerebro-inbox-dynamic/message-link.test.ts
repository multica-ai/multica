import { describe, it, expect } from "vitest";
import type { InboxItem, Channel } from "@multica/core/types";
import { messageKeyForEntry, findEntryByMessageKey, findEntryByInboxIssueParam, nextInboxMessageUrl } from "./message-link";
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

  it("falls back from a channel link to a notification for the same issue", () => {
    expect(findEntryByMessageKey(entries, "channel:iss-9")).toBe(entries[0]);
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

describe("nextInboxMessageUrl", () => {
  const base = {
    urlChat: "",
    urlIssue: "",
    selectedKey: null as string | null,
    currentMessageParam: null as string | null,
    inboxPath: "/acme/inbox",
  };

  it("writes ?message= for the open row when no message param is set yet", () => {
    expect(
      nextInboxMessageUrl({ ...base, selectedKey: "notif:iss-9" }),
    ).toBe("/acme/inbox?message=notif%3Aiss-9");
  });

  it("clears the URL to the bare inbox path when nothing is selected", () => {
    expect(
      nextInboxMessageUrl({ ...base, selectedKey: null, currentMessageParam: "notif:iss-9" }),
    ).toBe("/acme/inbox");
  });

  it("skips when the URL already reflects the open row", () => {
    expect(
      nextInboxMessageUrl({
        ...base,
        selectedKey: "notif:iss-9",
        currentMessageParam: "notif:iss-9",
      }),
    ).toBeNull();
  });

  // FIR-2677 — the regression: a message is open (selectedKey set) while a
  // sidebar "New message" intent has just landed in the URL. Rewriting
  // ?message= here would clobber that intent and re-open the old message,
  // discarding the new agent chat. The write must be skipped.
  it("does NOT write ?message= while a ?chat/?agent intent is unconsumed", () => {
    expect(
      nextInboxMessageUrl({
        ...base,
        urlChat: "new-chat",
        selectedKey: "notif:iss-9",
        currentMessageParam: null,
      }),
    ).toBeNull();
  });

  it("does NOT write ?message= while a ?issue intent is unconsumed", () => {
    expect(
      nextInboxMessageUrl({
        ...base,
        urlIssue: "chan-1",
        selectedKey: "notif:iss-9",
        currentMessageParam: null,
      }),
    ).toBeNull();
  });

  it("resumes normal syncing once the intent has been consumed and selection cleared", () => {
    // After consume: selection is cleared and the intent params are gone, so the
    // stale ?message= (if any) is cleared back to the bare inbox path.
    expect(
      nextInboxMessageUrl({ ...base, selectedKey: null, currentMessageParam: "notif:iss-9" }),
    ).toBe("/acme/inbox");
  });
});

describe("findEntryByInboxIssueParam", () => {
  it("prefers the channel row over a notification with the same issue id", () => {
    const channel = channelEntry("chan-1");
    const notif = notifEntry("item-1", { issue_id: "chan-1" });

    expect(findEntryByInboxIssueParam([notif, channel], "chan-1")).toBe(channel);
  });

  it("defers notification matches while channel data is still loading", () => {
    const notif = notifEntry("item-1", { issue_id: "chan-1" });

    expect(
      findEntryByInboxIssueParam([notif], "chan-1", { deferIssueNotifications: true }),
    ).toBeNull();
    expect(findEntryByInboxIssueParam([notif], "chan-1")).toBe(notif);
  });
});
