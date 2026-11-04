import { act, renderHook } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@multica/core/query-client";
import { api } from "@multica/core/api";
import { inboxKeys } from "@multica/core/inbox";
import type { InboxItem, InboxWorkspaceUnread } from "@multica/core/types";
import { useMarkIssueInboxReadWhileViewing } from "./use-mark-issue-inbox-read-while-viewing";

vi.mock("@multica/core/api", () => ({
  api: { markIssueInboxRead: vi.fn(), getInboxUnreadSummary: vi.fn() },
}));

const foreground = vi.hoisted(() => ({ value: true }));
vi.mock("../../common/use-app-foreground", () => ({ useAppForeground: () => foreground.value }));

const wsHandlers = vi.hoisted(() => new Map<string, (payload: unknown) => void>());
vi.mock("@multica/core/realtime", () => ({
  useWSEvent: (event: string, handler: (payload: unknown) => void) => {
    wsHandlers.set(event, handler);
  },
}));

function notification(issueId: string | null): { item: Partial<InboxItem> } {
  return { item: { id: `inbox-${Math.random()}`, issue_id: issueId, read: false } };
}

function deliver(payload: unknown) {
  act(() => wsHandlers.get("inbox:new")?.(payload));
}

const settle = () => act(() => new Promise((resolve) => setTimeout(resolve, 20)));

let client: ReturnType<typeof createQueryClient>;
const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={client}>{children}</QueryClientProvider>
);

function setUnreadSummary(count: number) {
  client.setQueryData<InboxWorkspaceUnread[]>(
    inboxKeys.unreadSummary(),
    count > 0 ? [{ workspace_id: "ws-1", count }] : [],
  );
}

beforeEach(() => {
  client = createQueryClient();
  foreground.value = true;
  wsHandlers.clear();
  vi.mocked(api.markIssueInboxRead).mockResolvedValue({ count: 1 });
  vi.mocked(api.getInboxUnreadSummary).mockResolvedValue([]);
});

afterEach(() => {
  client.clear();
  vi.resetAllMocks();
});

describe("useMarkIssueInboxReadWhileViewing", () => {
  it("reads the issue's notifications once when it opens", async () => {
    setUnreadSummary(3);

    const { rerender } = renderHook(() => useMarkIssueInboxReadWhileViewing("ws-1", "issue-1"), { wrapper });
    await settle();
    rerender();
    await settle();

    expect(api.markIssueInboxRead).toHaveBeenCalledTimes(1);
    expect(api.markIssueInboxRead).toHaveBeenCalledWith("issue-1");
  });

  it("sends nothing while the workspace has no unread notifications", async () => {
    setUnreadSummary(0);

    renderHook(() => useMarkIssueInboxReadWhileViewing("ws-1", "issue-1"), { wrapper });
    await settle();

    expect(api.markIssueInboxRead).not.toHaveBeenCalled();
  });

  it("reads a notification on the open issue as it arrives, and ignores other issues", async () => {
    setUnreadSummary(1);

    renderHook(() => useMarkIssueInboxReadWhileViewing("ws-1", "issue-1"), { wrapper });
    await settle();
    expect(api.markIssueInboxRead).toHaveBeenCalledTimes(1);
    // The write read the only unread issue; its refresh brings the count to 0.
    act(() => setUnreadSummary(0));

    // Realtime sync refreshes the summary on every inbox:new; mirror that.
    // The count rising for another issue is not a reason to write again.
    deliver(notification("issue-2"));
    deliver(notification(null));
    act(() => setUnreadSummary(2));
    await settle();
    expect(api.markIssueInboxRead).toHaveBeenCalledTimes(1);

    deliver(notification("issue-1"));
    act(() => setUnreadSummary(3));
    await settle();
    expect(api.markIssueInboxRead).toHaveBeenCalledTimes(2);
  });

  it("holds an arrival until the unread count reflects it", async () => {
    setUnreadSummary(1);

    renderHook(() => useMarkIssueInboxReadWhileViewing("ws-1", "issue-1"), { wrapper });
    await settle();
    act(() => setUnreadSummary(0));

    deliver(notification("issue-1"));
    await settle();
    expect(api.markIssueInboxRead).toHaveBeenCalledTimes(1);

    act(() => setUnreadSummary(1));
    await settle();
    expect(api.markIssueInboxRead).toHaveBeenCalledTimes(2);
  });

  it("waits for the app to come to the foreground", async () => {
    setUnreadSummary(1);
    foreground.value = false;

    const { rerender } = renderHook(() => useMarkIssueInboxReadWhileViewing("ws-1", "issue-1"), { wrapper });
    deliver(notification("issue-1"));
    await settle();
    expect(api.markIssueInboxRead).not.toHaveBeenCalled();

    foreground.value = true;
    rerender();
    await settle();
    expect(api.markIssueInboxRead).toHaveBeenCalledTimes(1);
  });

  it("does nothing where the host reads the inbox itself", async () => {
    setUnreadSummary(1);

    renderHook(() => useMarkIssueInboxReadWhileViewing("ws-1", "issue-1", false), { wrapper });
    deliver(notification("issue-1"));
    await settle();

    expect(api.markIssueInboxRead).not.toHaveBeenCalled();
  });

  it("does not resubmit after a failed write", async () => {
    setUnreadSummary(1);
    vi.mocked(api.markIssueInboxRead).mockRejectedValue(Object.assign(new Error("not found"), { status: 404 }));

    const { rerender } = renderHook(() => useMarkIssueInboxReadWhileViewing("ws-1", "issue-1"), { wrapper });
    await settle();
    rerender();
    await settle();

    expect(api.markIssueInboxRead).toHaveBeenCalledTimes(1);
  });
});
