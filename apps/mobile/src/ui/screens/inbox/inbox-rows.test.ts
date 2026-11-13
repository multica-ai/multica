import { describe, expect, it } from "vitest";
import type { InboxItem } from "@multica/core/types";
import type { ActorResolver } from "../../model/people";
import { stringsFor } from "../../strings";
import { inboxRows } from "./inbox-rows";

const now = Date.parse("2026-11-14T12:00:00Z");
const strings = stringsFor("en");
const resolveActor: ActorResolver = (type, id) => ({ kind: type === "agent" ? "agent" : "member", name: id === "a1" ? "Lambda" : "Ann" });
const statusLabel = (key: string) => (key === "in_review" ? "In review" : key);

function item(overrides: Partial<InboxItem>): InboxItem {
  return {
    id: "n1",
    workspace_id: "w",
    recipient_type: "member",
    recipient_id: "u",
    actor_type: "agent",
    actor_id: "a1",
    type: "new_comment",
    severity: "info",
    issue_id: "i1",
    title: "Multica iOS",
    body: null,
    issue_status: "in_progress",
    read: false,
    archived: false,
    created_at: "2026-11-14T11:58:00Z",
    details: null,
    ...overrides,
  } as InboxItem;
}

describe("inboxRows", () => {
  it("says who did what, with the comment body", () => {
    const [row] = inboxRows([item({ body: "PR is  ready\nfor review" })], resolveActor, statusLabel, strings, now);
    expect(row).toMatchObject({
      issueId: "i1",
      title: "Multica iOS",
      summary: "Lambda commented: PR is ready for review",
      time: "2m",
      badge: { kind: "icon", icon: "Quote" },
      unread: true,
    });
  });

  it("names the new status and shows its glyph", () => {
    const [row] = inboxRows([item({ type: "status_changed", details: { to: "in_review" }, read: true })], resolveActor, statusLabel, strings, now);
    expect(row?.summary).toBe("Lambda moved it to In review");
    expect(row?.badge).toEqual({ kind: "status", status: "in_review" });
    expect(row?.unread).toBe(false);
  });

  it("falls back to a generic verb for types it does not know", () => {
    const [row] = inboxRows([item({ type: "app_action_result" })], resolveActor, statusLabel, strings, now);
    expect(row?.summary).toBe("Lambda updated the issue");
  });
});
