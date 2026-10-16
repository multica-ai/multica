// @vitest-environment jsdom

import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent, AgentTask, Issue, Project } from "@multica/core/types";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import enProjects from "../../../locales/en/projects.json";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "../../../navigation";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, projects: enProjects },
};

// Fixtures must survive vi.mock hoisting, so build them inside vi.hoisted().
const FX = vi.hoisted(() => ({
  PROJECT_TITLE: "AI Mahjong",
  issue: {
    id: "issue-1",
    identifier: "SHAR-126",
    title: "LLM baseline run",
    project_id: "project-1",
  },
  project: { id: "project-1", title: "AI Mahjong", icon: null },
  task: {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-01-01T00:00:00.000Z",
    completed_at: null,
    created_at: "2026-01-01T00:00:00.000Z",
    result: null,
    error: null,
  },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/api", () => ({ api: {} }));
vi.mock("../../../common/task-transcript", () => ({
  TranscriptButton: () => null,
}));
vi.mock("@multica/core/agents", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/agents")>();
  return {
    ...actual,
    agentTaskSnapshotOptions: () => ({
      queryKey: ["snapshot"],
      queryFn: () => Promise.resolve([FX.task as unknown as AgentTask]),
    }),
    agentTasksOptions: () => ({
      queryKey: ["agent-tasks"],
      initialPageParam: undefined,
      getNextPageParam: () => undefined,
      queryFn: () => Promise.resolve({ tasks: [], nextCursor: null }),
    }),
    useWorkspaceActivityMap: () => ({ byAgent: new Map() }),
  };
});
vi.mock("@multica/core/issues/queries", () => ({
  issueDetailOptions: (_wsId: string, id: string) => ({
    queryKey: ["issue", id],
    queryFn: () => Promise.resolve(FX.issue as unknown as Issue),
  }),
}));
vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: () => ({
    queryKey: ["projects"],
    queryFn: () => Promise.resolve([FX.project as unknown as Project]),
  }),
  projectDetailOptions: () => ({
    queryKey: ["project-detail"],
    queryFn: () => Promise.resolve(FX.project as unknown as Project),
  }),
}));

import { ActivityTab } from "./activity-tab";

const baseAgent = { id: "agent-1", name: "Agent" } as unknown as Agent;

function renderTab() {
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
          <WorkspaceSlugProvider slug="acme">
            <ActivityTab agent={baseAgent} showPerformance={false} />
          </WorkspaceSlugProvider>
        </QueryClientProvider>
      </NavigationProvider>
    </I18nProvider>,
  );
}

describe("ActivityTab project tag", () => {
  it("tags a running row whose issue belongs to a project with the project name", async () => {
    renderTab();
    // Issue identity still renders.
    expect(await screen.findByText("SHAR-126")).toBeInTheDocument();
    expect(screen.getByText("LLM baseline run")).toBeInTheDocument();
    // Project name is surfaced as a tag on the same row once the project
    // list resolves.
    expect(await screen.findByText(FX.PROJECT_TITLE)).toBeInTheDocument();
  });
});
