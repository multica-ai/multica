import { screen, within } from "@testing-library/react";
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
    iconOf: () => null,
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

vi.mock("@multica/core/agents", () => ({
  agentTaskSnapshotOptions: (workspaceId: string) => ({
    queryKey: ["agent-tasks", workspaceId],
  }),
}));

vi.mock("@multica/core/projects", () => ({
  projectListOptions: (workspaceId: string) => ({
    queryKey: ["projects", workspaceId],
  }),
}));

vi.mock("@multica/core/github", () => ({
  issuePullRequestsOptions: (issueId: string) => ({
    queryKey: ["pull-requests", issueId],
  }),
}));

// The PR summary's verdict logic is owned and tested by pull-request-list.
vi.mock("./pull-request-list", () => ({
  PullRequestInlineSummary: ({ pr }: { pr: { number: number } }) => (
    <span data-testid="pr-summary">#{pr.number}</span>
  ),
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

const ACTOR_NAMES: Record<string, string> = {
  "user-9": "zain",
  "agent-triage": "GitHub Triage",
};

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_type: string, id: string) => ACTOR_NAMES[id] ?? id,
  }),
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

type Label = { id: string; name: string; color: string };

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
  creator_type: string;
  creator_id: string;
  parent_issue_id?: string | null;
  project_id?: string | null;
  due_date?: string | null;
  labels?: Label[];
  created_at: string;
  updated_at: string;
};

const NOW = Date.now();
const minutesAgo = (m: number) => new Date(NOW - m * 60_000).toISOString();

const BASE_ISSUE: Issue = {
  id: "issue-1",
  identifier: "MUL-3405",
  title: "A very long issue title that the inline chip never shows",
  status: "todo",
  priority: "none",
  creator_type: "agent",
  creator_id: "agent-triage",
  created_at: minutesAgo(3 * 24 * 60),
  updated_at: minutesAgo(14),
};

const PARENT_ISSUE = {
  id: "parent-1",
  identifier: "MUL-3400",
  title: "Weekly GitHub triage",
};

const NOT_FOUND_TEXT = "This issue does not exist or has been deleted in this workspace.";

/** The three query states the card body branches on, as react-query reports them. */
type DetailState =
  | { phase: "pending" }
  | { phase: "error" }
  | { phase: "success"; issue: Issue | undefined };

interface Fixtures {
  progress?: Map<string, { done: number; total: number }>;
  tasks?: { issue_id: string; agent_id: string; status: string }[];
  projects?: { id: string; title: string }[];
  pullRequests?: { id: string; number: number; state: string }[];
}

const settled = (data: unknown) =>
  ({ data, isPending: false, isError: false }) as unknown as ReturnType<typeof useQuery>;

/**
 * Routes each query the card makes to its own fixture, keyed by the first
 * segment of the query key the mocked options factories produce. A disabled
 * query reports no data, and a `select` is applied the way react-query would.
 * The detail fixture carries `isPending`/`isError` explicitly because the body
 * branches on them, not on `data` alone.
 */
function mockQueries(detail: DetailState, fixtures: Fixtures = {}): void {
  mockUseQuery.mockImplementation((options: unknown) => {
    const { queryKey, enabled, select } = options as {
      queryKey: string[];
      enabled?: boolean;
      select?: (data: unknown) => unknown;
    };
    if (enabled === false) return settled(undefined);
    const [key, id] = queryKey;
    const raw = (data: unknown) => settled(select ? select(data) : data);
    if (key === "issue" && id === "issue-1") {
      return {
        data: detail.phase === "success" ? detail.issue : undefined,
        isPending: detail.phase === "pending",
        isError: detail.phase === "error",
      } as unknown as ReturnType<typeof useQuery>;
    }
    if (key === "issue" && id === PARENT_ISSUE.id) return settled(PARENT_ISSUE);
    if (key === "child-progress") return settled(fixtures.progress);
    if (key === "agent-tasks") return raw(fixtures.tasks ?? []);
    if (key === "projects") return raw(fixtures.projects ?? []);
    if (key === "pull-requests") {
      return raw({ pull_requests: fixtures.pullRequests ?? [] });
    }
    throw new Error(`Unexpected query key: ${String(key)}`);
  });
}

