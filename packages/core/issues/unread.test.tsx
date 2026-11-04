/**
 * @vitest-environment jsdom
 */
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "../query-client";
import { api } from "../api";
import { inboxKeys } from "../inbox/queries";
import type { InboxItem, IssueUnread } from "../types";
import { issueUnreadKeys, issueUnreadTarget, refreshIssueUnread, useIssueUnread, useMarkIssueRead } from "./unread";

vi.mock("../api", () => ({ api: { listIssueUnread: vi.fn(), markIssueRead: vi.fn() } }));

const unread: IssueUnread = {
  issue_id: "issue-1",
  outcome: "completed",
  agent_id: "agent-1",
  task_id: "task-1",
  comment_id: "comment-1",
  created_at: "2026-10-31T10:00:00Z",
};

afterEach(() => vi.resetAllMocks());

describe("issueUnreadTarget", () => {
  it("points at the run's last comment, else the run, and nowhere for a manual mark", () => {
    expect(issueUnreadTarget(unread)).toBe("comment-1");
    expect(issueUnreadTarget({ ...unread, comment_id: null })).toBe("task-1");
    expect(issueUnreadTarget({ ...unread, outcome: "manual", agent_id: null, task_id: null, comment_id: null })).toBeNull();
    expect(issueUnreadTarget(null)).toBeNull();
  });
});

describe("refreshIssueUnread", () => {
  it("is not answered by a first load that started before the change", async () => {
    const client = createQueryClient();
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    let resolveInitial!: (items: IssueUnread[]) => void;
    vi.mocked(api.listIssueUnread)
      .mockImplementationOnce(() => new Promise((resolve) => (resolveInitial = resolve)))
      .mockResolvedValue([unread]);

    const { result } = renderHook(() => useIssueUnread("ws-1", "issue-1"), { wrapper });
    await waitFor(() => expect(api.listIssueUnread).toHaveBeenCalledTimes(1));
    await act(async () => {
      const refreshed = refreshIssueUnread(client, "ws-1");
      resolveInitial([]);
      await refreshed;
    });

    await waitFor(() => expect(result.current).toEqual(unread));
    client.clear();
  });
});

describe("useMarkIssueRead", () => {
  const inboxItem = (overrides: Partial<InboxItem>): InboxItem => ({
    id: "inbox-1", workspace_id: "ws-1", recipient_type: "member", recipient_id: "member-1",
    actor_type: "agent", actor_id: "agent-1", type: "new_comment", severity: "info", issue_id: "issue-1",
    title: "Issue", body: null, issue_status: null, read: false, archived: false,
    created_at: "2026-10-31T10:00:00Z", details: null, ...overrides,
  });

  function setup() {
    const client = createQueryClient();
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useMarkIssueRead("ws-1"), { wrapper });
    return { client, result };
  }

  it("reads the issue's Inbox rows and refreshes the badge from the server", async () => {
    const { client, result } = setup();
    client.setQueryData(issueUnreadKeys.all("ws-1"), [unread]);
    client.setQueryData(inboxKeys.list("ws-1"), [
      inboxItem({ id: "comment" }),
      inboxItem({ id: "other-issue", issue_id: "issue-2" }),
    ]);
    client.setQueryData(inboxKeys.unreadSummary(), [{ workspace_id: "ws-1", count: 2 }]);
    vi.mocked(api.markIssueRead).mockResolvedValue({ inboxRead: 1 });

    await act(() => result.current.mutateAsync("issue-1"));

    const list = client.getQueryData<InboxItem[]>(inboxKeys.list("ws-1"));
    expect(list?.map((item) => [item.id, item.read])).toEqual([["comment", true], ["other-issue", false]]);
    expect(client.getQueryData(issueUnreadKeys.all("ws-1"))).toEqual([]);
    expect(client.getQueryState(inboxKeys.unreadSummary())?.isInvalidated).toBe(true);
    client.clear();
  });

  it("re-reads nothing when the issue had no mark and the Inbox nothing to read", async () => {
    const { client, result } = setup();
    client.setQueryData(issueUnreadKeys.all("ws-1"), []);
    client.setQueryData(inboxKeys.unreadSummary(), [{ workspace_id: "ws-1", count: 2 }]);
    vi.mocked(api.markIssueRead).mockResolvedValue({ inboxRead: 0 });

    await act(() => result.current.mutateAsync("issue-1"));

    expect(client.getQueryState(issueUnreadKeys.all("ws-1"))?.isInvalidated).toBe(false);
    expect(client.getQueryState(inboxKeys.unreadSummary())?.isInvalidated).toBe(false);
    client.clear();
  });

  it("refreshes the badge when the server read rows the cache did not hold", async () => {
    const { client, result } = setup();
    client.setQueryData(issueUnreadKeys.all("ws-1"), []);
    client.setQueryData(inboxKeys.unreadSummary(), [{ workspace_id: "ws-1", count: 2 }]);
    vi.mocked(api.markIssueRead).mockResolvedValue({ inboxRead: 2 });

    await act(() => result.current.mutateAsync("issue-1"));

    expect(client.getQueryState(inboxKeys.unreadSummary())?.isInvalidated).toBe(true);
    client.clear();
  });
});
