import type { ComponentProps } from "react";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const historyState = vi.hoisted(() => ({
  canGoBack: true,
  canGoForward: true,
  historyEntries: ["/acme/issues", "/acme/projects", "/acme/agents"],
  historyIndex: 1,
  goBack: vi.fn(),
  goForward: vi.fn(),
  goToHistoryIndex: vi.fn(),
}));

vi.mock("@/hooks/use-tab-history", () => ({
  useTabHistory: () => historyState,
}));

vi.mock("@multica/views/layout", () => ({
  useTabPresentation: (url: string) => ({
    visual: { kind: "icon", icon: "Inbox" },
    title: `Title ${url}`,
  }),
  ResourceLeadingVisual: () => <span aria-hidden />,
}));

vi.mock("@multica/ui/components/ui/sidebar", () => ({
  SidebarTrigger: (props: ComponentProps<"button">) => (
    <button type="button" aria-label="Toggle sidebar" {...props} />
  ),
}));

const { WindowToolbar, historyIndicesForMenu } = await import(
  "./window-toolbar"
);

beforeEach(() => {
  historyState.goBack.mockReset();
  historyState.goForward.mockReset();
  historyState.goToHistoryIndex.mockReset();
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

  it("shows the full history newest-first without the current page", () => {
    expect(historyIndicesForMenu("all", 2, 5)).toEqual([4, 3, 1, 0]);
  });

  it("bounds the recently viewed menu to thirty entries", () => {
    expect(historyIndicesForMenu("all", 59, 60)).toHaveLength(30);
    expect(historyIndicesForMenu("back", 59, 60)).toHaveLength(30);
    expect(historyIndicesForMenu("forward", 0, 60)).toHaveLength(30);
  });
});

describe("WindowToolbar history controls", () => {
  it("opens the complete history and jumps directly to a selected entry", () => {
    render(<WindowToolbar />);

    fireEvent.click(screen.getByRole("button", { name: "History" }));
    expect(screen.getByText("Recently viewed")).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("menuitem", { name: "Title /acme/agents" }),
    );
    expect(historyState.goToHistoryIndex).toHaveBeenCalledWith(2);
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
});
