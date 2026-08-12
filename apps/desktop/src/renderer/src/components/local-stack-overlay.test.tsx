import {
  act,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LocalStackState } from "../../../shared/local-stack";
import {
  LocalStackOverlay,
  SKIP_VISIBLE_AFTER_MS,
  useLocalStackState,
} from "./local-stack-overlay";

// This repo has no @testing-library/user-event dependency — interaction tests
// use fireEvent (see src/renderer/src/components/route-error-page.test.tsx).

describe("LocalStackOverlay", () => {
  it("shows the current step while running", () => {
    render(
      <LocalStackOverlay
        state={{ phase: "running", step: "engine" }}
        onRetry={() => {}}
        onSkip={() => {}}
      />,
    );
    expect(screen.getByText(/starting docker engine/i)).toBeInTheDocument();
  });

  it("marks earlier steps as done", () => {
    render(
      <LocalStackOverlay
        state={{ phase: "running", step: "backend" }}
        onRetry={() => {}}
        onSkip={() => {}}
      />,
    );
    expect(screen.getByTestId("step-engine")).toHaveAttribute(
      "data-status",
      "done",
    );
    expect(screen.getByTestId("step-backend")).toHaveAttribute(
      "data-status",
      "active",
    );
  });

  it("shows the failure message and both actions on failure", () => {
    const onRetry = vi.fn();
    const onSkip = vi.fn();
    render(
      <LocalStackOverlay
        state={{ phase: "failed", step: "containers", message: "compose exploded" }}
        onRetry={onRetry}
        onSkip={onSkip}
      />,
    );

    expect(screen.getByText("compose exploded")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /retry/i }));
    expect(onRetry).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("button", { name: /continue anyway/i }));
    expect(onSkip).toHaveBeenCalledOnce();
  });

  it("marks the failed step so the user sees where it stopped", () => {
    render(
      <LocalStackOverlay
        state={{ phase: "failed", step: "containers", message: "boom" }}
        onRetry={() => {}}
        onSkip={() => {}}
      />,
    );
    expect(screen.getByTestId("step-containers")).toHaveAttribute(
      "data-status",
      "failed",
    );
  });

  // A config-load failure happens before any bring-up step runs. It must land
  // on the config row, not on probe — misattributing it sends the user
  // debugging docker when the real problem is a typo in their JSON.
  it("attributes a config failure to the config step, not probe", () => {
    render(
      <LocalStackOverlay
        state={{
          phase: "failed",
          step: "config",
          message: "local stack config: repoDir is required",
        }}
        onRetry={() => {}}
        onSkip={() => {}}
      />,
    );
    expect(screen.getByTestId("step-config")).toHaveAttribute(
      "data-status",
      "failed",
    );
    expect(screen.getByTestId("step-probe")).toHaveAttribute(
      "data-status",
      "pending",
    );
  });

  // The happy path never emits a running:config event — config loading is
  // instantaneous — so by the time probe is active, config must already read
  // as done rather than being stuck pending.
  it("shows config as done once probe is active", () => {
    render(
      <LocalStackOverlay
        state={{ phase: "running", step: "probe" }}
        onRetry={() => {}}
        onSkip={() => {}}
      />,
    );
    expect(screen.getByTestId("step-config")).toHaveAttribute(
      "data-status",
      "done",
    );
  });

  // Bounded worst case for a bring-up that hangs everywhere is roughly nine
  // minutes (three 180s command timeouts plus the 90s backend poll). Without an
  // escape hatch during `running` that is nine minutes of a window with no
  // buttons and no keyboard way out.
  describe("escape hatch while running", () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });
    afterEach(() => {
      vi.useRealTimers();
    });

    it("offers Continue anyway while running once the delay elapses", () => {
      const onSkip = vi.fn();
      render(
        <LocalStackOverlay
          state={{ phase: "running", step: "backend" }}
          onRetry={() => {}}
          onSkip={onSkip}
        />,
      );

      expect(
        screen.queryByRole("button", { name: /continue anyway/i }),
      ).not.toBeInTheDocument();

      act(() => {
        vi.advanceTimersByTime(SKIP_VISIBLE_AFTER_MS);
      });

      fireEvent.click(screen.getByRole("button", { name: /continue anyway/i }));
      expect(onSkip).toHaveBeenCalledOnce();
    });

    // Retry stays failure-only: re-running a bring-up that is still in flight
    // is what the main-process single-flight guard exists to prevent.
    it("does not offer Retry while running", () => {
      render(
        <LocalStackOverlay
          state={{ phase: "running", step: "backend" }}
          onRetry={() => {}}
          onSkip={() => {}}
        />,
      );

      act(() => {
        vi.advanceTimersByTime(SKIP_VISIBLE_AFTER_MS);
      });

      expect(
        screen.queryByRole("button", { name: /retry/i }),
      ).not.toBeInTheDocument();
    });

    // `idle` is what the renderer sees when main is up but the supervisor has
    // not reported yet. It blocks exactly like `running`, so it needs the same
    // way out.
    it("offers Continue anyway from a stuck idle state too", () => {
      render(
        <LocalStackOverlay
          state={{ phase: "idle" }}
          onRetry={() => {}}
          onSkip={() => {}}
        />,
      );

      act(() => {
        vi.advanceTimersByTime(SKIP_VISIBLE_AFTER_MS);
      });

      expect(
        screen.getByRole("button", { name: /continue anyway/i }),
      ).toBeInTheDocument();
    });
  });
});

