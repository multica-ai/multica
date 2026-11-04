import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@multica/core/query-client";
import { api } from "@multica/core/api";
import { inboxKeys } from "@multica/core/inbox/queries";
import { issueUnreadKeys } from "@multica/core/issues/unread";
import type { InboxItem, IssueUnread } from "@multica/core/types";
import { useMarkIssueReadWhileViewing } from "./use-mark-issue-read-while-viewing";

vi.mock("@multica/core/api", () => ({ api: { listIssueUnread: vi.fn(), markIssueRead: vi.fn() } }));
vi.mock("../../common/use-app-foreground", () => ({ useAppForeground: () => true }));

const completed: IssueUnread = {
  issue_id: "issue-1",
  outcome: "completed",
  agent_id: "agent-1",
  task_id: "task-1",
  comment_id: "comment-1",
  created_at: "2026-10-31T10:00:00Z",
};

const inboxItem = (overrides: Partial<InboxItem>): InboxItem => ({
  id: "inbox-1", workspace_id: "ws-1", recipient_type: "member", recipient_id: "member-1",
  actor_type: "agent", actor_id: "agent-1", type: "new_comment", severity: "info", issue_id: "issue-1",
  title: "Issue", body: null, issue_status: null, read: false, archived: false,
  created_at: "2026-10-31T10:00:00Z", details: null, ...overrides,
});

let client: ReturnType<typeof createQueryClient>;
const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={client}>{children}</QueryClientProvider>
);

const settle = () => act(() => new Promise((resolve) => setTimeout(resolve, 50)));

beforeEach(() => {
  client = createQueryClient();
});

afterEach(() => {
  client.clear();
  vi.resetAllMocks();
});

describe("useMarkIssueReadWhileViewing", () => {
  it("does not resubmit when the write fails and the mark rolls back", async () => {
    client.setQueryData(issueUnreadKeys.all("ws-1"), [completed]);
    vi.mocked(api.listIssueUnread).mockResolvedValue([completed]);
    vi.mocked(api.markIssueRead).mockImplementation(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10));
      throw Object.assign(new Error("forbidden"), { status: 403 });
    });

    renderHook(() => useMarkIssueReadWhileViewing("ws-1", "issue-1"), { wrapper });
    await waitFor(() => expect(api.markIssueRead).toHaveBeenCalled());
    await act(() => new Promise((resolve) => setTimeout(resolve, 200)));

    expect(api.markIssueRead).toHaveBeenCalledTimes(1);
  });

  it("clears a newer mark that arrives while the issue stays open", async () => {
    client.setQueryData(issueUnreadKeys.all("ws-1"), [completed]);
    vi.mocked(api.listIssueUnread).mockResolvedValue([]);
    vi.mocked(api.markIssueRead).mockResolvedValue({ inboxRead: 0 });

    renderHook(() => useMarkIssueReadWhileViewing("ws-1", "issue-1"), { wrapper });
    await waitFor(() => expect(api.markIssueRead).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(api.listIssueUnread).toHaveBeenCalled());

    act(() => {
      client.setQueryData(issueUnreadKeys.all("ws-1"), [{ ...completed, created_at: "2026-10-31T10:05:00Z" }]);
    });
    await waitFor(() => expect(api.markIssueRead).toHaveBeenCalledTimes(2));
  });

  it("keeps a mark set by hand while the issue is open", async () => {
    client.setQueryData(issueUnreadKeys.all("ws-1"), []);
    client.setQueryData(inboxKeys.unreadSummary(), []);
    vi.mocked(api.listIssueUnread).mockResolvedValue([]);

    renderHook(() => useMarkIssueReadWhileViewing("ws-1", "issue-1"), { wrapper });
    act(() => {
      client.setQueryData(issueUnreadKeys.all("ws-1"), [
        { ...completed, outcome: "manual", agent_id: null, created_at: "2026-10-31T10:05:00Z" },
      ]);
    });
    await settle();

    expect(api.markIssueRead).not.toHaveBeenCalled();
  });

  it("reads the issue's Inbox notifications on opening without an unread mark", async () => {
    client.setQueryData(issueUnreadKeys.all("ws-1"), []);
    client.setQueryData(inboxKeys.list("ws-1"), [inboxItem({ id: "comment" })]);
    vi.mocked(api.markIssueRead).mockResolvedValue({ inboxRead: 1 });

    renderHook(() => useMarkIssueReadWhileViewing("ws-1", "issue-1"), { wrapper });
    await waitFor(() => expect(api.markIssueRead).toHaveBeenCalledTimes(1));

    expect(client.getQueryData<InboxItem[]>(inboxKeys.list("ws-1"))?.[0]?.read).toBe(true);
  });

  it("writes once per opening, so an Inbox item marked unread while open stays unread", async () => {
    client.setQueryData(issueUnreadKeys.all("ws-1"), []);
    client.setQueryData(inboxKeys.list("ws-1"), [inboxItem({ id: "comment" })]);
    vi.mocked(api.markIssueRead).mockResolvedValue({ inboxRead: 1 });

    const { rerender } = renderHook(() => useMarkIssueReadWhileViewing("ws-1", "issue-1"), { wrapper });
    await waitFor(() => expect(api.markIssueRead).toHaveBeenCalledTimes(1));
    act(() => {
      client.setQueryData(inboxKeys.list("ws-1"), [inboxItem({ id: "comment", read: false })]);
    });
    rerender();
    await settle();

    expect(api.markIssueRead).toHaveBeenCalledTimes(1);
  });

  it("does not write when the Inbox has nothing this issue can read", async () => {
    client.setQueryData(issueUnreadKeys.all("ws-1"), []);
    client.setQueryData(inboxKeys.unreadSummary(), [{ workspace_id: "ws-1", count: 1 }]);
    client.setQueryData(inboxKeys.list("ws-1"), [
      inboxItem({ id: "assigned", type: "issue_assigned", severity: "action_required" }),
      inboxItem({ id: "comment" }),
    ]);

    renderHook(() => useMarkIssueReadWhileViewing("ws-1", "issue-1"), { wrapper });
    await settle();

    expect(api.markIssueRead).not.toHaveBeenCalled();
    expect(client.getQueryData<InboxItem[]>(inboxKeys.list("ws-1"))?.every((item) => !item.read)).toBe(true);
  });
});
