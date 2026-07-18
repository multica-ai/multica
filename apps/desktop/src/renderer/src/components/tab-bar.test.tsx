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
  path: string;
  url?: string;
  title: string;
  icon: string;
  pinned: boolean;
};

const state = vi.hoisted(() => ({
  activeWorkspaceSlug: "acme" as string | null,
  byWorkspace: {
    acme: {
      activeTabId: "tA",
      tabs: [
        { id: "tA", path: "/acme/issues", title: "Issues", icon: "ListTodo", pinned: false },
        { id: "tB", path: "/acme/projects", title: "Projects", icon: "ListTodo", pinned: false },
      ] as MockTab[],
    },
  } as Record<string, { activeTabId: string; tabs: MockTab[] }>,
  togglePin: vi.fn<(tabId: string) => void>(),
  closeTab: vi.fn<(tabId: string) => void>(),
  closeOtherTabs: vi.fn<(tabId: string) => void>(),
  setActiveTab: vi.fn<(tabId: string) => void>(),
  moveTab: vi.fn<(from: number, to: number) => void>(),
  addTab: vi.fn<(path: string, title: string, icon: string) => string>(),
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
  const resolveRouteIcon = () => "ListTodo";
  return { useTabStore, useActiveGroup, resolveRouteIcon };
});

vi.mock("@multica/core/paths", () => ({
  paths: {
    workspace: (slug: string) => ({
      issues: () => `/${slug}/issues`,
    }),
  },
}));

import { TabBar } from "./tab-bar";

function reset() {
  state.activeWorkspaceSlug = "acme";
  state.byWorkspace = {
    acme: {
      activeTabId: "tA",
      tabs: [
        { id: "tA", path: "/acme/issues", title: "Issues", icon: "ListTodo", pinned: false },
        { id: "tB", path: "/acme/projects", title: "Projects", icon: "ListTodo", pinned: false },
      ],
    },
  };
  state.togglePin.mockReset();
  state.closeTab.mockReset();
  state.closeOtherTabs.mockReset();
  state.setActiveTab.mockReset();
  state.moveTab.mockReset();
  state.addTab.mockReset();
  state.openIssueWindow.mockReset();
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
      { id: "tA", path: "/acme/issues", title: "Issues", icon: "ListTodo", pinned: true },
      { id: "tB", path: "/acme/projects", title: "Projects", icon: "ListTodo", pinned: false },
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
      { id: "tA", path: "/acme/issues", title: "Issues", icon: "ListTodo", pinned: true },
      { id: "tB", path: "/acme/projects", title: "Projects", icon: "ListTodo", pinned: false },
    ];
    const { getByLabelText } = render(<TabBar />);
    fireEvent.click(getByLabelText("Unpin tab"));
    expect(state.togglePin).toHaveBeenCalledWith("tA");
  });

  it("hides the X close button on a pinned tab but keeps it on an unpinned tab", () => {
    state.byWorkspace.acme.tabs = [
      { id: "tA", path: "/acme/issues", title: "Issues", icon: "ListTodo", pinned: true },
      { id: "tB", path: "/acme/projects", title: "Projects", icon: "ListTodo", pinned: false },
    ];
    const { queryAllByLabelText } = render(<TabBar />);
    // Only the unpinned tab exposes a Close affordance — pinned tab requires
    // explicit Unpin first (RFC §3 D3c FINAL).
    expect(queryAllByLabelText("Close tab")).toHaveLength(1);
  });

  it("keeps the full title visible on a pinned tab (no icon-only collapse)", () => {
    state.byWorkspace.acme.tabs = [
      { id: "tA", path: "/acme/issues", title: "Issues", icon: "ListTodo", pinned: true },
    ];
    const { getByLabelText } = render(<TabBar />);
    const pinnedTab = getByLabelText("Issues (pinned)");
    expect(within(pinnedTab).getByText("Issues")).toBeTruthy();
  });

  it("renders the Pin glyph as the leading icon on a pinned tab and the route icon on an unpinned tab", () => {
    state.byWorkspace.acme.tabs = [
      { id: "tA", path: "/acme/issues", title: "Issues", icon: "ListTodo", pinned: true },
      { id: "tB", path: "/acme/projects", title: "Projects", icon: "ListTodo", pinned: false },
    ];
    const { getByLabelText } = render(<TabBar />);
    const pinnedTab = getByLabelText("Issues (pinned)");
    const unpinnedTab = getByLabelText("Projects");
    // lucide-react renders the icon name into the class list. The leading
    // slot icon is size-3.5; the hover Pin/Unpin action button is size-2.5,
    // so we qualify on size to avoid matching the action glyph.
    expect(pinnedTab.querySelector(".lucide-pin.size-3\\.5")).toBeTruthy();
    expect(pinnedTab.querySelector(".lucide-list-todo")).toBeNull();
    expect(unpinnedTab.querySelector(".lucide-list-todo.size-3\\.5")).toBeTruthy();
    expect(unpinnedTab.querySelector(".lucide-pin.size-3\\.5")).toBeNull();
  });
});

describe("TabBar overflow", () => {
  it("keeps tabs readable in a bounded horizontal scroller", () => {
    state.byWorkspace.acme.tabs = Array.from({ length: 8 }, (_, index) => ({
      id: `t${index}`,
      path: `/acme/tab-${index}`,
      title: `Tab ${index}`,
      icon: "ListTodo",
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
      path: `/acme/tab-${index}`,
      title: `Tab ${index}`,
      icon: "ListTodo",
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
      path: `/acme/tab-${index}`,
      title: `Tab ${index}`,
      icon: "ListTodo",
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
        path: "/acme/tab-6",
        title: "Tab 6",
        icon: "ListTodo",
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
      path: `/acme/tab-${index}`,
      title: `Tab ${index}`,
      icon: "ListTodo",
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
        path: "/acme/tab-6",
        title: "Tab 6",
        icon: "ListTodo",
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
        path: "/acme/issues/issue-1",
        url: "/acme/issues/issue-1?comment=comment-1",
        title: "MUL-1: Fix tabs",
        icon: "ListTodo",
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
        path: "/acme/issues",
        url: "/acme/issues",
        title: "Issues",
        icon: "ListTodo",
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
      { id: "tA", path: "/acme/issues", title: "Issues", icon: "ListTodo", pinned: true },
      { id: "tB", path: "/acme/projects", title: "Projects", icon: "ListTodo", pinned: false },
      { id: "tC", path: "/acme/agents", title: "Agents", icon: "Bot", pinned: false },
    ];

    const { findByText, getByLabelText } = render(<TabBar />);
    fireEvent.contextMenu(getByLabelText("Projects"));
    fireEvent.click(await findByText("Close other tabs"));

    expect(state.closeOtherTabs).toHaveBeenCalledWith("tB");
  });

});
