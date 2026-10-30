/**
 * Bare in-app entity URLs render as chips (MUL-5499).
 *
 * A project has no `MUL-123` shorthand — only a UUID and a free-text title — so
 * the link copied out of the app IS how people reference one. This fixture pins
 * the three conditions that decide whether such a link becomes a chip, because
 * each of them fails silently: an over-eager rule eats an author's link label,
 * and a cross-workspace unfurl replaces a working link with a chip that can
 * never resolve.
 *
 * The real RichLink / preprocessing pipeline is exercised; only the chips
 * themselves are stubbed, so the assertions are about which branch the renderer
 * took, not about chip internals.
 */
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import { NavigationProvider } from "../navigation/context";
import type { NavigationAdapter } from "../navigation/types";
import type { Issue } from "@multica/core/types";

// Identifier → issue lookup, set per test. `null` is the honest default: a
// miss, a still-loading query and a cross-workspace identifier all look the
// same to the renderer.
let resolvedIssue: Issue | null = null;

vi.mock("../issues/hooks", () => ({
  useResolveIssueIdentifier: () => resolvedIssue,
}));

beforeEach(() => {
  resolvedIssue = null;
});

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

// Partial mock: only the workspace-context hooks are stubbed. The pure path
// helpers (isReservedSlug / isGlobalPath) stay real — the URL parser under test
// depends on the same reserved-slug list the backend enforces, and stubbing it
// would make the assertions meaningless.
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/acme/issues/${id}`,
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
  useWorkspaceSlug: () => "acme",
}));

vi.mock("../issues/components/issue-mention-card", () => ({
  IssueMentionCard: ({ issueId }: { issueId: string }) => (
    <span data-testid="issue-chip">{issueId}</span>
  ),
}));

vi.mock("../projects/components/project-chip", () => ({
  ProjectChip: ({ projectId }: { projectId: string }) => (
    <span data-testid="project-chip">{projectId}</span>
  ),
}));

vi.mock("../editor/link-hover-card", () => ({
  useLinkHover: () => ({}),
  LinkHoverCard: () => null,
}));

import { RichContent } from "./rich-content";

const APP_ORIGIN = "https://app.multica.ai";
const PROJECT_ID = "8f14e45f-ceea-4d0e-a1a2-9b1c0d3e4f5a";
const ISSUE_ID = "1b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4bed";

function adapter(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/",
    searchParams: new URLSearchParams(),
    hash: "",
    // The real platform adapters return an absolute URL; useAppOrigin derives
    // the deployment origin from it, and without that nothing is "in-app".
    getShareableUrl: (p) => `${APP_ORIGIN}${p}`,
  };
}

function renderContent(content: string) {
  return render(
    <NavigationProvider value={adapter()}>
      <RichContent content={content} />
    </NavigationProvider>,
  );
}

describe("bare entity URLs in readonly content", () => {
  it("renders a pasted project URL as a project chip linking to the project", () => {
    const { container, getByTestId } = renderContent(
      `Tracked under ${APP_ORIGIN}/acme/projects/${PROJECT_ID} for now.`,
    );

    expect(getByTestId("project-chip").textContent).toBe(PROJECT_ID);
    expect(
      container.querySelector(`a[href="/acme/projects/${PROJECT_ID}"]`),
    ).not.toBeNull();
  });

  it("renders a pasted issue URL as an issue chip", () => {
    const { getByTestId } = renderContent(
      `See ${APP_ORIGIN}/acme/issues/${ISSUE_ID} for context.`,
    );

    expect(getByTestId("issue-chip").textContent).toBe(ISSUE_ID);
  });

  // The identifier form — NOT the UUID one — is what leaves the app:
  // `copyLink` and `openInNewTab` build `paths.issueDetail(identifier || id)`,
  // and the issue route rewrites a UUID URL back to the identifier, so this is
  // the shape in the address bar too.
  it("renders a pasted identifier-form issue URL as an issue chip", () => {
    resolvedIssue = { id: ISSUE_ID, identifier: "MUL-1" } as Issue;

    const { getByTestId } = renderContent(
      `Blocked by ${APP_ORIGIN}/acme/issues/MUL-1 for now.`,
    );

    expect(getByTestId("issue-chip").textContent).toBe(ISSUE_ID);
  });

  it("keeps the link when an identifier-form issue URL resolves to nothing", () => {
    // A miss means the issue is gone or lives in a workspace this user cannot
    // see. The autolink path degrades to plain text there; a URL must not —
    // the author wrote a link, and dropping it would strip the only pointer.
    const { container, queryByTestId } = renderContent(
      `${APP_ORIGIN}/acme/issues/MUL-404`,
    );

    expect(queryByTestId("issue-chip")).toBeNull();
    expect(
      container.querySelector(`a[href="${APP_ORIGIN}/acme/issues/MUL-404"]`),
    ).not.toBeNull();
  });

  it("leaves a link the author labelled alone", () => {
    // Replacing this with a chip would throw away "Roadmap" — the author said
    // what they wanted the link to read as.
    const { container, queryByTestId } = renderContent(
      `[Roadmap](${APP_ORIGIN}/acme/projects/${PROJECT_ID})`,
    );

    expect(queryByTestId("project-chip")).toBeNull();
    const anchor = container.querySelector("a");
    expect(anchor?.textContent).toBe("Roadmap");
  });

  it("leaves a link into another workspace as a plain link", () => {
    // The chip resolves its title in the CURRENT workspace, so unfurling here
    // would swap a working link for a permanently empty chip.
    const { container, queryByTestId } = renderContent(
      `${APP_ORIGIN}/other-ws/projects/${PROJECT_ID}`,
    );

    expect(queryByTestId("project-chip")).toBeNull();
    expect(
      container.querySelector(
        `a[href="${APP_ORIGIN}/other-ws/projects/${PROJECT_ID}"]`,
      ),
    ).not.toBeNull();
  });

  it("leaves a project list URL as a plain link", () => {
    const { queryByTestId } = renderContent(`${APP_ORIGIN}/acme/projects`);
    expect(queryByTestId("project-chip")).toBeNull();
  });

  it("leaves an external URL as a plain link", () => {
    const { queryByTestId, container } = renderContent(
      `https://github.com/multica-ai/multica/pull/1`,
    );
    expect(queryByTestId("project-chip")).toBeNull();
    expect(queryByTestId("issue-chip")).toBeNull();
    expect(container.querySelector("a")).not.toBeNull();
  });
});
