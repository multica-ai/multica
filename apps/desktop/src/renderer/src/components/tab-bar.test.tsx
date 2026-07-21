import { afterAll, describe, expect, it, vi, beforeEach } from "vitest";
import {
  render,
  renderHook,
  fireEvent,
  waitFor,
  within,
} from "@testing-library/react";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";

type MockTab = {
  id: string;
  url: string;
  title: string;
  pinned: boolean;
};

const state = vi.hoisted(() => ({
  activeWorkspaceSlug: "acme" as string | null,
  byWorkspace: {
    acme: {
      activeTabId: "tA",
      tabs: [
        { id: "tA", url: "/acme/issues", title: "Issues", pinned: false },
        { id: "tB", url: "/acme/projects", title: "Projects", pinned: false },
      ] as MockTab[],
    },
  } as Record<string, { activeTabId: string; tabs: MockTab[] }>,
  togglePin: vi.fn<(tabId: string) => void>(),
  closeTab: vi.fn<(tabId: string) => void>(),
  closeOtherTabs: vi.fn<(tabId: string) => void>(),
  setActiveTab: vi.fn<(tabId: string) => void>(),
  moveTab: vi.fn<(from: number, to: number) => void>(),
  addTab: vi.fn<(path: string, title: string) => string>(),
  updateTab: vi.fn<(tabId: string, patch: { title?: string }) => void>(),
  openIssueWindow: vi.fn(),
}));

vi.mock("@/stores/tab-store", () => {
  const store = {
    get activeWorkspaceSlug() {
      return state.activeWorkspaceSlug;
    },
    get byWorkspace() {
      return state.byWorkspace;
    },
    togglePin: state.togglePin,
    closeTab: state.closeTab,
    closeOtherTabs: state.closeOtherTabs,
    setActiveTab: state.setActiveTab,
    moveTab: state.moveTab,
    addTab: state.addTab,
    updateTab: state.updateTab,
  };
  const useTabStore = Object.assign(
    (selector?: (s: typeof store) => unknown) =>
      selector ? selector(store) : store,
    { getState: () => store },
  );
  const useActiveGroup = () =>
    state.activeWorkspaceSlug
      ? (state.byWorkspace[state.activeWorkspaceSlug] ?? null)
      : null;
  return { useTabStore, useActiveGroup };
});

vi.mock("@multica/core/paths", async (importOriginal) => ({
  // Spread the real module so pure helpers (parseTabSubject etc.) keep working.
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  paths: {
    workspace: (slug: string) => ({
      issues: () => `/${slug}/issues`,
    }),
  },
}));

// The tab bar's presentation (URL/cache → visual + title) is covered by the
// views tab-presentation tests. Here we stub it so the strip-behavior tests
// (overflow, pin, close, context menu) don't need the whole query stack. By
// default `title` mirrors the persisted tab title (matching the real hook's
// fallback); a test can set `pres.title` to simulate a resolved title that
// differs, to exercise the active-tab persist effect.
const pres = vi.hoisted(() => ({ title: null as string | null }));
vi.mock("@multica/views/layout", () => ({
  useTabPresentation: (_url: string, fallbackTitle?: string) => ({
    visual: { kind: "icon", icon: "ListTodo" },
    title: pres.title ?? fallbackTitle ?? "",
  }),
  ResourceLeadingVisual: ({ visual }: { visual: { kind: string; icon?: string } }) => (
    <span data-testid="tab-leading" data-visual-kind={visual.kind} data-icon={visual.icon} />
  ),
}));

import { TabBar } from "./tab-bar";

function reset() {
  state.activeWorkspaceSlug = "acme";
  state.byWorkspace = {
    acme: {
      activeTabId: "tA",
      tabs: [
        { id: "tA", url: "/acme/issues", title: "Issues", pinned: false },
        { id: "tB", url: "/acme/projects", title: "Projects", pinned: false },
      ],
    },
  };
  state.togglePin.mockReset();
  state.closeTab.mockReset();
  state.closeOtherTabs.mockReset();
  state.setActiveTab.mockReset();
  state.moveTab.mockReset();
  state.addTab.mockReset();
  state.updateTab.mockReset();
  state.openIssueWindow.mockReset();
  pres.title = null;
}

beforeEach(() => {
  reset();
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }));
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  vi.stubGlobal("desktopAPI", {
    openIssueWindow: state.openIssueWindow,
  });
});

afterAll(() => vi.unstubAllGlobals());

