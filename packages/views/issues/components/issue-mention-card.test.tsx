import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { IssueMentionCard } from "./issue-mention-card";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/acme/issues/${id}`,
  }),
}));

vi.mock("./issue-chip", () => ({
  IssueChip: ({ fallbackLabel }: { fallbackLabel?: string }) => (
    <span data-testid="issue-chip">{fallbackLabel ?? "chip"}</span>
  ),
}));

function makeAdapter(
  overrides: Partial<NavigationAdapter> = {},
): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/",
    searchParams: new URLSearchParams(),
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function renderCard(adapter: NavigationAdapter) {
  return render(
    <NavigationProvider value={adapter}>
      <IssueMentionCard issueId="issue-1" fallbackLabel="MUL-7" />
    </NavigationProvider>,
  );
}

describe("IssueMentionCard", () => {
  it("renders a real anchor with no target — a chip click navigates in place", () => {
    renderCard(makeAdapter());
    const anchor = screen.getByTestId("issue-chip").closest("a");
    expect(anchor).toHaveAttribute("href", "/acme/issues/issue-1");
    expect(anchor).not.toHaveAttribute("target");
  });

  it("plain click pushes in place", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    renderCard(makeAdapter({ push, openInNewTab }));

    fireEvent.click(screen.getByTestId("issue-chip"));
    expect(push).toHaveBeenCalledWith("/acme/issues/issue-1");
    expect(openInNewTab).not.toHaveBeenCalled();
  });

  it("cmd-click opens a background tab labeled with the issue identifier (desktop)", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    renderCard(makeAdapter({ push, openInNewTab }));

    fireEvent.click(screen.getByTestId("issue-chip"), { metaKey: true });
    expect(openInNewTab).toHaveBeenCalledWith("/acme/issues/issue-1", "MUL-7");
    expect(push).not.toHaveBeenCalled();
  });

  it("cmd-click without an adapter (web) is left to the browser's native background-tab handling", () => {
    const push = vi.fn();
    renderCard(makeAdapter({ push }));

    const defaultNotPrevented = fireEvent.click(
      screen.getByTestId("issue-chip"),
      { metaKey: true },
    );
    expect(defaultNotPrevented).toBe(true);
    expect(push).not.toHaveBeenCalled();
  });
});
