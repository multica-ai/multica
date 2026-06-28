import { act, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  overlay: null as { type: string } | null,
  open: vi.fn<() => Promise<void>>(() => Promise.resolve()),
  hide: vi.fn<() => Promise<void>>(() => Promise.resolve()),
  setBounds: vi.fn<() => Promise<void>>(() => Promise.resolve()),
  onNavState: vi.fn<() => () => void>(() => vi.fn()),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => true,
}));

vi.mock("@/stores/window-overlay-store", () => {
  const useWindowOverlayStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  return { useWindowOverlayStore };
});

import { CerebroBrowserPage } from "./cerebro-browser-page";

describe("CerebroBrowserPage", () => {
  beforeEach(() => {
    state.overlay = null;
    state.open.mockClear();
    state.hide.mockClear();
    state.setBounds.mockClear();
    state.onNavState.mockClear();

    Object.defineProperty(window, "cerebroBrowser", {
      configurable: true,
      value: {
        open: state.open,
        hide: state.hide,
        setBounds: state.setBounds,
        navigate: vi.fn(),
        back: vi.fn(),
        forward: vi.fn(),
        reload: vi.fn(),
        onNavState: state.onNavState,
      },
    });

    class ResizeObserverMock {
      observe = vi.fn();
      disconnect = vi.fn();
    }
    Object.defineProperty(window, "ResizeObserver", {
      configurable: true,
      value: ResizeObserverMock,
    });
  });

  it("hides the native browser pane while a window overlay is active", async () => {
    state.overlay = { type: "new-workspace" };

    render(<CerebroBrowserPage />);
    await act(async () => {});

    await waitFor(() => expect(state.hide).toHaveBeenCalled());
    expect(state.open).not.toHaveBeenCalled();
  });

  it("reopens the native browser pane when the window overlay closes", async () => {
    state.overlay = { type: "new-workspace" };
    const { rerender } = render(<CerebroBrowserPage />);
    await act(async () => {});

    state.open.mockClear();
    state.overlay = null;
    rerender(<CerebroBrowserPage />);
    await act(async () => {});

    await waitFor(() => expect(state.open).toHaveBeenCalled());
  });
});
