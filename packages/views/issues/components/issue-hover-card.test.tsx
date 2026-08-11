import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { useQuery } from "@tanstack/react-query";
import { IssueHoverCard } from "./issue-hover-card";

vi.mock("@tanstack/react-query", () => ({
  useQuery: vi.fn(),
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

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "zain" }),
}));

vi.mock("./status-icon", () => ({
  StatusIcon: () => <svg data-testid="status-icon" />,
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

/**
 * Routes each query the card body makes to its own fixture, keyed by the first
 * segment of the query key the mocked options factories produce.
 */
function mockQueries(
  issue: Issue | undefined,
  progress?: Map<string, { done: number; total: number }>,
): void {
  mockUseQuery.mockImplementation((options: unknown) => {
    const [key] = (options as { queryKey: string[] }).queryKey;
    if (key === "issue") return { data: issue } as ReturnType<typeof useQuery>;
    if (key === "child-progress") return { data: progress } as ReturnType<typeof useQuery>;
    throw new Error(`Unexpected query key: ${String(key)}`);
  });
}

async function openCard(): Promise<void> {
  const user = userEvent.setup();
  render(
    <IssueHoverCard issueId="issue-1" delay={0}>
      <span>MUL-3405</span>
    </IssueHoverCard>,
  );
  await user.hover(screen.getByText("MUL-3405"));
  await screen.findByTestId("status-icon");
}

/** Paragraphs inside the open card: the title, plus the snippet when shown. */
function paragraphCount(): number {
  const title = screen.getByText(BASE_ISSUE.title);
  return title.parentElement?.querySelectorAll("p").length ?? 0;
}

describe("IssueHoverCard", () => {
  beforeEach(() => {
    mockUseQuery.mockReset();
    mockQueries(BASE_ISSUE);
  });

  it("does not fetch issue detail until the card opens", () => {
    render(
      <IssueHoverCard issueId="issue-1" delay={0}>
        <span>MUL-3405</span>
      </IssueHoverCard>,
    );

    // Assert the trigger actually rendered BEFORE asserting the absent fetch.
    // Without this, a component that throws or renders nothing would satisfy
    // the deferred-fetch assertion and the guarantee would be untested.
    expect(screen.getByText("MUL-3405")).toBeInTheDocument();
    expect(mockUseQuery).not.toHaveBeenCalled();
  });

  it("reveals the full title on hover", async () => {
    const user = userEvent.setup();
    render(
      <IssueHoverCard issueId="issue-1" delay={0}>
        <span>MUL-3405</span>
      </IssueHoverCard>,
    );

    await user.hover(screen.getByText("MUL-3405"));

    expect(
      await screen.findByText("A very long issue title that the inline chip never shows"),
    ).toBeInTheDocument();
    expect(mockUseQuery).toHaveBeenCalled();
  });

  it("shows the priority glyph ahead of the status icon", async () => {
    mockQueries({ ...BASE_ISSUE, priority: "high" });

    await openCard();

    const priority = screen.getByTestId("priority-icon");
    expect(priority).toHaveAttribute("data-priority", "high");
    // Ordering: priority leads the header, the status icon follows it.
    expect(priority.nextElementSibling).toBe(screen.getByTestId("status-icon"));
  });

  it("omits the priority glyph when the issue has no priority", async () => {
    mockQueries({ ...BASE_ISSUE, priority: "none" });

    await openCard();

    expect(screen.getByTestId("status-icon")).toBeInTheDocument();
    expect(screen.queryByTestId("priority-icon")).not.toBeInTheDocument();
  });

  it("shows the assignee avatar and name, without nesting another hover card", async () => {
    mockQueries({ ...BASE_ISSUE, assignee_type: "member", assignee_id: "user-9" });

    await openCard();

    const avatar = screen.getByTestId("actor-avatar");
    expect(avatar).toHaveAttribute("data-actor-type", "member");
    expect(avatar).toHaveAttribute("data-actor-id", "user-9");
    expect(avatar).toHaveAttribute("data-hover-card", "false");
    expect(avatar).toHaveAttribute("data-profile-link", "false");
    expect(screen.getByText("zain")).toBeInTheDocument();
  });

  it("omits the assignee when the issue is unassigned", async () => {
    mockQueries({ ...BASE_ISSUE, assignee_type: null, assignee_id: null });

    await openCard();

    expect(screen.queryByTestId("actor-avatar")).not.toBeInTheDocument();
    expect(screen.queryByText("zain")).not.toBeInTheDocument();
  });

  it("shows sub-issue progress when the workspace map has an entry", async () => {
    mockQueries(BASE_ISSUE, new Map([["issue-1", { done: 2, total: 5 }]]));

    await openCard();

    expect(screen.getByText("2/5")).toBeInTheDocument();
  });

  it("omits progress when the issue has no children", async () => {
    mockQueries(BASE_ISSUE, new Map([["other-issue", { done: 1, total: 3 }]]));

    await openCard();

    expect(screen.queryByText(/\d+\/\d+/)).not.toBeInTheDocument();
  });

  it("omits progress when the issue has an entry with no children counted", async () => {
    mockQueries(BASE_ISSUE, new Map([["issue-1", { done: 0, total: 0 }]]));

    await openCard();

    expect(screen.queryByText(/\d+\/\d+/)).not.toBeInTheDocument();
  });

  it("shows a flattened description snippet clamped to two lines", async () => {
    mockQueries({
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
    mockQueries({ ...BASE_ISSUE, description: null });

    await openCard();

    expect(screen.getByText(BASE_ISSUE.title)).toBeInTheDocument();
    expect(paragraphCount()).toBe(1);
  });

  it("omits the description block when the description flattens to nothing", async () => {
    // An image-only description: every visible token is stripped by the
    // preview, so rendering it would leave an empty paragraph.
    mockQueries({
      ...BASE_ISSUE,
      description: "![](/api/attachments/abc/download)",
    });

    await openCard();

    expect(screen.getByText(BASE_ISSUE.title)).toBeInTheDocument();
    expect(paragraphCount()).toBe(1);
  });
});
