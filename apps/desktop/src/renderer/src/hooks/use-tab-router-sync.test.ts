import { describe, expect, it, vi, beforeEach } from "vitest";
import { renderHook } from "@testing-library/react";
import type { DataRouter } from "react-router-dom";

// createTabRouter transitively pulls in route modules that expect a browser
// router context. For store-driven hook tests we stub it to a minimal
// disposable, same as tab-store.test.ts.
const createTabRouterMock = vi.hoisted(() =>
  vi.fn(() => ({
    dispose: vi.fn(),
    state: { location: { pathname: "/" } },
    navigate: vi.fn(),
    subscribe: vi.fn(() => () => {}),
  })),
);
vi.mock("../routes", () => ({
  createTabRouter: createTabRouterMock,
}));

import { useTabStore } from "@/stores/tab-store";
import { useTabRouterSync } from "./use-tab-router-sync";

type FakeRouterState = {
  location: { pathname: string; search: string };
  historyAction: "PUSH" | "POP" | "REPLACE";
};

// A controllable stand-in for the tab's memory router: the hook reads
// `state.location` on mount and re-syncs on every subscribe callback.
function makeFakeRouter(pathname: string, search: string) {
  let listener: ((state: FakeRouterState) => void) | null = null;
  const router = {
    state: { location: { pathname, search } },
    subscribe: (cb: (state: FakeRouterState) => void) => {
      listener = cb;
      return () => {
        listener = null;
      };
    },
  };
  return {
    router: router as unknown as DataRouter,
    navigate(state: FakeRouterState) {
      router.state.location = state.location;
      listener?.(state);
    },
  };
}

function activeTab() {
  const s = useTabStore.getState();
  return s.byWorkspace[s.activeWorkspaceSlug!].tabs[0];
}

beforeEach(() => {
  useTabStore.getState().reset();
  useTabStore.getState().switchWorkspace("acme");
});

describe("useTabRouterSync", () => {
  it("syncs initial pathname AND search into the tab store", () => {
    const { router } = makeFakeRouter("/acme/inbox", "?issue=a1");
    renderHook(() => useTabRouterSync(activeTab().id, router));

    const tab = activeTab();
    expect(tab.path).toBe("/acme/inbox");
    expect(tab.search).toBe("?issue=a1");
  });

  it("a query-only navigation updates the stored search (MUL-4120)", () => {
    const { router, navigate } = makeFakeRouter("/acme/inbox", "?issue=a1");
    renderHook(() => useTabRouterSync(activeTab().id, router));

    navigate({
      location: { pathname: "/acme/inbox", search: "?issue=b2" },
      historyAction: "REPLACE",
    });

    const tab = activeTab();
    expect(tab.path).toBe("/acme/inbox");
    expect(tab.search).toBe("?issue=b2");
  });

  it("navigating to a path without a query clears the stored search", () => {
    const { router, navigate } = makeFakeRouter("/acme/inbox", "?issue=a1");
    renderHook(() => useTabRouterSync(activeTab().id, router));

    navigate({
      location: { pathname: "/acme/issues", search: "" },
      historyAction: "PUSH",
    });

    const tab = activeTab();
    expect(tab.path).toBe("/acme/issues");
    expect(tab.search).toBe("");
  });
});
