import { describe, expect, it, vi, beforeEach } from "vitest";
import { act, render } from "@testing-library/react";
import { useEffect } from "react";

// MUL-4741: the adapter mutates tab sessions in the REAL store (no router,
// no mocks needed for it anymore) — the Coordinator, not tested here, is
// what projects sessions into the single router. Overlay and auth stay
// mocked so we can spy on their entry points.

const overlay = vi.hoisted(() => ({
  overlay: null as null | { type: string },
  open: vi.fn(),
  close: vi.fn(),
}));

vi.mock("@/stores/window-overlay-store", () => ({
  useWindowOverlayStore: Object.assign(() => null, {
    getState: () => overlay,
  }),
}));

const auth = vi.hoisted(() => ({ logout: vi.fn() }));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(() => null, {
    getState: () => auth,
  }),
}));

import { DesktopNavigationProvider, routeContentLinkPath } from "./navigation";
import { currentPath, useNavigation } from "@multica/views/navigation";
import { useTabStore, getActiveTab } from "@/stores/tab-store";

beforeEach(() => {
  overlay.open.mockReset();
  overlay.close.mockReset();
  overlay.overlay = null;
  auth.logout.mockReset();
  useTabStore.getState().reset();
  useTabStore.getState().switchWorkspace("acme"); // default tab /acme/issues
  Object.defineProperty(window, "desktopAPI", {
    configurable: true,
    value: {
      runtimeConfig: { ok: true, config: { appUrl: "https://app.example" } },
    },
  });
});

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

function renderProvider() {
  let adapter: ReturnType<typeof useNavigation> | null = null;
  const Probe = captureAdapter((a) => {
    adapter = a;
  });
  render(
    <DesktopNavigationProvider>
      <Probe />
    </DesktopNavigationProvider>,
  );
  return () => adapter!;
}

function acmeGroup() {
  return useTabStore.getState().byWorkspace.acme;
}

describe("openInNewTab", () => {
  it("opens a background tab (active tab unchanged) for a same-workspace path", () => {
    const getAdapter = renderProvider();
    const activeBefore = acmeGroup().activeTabId;

    getAdapter().openInNewTab!("/acme/agents", "Agents");

    const group = acmeGroup();
    expect(group.tabs.map((t) => t.url)).toEqual(["/acme/issues", "/acme/agents"]);
    expect(group.activeTabId).toBe(activeBefore);
  });

  it("activates the new tab when opts.activate is true (foreground)", () => {
    const getAdapter = renderProvider();

    getAdapter().openInNewTab!("/acme/agents", "Agents", { activate: true });

    const group = acmeGroup();
    const agents = group.tabs.find((t) => t.url === "/acme/agents")!;
    expect(group.activeTabId).toBe(agents.id);
  });

  it("delegates to switchWorkspace for a cross-workspace path", () => {
    const getAdapter = renderProvider();

    getAdapter().openInNewTab!("/butter/inbox");

    const s = useTabStore.getState();
    expect(s.activeWorkspaceSlug).toBe("butter");
    expect(getActiveTab(s)?.url).toBe("/butter/inbox");
    // acme's group is untouched.
    expect(s.byWorkspace.acme.tabs).toHaveLength(1);
  });
});

describe("push", () => {
  it("navigates the active session in-tab (url + virtual history)", () => {
    const getAdapter = renderProvider();

    getAdapter().push("/acme/projects?sort=name");

    const active = getActiveTab(useTabStore.getState())!;
    expect(active.url).toBe("/acme/projects?sort=name");
    expect(active.history).toEqual({
      stack: ["/acme/issues", "/acme/projects?sort=name"],
      index: 1,
    });
  });

  it("is a no-op when the target exactly matches the active session url", () => {
    const getAdapter = renderProvider();
    const before = acmeGroup();

    getAdapter().push("/acme/issues");

    expect(acmeGroup()).toBe(before);
  });

  it("switches workspace for a cross-workspace path", () => {
    const getAdapter = renderProvider();

    getAdapter().push("/butter/inbox");

    const s = useTabStore.getState();
    expect(s.activeWorkspaceSlug).toBe("butter");
    expect(getActiveTab(s)?.url).toBe("/butter/inbox");
  });

  it("logs out instead of navigating for /login", () => {
    const getAdapter = renderProvider();

    getAdapter().push("/login");

    expect(auth.logout).toHaveBeenCalledOnce();
    expect(getActiveTab(useTabStore.getState())?.url).toBe("/acme/issues");
  });

  it("routes transition paths to the window overlay without touching sessions", () => {
    const getAdapter = renderProvider();
    const before = acmeGroup();

    getAdapter().push("/workspaces/new");

    expect(overlay.open).toHaveBeenCalledWith({ type: "new-workspace" });
    expect(acmeGroup()).toBe(before);
  });
});

