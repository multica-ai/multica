import {
  act,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
} from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { LocalStackState } from "../../../shared/local-stack";
import { LocalStackOverlay, useLocalStackState } from "./local-stack-overlay";

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
});

// useLocalStackState is the single mechanism implementing the "read then
// subscribe" invariant: the bring-up starts before React mounts, so the hook
// must read getState() on mount (not rely on onState alone) or it would sit
// on "idle" forever. Stub window.localStackAPI directly rather than going
// through the real preload bridge.
function stubLocalStackAPI(overrides: {
  getState: ReturnType<typeof vi.fn>;
  onState: ReturnType<typeof vi.fn>;
}) {
  Object.defineProperty(window, "localStackAPI", {
    configurable: true,
    value: {
      getState: overrides.getState,
      onState: overrides.onState,
      retry: vi.fn(),
      skip: vi.fn(),
    },
  });
}

describe("useLocalStackState", () => {
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

  // The hook's `active` flag exists precisely for this: a slow initial
  // getState() call that only resolves after the component is gone must not
  // resurrect stale state or throw. Prove the guard by resolving it late.
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
