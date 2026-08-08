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

// IssueHoverCard is deliberately NOT mocked: it is the component that nests the
// AppLink inside a Base UI trigger and stops click events on its popup, so a
// mock would hide exactly the interaction these tests are here to protect.
// Its dependencies are stubbed instead, the same way issue-hover-card.test.tsx
// does — which also removes the need for a QueryClientProvider.
vi.mock("@tanstack/react-query", () => ({
  useQuery: vi.fn(() => ({ data: undefined })),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueDetailOptions: (_workspaceId: string, issueId: string) => ({
    queryKey: ["issue", issueId],
  }),
}));

vi.mock("./status-icon", () => ({
  StatusIcon: () => <svg data-testid="status-icon" />,
}));

/** Emitted by the real HoverCardTrigger, so it can't be faked by a mock. */
const HOVER_CARD_TRIGGER = "[data-slot='hover-card-trigger']";

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
  it("wraps the mention in a hover card in full mode", () => {
    render(
      <NavigationProvider value={makeAdapter()}>
        <IssueMentionCard issueId="issue-1" fallbackLabel="MUL-3405" />
      </NavigationProvider>,
    );

    expect(document.querySelector(HOVER_CARD_TRIGGER)).not.toBeNull();
  });

  it("wraps the mention in a hover card in compact mode", () => {
    mentionDisplayState.mode = "compact";
    render(
      <NavigationProvider value={makeAdapter()}>
        <IssueMentionCard issueId="issue-1" fallbackLabel="MUL-3405" />
      </NavigationProvider>,
    );

    expect(document.querySelector(HOVER_CARD_TRIGGER)).not.toBeNull();
  });

  it("wraps the mention in a hover card in plain mode", () => {
    mentionDisplayState.mode = "plain";
    render(
      <NavigationProvider value={makeAdapter()}>
        <IssueMentionCard issueId="issue-1" fallbackLabel="MUL-3405" />
      </NavigationProvider>,
    );

    expect(document.querySelector(HOVER_CARD_TRIGGER)).not.toBeNull();
  });

  // Every mode nests the AppLink inside a real HoverCardTrigger, and
  // HoverCardContent stops click/auxclick/dblclick from bubbling through the
  // React tree. Both navigation paths have to survive that nesting, so the
  // cases cover the new-tab preference on AND off across all three modes —
  // the new-tab path is the one most exposed to event suppression.
  it.each([
    { mode: "full", newTab: true },
    { mode: "compact", newTab: false },
    { mode: "plain", newTab: false },
  ] as const)(
    "with the real hover card mounted, a plain click in $mode mode navigates (new tab: $newTab)",
    ({ mode, newTab }) => {
      mentionDisplayState.mode = mode;
      issueLinkState.openInNewTab = newTab;
      const push = vi.fn();
      const openInNewTab = vi.fn();
      render(
        <NavigationProvider value={makeAdapter({ push, openInNewTab })}>
          <IssueMentionCard issueId="issue-1" fallbackLabel="MUL-3405" />
        </NavigationProvider>,
      );

      expect(document.querySelector(HOVER_CARD_TRIGGER)).not.toBeNull();
      const anchor = screen.getByRole("link");
      expect(anchor).toHaveAttribute("href", "/acme/issues/issue-1");

      fireEvent.click(screen.getByTestId("issue-chip"));

      if (newTab) {
        expect(anchor).toHaveAttribute("target", "_blank");
        expect(openInNewTab).toHaveBeenCalledWith("/acme/issues/issue-1", "MUL-3405", {
          activate: true,
        });
        expect(push).not.toHaveBeenCalled();
      } else {
        expect(anchor).not.toHaveAttribute("target");
        expect(push).toHaveBeenCalledWith("/acme/issues/issue-1");
        expect(openInNewTab).not.toHaveBeenCalled();
      }
    },
  );
});
