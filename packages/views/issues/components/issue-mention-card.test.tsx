import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { IssueMentionCard } from "./issue-mention-card";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";

const { issueLinkState, mentionDisplayState } = vi.hoisted(() => ({
  issueLinkState: { openInNewTab: true, setOpenInNewTab: vi.fn() },
  mentionDisplayState: { mode: "full" as "plain" | "compact" | "full", setMode: vi.fn() },
}));

vi.mock("@multica/core/issues/stores", () => {
  const useIssueLinkStore = (
    selector?: (s: typeof issueLinkState) => unknown,
  ) => (selector ? selector(issueLinkState) : issueLinkState);
  useIssueLinkStore.getState = () => issueLinkState;

  const useIssueMentionDisplayStore = (
    selector?: (s: typeof mentionDisplayState) => unknown,
  ) => (selector ? selector(mentionDisplayState) : mentionDisplayState);
  useIssueMentionDisplayStore.getState = () => mentionDisplayState;

  return { useIssueLinkStore, useIssueMentionDisplayStore };
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

vi.mock("./issue-hover-card", () => ({
  IssueHoverCard: ({ children }: { children: ReactNode }) => (
    <span data-testid="issue-hover-card">{children}</span>
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
    mentionDisplayState.mode = "full";
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

  // AppLink requires NavigationProvider (it calls useNavigation() internally),
  // so these renders are wrapped the same way renderCard() wraps the others,
  // even though the brief's snippet renders IssueMentionCard bare.
  it("renders the full chip without a hover card by default", () => {
    render(
      <NavigationProvider value={makeAdapter()}>
        <IssueMentionCard issueId="issue-1" fallbackLabel="MUL-3405" />
      </NavigationProvider>,
    );

    expect(document.querySelector("[data-testid='issue-hover-card']")).toBeNull();
  });

  it("wraps the mention in a hover card in compact mode", () => {
    mentionDisplayState.mode = "compact";
    render(
      <NavigationProvider value={makeAdapter()}>
        <IssueMentionCard issueId="issue-1" fallbackLabel="MUL-3405" />
      </NavigationProvider>,
    );

    expect(document.querySelector("[data-testid='issue-hover-card']")).not.toBeNull();
  });

  it("wraps the mention in a hover card in plain mode", () => {
    mentionDisplayState.mode = "plain";
    render(
      <NavigationProvider value={makeAdapter()}>
        <IssueMentionCard issueId="issue-1" fallbackLabel="MUL-3405" />
      </NavigationProvider>,
    );

    expect(document.querySelector("[data-testid='issue-hover-card']")).not.toBeNull();
  });

  // The hover card nests the AppLink inside HoverCardTrigger. This asserts the
  // link survives that nesting structurally. It does NOT prove real clicks
  // still navigate — HoverCardContent stops click/auxclick/dblclick from
  // bubbling through the React tree, and the mock above replaces exactly the
  // component that does so. Task 7's manual pass is what verifies actual
  // click and cmd-click behavior.
  it.each(["compact", "plain"] as const)(
    "keeps the mention a navigable link in %s mode",
    (mode) => {
      mentionDisplayState.mode = mode;
      render(
        <NavigationProvider value={makeAdapter()}>
          <IssueMentionCard issueId="issue-1" fallbackLabel="MUL-3405" />
        </NavigationProvider>,
      );

      const anchor = screen.getByRole("link");
      expect(anchor).toHaveAttribute("href", "/acme/issues/issue-1");
      expect(anchor).toHaveAttribute("target", "_blank");
    },
  );
});
