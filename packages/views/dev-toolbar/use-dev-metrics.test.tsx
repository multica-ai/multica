import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { ApiClient } from "@multica/core/api/client";
import { getDevRecording, resetDevRecorder } from "@multica/core/diagnostics";
import { useDevRecorder } from "./use-dev-metrics";

let queryClient: QueryClient;

beforeEach(() => {
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  vi.spyOn(console, "error").mockImplementation(() => {});
  vi.stubGlobal("fetch", vi.fn().mockImplementation(() => Promise.resolve(new Response("[]", { status: 200 }))));
});

afterEach(() => {
  resetDevRecorder();
  queryClient.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

describe("useDevRecorder", () => {
  it("stops recording requests and errors after the toolbar unmounts", async () => {
    const view = renderHook(() => useDevRecorder("/acme/issues", "connected"), { wrapper });
    view.unmount();
    console.error("while toolbar is off");
    await new ApiClient("http://localhost:18080").listMembers("ws-1");
    expect({ requests: getDevRecording().requests.length, errors: getDevRecording().errors.length }).toEqual({
      requests: 0,
      errors: 0,
    });
  });

  it("starts a fresh recording when the toolbar is shown again", async () => {
    const first = renderHook(() => useDevRecorder("/acme/issues", "connected"), { wrapper });
    console.error("from previous visible session");
    expect(getDevRecording().errors).toHaveLength(1);
    first.unmount();

    console.error("while toolbar is off");
    await new ApiClient("http://localhost:18080").listMembers("ws-1");

    const second = renderHook(() => useDevRecorder("/acme/issues", "connected"), { wrapper });
    const recording = getDevRecording();
    second.unmount();
    expect(recording.errors).toEqual([]);
    expect(recording.requests).toEqual([]);
    expect(recording.navigations.map((n) => n.route)).toEqual(["/acme/issues"]);
  });
});
