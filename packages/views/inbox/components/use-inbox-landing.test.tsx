import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { InboxItem, IssueUnread } from "@multica/core/types";
import { inboxLandingTarget, useInboxLanding } from "./use-inbox-landing";

const unreadState = vi.hoisted(() => ({ items: undefined as IssueUnread[] | undefined }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ select }: { select: (items: IssueUnread[]) => unknown }) => ({
    data: unreadState.items === undefined ? undefined : select(unreadState.items),
    isError: false,
  }),
}));
vi.mock("@multica/core/issues/unread", () => ({
  issueUnreadOptions: () => ({ queryKey: ["issue-unread"] }),
  issueUnreadTarget: (unread: IssueUnread | null) => unread?.comment_id ?? unread?.task_id ?? null,
}));

function row(type: string, details: Record<string, string> = {}): InboxItem {
  return { id: `inbox-${type}`, issue_id: "issue-a", type, details } as InboxItem;
}

function unread(fields: Partial<IssueUnread>): IssueUnread {
  return {
    issue_id: "issue-a", outcome: "completed", agent_id: "agent-1", task_id: "task-1",
    comment_id: null, created_at: "2026-01-01T00:00:00Z", ...fields,
  };
}

describe("inboxLandingTarget", () => {
  it("lands a hand-back on what the run left", () => {
    const item = row("agent_completed", { task_id: "task-1" });
    expect(inboxLandingTarget(item, unread({ comment_id: "comment-9" }))).toBe("comment-9");
    expect(inboxLandingTarget(item, unread({ comment_id: null }))).toBe("task-1");
  });

  it("falls back to the run the row names once the issue is read", () => {
    expect(inboxLandingTarget(row("task_failed", { task_id: "task-2" }), null)).toBe("task-2");
    expect(inboxLandingTarget(row("issue_assigned"), null)).toBeNull();
  });

  it("keeps a mention on the comment that named the viewer", () => {
    const item = row("mentioned", { comment_id: "comment-1" });
    expect(inboxLandingTarget(item, unread({ comment_id: "comment-9" }))).toBe("comment-1");
  });

  it("sends a mention in the description to the unread result", () => {
    expect(inboxLandingTarget(row("mentioned"), unread({ comment_id: "comment-9" }))).toBe("comment-9");
  });

  it("leaves Connected Apps rows on their own request", () => {
    expect(inboxLandingTarget(row("app_action_requested", { request_id: "req-1" }), unread({}))).toBeNull();
  });
});

describe("useInboxLanding", () => {
  it("waits for the unread list, then keeps its landing after the mark clears", () => {
    unreadState.items = undefined;
    const item = row("agent_completed", { task_id: "task-1" });
    const { result, rerender } = renderHook(() => useInboxLanding("ws", item));
    expect(result.current).toBeUndefined();

    unreadState.items = [unread({ comment_id: "comment-9" })];
    rerender();
    expect(result.current).toBe("comment-9");

    unreadState.items = [];
    rerender();
    expect(result.current).toBe("comment-9");
  });

  it("settles a mention without the unread list", () => {
    unreadState.items = undefined;
    const { result } = renderHook(() => useInboxLanding("ws", row("mentioned", { comment_id: "comment-1" })));
    expect(result.current).toBe("comment-1");
  });
});
