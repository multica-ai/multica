import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, fireEvent, within } from "@testing-library/react";

type MockTab = {
  id: string;
  path: string;
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
  setActiveTab: vi.fn<(tabId: string) => void>(),
  moveTab: vi.fn<(from: number, to: number) => void>(),
  addTab: vi.fn<(path: string, title: string, icon: string) => string>(),
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
  state.setActiveTab.mockReset();
  state.moveTab.mockReset();
  state.addTab.mockReset();
}

beforeEach(reset);

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
