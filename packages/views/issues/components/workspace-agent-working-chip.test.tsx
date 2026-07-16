// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask, Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockState = vi.hoisted(() => ({
  snapshot: [] as unknown[],
  // Captures what the chip hands its two children so a test can assert the
  // avatar stack and the hover card without reaching into their internals.
  avatarAgentIds: undefined as string[] | undefined,
  avatarOverflow: undefined as string | undefined,
  hoverProps: undefined as
    | {
        issues: readonly Issue[];
        taskCount: number;
        unlinkedCount: number;
        outOfScopeCount: number;
      }
    | undefined,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/agents", () => ({
  agentTaskSnapshotOptions: (wsId: string) => ({
    queryKey: ["agents", "task-snapshot", wsId],
  }),
}));

vi.mock("../../agents/components/agent-avatar-stack", () => ({
  AgentAvatarStack: ({
    agentIds,
    overflow,
  }: {
    agentIds: string[];
    overflow?: string;
  }) => {
    mockState.avatarAgentIds = agentIds;
    mockState.avatarOverflow = overflow;
    return <div data-testid="agent-avatar-stack">{agentIds.length}</div>;
  },
}));

vi.mock("../../agents/components/agent-activity-hover-content", () => ({
  WorkspaceAgentActivityHoverContent: (props: {
    issues: readonly Issue[];
    taskCount: number;
    unlinkedCount: number;
    outOfScopeCount: number;
  }) => {
    mockState.hoverProps = props;
    return <div data-testid="activity-hover">{props.taskCount}</div>;
  },
}));

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQuery: (opts: { queryKey?: readonly unknown[] }) => {
      if (opts.queryKey?.[1] === "task-snapshot") {
        return { data: mockState.snapshot };
      }
      return { data: undefined };
    },
  };
});

import {
  WorkspaceAgentWorkingChip,
  deriveWorkingChipView,
} from "./workspace-agent-working-chip";

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

function makeIssue(id: string): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `MUL-${id}`,
    title: `Issue ${id}`,
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

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  mockState.snapshot = [];
  mockState.avatarAgentIds = undefined;
  mockState.avatarOverflow = undefined;
  mockState.hoverProps = undefined;
});

