import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { IssueMentionCard } from "./issue-mention-card";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";

const { mentionDisplayState } = vi.hoisted(() => ({
  mentionDisplayState: { mode: "full" as "plain" | "compact" | "full", setMode: vi.fn() },
}));

vi.mock("@multica/core/issues/stores", () => {
  const useIssueMentionDisplayStore = (
    selector?: (s: typeof mentionDisplayState) => unknown,
  ) => (selector ? selector(mentionDisplayState) : mentionDisplayState);
  useIssueMentionDisplayStore.getState = () => mentionDisplayState;

  return { useIssueMentionDisplayStore };
});

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/acme/issues/${id}`,
  }),
}));

vi.mock("./issue-chip", () => ({
  IssueChip: ({ fallbackLabel, variant }: { fallbackLabel?: string; variant?: string }) => (
    <span data-testid="issue-chip" data-variant={variant}>
      {fallbackLabel ?? "chip"}
    </span>
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
  childIssueProgressOptions: (workspaceId: string) => ({
    queryKey: ["child-progress", workspaceId],
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
    mentionDisplayState.mode = "full";
  });

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

  // The one line that makes the whole preference do anything: without it every
  // mention silently renders `full` and no other test in the repo notices.
  it.each(["full", "compact", "plain"] as const)(
    "passes the reader's display mode to the chip as variant=%s",
    (mode) => {
      mentionDisplayState.mode = mode;
      renderCard(makeAdapter());
      expect(screen.getByTestId("issue-chip")).toHaveAttribute("data-variant", mode);
    },
  );

  it("keeps align-middle in the boxed modes so the chip centers in the line box", () => {
    mentionDisplayState.mode = "compact";
    renderCard(makeAdapter());
    expect(screen.getByTestId("issue-chip").closest("a")).toHaveClass("align-middle");
  });

  it("drops align-middle in plain mode so bare text sits on the sentence baseline", () => {
    mentionDisplayState.mode = "plain";
    renderCard(makeAdapter());
    expect(screen.getByTestId("issue-chip").closest("a")).not.toHaveClass("align-middle");
  });

  it.each(["full", "compact", "plain"] as const)(
    "wraps the mention in a hover card in %s mode",
    (mode) => {
      mentionDisplayState.mode = mode;
      renderCard(makeAdapter());

      expect(document.querySelector(HOVER_CARD_TRIGGER)).not.toBeNull();
    },
  );

  // Every mode nests the AppLink inside a real HoverCardTrigger, and
  // HoverCardContent stops click/auxclick/dblclick from bubbling through the
  // React tree. The navigation paths have to survive that nesting in every
  // mode, so re-assert AppLink's semantics through it rather than trusting the
  // full-mode cases above to cover the narrow ones.
  it.each(["full", "compact", "plain"] as const)(
    "with the real hover card mounted, a mention stays navigable in %s mode",
    (mode) => {
      mentionDisplayState.mode = mode;
      const push = vi.fn();
      const openInNewTab = vi.fn();
      renderCard(makeAdapter({ push, openInNewTab }));

      expect(document.querySelector(HOVER_CARD_TRIGGER)).not.toBeNull();
      const anchor = screen.getByRole("link");
      expect(anchor).toHaveAttribute("href", "/acme/issues/issue-1");
      expect(anchor).not.toHaveAttribute("target");

      fireEvent.click(screen.getByTestId("issue-chip"));
      expect(push).toHaveBeenCalledWith("/acme/issues/issue-1");
      expect(openInNewTab).not.toHaveBeenCalled();

      fireEvent.click(screen.getByTestId("issue-chip"), { metaKey: true });
      expect(openInNewTab).toHaveBeenCalledWith("/acme/issues/issue-1", "MUL-7");
    },
  );
});
