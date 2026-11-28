import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { getTestResources } from "../../test/i18n";
import type { TimelineEntry } from "@multica/core/types";

const { listTimeline } = vi.hoisted(() => ({ listTimeline: vi.fn() }));
vi.mock("@multica/core/api", () => ({ api: { listTimeline } }));
vi.mock("@multica/core/realtime", () => ({
  useWSEvent: () => {},
  useWSReconnect: () => {},
}));
vi.mock("@multica/core/issues/mutations", () => {
  const mutation = () => ({ mutate: vi.fn(), mutateAsync: vi.fn() });
  return {
    useCreateComment: mutation,
    useUpdateComment: mutation,
    useDeleteComment: mutation,
    useResolveComment: mutation,
    useToggleCommentReaction: mutation,
  };
});

import { useIssueTimeline } from "./use-issue-timeline";

const client = new QueryClient({
  defaultOptions: { queries: { retry: false, gcTime: Infinity } },
});
function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={getTestResources()}>
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    </I18nProvider>
  );
}
afterEach(() => {
  client.clear();
  vi.resetAllMocks();
});

it("exposes a failed Activity read and allows retrying the real timeline query", async () => {
  const failure = new Error("Timeline request failed");
  listTimeline.mockRejectedValueOnce(failure).mockResolvedValueOnce([]);
  const { result } = renderHook(() => useIssueTimeline("issue-1"), {
    wrapper: Wrapper,
  });

  await waitFor(() => expect(result.current.loading).toBe(false));
  await waitFor(() => expect(result.current.error).toBe(failure));
  expect(listTimeline).toHaveBeenCalledTimes(1);

  await act(async () => { await result.current.refetch(); });

  await waitFor(() => expect(result.current.error).toBeNull());
  expect(result.current.timeline).toEqual([]);
  expect(listTimeline).toHaveBeenCalledTimes(2);
});

it("keeps previously loaded history visible when a refresh fails", async () => {
  const saved: TimelineEntry = {
    type: "comment", id: "comment-1", actor_type: "member", actor_id: "member-1",
    content: "Previously loaded comment", created_at: "2026-11-26T12:00:00Z",
  };
  const failure = new Error("Refresh failed");
  listTimeline.mockResolvedValueOnce([saved]).mockRejectedValueOnce(failure);
  const { result } = renderHook(() => useIssueTimeline("issue-1"), { wrapper: Wrapper });
  await waitFor(() => expect(result.current.timeline).toEqual([saved]));

  await act(async () => { await result.current.refetch(); });

  await waitFor(() => expect(result.current.error).toBe(failure));
  expect(result.current.loading).toBe(false);
  expect(result.current.timeline).toEqual([saved]);
});