describe("WorkspaceAgentWorkingChip", () => {
  it("shows the row count the filter produces, not a snapshot re-derivation", () => {
    // The screenshot case from MUL-4884: 4 running tasks, 4 distinct agents,
    // 3 issues on screen. The chip says 3 — the number of rows a click
    // leaves — and says it with a unit.
    mockState.snapshot = [
      makeTask({ id: "t-1", agent_id: "agent-1", issue_id: "issue-1" }),
      makeTask({ id: "t-2", agent_id: "agent-2", issue_id: "issue-2" }),
      makeTask({ id: "t-3", agent_id: "agent-3", issue_id: "issue-3" }),
      makeTask({ id: "t-4", agent_id: "agent-4", issue_id: "issue-3" }),
    ];

    renderWithI18n(
      <WorkspaceAgentWorkingChip
        value={false}
        onToggle={() => {}}
        workingIssues={["issue-1", "issue-2", "issue-3"].map(makeIssue)}
      />,
    );

    expect(
      screen.getByRole("button", { name: "3 issues in progress" }),
    ).toBeTruthy();
    // The stack still shows every distinct agent behind that work...
    expect(mockState.avatarAgentIds).toEqual([
      "agent-1",
      "agent-2",
      "agent-3",
      "agent-4",
    ]);
    // ...but never as a rival "+N" number next to the issue count. The task
    // and agent units are explained in the hover card, which Base UI mounts
    // only on open — see agent-activity-hover-content.test.tsx.
    expect(mockState.avatarOverflow).toBe("fade");
  });

  it("counts exactly workingIssues.length even when the filter is on", () => {
    // With the filter on, workingIssues IS the rendered list. The chip must
    // agree with it and nothing else.
    mockState.snapshot = [
      makeTask({ id: "t-1", agent_id: "agent-1", issue_id: "issue-1" }),
      makeTask({ id: "t-2", agent_id: "agent-2", issue_id: "issue-2" }),
    ];

    renderWithI18n(
      <WorkspaceAgentWorkingChip
        value
        onToggle={() => {}}
        workingIssues={["issue-1", "issue-2"].map(makeIssue)}
      />,
    );

    expect(
      screen.getByRole("button", { name: "2 issues in progress" }),
    ).toBeTruthy();
  });

  it("uses the singular unit for one issue", () => {
    mockState.snapshot = [makeTask({ id: "t-1", issue_id: "issue-1" })];

    renderWithI18n(
      <WorkspaceAgentWorkingChip
        value={false}
        onToggle={() => {}}
        workingIssues={[makeIssue("issue-1")]}
      />,
    );

    expect(
      screen.getByRole("button", { name: "1 issue in progress" }),
    ).toBeTruthy();
  });

  it("shows 0 when nothing is in progress", () => {
    mockState.snapshot = [];

    renderWithI18n(
      <WorkspaceAgentWorkingChip
        value={false}
        onToggle={() => {}}
        workingIssues={[]}
      />,
    );

    expect(
      screen.getByRole("button", { name: "0 issues in progress" }),
    ).toBeTruthy();
    // No heads to show when nothing is running.
    expect(screen.queryByTestId("agent-avatar-stack")).toBeNull();
  });

  // Colour is two-step on purpose: the loud filled state is reserved for
  // "this filter is ON". Idle activity is a tint, so the chip reads as a
  // quiet tool rather than an alert.
  it("keeps the filled brand state for the active filter, not for mere activity", () => {
    mockState.snapshot = [makeTask({ id: "t-1", issue_id: "issue-1" })];

    const { rerender } = renderWithI18n(
      <WorkspaceAgentWorkingChip
        value={false}
        onToggle={() => {}}
        workingIssues={[makeIssue("issue-1")]}
      />,
    );

    // Activity, filter off → a tint, never the fill.
    const idle = screen.getByRole("button").className;
    expect(idle).toContain("bg-brand/5");
    expect(idle).not.toContain("bg-brand ");

    rerender(
      <WorkspaceAgentWorkingChip
        value
        onToggle={() => {}}
        workingIssues={[makeIssue("issue-1")]}
      />,
    );

    // Filter on → filled.
    expect(screen.getByRole("button").className).toContain("bg-brand ");
  });

  it("stays muted when there is no activity at all", () => {
    mockState.snapshot = [];

    renderWithI18n(
      <WorkspaceAgentWorkingChip
        value={false}
        onToggle={() => {}}
        workingIssues={[]}
      />,
    );

    const className = screen.getByRole("button").className;
    expect(className).toContain("text-muted-foreground");
    expect(className).not.toContain("bg-brand");
  });

  it("reports issue-less tasks to the hover card instead of counting them", () => {
    // Chat / autopilot tasks carry issue_id === "" (core/types/agent.ts).
    // They used to collapse into one phantom "" issue and inflate the count
    // by exactly 1 (MUL-4884).
    mockState.snapshot = [
      makeTask({ id: "t-1", agent_id: "agent-1", issue_id: "issue-1" }),
      makeTask({ id: "t-2", agent_id: "agent-2", issue_id: "" }),
      makeTask({ id: "t-3", agent_id: "agent-3", issue_id: "" }),
    ];

    renderWithI18n(
      <WorkspaceAgentWorkingChip
        value={false}
        onToggle={() => {}}
        workingIssues={[makeIssue("issue-1")]}
      />,
    );

    expect(
      screen.getByRole("button", { name: "1 issue in progress" }),
    ).toBeTruthy();
    // Their agents are not in the stack either: the stack explains the count.
    // (They are not dropped — deriveWorkingChipView routes them to the hover
    // card's "not counted" note; see the bucket tests below.)
    expect(mockState.avatarAgentIds).toEqual(["agent-1"]);
  });
});

describe("deriveWorkingChipView", () => {
  it("buckets every running task exactly once", () => {
    const view = deriveWorkingChipView(
      [
        makeTask({ id: "t-1", agent_id: "a1", issue_id: "issue-1" }),
        makeTask({ id: "t-2", agent_id: "a2", issue_id: "issue-1" }),
        makeTask({ id: "t-3", agent_id: "a3", issue_id: "" }),
        makeTask({ id: "t-4", agent_id: "a4", issue_id: "issue-offscreen" }),
        makeTask({
          id: "t-5",
          agent_id: "a5",
          issue_id: "issue-1",
          status: "queued",
        }),
      ],
      [makeIssue("issue-1")],
    );

    expect(view.taskCount).toBe(2);
    expect(view.unlinkedCount).toBe(1);
    // Running work on an issue the list isn't showing (filtered out, or past
    // the 50-per-status page) is disclosed rather than dropped.
    expect(view.outOfScopeCount).toBe(1);
    // Queued tasks are not "in progress" and land in no bucket.
    expect(view.agentIds).toEqual(["a1", "a2"]);
    expect(view.tasksByIssueId.get("issue-1")?.map((t) => t.id)).toEqual([
      "t-1",
      "t-2",
    ]);
  });

  it("dedupes agents that run several tasks at once", () => {
    const view = deriveWorkingChipView(
      [
        makeTask({ id: "t-1", agent_id: "a1", issue_id: "issue-1" }),
        makeTask({ id: "t-2", agent_id: "a1", issue_id: "issue-2" }),
      ],
      [makeIssue("issue-1"), makeIssue("issue-2")],
    );

    expect(view.agentIds).toEqual(["a1"]);
    expect(view.taskCount).toBe(2);
  });
});
