// FIR-1686 — archived surfaces sort by WHEN an item was archived (archived_at),
// not by its original activity time. This guards the sort-key derivation.
import { describe, it, expect } from "vitest";
import type { Channel, ChatSession, InboxItem } from "@multica/core/types";
import {
  archivedChannelSortTime,
  archivedNotifSortTime,
  buildArchivedEntries,
} from "./use-archived-entries";

function item(over: Partial<InboxItem> = {}): InboxItem {
  return {
    id: "n1",
    created_at: "2026-01-01T00:00:00Z",
    muted_until: null,
    ...over,
  } as InboxItem;
}

describe("archivedNotifSortTime", () => {
  it("uses archived_at when present", () => {
    const t = archivedNotifSortTime(
      item({ created_at: "2026-01-01T00:00:00Z", archived_at: "2026-06-20T12:00:00Z" }),
    );
    expect(t).toBe(Date.parse("2026-06-20T12:00:00Z"));
  });

  it("falls back to the activity time (created_at) when archived_at is absent", () => {
    const t = archivedNotifSortTime(item({ created_at: "2026-01-01T00:00:00Z" }));
    expect(t).toBe(Date.parse("2026-01-01T00:00:00Z"));
  });

  it("ranks a recently-archived OLD item above a freshly-created one", () => {
    // The exact bug FIR-1686 fixes: an old message archived just now must sort
    // to the top, above a newer message archived earlier.
    const oldButJustArchived = archivedNotifSortTime(
      item({ created_at: "2025-01-01T00:00:00Z", archived_at: "2026-06-20T12:00:00Z" }),
    );
    const newButArchivedEarlier = archivedNotifSortTime(
      item({ created_at: "2026-06-01T00:00:00Z", archived_at: "2026-06-10T00:00:00Z" }),
    );
    expect(oldButJustArchived).toBeGreaterThan(newButArchivedEarlier);
  });
});

// FIR-2791 — archived channels/DMs join the merged archived-entry list, one
// row per conversation, with their message notifications folded away.
function channel(over: Partial<Channel> = {}): Channel {
  return {
    id: "c1",
    kind: "dm",
    updated_at: "2026-06-01T00:00:00Z",
    unread_count: 0,
    ...over,
  } as Channel;
}

function chat(over: Partial<ChatSession> = {}): ChatSession {
  return { id: "s1", updated_at: "2026-06-01T00:00:00Z", ...over } as ChatSession;
}

describe("archivedChannelSortTime", () => {
  it("uses the last message time when present", () => {
    const t = archivedChannelSortTime(
      channel({
        updated_at: "2026-06-01T00:00:00Z",
        last_message: { created_at: "2026-06-20T12:00:00Z" } as Channel["last_message"],
      }),
    );
    expect(t).toBe(Date.parse("2026-06-20T12:00:00Z"));
  });

  it("falls back to updated_at for empty conversations", () => {
    const t = archivedChannelSortTime(channel({ updated_at: "2026-06-01T00:00:00Z" }));
    expect(t).toBe(Date.parse("2026-06-01T00:00:00Z"));
  });
});

describe("buildArchivedEntries", () => {
  it("includes archived channels as channel entries, time-sorted with the rest", () => {
    const entries = buildArchivedEntries(
      [item({ id: "n1", created_at: "2026-06-15T00:00:00Z" })],
      [chat({ id: "s1", updated_at: "2026-06-10T00:00:00Z" })],
      [channel({ id: "c1", updated_at: "2026-06-20T00:00:00Z" })],
    );
    expect(entries.map((e) => `${e.kind}:${e.id}`)).toEqual(["channel:c1", "notif:n1", "chat:s1"]);
  });

  it("folds message notifications for an archived channel into the channel row", () => {
    const entries = buildArchivedEntries(
      [
        item({ id: "n1", issue_id: "c1", created_at: "2026-06-25T00:00:00Z" }),
        item({ id: "n2", issue_id: "c1", created_at: "2026-06-24T00:00:00Z" }),
        item({ id: "n3", issue_id: "other-issue", created_at: "2026-06-23T00:00:00Z" }),
      ],
      [],
      [channel({ id: "c1" })],
    );
    // The archived group appears exactly once; its notifications are folded.
    expect(entries.map((e) => `${e.kind}:${e.id}`)).toEqual(["notif:n3", "channel:c1"]);
  });

  it("keeps notifications untouched when no channel is archived", () => {
    const entries = buildArchivedEntries(
      [item({ id: "n1", issue_id: "c1", created_at: "2026-06-25T00:00:00Z" })],
      [],
      [],
    );
    expect(entries.map((e) => `${e.kind}:${e.id}`)).toEqual(["notif:n1"]);
  });
});
