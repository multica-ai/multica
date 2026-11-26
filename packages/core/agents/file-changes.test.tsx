// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  useInfiniteQuery,
  useQuery,
} from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api, ApiError } from "../api";
import {
  fileChangeBodyOptions,
  fileChangeBodyState,
  fileChangesOptions,
  type RunFileChanges,
} from "./file-changes";
// Partial mock: the classification helpers pull ApiError/errorCode from this
// module, so the real exports must survive alongside the mocked client.
vi.mock("../api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api")>()),
  api: { listRunFileChanges: vi.fn(), getRunFileChangeBody: vi.fn() },
}));
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.clearAllMocks();
});
it("polls a collecting snapshot through completion, then stops polling", async () => {
  vi.useFakeTimers();
  const collecting: RunFileChanges = {
    collectionId: "c",
    status: "collecting",
    reason: "",
    scope: "registered_workdir_roots",
    roots: [],
    files: [],
    filesTotal: 0,
    totalsComplete: false,
    nextCursor: null,
  };
  vi.mocked(api.listRunFileChanges)
    .mockResolvedValueOnce(collecting)
    .mockResolvedValue({
      ...collecting,
      status: "ready",
      totalsComplete: true,
    });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const { result, unmount } = renderHook(
    () => useInfiniteQuery(fileChangesOptions("ws", "task")),
    {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    },
  );
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1);
  });
  expect(result.current.data?.pages[0]?.status).toBe("collecting");
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000);
  });
  expect(result.current.data?.pages[0]?.status).toBe("ready");
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10000);
  });
  expect(api.listRunFileChanges).toHaveBeenCalledTimes(2);
  unmount();
  client.clear();
});

function relayError(status: number, code: string): ApiError {
  return new ApiError(code, status, String(status), { code });
}
describe("fileChangeBodyState", () => {
  it.each([
    [503, "runtime_offline"],
    [504, "runtime_timeout"],
    [429, "relay_busy"],
  ])("maps HTTP %i (%s) onto the offline state", (status, code) => {
    expect(fileChangeBodyState(relayError(status, code))).toBe("offline");
  });
  it("maps 410 record_purged onto the purged state", () => {
    expect(fileChangeBodyState(relayError(410, "record_purged"))).toBe(
      "purged",
    );
  });
  it("classifies by the stable error code when the status is rewritten", () => {
    expect(fileChangeBodyState(relayError(500, "runtime_offline"))).toBe(
      "offline",
    );
    expect(fileChangeBodyState(relayError(500, "record_purged"))).toBe(
      "purged",
    );
  });
  it("treats everything else as a generic retryable error", () => {
    expect(fileChangeBodyState(new Error("offline"))).toBe("error");
    expect(fileChangeBodyState(relayError(404, "not_found"))).toBe("error");
    expect(fileChangeBodyState(relayError(500, "internal"))).toBe("error");
    expect(fileChangeBodyState(undefined)).toBe("error");
  });
});
it("keeps a failed body query from auto-retrying", async () => {
  vi.mocked(api.getRunFileChangeBody).mockRejectedValue(
    relayError(503, "runtime_offline"),
  );
  const client = new QueryClient();
  const { result, unmount } = renderHook(
    () => useQuery(fileChangeBodyOptions("ws", "task", "c", "f1", true)),
    {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    },
  );
  await waitFor(() => expect(result.current.isError).toBe(true));
  expect(result.current.error).toBeInstanceOf(ApiError);
  expect(api.getRunFileChangeBody).toHaveBeenCalledTimes(1);
  unmount();
  client.clear();
});