describe("push with pinned active tab", () => {
  function pinActive() {
    const store = useTabStore.getState();
    store.togglePin(acmeGroup().activeTabId);
  }

  it("redirects push to a new foreground tab when pathname differs", () => {
    pinActive();
    const getAdapter = renderProvider();
    const pinnedId = acmeGroup().activeTabId;

    getAdapter().push("/acme/projects");

    const group = acmeGroup();
    const pinned = group.tabs.find((t) => t.id === pinnedId)!;
    const projects = group.tabs.find((t) => t.url === "/acme/projects")!;
    // The pinned tab stays parked on its url; focus follows the new tab.
    expect(pinned.url).toBe("/acme/issues");
    expect(group.activeTabId).toBe(projects.id);
  });

  it("allows in-tab navigation when only search/hash changes (RFC §3 D2b)", () => {
    pinActive();
    const getAdapter = renderProvider();

    getAdapter().push("/acme/issues?filter=open");

    const group = acmeGroup();
    expect(group.tabs).toHaveLength(1); // no new tab
    expect(group.tabs[0].url).toBe("/acme/issues?filter=open");
  });

  it("leaves cross-workspace push to the workspace switcher (not pin)", () => {
    pinActive();
    const getAdapter = renderProvider();

    getAdapter().push("/butter/inbox");

    expect(useTabStore.getState().activeWorkspaceSlug).toBe("butter");
    // No extra tab was opened in acme by the pin interception.
    expect(useTabStore.getState().byWorkspace.acme.tabs).toHaveLength(1);
  });
});

describe("back", () => {
  it("moves the session's virtual history backwards", () => {
    const getAdapter = renderProvider();
    getAdapter().push("/acme/projects");

    getAdapter().back!();

    const active = getActiveTab(useTabStore.getState())!;
    expect(active.url).toBe("/acme/issues");
    expect(active.history.index).toBe(0);
  });
});

// Consumed by `useBackOrReplace` — a page whose subject was deleted steps back
// only when this says there is somewhere to step back to.
describe("canGoBack", () => {
  it("is false on a tab sitting at the start of its history", () => {
    const getAdapter = renderProvider();

    expect(getActiveTab(useTabStore.getState())!.history.index).toBe(0);
    expect(getAdapter().canGoBack!()).toBe(false);
  });

  it("is true once the tab has navigated in place", () => {
    const getAdapter = renderProvider();

    getAdapter().push("/acme/projects");

    expect(getActiveTab(useTabStore.getState())!.history.index).toBe(1);
    expect(getAdapter().canGoBack!()).toBe(true);
  });

  it("is false again after stepping back to the start", () => {
    const getAdapter = renderProvider();
    getAdapter().push("/acme/projects");

    getAdapter().back!();

    expect(getAdapter().canGoBack!()).toBe(false);
  });

  it("is false for a freshly opened tab, which starts its own history", () => {
    const getAdapter = renderProvider();
    getAdapter().push("/acme/projects");

    getAdapter().openInNewTab!("/acme/agents", "Agents", { activate: true });

    expect(getAdapter().canGoBack!()).toBe(false);
  });
});

// MUL-6784: the tab URL is the only place the fragment survives on desktop —
// the renderer's own `window.location` is the packaged file:// page — so a
// share or feedback link built from this adapter depends on `hash` being here.
describe("current location", () => {
  it("splits the active tab URL into pathname, search and fragment", () => {
    const getAdapter = renderProvider();

    act(() => getAdapter().push("/acme/issues/MUL-1?tab=activity#comment-c1"));

    expect(getAdapter().pathname).toBe("/acme/issues/MUL-1");
    expect(getAdapter().searchParams.get("tab")).toBe("activity");
    expect(getAdapter().hash).toBe("#comment-c1");
  });

  it('reports "" for a tab URL that carries no fragment', () => {
    const getAdapter = renderProvider();

    act(() => getAdapter().push("/acme/issues/MUL-1?tab=activity"));

    expect(getAdapter().hash).toBe("");
  });

  it("rebuilds the current page as a web URL that keeps the fragment", () => {
    const getAdapter = renderProvider();

    act(() => getAdapter().push("/acme/issues/MUL-1#comment-c1"));

    const adapter = getAdapter();
    expect(adapter.getShareableUrl(currentPath(adapter))).toBe(
      "https://app.example/acme/issues/MUL-1#comment-c1",
    );
  });
});

describe("flashActiveTab", () => {
  it("raises a flash signal on the active tab without navigating", () => {
    const getAdapter = renderProvider();
    const activeBefore = acmeGroup().activeTabId;

    getAdapter().flashActiveTab!();

    const s = useTabStore.getState();
    expect(s.flashSignal).toEqual({ tabId: activeBefore, seq: 1 });
    // No navigation happened.
    expect(getActiveTab(s)?.url).toBe("/acme/issues");
  });
});

describe("routeContentLinkPath (links inside content — MUL-5208)", () => {
  it("opens a same-workspace path in a foreground tab of its own", () => {
    routeContentLinkPath("/acme/issues/MUL-1");

    const group = useTabStore.getState().byWorkspace.acme;
    const opened = group.tabs.find((t) => t.url === "/acme/issues/MUL-1")!;
    expect(opened).toBeDefined();
    expect(group.activeTabId).toBe(opened.id);
  });

  it("switches workspace instead of opening the path under the active group", () => {
    routeContentLinkPath("/butter/issues/BUT-1");

    const state = useTabStore.getState();
    expect(state.activeWorkspaceSlug).toBe("butter");
    expect(
      state.byWorkspace.acme.tabs.some((t) => t.url === "/butter/issues/BUT-1"),
    ).toBe(false);
  });
});
