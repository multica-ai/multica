/**
 * Project mention accessibility (MUL-4922).
 *
 * A mention is a link. It must be reachable by Tab, activatable by Enter, and
 * carry a real URL — not a `<span onClick>` that only answers to a mouse.
 *
 * This is a regression fixture, not a hypothetical: Chat previously rendered
 * project mentions through AppLink, the readonly renderer used a click-handling
 * span, and unifying the two surfaces silently propagated the span to Chat. A
 * span and an anchor look identical on screen and behave identically under a
 * mouse, so only an assertion about the emitted element catches this.
 *
 * The real AppLink and NavigationProvider are used deliberately — mocking
 * AppLink to render an `<a>` would assert the mock, not the component.
 */
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NavigationProvider } from "../navigation/context";
import type { NavigationAdapter } from "../navigation/types";

vi.mock("../issues/hooks", () => ({
  useResolveIssueIdentifier: () => null,
}));

vi.mock("../i18n", async () => {
  const editor = (await import("../locales/en/editor.json")).default;
  return {
    useT: () => ({
      t: (select: (bundle: typeof editor) => string) => select(editor),
    }),
    useTimeAgo: () => "just now",
  };
});

vi.mock("@multica/core/api", () => ({
  api: { getAttachmentTextContent: vi.fn() },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/acme/issues/${id}`,
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
  useWorkspaceSlug: () => "acme",
}));

vi.mock("../issues/components/issue-mention-card", () => ({
  IssueMentionCard: ({ issueId }: { issueId: string }) => <span>{issueId}</span>,
}));

vi.mock("../projects/components/project-chip", () => ({
  ProjectChip: ({ projectId, fallbackLabel }: { projectId: string; fallbackLabel?: string }) => (
    <span data-testid="project-chip">{fallbackLabel ?? projectId}</span>
  ),
}));

vi.mock("../editor/link-hover-card", () => ({
  useLinkHover: () => ({}),
  LinkHoverCard: () => null,
}));

import { RichContent } from "./rich-content";

const PROJECT_ID = "8f14e45f-ceea-4d0e-a1a2-9b1c0d3e4f5a";
const MENTION = `Tracked under [Roadmap](mention://project/${PROJECT_ID}).`;

function makeAdapter(overrides: Partial<NavigationAdapter> = {}): NavigationAdapter {
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

function renderMention(adapter: NavigationAdapter = makeAdapter()) {
  return render(
    <NavigationProvider value={adapter}>
      <RichContent content={MENTION} />
    </NavigationProvider>,
  );
}

describe("project mention accessibility", () => {
  it("renders an anchor with the project href", () => {
    const { container } = renderMention();

    const anchor = container.querySelector(`a[href="/acme/projects/${PROJECT_ID}"]`);
    expect(anchor).not.toBeNull();
    expect(anchor?.tagName).toBe("A");
    expect(screen.getByTestId("project-chip")).toBeInTheDocument();
  });

  it("does not render the chip as a click-only span", () => {
    // The exact regression: a chip whose nearest interactive ancestor is a span
    // is unreachable by keyboard.
    const chip = renderMention().getByTestId("project-chip");
    expect(chip.closest("a")).not.toBeNull();
  });

  it("is keyboard focusable", async () => {
    const user = userEvent.setup();
    const { container } = renderMention();
    const anchor = container.querySelector("a") as HTMLAnchorElement;

    await user.tab();

    expect(document.activeElement).toBe(anchor);
  });

  it("activates on Enter", async () => {
    const push = vi.fn();
    const user = userEvent.setup();
    renderMention(makeAdapter({ push }));

    await user.tab();
    await user.keyboard("{Enter}");

    expect(push).toHaveBeenCalledWith(`/acme/projects/${PROJECT_ID}`);
  });

  it("navigates on click through the adapter", () => {
    const push = vi.fn();
    const { container } = renderMention(makeAdapter({ push }));

    fireEvent.click(container.querySelector("a") as HTMLAnchorElement);

    expect(push).toHaveBeenCalledWith(`/acme/projects/${PROJECT_ID}`);
  });

  it("opens in a new tab on modifier-click, labelled with the mention text", () => {
    const openInNewTab = vi.fn();
    const { container } = renderMention(makeAdapter({ openInNewTab }));

    fireEvent.click(container.querySelector("a") as HTMLAnchorElement, { metaKey: true });

    expect(openInNewTab).toHaveBeenCalledWith(`/acme/projects/${PROJECT_ID}`, "Roadmap");
  });
});
