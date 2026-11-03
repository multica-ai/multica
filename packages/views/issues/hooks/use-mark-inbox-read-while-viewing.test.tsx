import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@multica/core/query-client";
import { api } from "@multica/core/api";
import { inboxKeys } from "@multica/core/inbox/queries";
import { onInboxNew } from "@multica/core/inbox/ws-updaters";
import type { InboxItem } from "@multica/core/types";
import { useMarkInboxReadWhileViewing } from "./use-mark-inbox-read-while-viewing";

vi.mock("@multica/core/api", () => ({
  api: {
    markIssueInboxRead: vi.fn(),
    getInboxUnreadSummary: vi.fn(),
    listInbox: vi.fn(),
    listArchivedInbox: vi.fn(),
  },
}));
vi.mock("../../common/use-app-foreground", () => ({ useAppForeground: () => true }));

function item(overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: "ws-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "member",
    actor_id: "member-2",
    type: "mentioned",
    severity: "info",
    issue_id: "issue-1",
    title: "Issue",
    body: null,
    issue_status: null,
    section: "action",
    read: false,
    archived: false,
    created_at: "2026-11-02T08:00:00Z",
    details: null,
    ...overrides,
  };
}

let client: ReturnType<typeof createQueryClient>;
const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={client}>{children}</QueryClientProvider>
);

beforeEach(() => {
  client = createQueryClient();
  vi.mocked(api.markIssueInboxRead).mockResolvedValue({ count: 1 });
  vi.mocked(api.getInboxUnreadSummary).mockResolvedValue([]);
  vi.mocked(api.listInbox).mockResolvedValue([]);
});

afterEach(() => {
  client.clear();
  vi.resetAllMocks();
});

const settle = () => act(() => new Promise((resolve) => setTimeout(resolve, 50)));

describe("useMarkInboxReadWhileViewing", () => {
  it("marks the issue's inbox read once when it is opened", async () => {
    client.setQueryData(inboxKeys.unreadSummary(), [{ workspace_id: "ws-1", count: 1 }]);
    client.setQueryData(inboxKeys.list("ws-1"), [item()]);

    const { rerender } = renderHook(() => useMarkInboxReadWhileViewing("ws-1", "issue-1"), { wrapper });
    await waitFor(() => expect(api.markIssueInboxRead).toHaveBeenCalledWith("issue-1"));
    rerender();
    await settle();

    expect(api.markIssueInboxRead).toHaveBeenCalledTimes(1);
  });

  it("skips the request when the workspace has nothing unread", async () => {
    client.setQueryData(inboxKeys.unreadSummary(), []);

    renderHook(() => useMarkInboxReadWhileViewing("ws-1", "issue-1"), { wrapper });
    await settle();

    expect(api.markIssueInboxRead).not.toHaveBeenCalled();
  });

  it("skips the request when the issue only has activity or read rows", async () => {
    client.setQueryData(inboxKeys.unreadSummary(), [{ workspace_id: "ws-1", count: 3 }]);
    client.setQueryData(inboxKeys.list("ws-1"), [
      item({ id: "status", type: "status_changed", section: "activity" }),
      item({ id: "read-mention", read: true }),
      item({ id: "elsewhere", issue_id: "issue-2" }),
    ]);

    renderHook(() => useMarkInboxReadWhileViewing("ws-1", "issue-1"), { wrapper });
    await settle();

    expect(api.markIssueInboxRead).not.toHaveBeenCalled();
  });

  it("does not undo a manual mark-unread while the issue stays open", async () => {
    client.setQueryData(inboxKeys.unreadSummary(), [{ workspace_id: "ws-1", count: 1 }]);
    client.setQueryData(inboxKeys.list("ws-1"), [item()]);

    renderHook(() => useMarkInboxReadWhileViewing("ws-1", "issue-1"), { wrapper });
    await waitFor(() => expect(api.markIssueInboxRead).toHaveBeenCalledTimes(1));
    act(() => {
      client.setQueryData(inboxKeys.list("ws-1"), [item({ read: false })]);
      client.setQueryData(inboxKeys.unreadSummary(), [{ workspace_id: "ws-1", count: 2 }]);
    });
    await settle();

    expect(api.markIssueInboxRead).toHaveBeenCalledTimes(1);
  });

  it("marks again when another issue is opened", async () => {
    client.setQueryData(inboxKeys.unreadSummary(), [{ workspace_id: "ws-1", count: 2 }]);
    vi.mocked(api.getInboxUnreadSummary).mockResolvedValue([{ workspace_id: "ws-1", count: 1 }]);

    const { rerender } = renderHook(
      ({ issueId }) => useMarkInboxReadWhileViewing("ws-1", issueId),
      { wrapper, initialProps: { issueId: "issue-1" } },
    );
    await waitFor(() => expect(api.markIssueInboxRead).toHaveBeenCalledWith("issue-1"));
    rerender({ issueId: "issue-2" });
    await waitFor(() => expect(api.markIssueInboxRead).toHaveBeenCalledWith("issue-2"));
  });

  it("ignores an invalidated list that only holds the issue's old read row", async () => {
    vi.mocked(api.getInboxUnreadSummary).mockResolvedValue([{ workspace_id: "ws-1", count: 1 }]);
    vi.mocked(api.listInbox).mockResolvedValue([item({ id: "new-mention" })]);
    client.setQueryData(inboxKeys.list("ws-1"), [item({ id: "previous-mention", read: true })]);
    await onInboxNew(client, "ws-1", item({ id: "new-mention" }));
    expect(client.getQueryState(inboxKeys.list("ws-1"))?.isInvalidated).toBe(true);
    client.setQueryData(inboxKeys.unreadSummary(), [{ workspace_id: "ws-1", count: 1 }]);

    renderHook(() => useMarkInboxReadWhileViewing("ws-1", "issue-1"), { wrapper });

    await waitFor(() => expect(api.markIssueInboxRead).toHaveBeenCalledWith("issue-1"));
  });

  it("waits for an invalidated zero summary to refresh before deciding", async () => {
    vi.mocked(api.getInboxUnreadSummary).mockResolvedValue([{ workspace_id: "ws-1", count: 1 }]);
    client.setQueryData(inboxKeys.unreadSummary(), []);
    await client.invalidateQueries({ queryKey: inboxKeys.unreadSummary() });

    renderHook(() => useMarkInboxReadWhileViewing("ws-1", "issue-1"), { wrapper });

    await waitFor(() => expect(api.getInboxUnreadSummary).toHaveBeenCalled());
    await waitFor(() => expect(api.markIssueInboxRead).toHaveBeenCalledWith("issue-1"));
  });
});