describe("TabBar hover action buttons", () => {
  it("renders a Pin button on every unpinned tab and an Unpin button on every pinned tab", () => {
    state.byWorkspace.acme.tabs = [
      { id: "tA", url: "/acme/issues", title: "Issues", pinned: true },
      { id: "tB", url: "/acme/projects", title: "Projects", pinned: false },
    ];
    const { getAllByLabelText } = render(<TabBar />);
    expect(getAllByLabelText("Unpin tab")).toHaveLength(1);
    expect(getAllByLabelText("Pin tab")).toHaveLength(1);
  });

  it("clicking the Pin button calls togglePin for the tab", () => {
    const { getAllByLabelText } = render(<TabBar />);
    const pinButtons = getAllByLabelText("Pin tab");
    fireEvent.click(pinButtons[1]); // click Pin on tB (Projects)
    expect(state.togglePin).toHaveBeenCalledWith("tB");
  });

  it("clicking the Unpin button on a pinned tab calls togglePin", () => {
    state.byWorkspace.acme.tabs = [
      { id: "tA", url: "/acme/issues", title: "Issues", pinned: true },
      { id: "tB", url: "/acme/projects", title: "Projects", pinned: false },
    ];
    const { getByLabelText } = render(<TabBar />);
    fireEvent.click(getByLabelText("Unpin tab"));
    expect(state.togglePin).toHaveBeenCalledWith("tA");
  });

  it("hides the X close button on a pinned tab but keeps it on an unpinned tab", () => {
    state.byWorkspace.acme.tabs = [
      { id: "tA", url: "/acme/issues", title: "Issues", pinned: true },
      { id: "tB", url: "/acme/projects", title: "Projects", pinned: false },
    ];
    const { queryAllByLabelText } = render(<TabBar />);
    // Only the unpinned tab exposes a Close affordance — pinned tab requires
    // explicit Unpin first (RFC §3 D3c FINAL).
    expect(queryAllByLabelText("Close tab")).toHaveLength(1);
  });

  it("keeps the full title visible on a pinned tab (no icon-only collapse)", () => {
    state.byWorkspace.acme.tabs = [
      { id: "tA", url: "/acme/issues", title: "Issues", pinned: true },
    ];
    const { getByLabelText } = render(<TabBar />);
    const pinnedTab = getByLabelText("Issues (pinned)");
    expect(within(pinnedTab).getByText("Issues")).toBeTruthy();
  });

  // MUL-4370: each tab renders the shared ResourceLeadingVisual keyed off its
  // URL. (Which visual/title a URL resolves to is covered by the views
  // tab-presentation tests; here we only assert the strip wires it in.)
  it("renders the resource leading visual for every tab", () => {
    state.byWorkspace.acme.tabs = [
      { id: "tA", url: "/acme/autopilots", title: "Autopilots", pinned: false },
      { id: "tB", url: "/acme/projects/proj-1", title: "Project", pinned: false },
    ];
    const { getByLabelText } = render(<TabBar />);
    expect(
      getByLabelText("Autopilots").querySelector('[data-testid="tab-leading"]'),
    ).toBeTruthy();
    expect(
      getByLabelText("Project").querySelector('[data-testid="tab-leading"]'),
    ).toBeTruthy();
  });

  // Pin is a secondary state, not an identity: a pinned tab keeps its resource
  // leading visual rather than collapsing to a Pin glyph (PRD).
  it("keeps the resource leading visual on a pinned tab (pin does not replace it)", () => {
    state.byWorkspace.acme.tabs = [
      { id: "tA", url: "/acme/issues", title: "Issues", pinned: true },
      { id: "tB", url: "/acme/projects", title: "Projects", pinned: false },
    ];
    const { getByLabelText } = render(<TabBar />);
    const pinnedTab = getByLabelText("Issues (pinned)");
    // The leading slot is the resource visual (size-3.5), not a Pin glyph. The
    // only Pin lives in the size-2.5 hover action button.
    expect(pinnedTab.querySelector('[data-testid="tab-leading"]')).toBeTruthy();
    expect(pinnedTab.querySelector(".lucide-pin.size-3\\.5")).toBeNull();
  });
});

describe("TabBar active-tab title persistence", () => {
  it("persists the resolved title only for the active tab", () => {
    pres.title = "MUL-1: Fixed";
    state.byWorkspace.acme.tabs = [
      { id: "tA", url: "/acme/issues/i1", title: "Issues", pinned: false },
      { id: "tB", url: "/acme/projects", title: "Projects", pinned: false },
    ];
    state.byWorkspace.acme.activeTabId = "tA";
    render(<TabBar />);
    expect(state.updateTab).toHaveBeenCalledWith("tA", { title: "MUL-1: Fixed" });
    expect(state.updateTab).not.toHaveBeenCalledWith("tB", expect.anything());
    expect(state.updateTab).toHaveBeenCalledTimes(1);
  });

  it("does not re-persist when the resolved title already matches (no loop)", () => {
    // Active tab's persisted title equals the resolved title.
    pres.title = "Issues";
    state.byWorkspace.acme.activeTabId = "tA"; // tA.title === "Issues"
    render(<TabBar />);
    expect(state.updateTab).not.toHaveBeenCalled();
  });
});

