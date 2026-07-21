// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask, Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockState = vi.hoisted(() => ({
  snapshot: [] as unknown[],
  // Captures what the chip hands its children so a test can assert the
  // avatar stack, the hover card and the colour tier without reaching into
  // their internals.
  avatarAgentIds: undefined as string[] | undefined,
  avatarOverflow: undefined as string | undefined,
  buttonVariant: undefined as string | undefined,
  hoverProps: undefined as
    | { issues: readonly Issue[]; taskCount: number }
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
  }) => {
    mockState.hoverProps = props;
    return <div data-testid="activity-hover">{props.taskCount}</div>;
  },
}));

// Record the variant the chip picks. The colour tier lives entirely in the
// Button variant now, so this is the assertable surface.
vi.mock("@multica/ui/components/ui/button", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/ui/components/ui/button")>(
      "@multica/ui/components/ui/button",
    );
  return {
    ...actual,
    Button: (props: React.ComponentProps<typeof actual.Button>) => {
      mockState.buttonVariant = props.variant ?? undefined;
      return <actual.Button {...props} />;
    },
  };
});

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
  chipAppearance,
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
  mockState.buttonVariant = undefined;
  mockState.hoverProps = undefined;
});

describe("WorkspaceAgentWorkingChip", () => {
  it("counts agents — the same list the avatar stack shows", () => {
    // The screenshot case from MUL-4884: 4 running tasks, 4 distinct agents,
    // 3 issues on screen. The chip counts the agents, so its number and the
    // heads beside it are one unit and cannot contradict each other.
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
      screen.getByRole("button", { name: "4 agents working" }),
    ).toBeTruthy();
    expect(mockState.avatarAgentIds).toEqual([
      "agent-1",
      "agent-2",
      "agent-3",
      "agent-4",
    ]);
    // Overflow is the component's standard +N badge again: with an
    // agent-anchored number, "3 shown + 1 = 4" corroborates the text instead
    // of competing with it.
    expect(mockState.avatarOverflow).toBeUndefined();
  });

  it("counts one agent once, however many issues it is working", () => {
    // The accepted trade-off: the number no longer predicts the row count.
    // One agent across two issues reads "1 agent working" and opens 2 rows —
    // WHO vs WHERE, different questions.
    mockState.snapshot = [
      makeTask({ id: "t-1", agent_id: "agent-1", issue_id: "issue-1" }),
      makeTask({ id: "t-2", agent_id: "agent-1", issue_id: "issue-2" }),
    ];

    renderWithI18n(
      <WorkspaceAgentWorkingChip
        value
        onToggle={() => {}}
        workingIssues={["issue-1", "issue-2"].map(makeIssue)}
      />,
    );

    expect(
      screen.getByRole("button", { name: "1 agent working" }),
    ).toBeTruthy();
  });

  it("renders an indeterminate state when the scope is unknown, never a number", () => {
    // 3 agents ARE running workspace-wide, but the surface could not resolve
    // which of them belong to the current window (table window resolving /
    // failed / too large). Publishing any count here — 0 from an empty
    // fallback or 3 from the workspace — would be a precise-looking wrong
    // answer (round-5 review P2). The chip must say "unknown" instead.
    mockState.snapshot = [
      makeTask({ id: "t-1", agent_id: "agent-1", issue_id: "issue-1" }),
      makeTask({ id: "t-2", agent_id: "agent-2", issue_id: "issue-2" }),
      makeTask({ id: "t-3", agent_id: "agent-3", issue_id: "issue-3" }),
    ];

    renderWithI18n(
      <WorkspaceAgentWorkingChip
        value={false}
        onToggle={() => {}}
        workingIssues={undefined}
      />,
    );

    expect(
      screen.getByRole("button", { name: "Agents working: —" }),
    ).toBeTruthy();
    // No avatar stack: heads next to an unknown label would still read as a
    // claim about who is in scope.
    expect(mockState.avatarAgentIds).toBeUndefined();
  });

  it("shows 0 when nothing is running", () => {
    mockState.snapshot = [];

    renderWithI18n(
      <WorkspaceAgentWorkingChip
        value={false}
        onToggle={() => {}}
        workingIssues={[]}
      />,
    );

    expect(
      screen.getByRole("button", { name: "0 agents working" }),
    ).toBeTruthy();
    expect(screen.queryByTestId("agent-avatar-stack")).toBeNull();
  });

  // Colour is carried by three self-contained Button variants. The tier
  // rules are unit-tested against `chipAppearance` below; these two cover
  // the wiring.
  it("uses the filled brand variant only while the filter is on", () => {
    mockState.snapshot = [makeTask({ id: "t-1", issue_id: "issue-1" })];

    const { rerender } = renderWithI18n(
      <WorkspaceAgentWorkingChip
        value={false}
        onToggle={() => {}}
        workingIssues={[makeIssue("issue-1")]}
      />,
    );

    // Activity, filter off → the tint variant.
    expect(mockState.buttonVariant).toBe("brandSubtle");

    rerender(
      <WorkspaceAgentWorkingChip
        value
        onToggle={() => {}}
        workingIssues={[makeIssue("issue-1")]}
      />,
    );

    expect(mockState.buttonVariant).toBe("brand");
  });

  it("stays a plain control when nothing is running", () => {
    mockState.snapshot = [];

    renderWithI18n(
      <WorkspaceAgentWorkingChip
        value={false}
        onToggle={() => {}}
        workingIssues={[]}
      />,
    );

    expect(mockState.buttonVariant).toBe("outline");
  });

  it("keeps issue-less tasks out of the agent count", () => {
    // Chat / autopilot tasks carry issue_id === "" (core/types/agent.ts).
    // Their agents are not working on any issue, so they must not join the
    // count or the stack.
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
      screen.getByRole("button", { name: "1 agent working" }),
    ).toBeTruthy();
    expect(mockState.avatarAgentIds).toEqual(["agent-1"]);
  });
});

