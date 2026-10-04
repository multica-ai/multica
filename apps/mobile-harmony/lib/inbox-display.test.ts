import { describe, expect, it } from "vitest";

import { getInboxDisplayTitle, deduplicateInboxItems } from "./inbox-display";
import type { InboxItem } from "@multica/core/types";

// @vitest-environment node

function item(partial: Partial<InboxItem>): InboxItem {
  return {
    id: "i1",
    workspace_id: "ws",
    recipient_type: "member",
    recipient_id: "u1",
    actor_type: "member",
    actor_id: "a1",
    type: "issue_assigned",
    severity: "info",
    issue_id: null,
    title: "",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: new Date().toISOString(),
    details: null,
    ...partial,
  };
}

describe("getInboxDisplayTitle", () => {
  it("prefers the server title when present", () => {
    const row = item({ title: "Server copy" });
    expect(getInboxDisplayTitle(row)).toBe("Server copy");
  });

  it("returns the raw title as-is when it is empty", () => {
    // The display layer deliberately does not invent fallback copy; callers
    // render an empty string and rely on body/actor context instead.
    const row = item({ title: "" });
    expect(getInboxDisplayTitle(row)).toBe("");
  });

  it("maps quota notices to a stable user-facing label", () => {
    const row = item({ type: "autopilot_quota_exceeded", title: "raw 5/5" });
    expect(getInboxDisplayTitle(row)).toBe("Autopilot run limit reached");
  });
});

describe("deduplicateInboxItems", () => {
  it("drops later rows that repeat an id", () => {
    const first = item({ id: "a", title: "first" });
    const second = item({ id: "a", title: "second" });
    const other = item({ id: "b" });
    expect(deduplicateInboxItems([first, second, other])).toEqual([
      first,
      other,
    ]);
  });
});
