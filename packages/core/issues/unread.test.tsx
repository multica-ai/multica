/**
 * @vitest-environment jsdom
 */
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "../query-client";
import { api } from "../api";
import type { IssueUnread } from "../types";
import { issueUnreadTarget, refreshIssueUnread, useIssueUnread } from "./unread";

vi.mock("../api", () => ({ api: { listIssueUnread: vi.fn() } }));

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