describe("TabBar overflow", () => {
  it("keeps tabs readable in a bounded horizontal scroller", () => {
    state.byWorkspace.acme.tabs = Array.from({ length: 8 }, (_, index) => ({
      id: `t${index}`,
      url: `/acme/tab-${index}`,
      title: `Tab ${index}`,
      pinned: index === 0,
    }));

    const { container, getByLabelText } = render(<TabBar />);
    const tabBar = container.firstElementChild;
    const tabScroller = container.querySelector("[data-tab-scroll-container]");

    expect(tabBar).toHaveClass("min-w-0", "max-w-full");
    expect(tabScroller).toHaveClass(
      "min-w-0",
      "no-scrollbar",
      "overflow-x-auto",
      "overflow-y-hidden",
    );
    expect(getByLabelText("Tab 1").closest("[data-tab-frame]")).toHaveClass(
      "w-40",
      "min-w-32",
    );

    const newTabButton = getByLabelText("New tab");
    expect(tabScroller).not.toContainElement(newTabButton);
  });

  it("uses a directional mask instead of a visible scrollbar", async () => {
    const tabScroller = document.createElement("div");
    Object.defineProperties(tabScroller, {
      clientWidth: { configurable: true, value: 320 },
      scrollWidth: { configurable: true, value: 960 },
    });
    const tabScrollRef = { current: tabScroller };
    const { result } = renderHook(() =>
      useScrollFade(tabScrollRef, 24, "horizontal"),
    );

    tabScroller.scrollLeft = 0;
    fireEvent.scroll(tabScroller);

    await waitFor(() => {
      expect(result.current?.maskImage).toBe(
        "linear-gradient(to right, black 0%, black calc(100% - 24px), transparent 100%)",
      );
    });

    tabScroller.scrollLeft = 240;
    fireEvent.scroll(tabScroller);

    await waitFor(() => {
      expect(result.current?.maskImage).toBe(
        "linear-gradient(to right, transparent 0%, black 24px, black calc(100% - 24px), transparent 100%)",
      );
    });
  });

  it("scrolls only the tab strip when the active tab moves out of view", () => {
    state.byWorkspace.acme.tabs = Array.from({ length: 6 }, (_, index) => ({
      id: `t${index}`,
      url: `/acme/tab-${index}`,
      title: `Tab ${index}`,
      pinned: false,
    }));
    state.byWorkspace.acme.activeTabId = "t0";

    const { container, getByLabelText, rerender } = render(<TabBar />);
    const tabScroller = container.querySelector(
      "[data-tab-scroll-container]",
    ) as HTMLDivElement;
    const lastTab = getByLabelText("Tab 5");

    vi.spyOn(tabScroller, "getBoundingClientRect").mockReturnValue({
      left: 100,
      right: 420,
    } as DOMRect);
    vi.spyOn(lastTab, "getBoundingClientRect").mockReturnValue({
      left: 450,
      right: 578,
    } as DOMRect);
    Object.defineProperties(tabScroller, {
      clientWidth: { configurable: true, value: 320 },
      scrollWidth: { configurable: true, value: 960 },
    });
    tabScroller.scrollLeft = 40;

    state.byWorkspace.acme.activeTabId = "t5";
    rerender(<TabBar />);

    expect(tabScroller.scrollLeft).toBe(222);
  });

  it("smoothly reveals a newly added active tab", () => {
    state.byWorkspace.acme.tabs = Array.from({ length: 6 }, (_, index) => ({
      id: `t${index}`,
      url: `/acme/tab-${index}`,
      title: `Tab ${index}`,
      pinned: false,
    }));
    state.byWorkspace.acme.activeTabId = "t0";

    const rectSpy = vi
      .spyOn(HTMLElement.prototype, "getBoundingClientRect")
      .mockImplementation(function (this: HTMLElement) {
        if (this.matches("[data-tab-scroll-container]")) {
          return { left: 100, right: 420 } as DOMRect;
        }
        if (this.matches('[data-tab-id="t6"]')) {
          return { left: 450, right: 578 } as DOMRect;
        }
        return { left: 120, right: 248 } as DOMRect;
      });

    const { container, getByLabelText, rerender } = render(<TabBar />);
    const tabScroller = container.querySelector(
      "[data-tab-scroll-container]",
    ) as HTMLDivElement;
    Object.defineProperties(tabScroller, {
      clientWidth: { configurable: true, value: 320 },
      scrollWidth: { configurable: true, value: 960 },
    });
    tabScroller.scrollLeft = 40;
    const scrollTo = vi.fn(({ left }: ScrollToOptions) => {
      if (typeof left === "number") tabScroller.scrollLeft = left;
    });
    Object.defineProperty(tabScroller, "scrollTo", {
      configurable: true,
      value: scrollTo,
    });

    state.byWorkspace.acme.tabs = [
      ...state.byWorkspace.acme.tabs,
      {
        id: "t6",
        url: "/acme/tab-6",
        title: "Tab 6",
        pinned: false,
      },
    ];
    state.byWorkspace.acme.activeTabId = "t6";
    rerender(<TabBar />);

    expect(getByLabelText("Tab 6")).toHaveAttribute(
      "data-tab-entering",
      "true",
    );
    expect(scrollTo).toHaveBeenCalledWith({ left: 222, behavior: "smooth" });
    rectSpy.mockRestore();
  });

  it("keeps background additions offscreen and acknowledges them at the edge", () => {
    state.byWorkspace.acme.tabs = Array.from({ length: 6 }, (_, index) => ({
      id: `t${index}`,
      url: `/acme/tab-${index}`,
      title: `Tab ${index}`,
      pinned: false,
    }));
    state.byWorkspace.acme.activeTabId = "t0";

    const rectSpy = vi
      .spyOn(HTMLElement.prototype, "getBoundingClientRect")
      .mockImplementation(function (this: HTMLElement) {
        if (this.matches("[data-tab-scroll-container]")) {
          return { left: 100, right: 420 } as DOMRect;
        }
        if (this.matches('[data-tab-id="t6"]')) {
          return { left: 450, right: 578 } as DOMRect;
        }
        return { left: 120, right: 248 } as DOMRect;
      });

    const { container, rerender } = render(<TabBar />);
    const tabScroller = container.querySelector(
      "[data-tab-scroll-container]",
    ) as HTMLDivElement;
    Object.defineProperties(tabScroller, {
      clientWidth: { configurable: true, value: 320 },
      scrollWidth: { configurable: true, value: 960 },
    });
    tabScroller.scrollLeft = 40;
    const scrollTo = vi.fn();
    Object.defineProperty(tabScroller, "scrollTo", {
      configurable: true,
      value: scrollTo,
    });

    state.byWorkspace.acme.tabs = [
      ...state.byWorkspace.acme.tabs,
      {
        id: "t6",
        url: "/acme/tab-6",
        title: "Tab 6",
        pinned: false,
      },
    ];
    rerender(<TabBar />);

    expect(tabScroller.scrollLeft).toBe(40);
    expect(scrollTo).not.toHaveBeenCalled();
    expect(
      container.querySelector('[data-new-tab-edge-feedback="true"]'),
    ).toBeInTheDocument();
    rectSpy.mockRestore();
  });
});