describe("deriveWorkingChipView", () => {
  it("counts only running tasks whose issue is on screen", () => {
    const view = deriveWorkingChipView(
      [
        makeTask({ id: "t-1", agent_id: "a1", issue_id: "issue-1" }),
        makeTask({ id: "t-2", agent_id: "a2", issue_id: "issue-1" }),
        // chat/autopilot run — no linked issue
        makeTask({ id: "t-3", agent_id: "a3", issue_id: "" }),
        // running, but its issue is filtered out / past the loaded page
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

    expect(view.agentIds).toEqual(["a1", "a2"]);
    expect(view.taskCount).toBe(2);
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

    // Two issues, two tasks, but one agent — this is the number the chip
    // shows.
    expect(view.agentIds).toEqual(["a1"]);
    expect(view.taskCount).toBe(2);
  });
});

// The chip's colour must come from its Button variant and nothing else. A
// colour class in `className` is appended AFTER the variant, so
// tailwind-merge keeps it and it beats the variant — the opposite of what
// "the variant owns the colour" implies. These pin the tier rules including
// the state that already got this wrong.
describe("chipAppearance", () => {
  it("wears the filled brand tier while the filter is on", () => {
    expect(chipAppearance(true, true).variant).toBe("brand");
  });

  it("wears the tint tier for activity without the filter", () => {
    expect(chipAppearance(false, true).variant).toBe("brandSubtle");
  });

  it("wears the plain tier with muted text when nothing is running", () => {
    const a = chipAppearance(false, false);
    expect(a.variant).toBe("outline");
    // `outline` sets no text colour, so this tier supplies its own.
    expect(a.className).toContain("text-muted-foreground");
  });

  it("does not mute the text when the filter is on with 0 agents", () => {
    // Real state: the filter stays on after the last agent finishes. The
    // variant is `brand` here, so appending `text-muted-foreground` would
    // WIN over the variant's `text-brand-foreground` and paint grey text on
    // a brand-blue fill.
    const a = chipAppearance(true, false);
    expect(a.variant).toBe("brand");
    expect(a.className).not.toContain("text-muted-foreground");
  });

  it("never carries a colour class for either brand tier", () => {
    for (const a of [chipAppearance(true, true), chipAppearance(false, true)]) {
      expect(a.className).not.toMatch(/(^|\s)(text|bg|border)-/);
    }
  });
});