/** Shorthand for the common case: the detail query settled with an issue. */
function mockIssue(issue: Issue, fixtures?: Fixtures): void {
  mockQueries({ phase: "success", issue }, fixtures);
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

/** The value cell of a property row, found by its term. */
function propertyValue(term: string): HTMLElement {
  const dt = screen.getByText(term, { selector: "dt" });
  return dt.nextElementSibling as HTMLElement;
}

describe("IssueHoverCard", () => {
  beforeEach(() => {
    mockUseQuery.mockReset();
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

  it("names the status and the priority in text, not only as glyphs", async () => {
    mockIssue({ ...BASE_ISSUE, status: "in_progress", priority: "high" });

    await openCard();

    // Localized names from the shipped issues namespace.
    expect(screen.getByText("In Progress")).toBeInTheDocument();
    const priority = screen.getByText("High");
    expect(priority).toContainElement(screen.getByTestId("priority-icon"));
    expect(screen.getByTestId("priority-icon")).toHaveAttribute("data-priority", "high");
  });

  it("paints a custom status with its catalog color", async () => {
    mockIssue({
      ...BASE_ISSUE,
      status: "awaiting_response",
      status_category: "started",
    });

    await openCard();

    expect(screen.getByTestId("status-icon")).toHaveAttribute(
      "data-category",
      "started",
    );
    expect(screen.getByTestId("status-icon")).toHaveAttribute(
      "data-color",
      "#f97316",
    );
  });

  it("omits the priority when the issue has no priority", async () => {
    mockIssue({ ...BASE_ISSUE, priority: "none" });

    await openCard();

    expect(screen.getByTestId("status-icon")).toBeInTheDocument();
    expect(screen.queryByTestId("priority-icon")).not.toBeInTheDocument();
  });

  it("shows the assignee avatar and name, without nesting another hover card", async () => {
    mockIssue({ ...BASE_ISSUE, assignee_type: "member", assignee_id: "user-9" });

    await openCard();

    const assignee = propertyValue("Assignee");
    const avatar = within(assignee).getByTestId("actor-avatar");
    expect(avatar).toHaveAttribute("data-actor-type", "member");
    expect(avatar).toHaveAttribute("data-actor-id", "user-9");
    expect(avatar).toHaveAttribute("data-hover-card", "false");
    expect(avatar).toHaveAttribute("data-profile-link", "false");
    expect(within(assignee).getByText("zain")).toBeInTheDocument();
  });

  it("says Unassigned rather than dropping the assignee row", async () => {
    mockIssue({ ...BASE_ISSUE, assignee_type: null, assignee_id: null });

    await openCard();

    const assignee = propertyValue("Assignee");
    expect(within(assignee).getByText("Unassigned")).toBeInTheDocument();
    expect(within(assignee).queryByTestId("actor-avatar")).not.toBeInTheDocument();
  });

  it("marks the assignee as working while an agent runs on the issue", async () => {
    mockIssue(
      { ...BASE_ISSUE, assignee_type: "agent", assignee_id: "agent-triage" },
      {
        tasks: [
          { issue_id: "issue-1", agent_id: "agent-triage", status: "running" },
          { issue_id: "other-issue", agent_id: "agent-x", status: "queued" },
        ],
      },
    );

    await openCard();

    // ShimmerText renders an aria-hidden copy of its label for the highlight,
    // so match the cell's text rather than a single element.
    expect(propertyValue("Assignee")).toHaveTextContent("Working");
  });

  it("shows no activity when only other issues have agent tasks", async () => {
    mockIssue(
      { ...BASE_ISSUE, assignee_type: "agent", assignee_id: "agent-triage" },
      { tasks: [{ issue_id: "other-issue", agent_id: "agent-x", status: "running" }] },
    );

    await openCard();

    expect(screen.queryByText("Working")).not.toBeInTheDocument();
    expect(screen.queryByText("Queued")).not.toBeInTheDocument();
  });

  it("marks the assignee as queued while only its own run waits", async () => {
    mockIssue(
      { ...BASE_ISSUE, assignee_type: "agent", assignee_id: "agent-triage" },
      {
        tasks: [
          { issue_id: "issue-1", agent_id: "agent-triage", status: "queued" },
          { issue_id: "issue-1", agent_id: "agent-reviewer", status: "running" },
        ],
      },
    );

    await openCard();

    expect(propertyValue("Assignee")).toHaveTextContent("Queued");
    expect(propertyValue("Assignee")).not.toHaveTextContent("Working");
  });

  it("does not credit another agent's run on the issue to the assignee", async () => {
    mockIssue(
      { ...BASE_ISSUE, assignee_type: "agent", assignee_id: "agent-triage" },
      { tasks: [{ issue_id: "issue-1", agent_id: "agent-reviewer", status: "running" }] },
    );

    await openCard();

    expect(screen.queryByText("Working")).not.toBeInTheDocument();
    expect(screen.queryByText("Queued")).not.toBeInTheDocument();
  });

  it("never marks a member assignee with agent activity", async () => {
    mockIssue(
      { ...BASE_ISSUE, assignee_type: "member", assignee_id: "user-9" },
      { tasks: [{ issue_id: "issue-1", agent_id: "user-9", status: "queued" }] },
    );

    await openCard();

    expect(screen.queryByText("Working")).not.toBeInTheDocument();
    expect(screen.queryByText("Queued")).not.toBeInTheDocument();
  });

  it("names who created the issue and when it last moved", async () => {
    await openCard();

    const creator = screen
      .getAllByTestId("actor-avatar")
      .find((el) => el.getAttribute("data-actor-id") === "agent-triage");
    expect(creator).toHaveAttribute("data-hover-card", "false");
    expect(screen.getByText("GitHub Triage")).toBeInTheDocument();
    expect(screen.getByText("created 3d ago · updated 14m ago")).toBeInTheDocument();
  });

  it("shows the project and parent issue when the issue has them", async () => {
    mockIssue(
      { ...BASE_ISSUE, project_id: "project-1", parent_issue_id: PARENT_ISSUE.id },
      { projects: [{ id: "project-1", title: "Runtimes" }] },
    );

    await openCard();

    expect(within(propertyValue("Project")).getByText("Runtimes")).toBeInTheDocument();
    const parent = propertyValue("Parent issue");
    expect(within(parent).getByText("MUL-3400")).toBeInTheDocument();
    expect(within(parent).getByText("Weekly GitHub triage")).toBeInTheDocument();
  });

  it("does not query projects or a parent for an issue that has neither", async () => {
    await openCard();

    expect(screen.queryByText("Project", { selector: "dt" })).not.toBeInTheDocument();
    expect(screen.queryByText("Parent issue", { selector: "dt" })).not.toBeInTheDocument();
    const projectQuery = mockUseQuery.mock.calls.find(
      ([options]) => (options as { queryKey: string[] }).queryKey[0] === "projects",
    );
    expect((projectQuery?.[0] as { enabled?: boolean }).enabled).toBe(false);
  });

  it("caps the labels and counts the rest", async () => {
    mockIssue({
      ...BASE_ISSUE,
      labels: ["bug", "runtime", "desktop", "p1"].map((name, i) => ({
        id: `label-${i}`,
        name,
        color: "#ef4444",
      })),
    });

    await openCard();

    const labels = propertyValue("Labels");
    expect(within(labels).getByText("bug")).toBeInTheDocument();
    expect(within(labels).getByText("desktop")).toBeInTheDocument();
    expect(within(labels).queryByText("p1")).not.toBeInTheDocument();
    expect(within(labels).getByText("+1")).toBeInTheDocument();
  });

  it("leads with an open pull request and counts the rest", async () => {
    mockIssue(BASE_ISSUE, {
      pullRequests: [
        { id: "pr-1", number: 7600, state: "merged" },
        { id: "pr-2", number: 7612, state: "open" },
      ],
    });

    await openCard();

    const prs = propertyValue("Pull requests");
    expect(within(prs).getByTestId("pr-summary")).toHaveTextContent("#7612");
    expect(within(prs).getByText("+1")).toBeInTheDocument();
  });

  it("omits the pull request row when none are linked", async () => {
    await openCard();

    expect(screen.queryByText("Pull requests", { selector: "dt" })).not.toBeInTheDocument();
  });

  it("shows sub-issue progress when the workspace map has an entry", async () => {
    mockIssue(BASE_ISSUE, { progress: new Map([["issue-1", { done: 2, total: 5 }]]) });

    await openCard();

    expect(within(propertyValue("Sub-issues")).getByText("2/5")).toBeInTheDocument();
  });

  it("omits progress when the issue has no children", async () => {
    mockIssue(BASE_ISSUE, { progress: new Map([["other-issue", { done: 1, total: 3 }]]) });

    await openCard();

    expect(screen.queryByText(/^\d+\/\d+$/)).not.toBeInTheDocument();
  });

  it("omits progress when the issue has an entry with no children counted", async () => {
    mockIssue(BASE_ISSUE, { progress: new Map([["issue-1", { done: 0, total: 0 }]]) });

    await openCard();

    expect(screen.queryByText(/^\d+\/\d+$/)).not.toBeInTheDocument();
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
