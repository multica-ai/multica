import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { useQuery } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";
import { IssueHoverCard } from "./issue-hover-card";

vi.mock("@tanstack/react-query", () => ({
  useQuery: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/issue-statuses/hooks", () => ({
  // This suite replaces useQuery wholesale, so the catalog hook is stubbed
  // rather than left to fall through the shared useQuery mock.
  useIssueStatuses: () => ({
    statuses: [],
    activeStatuses: [],
    categoryOf: (key: string) => key,
    colorOf: (key: string) =>
      key === "awaiting_response" ? "#f97316" : null,
    labelOf: (key: string) => key,
    entryOf: () => undefined,
    inCategory: () => [],
    isLoaded: true,
  }),
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueDetailOptions: (_workspaceId: string, issueId: string) => ({
    queryKey: ["issue", issueId],
  }),
  childIssueProgressOptions: (workspaceId: string) => ({
    queryKey: ["child-progress", workspaceId],
  }),
}));

// The real ActorAvatar pulls in navigation, presence, and four profile cards.
// The card only needs to know it rendered for the right actor.
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({
    actorType,
    actorId,
    enableHoverCard,
    profileLink,
  }: {
    actorType: string;
    actorId: string;
    enableHoverCard?: boolean;
    profileLink?: boolean;
  }) => (
    <span
      data-testid="actor-avatar"
      data-actor-type={actorType}
      data-actor-id={actorId}
      data-hover-card={String(enableHoverCard)}
      data-profile-link={String(profileLink)}
    />
  ),
}));

// A spy, not a stub: `useActorName` subscribes to the workspace member list,
// so "was this hook mounted at all" is the assertion that keeps the assignee
// row from being inlined back into the card body.
const mockUseActorName = vi.hoisted(() => vi.fn(() => ({ getActorName: () => "zain" })));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: mockUseActorName,
}));

vi.mock("./status-icon", () => ({
  StatusIcon: ({
    status,
    category,
    color,
  }: {
    status: string;
    category?: string;
    color?: string | null;
  }) => (
    <svg
      data-testid="status-icon"
      data-status={status}
      data-category={category}
      data-color={color}
    />
  ),
}));

vi.mock("./priority-icon", () => ({
  PriorityIcon: ({ priority }: { priority: string }) => (
    <svg data-testid="priority-icon" data-priority={priority} />
  ),
}));

const mockUseQuery = vi.mocked(useQuery);

type Issue = {
  id: string;
  identifier: string;
  title: string;
  status: string;
  status_category?: string;
  priority: string;
  description?: string | null;
  assignee_type?: string | null;
  assignee_id?: string | null;
};

const BASE_ISSUE: Issue = {
  id: "issue-1",
  identifier: "MUL-3405",
  title: "A very long issue title that the inline chip never shows",
  status: "todo",
  priority: "none",
};

const NOT_FOUND_TEXT = "This issue does not exist or has been deleted in this workspace.";

/** The three query states the card body branches on, as react-query reports them. */
type DetailState =
  | { phase: "pending" }
  | { phase: "error" }
  | { phase: "success"; issue: Issue | undefined };

/**
 * Routes each query the card body makes to its own fixture, keyed by the first
 * segment of the query key the mocked options factories produce. The detail
 * fixture carries `isPending`/`isError` explicitly because the body branches on
 * them, not on `data` alone.
 */
function mockQueries(
  detail: DetailState,
  progress?: Map<string, { done: number; total: number }>,
): void {
  mockUseQuery.mockImplementation((options: unknown) => {
    const [key] = (options as { queryKey: string[] }).queryKey;
    if (key === "issue") {
      return {
        data: detail.phase === "success" ? detail.issue : undefined,
        isPending: detail.phase === "pending",
        isError: detail.phase === "error",
      } as unknown as ReturnType<typeof useQuery>;
    }
    if (key === "child-progress") {
      return { data: progress, isPending: false, isError: false } as unknown as ReturnType<
        typeof useQuery
      >;
    }
    throw new Error(`Unexpected query key: ${String(key)}`);
  });
}

/** Shorthand for the common case: the detail query settled with an issue. */
function mockIssue(
  issue: Issue,
  progress?: Map<string, { done: number; total: number }>,
): void {
  mockQueries({ phase: "success", issue }, progress);
}

