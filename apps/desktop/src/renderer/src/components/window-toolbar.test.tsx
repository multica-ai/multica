import type { ComponentProps } from "react";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const historyState = vi.hoisted(() => ({
  canGoBack: true,
  canGoForward: true,
  historyEntries: ["/acme/issues", "/acme/projects", "/acme/agents"],
  historyIndex: 1,
  browsingHistory: [
    { url: "/acme/settings", title: "Settings" },
    { url: "/acme/issues/issue-1", title: "MUL-1: Fix history" },
    { url: "/acme/projects", title: "Projects" },
  ],
  goBack: vi.fn(),
  goForward: vi.fn(),
  goToHistoryIndex: vi.fn(),
}));

const navigationState = vi.hoisted(() => ({ push: vi.fn() }));
const sidebarState = vi.hoisted(() => ({
  state: "expanded" as "expanded" | "collapsed",
  isCompact: false,
}));

vi.mock("@/hooks/use-tab-history", () => ({
  useTabHistory: () => historyState,
}));

vi.mock("@multica/views/navigation", () => ({
  useNavigation: () => navigationState,
}));

vi.mock("@multica/views/layout", () => ({
  useTabPresentation: (url: string, fallbackTitle?: string) => ({
    visual: { kind: "icon", icon: "Inbox" },
    title: fallbackTitle ?? `Title ${url}`,
  }),
  ResourceLeadingVisual: () => <span aria-hidden />,
}));

vi.mock("@multica/ui/components/ui/sidebar", () => ({
  useSidebar: () => sidebarState,
  SidebarTrigger: (props: ComponentProps<"button">) => (
    <button type="button" aria-label="Toggle sidebar" {...props} />
  ),
}));

const {
  WINDOW_TOOLBAR_CLEARANCE,
  WindowToolbar,
  browsingHistoryForMenu,
  historyIndicesForMenu,
} = await import("./window-toolbar");

