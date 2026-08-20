import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { renderWithI18n } from "../../test/i18n";
import { IssueMentionCard } from "./issue-mention-card";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";
import {
  CurrentIssueRenderContextProvider,
  type CurrentIssueRenderContextValue,
} from "../current-issue-render-context";

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/acme/issues/${id}`,
  }),
}));

vi.mock("./issue-chip", () => ({
  IssueChip: ({
    fallbackLabel,
    children,
  }: {
    fallbackLabel?: string;
    children?: ReactNode;
  }) => (
    <span
      data-testid="issue-chip"
      data-current={children !== undefined ? "true" : "false"}
    >
      {children ?? fallbackLabel ?? "chip"}
    </span>
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

function renderCard(
  adapter: NavigationAdapter,
  context?: CurrentIssueRenderContextValue,
  issueId = "issue-1",
) {
  const card = (
    <IssueMentionCard issueId={issueId} fallbackLabel="MUL-7" />
  );
  return renderWithI18n(
    <NavigationProvider value={adapter}>
      {context ? (
        <CurrentIssueRenderContextProvider value={context}>
          {card}
        </CurrentIssueRenderContextProvider>
      ) : (
        card
      )}
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

  // The real IssueHoverCard is deliberately not mocked: the point of these two
  // is that mentions get the card for real, and that wrapping the link in a
  // trigger did not cost the link anything. Neither opens the card — the card's
  // own contents are covered in issue-hover-card.test.tsx, and hovering here
  // would only duplicate that file's query and avatar mocks.
  it("wraps the mention in a real hover-card trigger", () => {
    renderCard(makeAdapter());

    const trigger = screen
      .getByTestId("issue-chip")
      .closest('[data-slot="hover-card-trigger"]');
    expect(trigger).toBeInTheDocument();
    // A span, not the trigger's default anchor: the link is inside it, and
    // nested anchors would break both the DOM and the assertions above.
    expect(trigger?.tagName).toBe("SPAN");
  });

  it("keeps the chip a navigable link inside the trigger", () => {
    const push = vi.fn();
    renderCard(makeAdapter({ push }));

    const anchor = screen.getByTestId("issue-chip").closest("a");
    expect(anchor).toHaveAttribute("href", "/acme/issues/issue-1");
    expect(
      anchor?.closest('[data-slot="hover-card-trigger"]'),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("issue-chip"));
    expect(push).toHaveBeenCalledWith("/acme/issues/issue-1");
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

  it("uses current-issue content when the resolved target matches the current issue id", () => {
    renderCard(makeAdapter(), { id: "issue-1", identifier: "MUL-7" });

    expect(screen.getByTestId("issue-chip")).toHaveAttribute("data-current", "true");
    expect(screen.getByTestId("issue-chip")).toHaveTextContent("This issue · MUL-7");
  });

  it("does not infer current-issue context from the visible fallback label", () => {
    renderCard(makeAdapter(), { id: "issue-2", identifier: "MUL-7" });

    expect(screen.getByTestId("issue-chip")).toHaveAttribute("data-current", "false");
  });
});
