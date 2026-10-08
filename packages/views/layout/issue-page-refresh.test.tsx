import { act, cleanup, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";
import { IssuesPage } from "../issues/components/issues-page";
import { MyIssuesPage } from "../my-issues/components/my-issues-page";

const state = vi.hoisted(() => ({ refreshing: false, user: { id: "user-1" } as { id: string } | null }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (s: typeof state) => unknown) => select(state),
}));
vi.mock("@multica/core/issues/stores/issues-scope-store", () => ({
  useIssuesScope: () => "all",
}));
vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  useViewStore: (select: (s: object) => unknown) => select({ dateFilter: null, setDateFilter: vi.fn() }),
}));
vi.mock("@multica/core/issues/stores/my-issues-view-store", () => {
  const scopeState = { scope: "all", setScope: vi.fn() };
  return {
    myIssuesRelationFromScope: () => "all",
    myIssuesViewStore: { getState: () => scopeState, getInitialState: () => scopeState, subscribe: () => () => {} },
  };
});
vi.mock("../issues/surface/issue-surface", () => ({
  IssueSurface: ({ renderHeader }: { renderHeader: (context: object) => React.ReactNode }) => (
    <>{renderHeader({ controller: { isRefreshing: state.refreshing, surfaceIssues: [] } })}</>
  ),
}));
vi.mock("../issues/components/issues-header", () => ({
  IssuesHeader: () => <div>Issue controls</div>,
}));
vi.mock("../my-issues/components/my-issues-header", () => ({
  MyIssuesHeader: () => <div>My issue controls</div>,
}));

describe("issue page refresh feedback", () => {
  beforeEach(() => {
    state.refreshing = false;
    state.user = { id: "user-1" };
    vi.useFakeTimers();
  });
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it.each([IssuesPage, MyIssuesPage])("routes surface refresh state into the page title (%#)", (Page) => {
    const { rerender } = renderWithI18n(<Page />);
    const header = screen.getByRole("banner");
    expect(within(header).getByRole("heading")).toBeInTheDocument();
    expect(within(header).queryByRole("status")).not.toBeInTheDocument();

    state.refreshing = true;
    rerender(<Page />);
    act(() => vi.advanceTimersByTime(300));
    expect(within(header).getByRole("status")).toBeInTheDocument();

    state.refreshing = false;
    rerender(<Page />);
    expect(within(header).queryByRole("status")).not.toBeInTheDocument();
    expect(screen.getAllByRole("banner")).toHaveLength(1);
  });

  it("keeps the My Issues title visible before the user is available", () => {
    state.user = null;
    renderWithI18n(<MyIssuesPage />);
    expect(within(screen.getByRole("banner")).getByRole("heading")).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });
});