function renderCard(fallbackLabel?: string): void {
  renderWithI18n(
    <IssueHoverCard issueId="issue-1" delay={0} fallbackLabel={fallbackLabel}>
      <span>MUL-3405</span>
    </IssueHoverCard>,
  );
}

async function openCard(): Promise<void> {
  const user = userEvent.setup();
  renderCard();
  await user.hover(screen.getByText("MUL-3405"));
  await screen.findByTestId("status-icon");
}

/** Opens a card whose detail query never resolves into an issue. */
async function openFailedCard(fallbackLabel = "MUL-7"): Promise<void> {
  const user = userEvent.setup();
  renderCard(fallbackLabel);
  await user.hover(screen.getByText("MUL-3405"));
  await screen.findByText(NOT_FOUND_TEXT);
}

/** Paragraphs inside the open card: the title, plus the snippet when shown. */
function paragraphCount(): number {
  const title = screen.getByText(BASE_ISSUE.title);
  return title.parentElement?.querySelectorAll("p").length ?? 0;
}

describe("IssueHoverCard", () => {
  beforeEach(() => {
    mockUseQuery.mockReset();
    mockUseActorName.mockClear();
    mockIssue(BASE_ISSUE);
  });

  it("does not fetch issue detail until the card opens", () => {
    renderCard();

    // Assert the trigger actually rendered BEFORE asserting the absent fetch.
    // Without this, a component that throws or renders nothing would satisfy
    // the deferred-fetch assertion and the guarantee would be untested.
    expect(screen.getByText("MUL-3405")).toBeInTheDocument();
    expect(mockUseQuery).not.toHaveBeenCalled();
  });

  it("reveals the full title on hover", async () => {
    const user = userEvent.setup();
    renderCard();

    await user.hover(screen.getByText("MUL-3405"));

    expect(
      await screen.findByText("A very long issue title that the inline chip never shows"),
    ).toBeInTheDocument();
    expect(mockUseQuery).toHaveBeenCalled();
  });

  it("shows the priority glyph ahead of the status icon", async () => {
    mockIssue({ ...BASE_ISSUE, priority: "high" });

    await openCard();

    const priority = screen.getByTestId("priority-icon");
    expect(priority).toHaveAttribute("data-priority", "high");
    // Ordering: priority leads the header, the status icon follows it. Both
    // glyphs sit inside their own naming wrapper, so compare the wrappers.
    expect(priority.parentElement?.nextElementSibling).toContainElement(
      screen.getByTestId("status-icon"),
    );
  });

  it("names the status and priority glyphs for assistive tech", async () => {
    mockIssue({ ...BASE_ISSUE, status: "in_progress", priority: "high" });

    await openCard();

    // Localized names from the shipped issues namespace — no key of this
    // card's own. Removing either aria-label drops the element from the query.
    expect(screen.getByLabelText("In Progress")).toContainElement(
      screen.getByTestId("status-icon"),
    );
    expect(screen.getByLabelText("High")).toContainElement(
      screen.getByTestId("priority-icon"),
    );
  });

  it("paints a custom status with its catalog color", async () => {
    mockIssue({
      ...BASE_ISSUE,
      status: "awaiting_response",
      status_category: "in_review",
    });

    await openCard();

    expect(screen.getByTestId("status-icon")).toHaveAttribute(
      "data-category",
      "in_review",
    );
    expect(screen.getByTestId("status-icon")).toHaveAttribute(
      "data-color",
      "#f97316",
    );
  });

  it("omits the priority glyph when the issue has no priority", async () => {
    mockIssue({ ...BASE_ISSUE, priority: "none" });

    await openCard();

    expect(screen.getByTestId("status-icon")).toBeInTheDocument();
    expect(screen.queryByTestId("priority-icon")).not.toBeInTheDocument();
  });

  it("shows the assignee avatar and name, without nesting another hover card", async () => {
    mockIssue({ ...BASE_ISSUE, assignee_type: "member", assignee_id: "user-9" });

    await openCard();

    const avatar = screen.getByTestId("actor-avatar");
    expect(avatar).toHaveAttribute("data-actor-type", "member");
    expect(avatar).toHaveAttribute("data-actor-id", "user-9");
    expect(avatar).toHaveAttribute("data-hover-card", "false");
    expect(avatar).toHaveAttribute("data-profile-link", "false");
    expect(screen.getByText("zain")).toBeInTheDocument();
    expect(mockUseActorName).toHaveBeenCalled();
  });

  it("omits the assignee when the issue is unassigned", async () => {
    mockIssue({ ...BASE_ISSUE, assignee_type: null, assignee_id: null });

    await openCard();

    expect(screen.queryByTestId("actor-avatar")).not.toBeInTheDocument();
    expect(screen.queryByText("zain")).not.toBeInTheDocument();
    // The member directory stays unfetched: the row that reads it never mounts.
    expect(mockUseActorName).not.toHaveBeenCalled();
  });

  it("shows sub-issue progress when the workspace map has an entry", async () => {
    mockIssue(BASE_ISSUE, new Map([["issue-1", { done: 2, total: 5 }]]));

    await openCard();

    expect(screen.getByText("2/5")).toBeInTheDocument();
  });

  it("omits progress when the issue has no children", async () => {
    mockIssue(BASE_ISSUE, new Map([["other-issue", { done: 1, total: 3 }]]));

    await openCard();

    expect(screen.queryByText(/\d+\/\d+/)).not.toBeInTheDocument();
  });

  it("omits progress when the issue has an entry with no children counted", async () => {
    mockIssue(BASE_ISSUE, new Map([["issue-1", { done: 0, total: 0 }]]));

    await openCard();

    expect(screen.queryByText(/\d+\/\d+/)).not.toBeInTheDocument();
  });

  it("shows a flattened description snippet clamped to two lines", async () => {
    mockIssue({
      ...BASE_ISSUE,
      description: "**Set up** your first runtime so [Mika](/agents/mika) can pick up work",
    });

    await openCard();

    const snippet = screen.getByText("Set up your first runtime so Mika can pick up work");
    expect(snippet).toHaveClass("line-clamp-2");
  });

  // Both no-description cases count paragraphs rather than probing for absent
  // text: an always-rendered block would emit an empty <p>, which no text query
  // can see. The title is the only paragraph a description-less card may have.
  it("omits the description block when there is no description", async () => {
    mockIssue({ ...BASE_ISSUE, description: null });

    await openCard();

    expect(screen.getByText(BASE_ISSUE.title)).toBeInTheDocument();
    expect(paragraphCount()).toBe(1);
  });

  it("omits the description block when the description flattens to nothing", async () => {
    // An image-only description: every visible token is stripped by the
    // preview, so rendering it would leave an empty paragraph.
    mockIssue({
      ...BASE_ISSUE,
      description: "![](/api/attachments/abc/download)",
    });

    await openCard();

    expect(screen.getByText(BASE_ISSUE.title)).toBeInTheDocument();
    expect(paragraphCount()).toBe(1);
  });

  it("breaks a long unbroken title instead of overflowing the card", async () => {
    const title = "https://example.com/a/very/long/path/that/never/offers/a/break/opportunity";
    mockIssue({ ...BASE_ISSUE, title });

    await openCard();

    const titleEl = screen.getByText(title);
    expect(titleEl).toHaveClass("break-words");
    expect(titleEl).not.toHaveClass("truncate");
  });

  it("keeps the skeleton while the detail query is pending", async () => {
    mockQueries({ phase: "pending" });
    const user = userEvent.setup();
    renderCard("MUL-7");

    await user.hover(screen.getByText("MUL-3405"));

    expect(await screen.findByTestId("issue-hover-card-skeleton")).toBeInTheDocument();
    expect(screen.queryByText(NOT_FOUND_TEXT)).not.toBeInTheDocument();
  });

  it("replaces the skeleton with the not-found state when the detail query fails", async () => {
    mockQueries({ phase: "error" });

    await openFailedCard();

    expect(screen.queryByTestId("issue-hover-card-skeleton")).not.toBeInTheDocument();
    expect(screen.getByText("MUL-7")).toBeInTheDocument();
  });

  it("replaces the skeleton with the not-found state when the query settles with no issue", async () => {
    mockQueries({ phase: "success", issue: undefined });

    await openFailedCard();

    expect(screen.queryByTestId("issue-hover-card-skeleton")).not.toBeInTheDocument();
    expect(screen.getByText("MUL-7")).toBeInTheDocument();
  });
});
