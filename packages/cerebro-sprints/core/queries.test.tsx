import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const mockCompleteSprint = vi.hoisted(() => vi.fn());

vi.mock("./api", async () => {
  const actual = await vi.importActual<typeof import("./api")>("./api");
  return {
    ...actual,
    completeSprint: mockCompleteSprint,
  };
});

import { useCompleteSprint } from "./queries";

function makeWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useCompleteSprint", () => {
  it("invalidates issue lists when completion can move issues between boards", async () => {
    mockCompleteSprint.mockResolvedValueOnce({
      sprint: {
        id: "sprint-1",
        workspace_id: "ws-1",
        project_id: "project-1",
        name: "Sprint 1",
        sequence_no: 1,
        status: "done",
        start_date: "2026-07-01",
        end_date: "2026-07-14",
        created_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-08T00:00:00Z",
      },
      issues_moved: 1,
    });
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    const { result } = renderHook(() => useCompleteSprint("ws-1", "project-1"), {
      wrapper: makeWrapper(qc),
    });

    await act(async () => {
      result.current.mutate({
        id: "sprint-1",
        payload: { incomplete_issues_action: "backlog" },
      });
    });

    await waitFor(() => expect(mockCompleteSprint).toHaveBeenCalledTimes(1));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["issues"] });
  });
});
