import { describe, expect, it, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { useEffect } from "react";

// Shared in-memory state that the mocked tab store reads / mutates. The test
// records every method call so we can assert openInNewTab does NOT activate
// the new tab (i.e. setActiveTab is never invoked on the same-workspace path).
const state = vi.hoisted(() => ({
  activeWorkspaceSlug: "acme" as string | null,
  byWorkspace: {
    acme: {
      activeTabId: "tA",
      tabs: [{ id: "tA", path: "/acme/issues" }],
    },
  } as Record<
    string,
    { activeTabId: string; tabs: { id: string; path: string }[] }
  >,
  openTab: vi.fn<(path: string, title?: string, icon?: string) => string>(),
  setActiveTab: vi.fn<(tabId: string) => void>(),
  switchWorkspace: vi.fn<(slug: string, openPath?: string) => void>(),
}));

vi.mock("@/stores/tab-store", () => {
  const store = {
    get activeWorkspaceSlug() {
      return state.activeWorkspaceSlug;
    },
    get byWorkspace() {
      return state.byWorkspace;
    },
    openTab: state.openTab,
    setActiveTab: state.setActiveTab,
    switchWorkspace: state.switchWorkspace,
  };
  const useTabStore = Object.assign(
    (selector?: (s: typeof store) => unknown) =>
      selector ? selector(store) : store,
    { getState: () => store },
  );
  const getActiveTab = () => {
    const slug = state.activeWorkspaceSlug;
    if (!slug) return null;
    const group = state.byWorkspace[slug];
    if (!group) return null;
    return group.tabs.find((t) => t.id === group.activeTabId) ?? null;
  };
  const useActiveTabIdentity = () => ({
    slug: state.activeWorkspaceSlug,
    tabId: state.activeWorkspaceSlug
      ? (state.byWorkspace[state.activeWorkspaceSlug]?.activeTabId ?? null)
      : null,
  });
  const useActiveTabRouter = () => null;
  const resolveRouteIcon = () => "File";
  return {
    useTabStore,
    getActiveTab,
    useActiveTabIdentity,
    useActiveTabRouter,
    resolveRouteIcon,
  };
});

vi.mock("@/stores/window-overlay-store", () => ({
  useWindowOverlayStore: Object.assign(
    () => null,
    { getState: () => ({ overlay: null, open: vi.fn(), close: vi.fn() }) },
  ),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    () => null,
    { getState: () => ({ logout: vi.fn() }) },
  ),
}));

vi.mock("@multica/core/paths", () => ({
  isReservedSlug: (s: string) =>
    ["login", "workspaces", "invite", "onboarding", "invitations"].includes(s),
}));

// DesktopNavigationProvider reads window.desktopAPI.runtimeConfig synchronously.
beforeEach(() => {
  state.openTab.mockReset();
  state.setActiveTab.mockReset();
  state.switchWorkspace.mockReset();
  state.openTab.mockImplementation(() => "tNew");
  state.activeWorkspaceSlug = "acme";
  state.byWorkspace = {
    acme: {
      activeTabId: "tA",
      tabs: [{ id: "tA", path: "/acme/issues" }],
    },
  };
  Object.defineProperty(window, "desktopAPI", {
    configurable: true,
    value: {
      runtimeConfig: { ok: true, config: { appUrl: "https://app.example" } },
    },
  });
});

import {
  DesktopNavigationProvider,
  TabNavigationProvider,
} from "./navigation";
import { useNavigation } from "@multica/views/navigation";

function captureAdapter(onAdapter: (adapter: ReturnType<typeof useNavigation>) => void) {
  function Probe() {
    const nav = useNavigation();
    useEffect(() => {
      onAdapter(nav);
    }, [nav]);
    return null;
  }
  return Probe;
}

describe("DesktopNavigationProvider.openInNewTab", () => {
  it("opens a background tab (no setActiveTab) for a same-workspace path", () => {
    let adapter: ReturnType<typeof useNavigation> | null = null;
    const Probe = captureAdapter((a) => {
      adapter = a;
    });
    render(
      <DesktopNavigationProvider>
        <Probe />
      </DesktopNavigationProvider>,
    );
    expect(adapter).not.toBeNull();
    adapter!.openInNewTab!("/acme/agents", "Agents");
    expect(state.openTab).toHaveBeenCalledWith("/acme/agents", "Agents", "File");
    expect(state.setActiveTab).not.toHaveBeenCalled();
    expect(state.switchWorkspace).not.toHaveBeenCalled();
  });

  it("activates the new tab when opts.activate is true (foreground)", () => {
    let adapter: ReturnType<typeof useNavigation> | null = null;
    const Probe = captureAdapter((a) => {
      adapter = a;
    });
    render(
      <DesktopNavigationProvider>
        <Probe />
      </DesktopNavigationProvider>,
    );
    adapter!.openInNewTab!("/acme/agents", "Agents", { activate: true });
    expect(state.openTab).toHaveBeenCalledWith("/acme/agents", "Agents", "File");
    expect(state.setActiveTab).toHaveBeenCalledWith("tNew");
    expect(state.switchWorkspace).not.toHaveBeenCalled();
  });

  it("delegates to switchWorkspace for a cross-workspace path", () => {
    let adapter: ReturnType<typeof useNavigation> | null = null;
    const Probe = captureAdapter((a) => {
      adapter = a;
    });
    render(
      <DesktopNavigationProvider>
        <Probe />
      </DesktopNavigationProvider>,
    );
    adapter!.openInNewTab!("/butter/inbox");
    expect(state.switchWorkspace).toHaveBeenCalledWith("butter", "/butter/inbox");
    expect(state.openTab).not.toHaveBeenCalled();
    expect(state.setActiveTab).not.toHaveBeenCalled();
  });
});

describe("TabNavigationProvider.openInNewTab", () => {
  function renderTabProvider() {
    let adapter: ReturnType<typeof useNavigation> | null = null;
    const Probe = captureAdapter((a) => {
      adapter = a;
    });
    const fakeRouter = {
      state: { location: { pathname: "/acme/issues", search: "" } },
      subscribe: () => () => {},
      navigate: vi.fn(),
    } as unknown as Parameters<typeof TabNavigationProvider>[0]["router"];
    render(
      <TabNavigationProvider router={fakeRouter}>
        <Probe />
      </TabNavigationProvider>,
    );
    return () => adapter!;
  }

  it("opens a background tab (no setActiveTab) for a same-workspace path", () => {
    const getAdapter = renderTabProvider();
    getAdapter().openInNewTab!("/acme/agents", "Agents");
    expect(state.openTab).toHaveBeenCalledWith("/acme/agents", "Agents", "File");
    expect(state.setActiveTab).not.toHaveBeenCalled();
    expect(state.switchWorkspace).not.toHaveBeenCalled();
  });

  it("activates the new tab when opts.activate is true (foreground)", () => {
    const getAdapter = renderTabProvider();
    getAdapter().openInNewTab!("/acme/agents", "Agents", { activate: true });
    expect(state.openTab).toHaveBeenCalledWith("/acme/agents", "Agents", "File");
    expect(state.setActiveTab).toHaveBeenCalledWith("tNew");
    expect(state.switchWorkspace).not.toHaveBeenCalled();
  });
});
