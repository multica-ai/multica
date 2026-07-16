// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentTask, Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// The hover card renders one row per task and counts tasks, so its header
// must describe tasks — not agents. A single agent can run several tasks at
// once (e.g. the workspace chip reads "2 working" for two unique agents while
// the card lists three task rows). An agent-worded header here would print
// "3 agents working" for those two agents, contradicting the chip. MUL-3872.

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_type: string, id: string) =>
      ({ "agent-1": "Niko", "agent-2": "J" })[id] ?? "Unknown Agent",
    getActorInitials: (_type: string, id: string) =>
      ({ "agent-1": "NI", "agent-2": "J" })[id] ?? "UA",
    getActorAvatarUrl: () => null,
  }),
}));

// The card only reads these query results for avatars / availability, never
// for the header count, so empty lists keep the row chrome inert while the
// header still derives from the task array.
vi.mock("@multica/core/runtimes/queries", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));

vi.mock("@multica/core/agents", () => ({
  deriveAgentAvailability: () => "online",
}));

vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: ({ name }: { name: string }) => (
    <span data-testid="actor-avatar">{name}</span>
  ),
}));

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return { ...actual, useQuery: () => ({ data: [] }) };
});

import {
  AgentActivityHoverContent,
  WorkspaceAgentActivityHoverContent,
} from "./agent-activity-hover-content";

function makeIssue(id: string, identifier: string, title: string): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier,
    title,
    description: null,
    status: "in_progress",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "2026-06-08T08:00:00Z",
    updated_at: "2026-06-08T08:00:00Z",
  };
}

function makeTask(overrides: Partial<AgentTask>): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-06-08T08:00:00Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-06-08T08:00:00Z",
    ...overrides,
  };
}

afterEach(cleanup);

describe("AgentActivityHoverContent", () => {
  // Two agents, three running tasks (Niko runs two at once). The header must
  // count the three task rows, not the two agents.
  const threeTasksTwoAgents = [
    makeTask({ id: "t1", agent_id: "agent-1" }),
    makeTask({ id: "t2", agent_id: "agent-1" }),
    makeTask({ id: "t3", agent_id: "agent-2" }),
  ];

  it("counts tasks, not agents, in the header", () => {
    renderWithI18n(<AgentActivityHoverContent tasks={threeTasksTwoAgents} />);

    expect(screen.getByText("3 tasks working")).toBeInTheDocument();
    // The old agent-worded copy would have read "3 agents working" here and
    // disagreed with the chip's unique-agent count.
    expect(screen.queryByText(/agents? working/)).not.toBeInTheDocument();
    // One row per task — three avatars for three tasks.
    expect(screen.getAllByTestId("actor-avatar")).toHaveLength(3);
  });

  it("uses the singular task copy for a single task", () => {
    renderWithI18n(<AgentActivityHoverContent tasks={[makeTask({})]} />);

    expect(screen.getByText("1 task working")).toBeInTheDocument();
  });

  it("renders the requested Chinese task copy", () => {
    renderWithI18n(<AgentActivityHoverContent tasks={threeTasksTwoAgents} />, {
      locale: "zh-Hans",
    });

    expect(screen.getByText("3 个 task 工作中")).toBeInTheDocument();
  });
});

// The workspace chip can only carry one number (issues — the rows a click
// produces). This card is where the other units get stated instead of
// silently contradicting it, and where everything the number excludes is
// disclosed rather than dropped. MUL-4884.
describe("WorkspaceAgentActivityHoverContent", () => {
  it("names both counted units so the chip's number stops looking wrong", () => {
    // The MUL-4884 screenshot: chip says 3 while 4 agent heads show. Naming
    // both units is what resolves that, rather than hiding one.
    renderWithI18n(
      <WorkspaceAgentActivityHoverContent
        issues={[
          makeIssue("i-1", "MUL-4879", "Counting logic looks wrong"),
          makeIssue("i-2", "MUL-4881", "daemon extra work dir"),
          makeIssue("i-3", "MUL-4883", "First PR review flow"),
        ]}
        tasksByIssueId={
          new Map([
            [
              "i-1",
              [
                makeTask({ id: "t1", agent_id: "agent-1", issue_id: "i-1" }),
                makeTask({ id: "t4", agent_id: "agent-2", issue_id: "i-1" }),
              ],
            ],
            ["i-2", [makeTask({ id: "t2", agent_id: "agent-1", issue_id: "i-2" })]],
            ["i-3", [makeTask({ id: "t3", agent_id: "agent-2", issue_id: "i-3" })]],
          ])
        }
        taskCount={4}
        unlinkedCount={0}
        outOfScopeCount={0}
      />,
    );

    expect(screen.getByText("3 issues in progress · 4 tasks")).toBeInTheDocument();
    // Rows group under their issue, mirroring what the filter does.
    expect(screen.getByText("MUL-4879")).toBeInTheDocument();
    expect(screen.getByText("Counting logic looks wrong")).toBeInTheDocument();
    expect(screen.getAllByTestId("actor-avatar")).toHaveLength(4);
  });

  it("states what the number excludes rather than dropping it", () => {
    renderWithI18n(
      <WorkspaceAgentActivityHoverContent
        issues={[makeIssue("i-1", "MUL-1", "One")]}
        tasksByIssueId={
          new Map([["i-1", [makeTask({ id: "t1", issue_id: "i-1" })]]])
        }
        taskCount={1}
        unlinkedCount={1}
        outOfScopeCount={2}
      />,
    );

    expect(
      screen.getByText(
        "1 more task has no linked issue (chat/autopilot) — not counted",
      ),
    ).toBeInTheDocument();
    // The list loads one page per status, so running work can sit past the
    // window. Say so instead of silently under-counting.
    expect(
      screen.getByText(
        "2 more tasks are outside the current filters or loaded range — not counted",
      ),
    ).toBeInTheDocument();
  });

  it("stays quiet when there is nothing to disclose", () => {
    renderWithI18n(
      <WorkspaceAgentActivityHoverContent
        issues={[makeIssue("i-1", "MUL-1", "One")]}
        tasksByIssueId={
          new Map([["i-1", [makeTask({ id: "t1", issue_id: "i-1" })]]])
        }
        taskCount={1}
        unlinkedCount={0}
        outOfScopeCount={0}
      />,
    );

    expect(screen.queryByText(/not counted/)).not.toBeInTheDocument();
  });

  it("still discloses excluded work when nothing is counted", () => {
    // Only running work is a chat task: the chip reads 0, and the card has to
    // explain why rather than look broken.
    renderWithI18n(
      <WorkspaceAgentActivityHoverContent
        issues={[]}
        tasksByIssueId={new Map()}
        taskCount={0}
        unlinkedCount={1}
        outOfScopeCount={0}
      />,
    );

    expect(
      screen.getByText("No issues in progress right now"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "1 more task has no linked issue (chat/autopilot) — not counted",
      ),
    ).toBeInTheDocument();
  });

  it("renders the Chinese copy for the counted units", () => {
    renderWithI18n(
      <WorkspaceAgentActivityHoverContent
        issues={[makeIssue("i-1", "MUL-1", "One")]}
        tasksByIssueId={
          new Map([["i-1", [makeTask({ id: "t1", issue_id: "i-1" })]]])
        }
        taskCount={2}
        unlinkedCount={0}
        outOfScopeCount={0}
      />,
      { locale: "zh-Hans" },
    );

    expect(screen.getByText("1 个 issue 进行中 · 2 个 task")).toBeInTheDocument();
  });
});
