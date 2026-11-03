// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useWorkspacePresenceMap } from "./use-agent-presence";

const clock = vi.hoisted(() => ({
  now: undefined as number | undefined,
  setup: undefined as (() => () => void) | undefined,
  onAppState: undefined as ((state: string) => void) | undefined,
  derive: vi.fn(() => new Map()),
  remove: vi.fn(),
}));

// Exercise the hook's timer and native subscriptions without loading RN.
vi.mock("react", () => ({
  useState: (initial: () => number) => {
    clock.now ??= initial();
    return [clock.now, (now: number) => { clock.now = now; }];
  },
  useEffect: (setup: () => () => void) => { clock.setup = setup; },
  useMemo: (derive: () => unknown) => derive(),
}));
vi.mock("react-native", () => ({
  AppState: {
    currentState: "active",
    addEventListener: (_event: string, handler: (state: string) => void) => {
      clock.onAppState = handler;
      return { remove: clock.remove };
    },
  },
}));
vi.mock("@tanstack/react-query", () => ({ useQuery: () => ({ data: [] }), queryOptions: (options: unknown) => options }));
vi.mock("@multica/core/agents", () => ({ buildPresenceMap: clock.derive }));
vi.mock("@/data/api", () => ({ api: {} }));

describe("presence clock", () => {
  let cleanup: (() => void) | undefined;
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(1_000_000);
    clock.now = undefined;
    clock.derive.mockClear();
    clock.remove.mockClear();
  });
  afterEach(() => {
    cleanup?.();
    cleanup = undefined;
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it("samples time on ticks and immediately on foreground recovery", () => {
    useWorkspacePresenceMap("workspace");
    cleanup = clock.setup!();
    expect(clock.derive).toHaveBeenLastCalledWith(expect.objectContaining({ now: 1_000_000 }));

    vi.advanceTimersByTime(30_000);
    useWorkspacePresenceMap("workspace");
    expect(clock.derive).toHaveBeenLastCalledWith(expect.objectContaining({ now: 1_030_000 }));

    clock.onAppState!("background");
    vi.advanceTimersByTime(120_000);
    expect(clock.now).toBe(1_030_000);
    clock.onAppState!("active");
    useWorkspacePresenceMap("workspace");
    expect(clock.derive).toHaveBeenLastCalledWith(expect.objectContaining({ now: 1_150_000 }));

    cleanup();
    cleanup = undefined;
    expect(clock.remove).toHaveBeenCalledOnce();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("does not read the wall clock again during a render", () => {
    useWorkspacePresenceMap("workspace");
    const readClock = vi.spyOn(Date, "now").mockImplementation(() => {
      throw new Error("render must use the clock snapshot");
    });
    expect(() => useWorkspacePresenceMap("workspace")).not.toThrow();
    expect(readClock).not.toHaveBeenCalled();
  });
});
