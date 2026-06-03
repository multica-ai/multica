import { describe, it, expect } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { onInboxIssueDeleted, onInboxIssueStatusChanged } from "./ws-updaters";
import { inboxKeys } from "./queries";
import type { InboxItem } from "../types";

const wsId = "ws-1";

function makeItem(
  id: string,
  issueId: string | null,
  overrides: Partial<InboxItem> = {},
): InboxItem {
  return {
    id,
    workspace_id: wsId,
    recipient_type: "member",
    recipient_id: "user-1",
    actor_type: null,
    actor_id: null,
    type: "mentioned",
    severity: "info",
    issue_id: issueId,
    title: `item ${id}`,
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2025-01-01T00:00:00Z",
    details: null,
    ...overrides,
  };
}

describe("onInboxIssueDeleted", () => {
  it("removes all inbox items referencing the deleted issue", () => {
    const qc = new QueryClient();
    const items = [
      makeItem("i1", "issue-a"),
      makeItem("i2", "issue-a"),
      makeItem("i3", "issue-b"),
      makeItem("i4", null),
    ];
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), items);

    onInboxIssueDeleted(qc, wsId, "issue-a");

    const after = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
    expect(after?.map((i) => i.id)).toEqual(["i3", "i4"]);
  });

  it("is a no-op when the inbox cache is empty", () => {
    const qc = new QueryClient();
    expect(() => onInboxIssueDeleted(qc, wsId, "issue-a")).not.toThrow();
    expect(qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))).toBeUndefined();
  });
});

describe("onInboxIssueStatusChanged", () => {
  it("updates issue_status only for items referencing the issue", () => {
    const qc = new QueryClient();
    const items = [
      makeItem("i1", "issue-a", { issue_status: "todo" }),
      makeItem("i2", "issue-b", { issue_status: "todo" }),
    ];
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), items);

    onInboxIssueStatusChanged(qc, wsId, "issue-a", "done");

    const after = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
    expect(after?.find((i) => i.id === "i1")?.issue_status).toBe("done");
    expect(after?.find((i) => i.id === "i2")?.issue_status).toBe("todo");
  });
});

// ---------------------------------------------------------------------------
// deduplicateInboxItems – comment_id forwarding
// ---------------------------------------------------------------------------
import { deduplicateInboxItems } from "./queries";

describe("deduplicateInboxItems", () => {
  it("returns newest item when it already has a comment_id", () => {
    const items = [
      makeItem("i2", "issue-a", {
        type: "new_comment",
        created_at: "2025-01-02T00:00:00Z",
        details: { comment_id: "c2" },
      }),
      makeItem("i1", "issue-a", {
        type: "mentioned",
        created_at: "2025-01-01T00:00:00Z",
        details: { comment_id: "c1" },
      }),
    ];
    const result = deduplicateInboxItems(items);
    expect(result).toHaveLength(1);
    expect(result[0]?.id).toBe("i2");
    expect(result[0]?.details?.comment_id).toBe("c2");
  });

  it("injects best comment_id when newest item lacks one (e.g. task_completed)", () => {
    const items = [
      makeItem("i3", "issue-a", {
        type: "task_completed",
        created_at: "2025-01-03T00:00:00Z",
        details: null,
      }),
      makeItem("i2", "issue-a", {
        type: "new_comment",
        created_at: "2025-01-02T00:00:00Z",
        details: { comment_id: "c2" },
      }),
      makeItem("i1", "issue-a", {
        type: "mentioned",
        created_at: "2025-01-01T00:00:00Z",
        details: { comment_id: "c1" },
      }),
    ];
    const result = deduplicateInboxItems(items);
    expect(result).toHaveLength(1);
    // Representative is the newest (i3)
    expect(result[0]?.id).toBe("i3");
    // But comment_id is lifted from the most recent item that had one
    expect(result[0]?.details?.comment_id).toBe("c2");
  });

  it("leaves items without any comment_id unchanged", () => {
    const items = [
      makeItem("i2", "issue-a", {
        type: "agent_completed",
        created_at: "2025-01-02T00:00:00Z",
        details: null,
      }),
      makeItem("i1", "issue-a", {
        type: "task_completed",
        created_at: "2025-01-01T00:00:00Z",
        details: null,
      }),
    ];
    const result = deduplicateInboxItems(items);
    expect(result).toHaveLength(1);
    expect(result[0]?.id).toBe("i2");
    expect(result[0]?.details?.comment_id).toBeUndefined();
  });

  it("deduplicates channel mention rows by channel message instead of null issue id", () => {
    const items = [
      makeItem("newest-same-message", null, {
        created_at: "2025-01-03T00:00:00Z",
        details: {
          source_type: "channel_message",
          channel_id: "channel-a",
          message_id: "message-a",
        },
      }),
      makeItem("older-same-message", null, {
        created_at: "2025-01-02T00:00:00Z",
        details: {
          source_type: "channel_message",
          channel_id: "channel-a",
          message_id: "message-a",
        },
      }),
      makeItem("different-message", null, {
        created_at: "2025-01-01T00:00:00Z",
        details: {
          source_type: "channel_message",
          channel_id: "channel-a",
          message_id: "message-b",
        },
      }),
    ];

    const result = deduplicateInboxItems(items);

    expect(result.map((i) => i.id)).toEqual([
      "newest-same-message",
      "different-message",
    ]);
  });
});
