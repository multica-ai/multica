import { describe, expect, it } from "vitest";
import { bucketizeInboxParent, INBOX_PARENT_GROUP_BY_OPTION } from "./parent-grouping";
import type { InboxItem } from "@multica/core/types";

function notif(overrides: Partial<InboxItem>): { kind: "notif"; item: InboxItem } {
  return {
    kind: "notif",
    item: {
      id: "n1",
      workspace_id: "w1",
      recipient_type: "member",
      recipient_id: "u1",
      actor_type: null,
      actor_id: null,
      type: "mentioned",
      severity: "info",
      route: "inbox",
      issue_id: "child-1",
      project_id: null,
      title: "x",
      body: null,
      issue_status: null,
      read: false,
      archived: false,
      muted_until: null,
      created_at: "2026-06-21T00:00:00Z",
      details: null,
      ...overrides,
    } as InboxItem,
  };
}

describe("bucketizeInboxParent", () => {
  it("buckets an item by its parent issue id and title", () => {
    const r = bucketizeInboxParent(notif({ parent_issue_id: "p1", parent_issue_title: "Parent A" }));
    expect(r).toEqual({ key: "p1", label: "Parent A", isFallback: false });
  });

  it("clusters two children of the same parent into the same key", () => {
    const a = bucketizeInboxParent(notif({ issue_id: "c1", parent_issue_id: "p1", parent_issue_title: "Parent A" }));
    const b = bucketizeInboxParent(notif({ issue_id: "c2", parent_issue_id: "p1", parent_issue_title: "Parent A" }));
    expect(a.key).toBe(b.key);
  });

  it("falls back to a Unknown label when the parent title is missing", () => {
    const r = bucketizeInboxParent(notif({ parent_issue_id: "p1", parent_issue_title: null }));
    expect(r).toEqual({ key: "p1", label: "Unknown parent", isFallback: false });
  });

  it("falls back to No parent issue when the issue has no parent", () => {
    const r = bucketizeInboxParent(notif({ parent_issue_id: null }));
    expect(r).toEqual({ key: "__no_parent__", label: "No parent issue", isFallback: true });
  });

  it("buckets non-issue rows (chat / channel) into the No parent fallback", () => {
    expect(bucketizeInboxParent({ kind: "chat", session: {} }).key).toBe("__no_parent__");
    expect(bucketizeInboxParent({ kind: "channel", channel: {} }).isFallback).toBe(true);
  });

  it("exposes a stable group-by option value", () => {
    expect(INBOX_PARENT_GROUP_BY_OPTION.value).toBe("parent");
  });
});
