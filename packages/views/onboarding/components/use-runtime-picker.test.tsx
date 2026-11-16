import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import {
  focusManager,
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import { setApiInstance, type ApiClient } from "@multica/core/api";
import type { AgentRuntime } from "@multica/core/types";
import {
  runtimePickerRefetchInterval,
  useRuntimePicker,
} from "./use-runtime-picker";

const ws = vi.hoisted(() => ({
  event: undefined as string | undefined,
  handler: undefined as (() => void) | undefined,
}));

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: (event: string, handler: () => void) => {
    ws.event = event;
    ws.handler = handler;
  },
}));

const listRuntimes = vi.fn<() => Promise<AgentRuntime[]>>();
const runtime = { id: "rt_1", status: "online" } as AgentRuntime;

function mountPicker() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return renderHook(() => useRuntimePicker("ws_1", "slug"), { wrapper });
}

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  focusManager.setFocused(true);
  ws.event = undefined;
  ws.handler = undefined;
  listRuntimes.mockReset();
  listRuntimes.mockResolvedValue([]);
  setApiInstance({ listRuntimes } as unknown as ApiClient);
});

afterEach(() => {
  cleanup();
  focusManager.setFocused(undefined);
  vi.useRealTimers();
});

describe("runtimePickerRefetchInterval", () => {
  it("climbs 2s, 5s, then stays at 15s until a runtime is listed", () => {
    const empty = (dataUpdateCount: number, errorUpdateCount = 0) => ({
      state: { data: [] as AgentRuntime[], dataUpdateCount, errorUpdateCount },
    });

    expect(runtimePickerRefetchInterval(empty(0))).toBe(2000);
    expect(runtimePickerRefetchInterval(empty(1))).toBe(2000);
    expect(runtimePickerRefetchInterval(empty(2))).toBe(5000);
    expect(runtimePickerRefetchInterval(empty(3))).toBe(15000);
    expect(runtimePickerRefetchInterval(empty(10))).toBe(15000);
    expect(runtimePickerRefetchInterval({
      state: { data: undefined, dataUpdateCount: 0, errorUpdateCount: 2 },
    })).toBe(5000);
    expect(runtimePickerRefetchInterval({
      state: { data: [runtime], dataUpdateCount: 0, errorUpdateCount: 0 },
    })).toBe(false);
    expect(runtimePickerRefetchInterval({
      state: { data: [runtime], dataUpdateCount: 10, errorUpdateCount: 4 },
    })).toBe(false);
  });
});

describe("useRuntimePicker", () => {
  it("backs off empty-list polling so 60 seconds stays at or under 8 requests", async () => {
    mountPicker();
    await advance(60_000);
    // Ladder from mount: t=0, then +2s, +5s, then +15s. Six calls land inside 60s.
    expect(listRuntimes.mock.calls.length).toBeGreaterThanOrEqual(6);
    expect(listRuntimes.mock.calls.length).toBeLessThanOrEqual(8);
  });

  it("refetches immediately on daemon:register without waiting out the 15s cap", async () => {
    mountPicker();
    // Initial response, then the 2s rung, then the 5s rung. The next wait is 15s.
    await advance(2_000);
    await advance(5_000);
    const calls = listRuntimes.mock.calls.length;
    await advance(14_000);
    expect(listRuntimes).toHaveBeenCalledTimes(calls);

    expect(ws.event).toBe("daemon:register");
    await act(async () => {
      ws.handler?.();
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(listRuntimes).toHaveBeenCalledTimes(calls + 1);
  });

  it("stops polling once a runtime is listed", async () => {
    const hook = mountPicker();
    await advance(0);
    const calls = listRuntimes.mock.calls.length;
    expect(calls).toBeGreaterThan(0);

    listRuntimes.mockResolvedValue([runtime]);
    await advance(2_000);
    // The observer notifies on a 0ms timer. Advancing 0 does not drain it.
    await advance(1);
    expect(listRuntimes).toHaveBeenCalledTimes(calls + 1);
    expect(hook.result.current.hasRuntimes).toBe(true);

    await advance(20_000);
    expect(listRuntimes).toHaveBeenCalledTimes(calls + 1);
  });

  it("does not poll after unmount", async () => {
    const hook = mountPicker();
    await advance(0);
    const calls = listRuntimes.mock.calls.length;
    expect(calls).toBeGreaterThan(0);

    hook.unmount();
    await advance(20_000);
    expect(listRuntimes).toHaveBeenCalledTimes(calls);
  });
});
