// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

// useWorkspaceId is a Context-backed hook in core; stub it to a static id so
// the card runs outside a WorkspaceIdProvider in tests.
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// Paths only needs issueDetail for the "Now on" link. A simple stub keeps the
// test free of WorkspaceSlugProvider wiring.
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/test/issues/${id}`,
  }),
}));

// AppLink is just a plain anchor here — wiring the navigation adapter would
// add nothing to these assertions.
vi.mock("../../navigation", () => ({
  AppLink: ({
    href,
    children,
    ...rest
  }: {
    href: string;
    children: React.ReactNode;
    [k: string]: unknown;
  }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}));

// Each test sets these up via beforeEach.
const mockAgents = vi.hoisted(() => ({ current: [] as unknown[] }));
const mockSnapshot = vi.hoisted(() => ({ current: [] as unknown[] }));
const mockIssue = vi.hoisted(() => ({ current: null as unknown }));
const mockPresence = vi.hoisted(
  () => ({ current: "loading" as unknown }),
);

// Distinguish queries by the function reference of the queryFn — the agent
// list, snapshot, and issue detail are all `queryOptions(...)` records that
// the component spreads into useQuery. Match on `queryKey[2]` which we know
// is unique per query factory.
vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
    "@tanstack/react-query",
  );
  return {
    ...actual,
    useQuery: (opts: { queryKey: readonly unknown[]; enabled?: boolean }) => {
      const key = opts.queryKey;
      // Distinguish by the third segment which is the factory tag:
      //   ["workspaces", wsId, "agents"]                       — agent list
      //   ["workspaces", wsId, "agent-task-snapshot", "list"]  — snapshot
      //   ["issues",     wsId, "detail", id]                   — issue detail
      const root = key[0];
      const marker = key[2];
      if (root === "workspaces" && marker === "agents") {
        return { data: mockAgents.current, isLoading: false };
      }
      if (root === "workspaces" && marker === "agent-task-snapshot") {
        return { data: mockSnapshot.current, isLoading: false };
      }
      if (root === "issues" && marker === "detail") {
        return {
          data: opts.enabled ? mockIssue.current : undefined,
          isLoading: false,
        };
      }
      return { data: undefined, isLoading: false };
    },
  };
});

vi.mock("@multica/core/agents", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/agents")>(
      "@multica/core/agents",
    );
  return {
    ...actual,
    useAgentPresenceDetail: () => mockPresence.current,
  };
});

import { AgentLivePeekCard } from "./agent-live-peek-card";

function makeAgent(overrides: Record<string, unknown> = {}) {
  return {
    id: "agent-1",
    workspace_id: "ws-1",
    runtime_id: "rt-1",
    name: "Squirtle",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local" as const,
    runtime_config: {},
    custom_env: {},
    custom_args: [],
    custom_env_redacted: false,
    visibility: "private" as const,
    status: "idle" as const,
    max_concurrent_tasks: 1,
    model: "",
    owner_id: "user-me",
    skills: [],
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function makeTask(overrides: Record<string, unknown>) {
  return {
    id: "task-x",
    agent_id: "agent-1",
    runtime_id: "rt-1",
    issue_id: "",
    status: "completed" as const,
    priority: 0,
    dispatched_at: null,
    started_at: null,
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-05-14T00:00:00Z",
    ...overrides,
  };
}

function renderCard() {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <AgentLivePeekCard agentId="agent-1" />
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  cleanup();
  mockAgents.current = [makeAgent()];
  mockSnapshot.current = [];
  mockIssue.current = null;
  mockPresence.current = {
    availability: "online",
    workload: "idle",
    runningCount: 0,
    queuedCount: 0,
    capacity: 1,
  };
});

describe("AgentLivePeekCard", () => {
  it("renders Working state with the linked current issue", () => {
    mockSnapshot.current = [
      makeTask({
        id: "task-running",
        status: "running",
        issue_id: "issue-42",
        started_at: "2026-05-14T08:00:00Z",
      }),
    ];
    mockIssue.current = {
      id: "issue-42",
      identifier: "MUL-42",
      title: "Wire up live peek",
    };
    mockPresence.current = {
      availability: "online",
      workload: "working",
      runningCount: 1,
      queuedCount: 0,
      capacity: 1,
    };

    renderCard();

    expect(screen.getByText("Working")).toBeInTheDocument();
    // identifier + title both render under the same link.
    const link = screen.getByRole("link", { name: /MUL-42/ });
    expect(link).toHaveAttribute("href", "/test/issues/issue-42");
    expect(link.textContent).toContain("Wire up live peek");
  });

  it("renders Idle + empty issue copy when nothing is running", () => {
    mockPresence.current = {
      availability: "online",
      workload: "idle",
      runningCount: 0,
      queuedCount: 0,
      capacity: 1,
    };
    mockSnapshot.current = [
      makeTask({
        id: "task-done",
        status: "completed",
        completed_at: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
      }),
    ];

    renderCard();

    expect(screen.getByText("Idle")).toBeInTheDocument();
    expect(screen.getByText(enAgents.live_peek.no_current_issue)).toBeInTheDocument();
    // "5m ago" — proves last activity falls back to the most recent terminal
    // task in the snapshot.
    expect(screen.getByText(/5m ago/)).toBeInTheDocument();
    // No failed indicator on a completed terminal state.
    expect(screen.queryByText(enAgents.live_peek.failed_indicator)).toBeNull();
  });

  it("shows the failed indicator on the last-activity row when the most recent terminal task failed", () => {
    mockPresence.current = {
      availability: "online",
      // Per the project's deliberate split, workload is current-only — so
      // a failed terminal task does NOT flip workload to anything besides
      // idle / queued / working.
      workload: "idle",
      runningCount: 0,
      queuedCount: 0,
      capacity: 1,
    };
    mockSnapshot.current = [
      makeTask({
        id: "task-failed",
        status: "failed",
        completed_at: new Date(Date.now() - 2 * 60 * 1000).toISOString(),
      }),
    ];

    renderCard();

    expect(screen.getByText("Idle")).toBeInTheDocument();
    expect(screen.getByText(enAgents.live_peek.failed_indicator)).toBeInTheDocument();
  });
});
