import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@multica/core/query-client";
import { api } from "@multica/core/api";
import { issueUnreadKeys } from "@multica/core/issues/unread";
import type { IssueUnread } from "@multica/core/types";
import { useMarkIssueReadWhileViewing } from "./use-mark-issue-read-while-viewing";

vi.mock("@multica/core/api", () => ({ api: { listIssueUnread: vi.fn(), markIssueRead: vi.fn() } }));
vi.mock("../../common/use-app-foreground", () => ({ useAppForeground: () => true }));

const completed: IssueUnread = {
  issue_id: "issue-1",
  outcome: "completed",
  agent_id: "agent-1",
  created_at: "2026-10-31T10:00:00Z",
};

let client: ReturnType<typeof createQueryClient>;
const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={client}>{children}</QueryClientProvider>
);

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
    vi.mocked(api.markIssueRead).mockResolvedValue(undefined);

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
    vi.mocked(api.listIssueUnread).mockResolvedValue([]);

    renderHook(() => useMarkIssueReadWhileViewing("ws-1", "issue-1"), { wrapper });
    act(() => {
      client.setQueryData(issueUnreadKeys.all("ws-1"), [
        { ...completed, outcome: "manual", agent_id: null, created_at: "2026-10-31T10:05:00Z" },
      ]);
    });
    await act(() => new Promise((resolve) => setTimeout(resolve, 50)));

    expect(api.markIssueRead).not.toHaveBeenCalled();
  });
});
