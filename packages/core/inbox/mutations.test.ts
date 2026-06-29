import { describe, expect, it } from "vitest";
import { archiveInboxOptimisticItems } from "./mutations";
import type { InboxItem } from "../types";

function item(overrides: Partial<InboxItem>): InboxItem {
  return {
    id: "item-1",
    workspace_id: "ws-1",
    recipient_type: "member",
    recipient_id: "user-1",
    actor_type: null,
    actor_id: null,
    type: "new_comment",
    severity: "info",
    route: "inbox",
    issue_id: "issue-1",
    project_id: null,
    title: "Inbox item",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    muted_until: null,
    created_at: "2026-05-27T08:00:00Z",
    details: null,
    ...overrides,
  };
}

describe("archiveInboxOptimisticItems", () => {
  it("keeps issue-level archive for normal issue notifications", () => {
    const next = archiveInboxOptimisticItems(
      [
        item({ id: "comment", issue_id: "issue-1" }),
        item({ id: "manual", type: "manually_added", issue_id: "issue-1" }),
        item({ id: "other", issue_id: "issue-2" }),
      ],
      "comment",
    );

    expect(next?.map((i) => [i.id, i.archived])).toEqual([
      ["comment", true],
      ["manual", true],
      ["other", false],
    ]);
  });

  it("archives only the reminder row when the target is a standalone fired reminder", () => {
    const next = archiveInboxOptimisticItems(
      [
        item({
          id: "reminder",
          type: "reminder",
          issue_id: "issue-1",
          severity: "action_required",
        }),
        item({ id: "comment", issue_id: "issue-1" }),
        item({ id: "other", issue_id: "issue-2" }),
      ],
      "reminder",
    );

    expect(next?.map((i) => [i.id, i.archived])).toEqual([
      ["reminder", true],
      ["comment", false],
      ["other", false],
    ]);
  });
});
