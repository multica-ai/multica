import { describe, expect, it, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";

// vi.hoisted shared state — every store mock reads the same object so each
// test can mutate it then re-render to drive the tracker.
const state = vi.hoisted(() => ({
  user: null as { id: string } | null,
  overlay: null as { type: string; invitationId?: string } | null,
  activeWorkspaceSlug: null as string | null,
  byWorkspace: {} as Record<
    string,
    { activeTabId: string; tabs: { id: string; path: string }[] }
  >,
  capturePageview: vi.fn<(path?: string) => void>(),
}));

vi.mock("@multica/core/analytics", () => ({
  capturePageview: state.capturePageview,
}));

// Auth store — single selector pattern (`s => s.user`).
vi.mock("@multica/core/auth", () => {
  const useAuthStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  return { useAuthStore };
});

// Window overlay store — same shape.
vi.mock("@/stores/window-overlay-store", () => {
  const useWindowOverlayStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  return { useWindowOverlayStore };
});

// Tab store — selectors read activeWorkspaceSlug + byWorkspace. Also expose
// getState() for the seed pass.
vi.mock("@/stores/tab-store", () => {
  const useTabStore = Object.assign(
    (selector: (s: typeof state) => unknown) => selector(state),
    { getState: () => state },
  );
  return { useTabStore };
});

import { PageviewTracker } from "./pageview-tracker";

function reset() {
  state.user = { id: "u1" };
  state.overlay = null;
  state.activeWorkspaceSlug = null;
  state.byWorkspace = {};
  state.capturePageview.mockClear();
}

beforeEach(() => {
  reset();
});

describe("PageviewTracker", () => {
  it("suppresses pageview when switching to a previously-visible tab on its existing path", () => {
    state.byWorkspace = {
      acme: {
        activeTabId: "tA",
        tabs: [
          { id: "tA", path: "/acme/issues" },
          { id: "tB", path: "/acme/inbox" },
        ],
      },
    };
    state.activeWorkspaceSlug = "acme";

    const { rerender } = render(<PageviewTracker />);
    // Initial mount on tA — seeded as observed, no pageview because both
    // tabs were already in the persisted store before the tracker mounted.
    expect(state.capturePageview).not.toHaveBeenCalled();

    // Switch to tB (already-known tab on its already-known path).
    state.byWorkspace = {
      acme: {
        activeTabId: "tB",
        tabs: [
          { id: "tA", path: "/acme/issues" },
          { id: "tB", path: "/acme/inbox" },
        ],
      },
    };
    rerender(<PageviewTracker />);
    expect(state.capturePageview).not.toHaveBeenCalled();

    // Switch back to tA — still no pageview.
    state.byWorkspace = {
      acme: {
        activeTabId: "tA",
        tabs: [
          { id: "tA", path: "/acme/issues" },
          { id: "tB", path: "/acme/inbox" },
        ],
      },
    };
    rerender(<PageviewTracker />);
    expect(state.capturePageview).not.toHaveBeenCalled();
  });

  it("fires pageview when a new tab is opened (openInNewTab / addTab)", () => {
    state.byWorkspace = {
      acme: {
        activeTabId: "tA",
        tabs: [{ id: "tA", path: "/acme/issues" }],
      },
    };
    state.activeWorkspaceSlug = "acme";

    const { rerender } = render(<PageviewTracker />);
    state.capturePageview.mockClear();

    // Simulate openInNewTab("/acme/agents") → new tab tC added and activated.
    state.byWorkspace = {
      acme: {
        activeTabId: "tC",
        tabs: [
          { id: "tA", path: "/acme/issues" },
          { id: "tC", path: "/acme/agents" },
        ],
      },
    };
    rerender(<PageviewTracker />);

    expect(state.capturePageview).toHaveBeenCalledTimes(1);
    expect(state.capturePageview).toHaveBeenCalledWith("/acme/agents");
  });

  it("fires pageview when switchWorkspace opens a new path in another workspace", () => {
    state.byWorkspace = {
      acme: {
        activeTabId: "tA",
        tabs: [{ id: "tA", path: "/acme/issues" }],
      },
    };
    state.activeWorkspaceSlug = "acme";

    const { rerender } = render(<PageviewTracker />);
    state.capturePageview.mockClear();

    // Cross-workspace navigation: switchWorkspace("butter", "/butter/inbox")
    // creates a fresh tab in the destination workspace and makes it active.
    state.byWorkspace = {
      acme: { activeTabId: "tA", tabs: [{ id: "tA", path: "/acme/issues" }] },
      butter: {
        activeTabId: "tD",
        tabs: [{ id: "tD", path: "/butter/inbox" }],
      },
    };
    state.activeWorkspaceSlug = "butter";
    rerender(<PageviewTracker />);

    expect(state.capturePageview).toHaveBeenCalledTimes(1);
    expect(state.capturePageview).toHaveBeenCalledWith("/butter/inbox");
  });

  it("fires pageview on intra-tab navigation (path changes for the same tabId)", () => {
    state.byWorkspace = {
      acme: {
        activeTabId: "tA",
        tabs: [{ id: "tA", path: "/acme/issues" }],
      },
    };
    state.activeWorkspaceSlug = "acme";

    const { rerender } = render(<PageviewTracker />);
    state.capturePageview.mockClear();

    state.byWorkspace = {
      acme: {
        activeTabId: "tA",
        tabs: [{ id: "tA", path: "/acme/issues/123" }],
      },
    };
    rerender(<PageviewTracker />);

    expect(state.capturePageview).toHaveBeenCalledTimes(1);
    expect(state.capturePageview).toHaveBeenCalledWith("/acme/issues/123");
  });

  it("fires overlay and login pageviews and suppresses re-entry into the same tab afterward", () => {
    state.byWorkspace = {
      acme: {
        activeTabId: "tA",
        tabs: [{ id: "tA", path: "/acme/issues" }],
      },
    };
    state.activeWorkspaceSlug = "acme";

    const { rerender } = render(<PageviewTracker />);
    state.capturePageview.mockClear();

    // Open onboarding overlay.
    state.overlay = { type: "onboarding" };
    rerender(<PageviewTracker />);
    expect(state.capturePageview).toHaveBeenLastCalledWith("/onboarding");

    // Close overlay back to the tab — the tab is already observed on
    // /acme/issues so this is a re-activation, no pageview.
    state.capturePageview.mockClear();
    state.overlay = null;
    rerender(<PageviewTracker />);
    expect(state.capturePageview).not.toHaveBeenCalled();

    // Logout fires /login.
    state.user = null;
    rerender(<PageviewTracker />);
    expect(state.capturePageview).toHaveBeenLastCalledWith("/login");
  });

  it("suppresses on initial mount when the active tab was restored from persistence", () => {
    state.byWorkspace = {
      acme: {
        activeTabId: "tA",
        tabs: [{ id: "tA", path: "/acme/issues" }],
      },
    };
    state.activeWorkspaceSlug = "acme";

    render(<PageviewTracker />);
    // Restored tab — seeded, treated as a re-activation.
    expect(state.capturePageview).not.toHaveBeenCalled();
  });
});