// useLocalStackState implements the "seed synchronously, then read, then
// subscribe" invariant: the bring-up starts before React mounts, so the hook
// seeds from the preload's synchronous snapshot and still reads getState() on
// mount (an event emitted between the snapshot and the subscription would
// otherwise be lost). Stub window.localStackAPI directly rather than going
// through the real preload bridge.
function stubLocalStackAPI(overrides: {
  getState: ReturnType<typeof vi.fn>;
  onState: ReturnType<typeof vi.fn>;
  initialState?: LocalStackState;
}) {
  Object.defineProperty(window, "localStackAPI", {
    configurable: true,
    value: {
      initialState: overrides.initialState ?? ({ phase: "idle" } as LocalStackState),
      getState: overrides.getState,
      onState: overrides.onState,
      retry: vi.fn(),
      skip: vi.fn(),
    },
  });
}

describe("useLocalStackState", () => {
  // The first render decides whether App paints the overlay or mounts
  // CoreProvider. Seeding from the preload snapshot is what keeps a SaaS build
  // — where main resolved `ready` long before the window existed — from showing
  // the startup overlay as its first visible frame.
  it("seeds synchronously from the preload snapshot, before any IPC resolves", () => {
    const getState = vi.fn(() => new Promise<LocalStackState>(() => {}));
    const onState = vi.fn(() => () => {});
    stubLocalStackAPI({
      getState,
      onState,
      initialState: { phase: "ready" },
    });

    const { result } = renderHook(() => useLocalStackState());

    expect(result.current).toEqual({ phase: "ready" });
  });

  // A rejected invoke (no handler registered, main torn down) must open the
  // gate, not hold it shut: the failure mode it replaces is a button-less
  // overlay with no timeout and no keyboard escape.
  it("falls back to ready when getState() rejects", async () => {
    const getState = vi.fn().mockRejectedValue(new Error("no handler"));
    const onState = vi.fn(() => () => {});
    stubLocalStackAPI({ getState, onState, initialState: { phase: "idle" } });

    const { result } = renderHook(() => useLocalStackState());

    await waitFor(() => expect(result.current).toEqual({ phase: "ready" }));
  });

  it("calls getState() on mount and lands the resolved value in state", async () => {
    const getState = vi
      .fn()
      .mockResolvedValue({ phase: "running", step: "engine" } as LocalStackState);
    const onState = vi.fn(() => () => {});
    stubLocalStackAPI({ getState, onState });

    const { result } = renderHook(() => useLocalStackState());

    expect(getState).toHaveBeenCalledTimes(1);
    await waitFor(() =>
      expect(result.current).toEqual({ phase: "running", step: "engine" }),
    );
  });

  it("subscribes via onState, and a pushed state updates the returned state", async () => {
    const getState = vi.fn().mockResolvedValue({ phase: "idle" } as LocalStackState);
    let pushState: ((state: LocalStackState) => void) | undefined;
    const onState = vi.fn((callback: (state: LocalStackState) => void) => {
      pushState = callback;
      return () => {};
    });
    stubLocalStackAPI({ getState, onState });

    const { result } = renderHook(() => useLocalStackState());
    await waitFor(() => expect(onState).toHaveBeenCalledTimes(1));

    act(() => {
      pushState?.({ phase: "running", step: "probe" });
    });

    expect(result.current).toEqual({ phase: "running", step: "probe" });
  });

  it("calls the onState unsubscribe function on unmount", async () => {
    const getState = vi.fn().mockResolvedValue({ phase: "idle" } as LocalStackState);
    const unsubscribe = vi.fn();
    const onState = vi.fn(() => unsubscribe);
    stubLocalStackAPI({ getState, onState });

    const { unmount } = renderHook(() => useLocalStackState());
    await waitFor(() => expect(onState).toHaveBeenCalledTimes(1));

    expect(unsubscribe).not.toHaveBeenCalled();
    unmount();
    expect(unsubscribe).toHaveBeenCalledTimes(1);
  });

  // Documents the intent of the hook's `active` flag: a slow initial getState()
  // that resolves after unmount must not resurrect stale state. This is not a
  // regression guard — React 18 silently no-ops a post-unmount setState, so the
  // assertions below hold with or without the flag.
  it("ignores a getState() resolution that arrives after unmount", async () => {
    let resolveGetState: ((state: LocalStackState) => void) | undefined;
    const getState = vi.fn(
      () =>
        new Promise<LocalStackState>((resolve) => {
          resolveGetState = resolve;
        }),
    );
    const onState = vi.fn(() => () => {});
    stubLocalStackAPI({ getState, onState });
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    const { result, unmount } = renderHook(() => useLocalStackState());
    unmount();

    await act(async () => {
      resolveGetState?.({ phase: "ready" });
      await Promise.resolve();
    });

    expect(result.current).toEqual({ phase: "idle" });
    expect(errorSpy).not.toHaveBeenCalled();
    errorSpy.mockRestore();
  });
});