beforeEach(() => {
  historyState.canGoBack = true;
  historyState.canGoForward = true;
  historyState.historyEntries = [
    "/acme/issues",
    "/acme/projects",
    "/acme/agents",
  ];
  historyState.historyIndex = 1;
  historyState.browsingHistory = [
    { url: "/acme/settings", title: "Settings" },
    { url: "/acme/issues/issue-1", title: "MUL-1: Fix history" },
    { url: "/acme/projects", title: "Projects" },
  ];
  historyState.goBack.mockReset();
  historyState.goForward.mockReset();
  historyState.goToHistoryIndex.mockReset();
  navigationState.push.mockReset();
  sidebarState.state = "expanded";
  sidebarState.isCompact = false;
  vi.useFakeTimers();
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("historyIndicesForMenu", () => {
  it("orders backward destinations from nearest to furthest", () => {
    expect(historyIndicesForMenu("back", 3, 5)).toEqual([2, 1, 0]);
  });

  it("orders forward destinations from nearest to furthest", () => {
    expect(historyIndicesForMenu("forward", 1, 5)).toEqual([2, 3, 4]);
  });

  it("bounds directional history to thirty entries", () => {
    expect(historyIndicesForMenu("back", 59, 60)).toHaveLength(30);
    expect(historyIndicesForMenu("forward", 0, 60)).toHaveLength(30);
  });
});

describe("browsingHistoryForMenu", () => {
  it("uses workspace browsing history across tabs and excludes the current resource", () => {
    expect(
      browsingHistoryForMenu(
        [
          { url: "/acme/issues/issue-2", title: "Issue 2" },
          { url: "/acme/issues?filter=mine", title: "Issues" },
          { url: "/acme/projects", title: "Projects" },
        ],
        "/acme/issues",
      ),
    ).toEqual([
      { url: "/acme/issues/issue-2", title: "Issue 2" },
      { url: "/acme/projects", title: "Projects" },
    ]);
  });

  it("excludes only the current Inbox issue while keeping other Inbox visits", () => {
    expect(
      browsingHistoryForMenu(
        [
          { url: "/acme/inbox?issue=issue-b", title: "Issue B" },
          { url: "/acme/inbox?issue=issue-a", title: "Issue A" },
          { url: "/acme/inbox", title: "Inbox" },
          { url: "/acme/projects", title: "Projects" },
        ],
        "/acme/inbox?view=archived&issue=issue-b",
      ),
    ).toEqual([
      { url: "/acme/inbox?issue=issue-a", title: "Issue A" },
      { url: "/acme/inbox", title: "Inbox" },
      { url: "/acme/projects", title: "Projects" },
    ]);
  });

  it("bounds the recently viewed menu to thirty entries", () => {
    const entries = Array.from(
      { length: 60 },
      (_, index) => ({
        url: `/acme/issues/issue-${index}`,
        title: `Issue ${index}`,
      }),
    );
    expect(browsingHistoryForMenu(entries, "/acme/settings")).toHaveLength(30);
  });
});

describe("WindowToolbar history controls", () => {
  it("right-aligns the controls to the expanded sidebar edge", () => {
    render(<WindowToolbar />);

    const toolbar = document.querySelector('[data-slot="window-toolbar"]');
    expect(toolbar).toHaveClass("justify-end");
    expect(toolbar).toHaveStyle({
      width:
        "max(var(--sidebar-live-width, var(--sidebar-width)), 256px)",
    });
    expect(toolbar).toHaveAttribute("data-sidebar-resize-consumer");
    expect(toolbar).not.toHaveClass("transition-[width]");
  });

  it("keeps the controls clear of the traffic lights after toggling the sidebar", () => {
    const { rerender } = render(<WindowToolbar />);
    sidebarState.state = "collapsed";
    rerender(<WindowToolbar />);

    const toolbar = document.querySelector('[data-slot="window-toolbar"]');
    expect(WINDOW_TOOLBAR_CLEARANCE).toBe(256);
    expect(toolbar).toHaveClass("justify-end");
    expect(toolbar).toHaveStyle({ width: "256px" });
  });

  it("opens workspace browsing history and navigates the active tab to a selected entry", () => {
    render(<WindowToolbar />);

    fireEvent.click(screen.getByRole("button", { name: "History" }));
    expect(screen.getByText("Recently viewed")).toBeInTheDocument();
    expect(screen.getByTitle("MUL-1: Fix history")).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("menuitem", { name: "MUL-1: Fix history" }),
    );
    expect(navigationState.push).toHaveBeenCalledWith(
      "/acme/issues/issue-1",
    );
    expect(historyState.goToHistoryIndex).not.toHaveBeenCalled();
  });

  it("keeps browsing history available in a fresh tab while its Back and Forward stay disabled", () => {
    historyState.canGoBack = false;
    historyState.canGoForward = false;
    historyState.historyEntries = ["/acme/issues"];
    historyState.historyIndex = 0;
    historyState.browsingHistory = [
      { url: "/acme/issues", title: "Issues" },
      { url: "/acme/issues/issue-1", title: "MUL-1: Fix history" },
    ];

    render(<WindowToolbar />);

    expect(screen.getByRole("button", { name: "History" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Go back" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Go forward" })).toBeDisabled();
  });

  it("keeps a normal Back click as one-step navigation", () => {
    render(<WindowToolbar />);

    fireEvent.click(screen.getByRole("button", { name: "Go back" }));
    expect(historyState.goBack).toHaveBeenCalledOnce();
  });

  it("opens Back history on a sustained hold without also stepping back", () => {
    render(<WindowToolbar />);
    const back = screen.getByRole("button", { name: "Go back" });

    fireEvent.pointerDown(back, { button: 0, clientX: 20, clientY: 20 });
    act(() => vi.advanceTimersByTime(2_000));
    expect(screen.getByText("Back history")).toBeInTheDocument();

    fireEvent.pointerUp(back, { button: 0, clientX: 20, clientY: 20 });
    fireEvent.click(back);
    expect(historyState.goBack).not.toHaveBeenCalled();
    expect(screen.getByText("Back history")).toBeInTheDocument();
  });

  it("offers the same menu from ArrowDown for keyboard users", () => {
    render(<WindowToolbar />);
    const forward = screen.getByRole("button", { name: "Go forward" });

    fireEvent.keyDown(forward, { key: "ArrowDown" });
    expect(screen.getByText("Forward history")).toBeInTheDocument();
  });

  it("jumps to the selected Back history index", () => {
    render(<WindowToolbar />);
    const back = screen.getByRole("button", { name: "Go back" });

    fireEvent.keyDown(back, { key: "ArrowDown" });
    fireEvent.click(
      screen.getByRole("menuitem", { name: "Title /acme/issues" }),
    );

    expect(historyState.goToHistoryIndex).toHaveBeenCalledWith(0);
  });

  it("restores focus to the toolbar button when its menu closes", () => {
    render(<WindowToolbar />);
    const forward = screen.getByRole("button", { name: "Go forward" });
    forward.focus();

    fireEvent.keyDown(forward, { key: "ArrowDown" });
    act(() => vi.runOnlyPendingTimers());
    expect(screen.getAllByRole("menuitem")[0]).toHaveFocus();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
    act(() => vi.runOnlyPendingTimers());

    expect(forward).toHaveFocus();
  });
});
