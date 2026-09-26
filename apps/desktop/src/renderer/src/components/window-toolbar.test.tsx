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

const sidebarState = vi.hoisted(() => ({
  state: "expanded" as "expanded" | "collapsed",
  isCompact: false,
}));

vi.mock("@/hooks/use-tab-history", () => ({
  useTabHistory: () => historyState,
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

const { WINDOW_TOOLBAR_CLEARANCE, WindowToolbar, historyIndicesForMenu } =
  await import("./window-toolbar");

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

describe("WindowToolbar history controls", () => {
  it("left-aligns the controls past the traffic lights across the expanded sidebar", () => {
    render(<WindowToolbar />);

    const toolbar = document.querySelector('[data-slot="window-toolbar"]');
    expect(toolbar).not.toHaveClass("justify-end");
    expect(toolbar).toHaveStyle({
      paddingLeft: "88px",
      width:
        "max(var(--sidebar-live-width, var(--sidebar-width)), 200px)",
    });
    expect(toolbar).toHaveAttribute("data-sidebar-resize-consumer");
    expect(toolbar).not.toHaveClass("transition-[width]");
  });

  it("keeps the controls clear of the traffic lights after toggling the sidebar", () => {
    const { rerender } = render(<WindowToolbar />);
    sidebarState.state = "collapsed";
    rerender(<WindowToolbar />);

    const toolbar = document.querySelector('[data-slot="window-toolbar"]');
    expect(WINDOW_TOOLBAR_CLEARANCE).toBe(200);
    expect(toolbar).toHaveStyle({ paddingLeft: "88px", width: "200px" });
  });

  it("orders Back, Forward, then the sidebar toggle with no separate History button", () => {
    render(<WindowToolbar />);

    expect(
      screen.getAllByRole("button").map((button) => button.getAttribute("aria-label")),
    ).toEqual(["Go back", "Go forward", "Toggle sidebar"]);
    expect(screen.queryByRole("button", { name: "History" })).toBeNull();
  });

  it("disables Back and Forward in a fresh tab", () => {
    historyState.canGoBack = false;
    historyState.canGoForward = false;
    historyState.historyEntries = ["/acme/issues"];
    historyState.historyIndex = 0;

    render(<WindowToolbar />);

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
