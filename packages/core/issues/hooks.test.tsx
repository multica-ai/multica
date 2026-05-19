/**
 * @vitest-environment jsdom
 */
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { TimelineEntry } from "../types";
import { useIssueTimelineEntries } from "./hooks";

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useIssueTimelineEntries", () => {
  let qc: QueryClient;
  let listTimeline: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    listTimeline = vi.fn();
    setApiInstance({ listTimeline } as unknown as ApiClient);
  });

  it("returns the flat timeline array from ApiClient.listTimeline", async () => {
    const entries: TimelineEntry[] = [
      {
        type: "comment",
        id: "comment-1",
        actor_type: "member",
        actor_id: "member-1",
        content: "Visible comment",
        created_at: "2026-05-19T10:00:00Z",
      },
      {
        type: "activity",
        id: "activity-1",
        actor_type: "member",
        actor_id: "member-1",
        action: "updated",
        created_at: "2026-05-19T10:01:00Z",
      },
    ];
    listTimeline.mockResolvedValue(entries);

    const { result } = renderHook(
      () => useIssueTimelineEntries("workspace-1", "issue-1"),
      { wrapper: createWrapper(qc) },
    );

    await waitFor(() => expect(result.current.data).toEqual(entries));
    expect(listTimeline).toHaveBeenCalledWith("issue-1");
  });

  it("keeps consumers on an array when the API result is malformed", async () => {
    listTimeline.mockResolvedValue({ entries: [] });

    const { result } = renderHook(
      () => useIssueTimelineEntries("workspace-1", "issue-1"),
      { wrapper: createWrapper(qc) },
    );

    await waitFor(() => expect(result.current.data).toEqual([]));
  });
});
