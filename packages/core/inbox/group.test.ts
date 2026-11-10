import { describe, expect, it } from "vitest";
import type { InboxItem } from "../types";
import { inboxGroupUnread, isInboxNotice, summarizeInboxGroups } from "./group";

let seq = 0;
function item(overrides: Partial<InboxItem>): InboxItem {
  seq += 1;
  return {
    id: `inbox-${seq}`,
    workspace_id: "workspace-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: "issue-1",
    title: "Issue title",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2026-01-01T00:00:00Z",
    details: null,
    ...overrides,
  } as InboxItem;
}

describe("summarizeInboxGroups", () => {
  it("counts an issue's other notifications and previews the newest comment", () => {
    const summaries = summarizeInboxGroups([
      item({ type: "agent_completed", created_at: "2026-01-03T00:00:00Z" }),
      item({ body: "Fixed the\n flaky   test", created_at: "2026-01-02T00:00:00Z" }),
      item({ body: "older", created_at: "2026-01-01T00:00:00Z" }),
      item({ issue_id: "issue-2", type: "issue_assigned" }),
    ]);

    expect(summaries.get("issue-1")).toEqual({ more: 2, preview: "Fixed the flaky test" });
    expect(summaries.get("issue-2")).toEqual({ more: 0, preview: null });
  });
});

describe("inboxGroupUnread", () => {
  it("starts at the oldest unread comment or run on the issue", () => {
    const rows = [
      item({ created_at: "2026-01-04T00:00:00Z", details: { comment_id: "c-new" } }),
      item({ type: "agent_completed", created_at: "2026-01-03T00:00:00Z", details: { task_id: "t-1" } }),
      item({ created_at: "2026-01-02T00:00:00Z", details: { comment_id: "c-read" }, read: true }),
      item({ issue_id: "issue-2", details: { comment_id: "c-other" } }),
    ];
    expect(inboxGroupUnread(rows, "issue-1")).toEqual({ target: "t-1", count: 2 });
  });

  it("leaves out a reaction and rows that point nowhere in the timeline", () => {
    const rows = [
      item({ type: "reaction_added", details: { comment_id: "c-mine", emoji: "+1" } }),
      item({ type: "issue_assigned" }),
    ];
    expect(inboxGroupUnread(rows, "issue-1")).toEqual({ target: null, count: 0 });
  });
});

describe("isInboxNotice", () => {
  it("separates notices from notifications that wait on the user", () => {
    expect(isInboxNotice("reaction_added")).toBe(true);
    expect(isInboxNotice("quick_create_done")).toBe(true);
    expect(isInboxNotice("mentioned")).toBe(false);
    expect(isInboxNotice("agent_completed")).toBe(false);
  });
});
