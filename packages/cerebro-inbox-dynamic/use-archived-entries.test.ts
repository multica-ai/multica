// FIR-1686 — archived surfaces sort by WHEN an item was archived (archived_at),
// not by its original activity time. This guards the sort-key derivation.
import { describe, it, expect } from "vitest";
import type { InboxItem } from "@multica/core/types";
import { archivedNotifSortTime } from "./use-archived-entries";

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
