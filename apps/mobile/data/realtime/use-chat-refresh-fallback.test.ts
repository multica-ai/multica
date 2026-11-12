// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { chatKeys } from "@/data/queries/chat";
import { useChatRefreshFallback } from "./use-chat-refresh-fallback";

const harness = vi.hoisted(() => ({
  connected: false,
  cleanups: [] as (() => void)[],
  invalidateQueries: vi.fn(),
}));

vi.mock("react", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react")>()),
  useEffect: (setup: () => void | (() => void)) => {
    const cleanup = setup();
    if (cleanup) harness.cleanups.push(cleanup);
  },
  useRef: (current: unknown) => ({ current }),
  useState: (initial: unknown) => [
    typeof initial === "function" ? (initial as () => unknown)() : initial,
    () => {},
  ],
}));

vi.mock("react-native", () => ({
  AppState: {
    currentState: "active",
    addEventListener: () => ({ remove: vi.fn() }),
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries: harness.invalidateQueries }),
}));

vi.mock("./realtime-provider", () => ({
  useWSConnected: () => harness.connected,
}));

// `chatKeys` lives beside the API factory; mock the native API chain so the
// test stays in the headless lane (same boundary as other data-layer tests).
vi.mock("@/data/api", () => ({ api: {} }));

function runHook(options: {
  sessionId?: string | null;
  focused?: boolean;
  pendingTaskId?: string | null;
}) {
  useChatRefreshFallback({
    sessionId: options.sessionId === undefined ? "session-1" : options.sessionId,
    focused: options.focused ?? true,
    pendingTaskId:
      options.pendingTaskId === undefined ? "task-1" : options.pendingTaskId,
  });
}

describe("useChatRefreshFallback", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    harness.connected = false;
    harness.cleanups = [];
    harness.invalidateQueries.mockReset();
  });

  afterEach(() => {
    for (const cleanup of harness.cleanups) cleanup();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("polls messages, pending task, and transcript while the socket is down", () => {
    runHook({});

    expect(harness.invalidateQueries).toHaveBeenCalledWith({
      queryKey: chatKeys.messages("session-1"),
      refetchType: "active",
    });
    expect(harness.invalidateQueries).toHaveBeenCalledWith({
      queryKey: chatKeys.pendingTask("session-1"),
      refetchType: "active",
    });
    expect(harness.invalidateQueries).toHaveBeenCalledWith({
      queryKey: chatKeys.taskMessages("task-1"),
      refetchType: "active",
    });

    harness.invalidateQueries.mockClear();
    vi.advanceTimersByTime(4_000);
    expect(harness.invalidateQueries).toHaveBeenCalledTimes(3);
  });

  it("does not poll once the socket is authenticated", () => {
    harness.connected = true;
    runHook({});

    expect(harness.invalidateQueries).toHaveBeenCalledTimes(3);
    harness.invalidateQueries.mockClear();
    vi.advanceTimersByTime(20_000);
    expect(harness.invalidateQueries).not.toHaveBeenCalled();
  });

  it("stays idle without focus or an active session", () => {
    runHook({ focused: false });
    runHook({ sessionId: null });

    expect(harness.invalidateQueries).not.toHaveBeenCalled();
    vi.advanceTimersByTime(20_000);
    expect(harness.invalidateQueries).not.toHaveBeenCalled();
  });
});
