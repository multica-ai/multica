// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent, AgentActivityBucket } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "../../../navigation";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/issues/${id}` }),
}));

// TaskRow never mounts in these aggregate/loading/empty-state tests.
vi.mock("@multica/core/api", () => ({ api: {} }));
vi.mock("../../../common/task-transcript", () => ({
  TranscriptButton: () => null,
}));

// Keep "Now" empty while varying activity outcomes and task-list loading.
const agentTasksRef = vi.hoisted(() => ({
  current: () => new Promise<unknown>(() => {}),
}));
const activityRef = vi.hoisted(() => ({ current: [] as AgentActivityBucket[] }));
vi.mock("@multica/core/agents", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/agents")>();
  return {
    ...actual,
    agentTaskSnapshotOptions: () => ({
      queryKey: ["snapshot"],
      queryFn: () => Promise.resolve([]),
    }),
    agentTasksOptions: () => ({
      queryKey: ["agent-tasks"],
      queryFn: () => agentTasksRef.current(),
    }),
    useWorkspaceActivityMap: () => ({
      byAgent: new Map([[
        "agent-1",
        actual.deriveAgentActivity(activityRef.current, "2026-01-01", Date.now()),
      ]]),
    }),
  };
});

import { ActivityTab, AgentPerformanceSummary } from "./activity-tab";

const baseAgent = {
  id: "agent-1",
  name: "Agent",
} as unknown as Agent;

const EMPTY_RECENT = "This agent hasn't completed anything yet.";

function renderTab(performance = false) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/agents/agent-1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <NavigationProvider value={navigation}>
        <QueryClientProvider client={queryClient}>
          {performance && <AgentPerformanceSummary agent={baseAgent} />}
          <ActivityTab agent={baseAgent} showPerformance={performance} />
        </QueryClientProvider>
      </NavigationProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  agentTasksRef.current = () => new Promise<unknown>(() => {});
  activityRef.current = [];
});

describe("agent outcome presentation", () => {
  it("uses completed and failed outcomes in both summaries and shows cancellations separately", () => {
    activityRef.current = [{
      agent_id: "agent-1",
      bucket_at: new Date().toISOString(),
      task_count: 10,
      failed_count: 1,
      completed_count: 1,
      cancelled_count: 8,
    }];
    renderTab(true);
    expect(screen.getByText("50%")).toBeInTheDocument();
    expect(screen.getByText("50% success")).toBeInTheDocument();
    expect(screen.getAllByText("8 cancelled")).toHaveLength(2);
    expect(screen.queryByText("90%")).not.toBeInTheDocument();
  });

  it("does not claim success for cancelled-only data", () => {
    activityRef.current = [{
      agent_id: "agent-1",
      bucket_at: new Date().toISOString(),
      task_count: 8,
      failed_count: 0,
      completed_count: 0,
      cancelled_count: 8,
    }];
    const { container } = renderTab(true);
    expect(screen.queryByText("100%")).not.toBeInTheDocument();
    expect(screen.queryByText("100% success")).not.toBeInTheDocument();
    expect(container.querySelectorAll('rect[fill="var(--color-brand)"]')).toHaveLength(0);
    expect(screen.getByText("success rate").parentElement).toHaveTextContent("—");
  });
});

describe("ActivityTab Recent work loading state", () => {
  it("shows a skeleton, not the empty state, while the task list is loading", () => {
    // Never-resolving queryFn keeps the per-agent task query pending, which is
    // exactly the first-paint window the skeleton is meant to cover.
    const { container } = renderTab();
    expect(
      container.querySelectorAll('[data-slot="skeleton"]').length,
    ).toBeGreaterThan(0);
    expect(screen.queryByText(EMPTY_RECENT)).not.toBeInTheDocument();
  });

  it("shows the empty state once the task list resolves to no runs", async () => {
    agentTasksRef.current = () => Promise.resolve([]);
    renderTab();
    expect(await screen.findByText(EMPTY_RECENT)).toBeInTheDocument();
    expect(
      document.querySelectorAll('[data-slot="skeleton"]').length,
    ).toBe(0);
  });

  it.each([
    { status: "completed", cancelled_by: undefined, label: "Succeeded" },
    {
      status: "cancelled",
      cancelled_by: { type: "member", name: "Alex" },
      label: "Cancelled by Alex",
    },
  ])("keeps cleanup warnings alongside $status attribution", async ({ status, cancelled_by, label }) => {
    agentTasksRef.current = () =>
      Promise.resolve([
        {
          id: "task-1",
          agent_id: "agent-1",
          runtime_id: "runtime-1",
          issue_id: "",
          status,
          cancelled_by,
          priority: 0,
          dispatched_at: null,
          started_at: null,
          completed_at: "2026-08-28T01:00:00Z",
          result: null,
          error: null,
          warnings: ["automatic retry could not be recorded"],
          created_at: "2026-08-28T00:00:00Z",
          kind: "quick_create",
        },
      ]);

    renderTab();

    expect(await screen.findByText("Warning")).toBeInTheDocument();
    expect(screen.getByText(label)).toBeInTheDocument();
  });
});