describe("TabBar context menu", () => {
  it("opens an issue-detail tab as a dedicated window", async () => {
    state.byWorkspace.acme.tabs = [
      {
        id: "tA",
        url: "/acme/issues/issue-1?comment=comment-1",
        title: "MUL-1: Fix tabs",
        pinned: false,
      },
    ];

    const { findByText, getByLabelText } = render(<TabBar />);
    fireEvent.contextMenu(getByLabelText("MUL-1: Fix tabs"));
    fireEvent.click(await findByText("Open as new window"));

    expect(state.openIssueWindow).toHaveBeenCalledWith({
      path: "/acme/issues/issue-1?comment=comment-1",
      title: "MUL-1: Fix tabs",
    });
  });

  it("does not offer a dedicated window for non-issue tabs", async () => {
    state.byWorkspace.acme.tabs = [
      {
        id: "tA",
        url: "/acme/issues",
        title: "Issues",
        pinned: false,
      },
    ];

    const { findByText, getByLabelText, queryByText } = render(<TabBar />);
    fireEvent.contextMenu(getByLabelText("Issues"));
    await findByText("Pin tab");

    expect(queryByText("Open as new window")).toBeNull();
  });

  it("closes other tabs from the context menu", async () => {
    state.byWorkspace.acme.tabs = [
      { id: "tA", url: "/acme/issues", title: "Issues", pinned: true },
      { id: "tB", url: "/acme/projects", title: "Projects", pinned: false },
      { id: "tC", url: "/acme/agents", title: "Agents", pinned: false },
    ];

    const { findByText, getByLabelText } = render(<TabBar />);
    fireEvent.contextMenu(getByLabelText("Projects"));
    fireEvent.click(await findByText("Close other tabs"));

    expect(state.closeOtherTabs).toHaveBeenCalledWith("tB");
  });

});
