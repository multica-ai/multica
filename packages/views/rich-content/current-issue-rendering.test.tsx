import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import type { ReactNode } from "react";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider } from "../navigation/context";
import type { NavigationAdapter } from "../navigation/types";
import {
  CurrentIssueRenderContextProvider,
  type CurrentIssueRenderContextValue,
} from "../issues/current-issue-render-context";

const resolvedIssues = vi.hoisted(() => ({
  current: null as Issue | null,
  other: null as Issue | null,
}));

vi.mock("../issues/hooks", () => ({
  useResolveIssueIdentifier: (identifier: string) => {
    if (identifier === "MUL-7") return resolvedIssues.current;
    if (identifier === "MUL-8") return resolvedIssues.other;
    return null;
  },
}));

vi.mock("../issues/components/issue-chip", () => ({
  IssueChip: ({
    issueId,
    children,
  }: {
    issueId: string;
    children?: ReactNode;
  }) => (
    <span
      data-testid="issue-chip"
      data-issue-id={issueId}
      data-current={children !== undefined ? "true" : "false"}
    >
      {children ?? issueId}
    </span>
  ),
}));

vi.mock("../issues/components/issue-hover-card", () => ({
  IssueHoverCard: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock("../editor/link-hover-card", () => ({
  useLinkHover: () => ({}),
  LinkHoverCard: () => null,
}));

vi.mock("@multica/core/api", () => ({
  api: { getAttachmentTextContent: vi.fn() },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/acme/issues/${id}`,
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
  useWorkspaceSlug: () => "acme",
}));

vi.mock("mermaid", () => ({
  default: { initialize: vi.fn(), render: vi.fn() },
}));

import { RichContent } from "./rich-content";

const APP_ORIGIN = "https://app.example";
const CURRENT_ID = "11111111-1111-4111-8111-111111111111";
const OTHER_ID = "22222222-2222-4222-8222-222222222222";
const CURRENT_CONTEXT: CurrentIssueRenderContextValue = {
  id: CURRENT_ID,
  identifier: "MUL-7",
};
const CONTENT = [
  `See [MUL-7](mention://issue/${CURRENT_ID})`,
  "MUL-8",
  `${APP_ORIGIN}/acme/issues/${OTHER_ID}.`,
].join(" and ");

/**
 * The self-reference written every other way it can be written, since a
 * reference reaches the mention card through a different resolution path per
 * form: the autolink preprocessor for bare prose, and the entity-link unfurl
 * for a pasted URL — which itself splits on whether "copy link" produced a
 * UUID or an identifier.
 *
 * Each pairs the self-reference with an other-issue reference on the SAME
 * path, so a failure here means the current-issue check stopped applying, not
 * that the path stopped resolving at all.
 */
const SELF_REFERENCE_FORMS: ReadonlyArray<readonly [string, string]> = [
  ["a bare identifier", "See MUL-7 and MUL-8."],
  [
    "a UUID URL",
    `See ${APP_ORIGIN}/acme/issues/${CURRENT_ID} and ${APP_ORIGIN}/acme/issues/${OTHER_ID}.`,
  ],
  [
    "an identifier URL",
    `See ${APP_ORIGIN}/acme/issues/MUL-7 and ${APP_ORIGIN}/acme/issues/MUL-8.`,
  ],
];

function makeIssue(id: string, identifier: string): Issue {
  return { id, identifier } as unknown as Issue;
}

function adapter(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/",
    searchParams: new URLSearchParams(),
    getShareableUrl: (path) => `${APP_ORIGIN}${path}`,
  };
}

function renderContent(
  context?: CurrentIssueRenderContextValue,
  content = CONTENT,
) {
  return renderWithI18n(
    <NavigationProvider value={adapter()}>
      {context ? (
        <CurrentIssueRenderContextProvider value={context}>
          <RichContent content={content} />
        </CurrentIssueRenderContextProvider>
      ) : (
        <RichContent content={content} />
      )}
    </NavigationProvider>,
  );
}

beforeEach(() => {
  resolvedIssues.current = makeIssue(CURRENT_ID, "MUL-7");
  resolvedIssues.other = makeIssue(OTHER_ID, "MUL-8");
});

describe("RichContent current-issue rendering", () => {
  it("marks only the current UUID mention in a paragraph with two other references as current", () => {
    renderContent(CURRENT_CONTEXT);

    const chips = screen.getAllByTestId("issue-chip");
    expect(chips).toHaveLength(3);
    expect(chips.map((chip) => chip.getAttribute("data-issue-id"))).toEqual([
      CURRENT_ID,
      OTHER_ID,
      OTHER_ID,
    ]);
    expect(chips.map((chip) => chip.getAttribute("data-current"))).toEqual([
      "true",
      "false",
      "false",
    ]);
    expect(chips[0]).toHaveTextContent("This issue · MUL-7");
  });

  it.each(SELF_REFERENCE_FORMS)(
    "marks the current issue written as %s, and only it, as current",
    (_form, content) => {
      renderContent(CURRENT_CONTEXT, content);

      const chips = screen.getAllByTestId("issue-chip");
      expect(chips.map((chip) => chip.getAttribute("data-issue-id"))).toEqual([
        CURRENT_ID,
        OTHER_ID,
      ]);
      expect(chips.map((chip) => chip.getAttribute("data-current"))).toEqual([
        "true",
        "false",
      ]);
      expect(chips[0]).toHaveTextContent("This issue · MUL-7");
    },
  );

  it("keeps every chip on regular content without a current-issue provider", () => {
    renderContent();

    const chips = screen.getAllByTestId("issue-chip");
    expect(chips).toHaveLength(3);
    for (const chip of chips) {
      expect(chip).toHaveAttribute("data-current", "false");
    }
  });
});
