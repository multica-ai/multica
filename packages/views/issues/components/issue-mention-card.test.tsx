import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { IssueMentionCard } from "./issue-mention-card";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";

const { issueLinkState } = vi.hoisted(() => ({
  issueLinkState: { openInNewTab: true, setOpenInNewTab: vi.fn() },
}));

vi.mock("@multica/core/issues/stores", () => {
  const useIssueLinkStore = (
    selector?: (s: typeof issueLinkState) => unknown,
  ) => (selector ? selector(issueLinkState) : issueLinkState);
  useIssueLinkStore.getState = () => issueLinkState;
  return { useIssueLinkStore };
});

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
  beforeEach(() => {
    issueLinkState.openInNewTab = true;
  });

  it("with the new-tab preference on (default), plain click opens a foreground new tab and does not push", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    renderCard(makeAdapter({ push, openInNewTab }));

    const anchor = screen.getByTestId("issue-chip").closest("a");
    expect(anchor).toHaveAttribute("target", "_blank");

    fireEvent.click(screen.getByTestId("issue-chip"));
    expect(openInNewTab).toHaveBeenCalledWith("/acme/issues/issue-1", "MUL-7", {
      activate: true,
    });
    expect(push).not.toHaveBeenCalled();
  });

  it("with the preference off, plain click navigates in place", () => {
    issueLinkState.openInNewTab = false;
    const push = vi.fn();
    const openInNewTab = vi.fn();
    renderCard(makeAdapter({ push, openInNewTab }));

    const anchor = screen.getByTestId("issue-chip").closest("a");
    expect(anchor).not.toHaveAttribute("target");

    fireEvent.click(screen.getByTestId("issue-chip"));
    expect(push).toHaveBeenCalledWith("/acme/issues/issue-1");
    expect(openInNewTab).not.toHaveBeenCalled();
  });

  it("with the preference on but no adapter openInNewTab (web), leaves the click to the browser's native target=_blank handling", () => {
    const push = vi.fn();
    renderCard(makeAdapter({ push }));

    const defaultNotPrevented = fireEvent.click(
      screen.getByTestId("issue-chip"),
    );
    expect(defaultNotPrevented).toBe(true);
    expect(push).not.toHaveBeenCalled();
  });
});
