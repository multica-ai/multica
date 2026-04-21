// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent, AgentTask, Issue } from "@multica/core/types";

const mockListAgentTasks = vi.hoisted(() => vi.fn());
const mockListIssues = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    useWorkspacePaths: () => actual.paths.workspace("test"),
  };
});

vi.mock("@multica/core/api", () => ({
  api: {
    listAgentTasks: (...args: unknown[]) => mockListAgentTasks(...args),
    listIssues: (...args: unknown[]) => mockListIssues(...args),
  },
}));

vi.mock("../../../navigation", () => ({
  AppLink: ({ children, href, ...props }: any) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));

import { TasksTab } from "./tasks-tab";

const agent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_env: {},
  custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace",
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: null,
  skills: [],
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function renderTasksTab(tasks: AgentTask[], issues: Issue[]) {
  mockListAgentTasks.mockResolvedValue(tasks);
  mockListIssues.mockImplementation(
    ({ open_only, status }: { open_only?: boolean; status?: string }) =>
      Promise.resolve({
        issues: open_only ? issues : status === "done" ? [] : [],
        total: open_only ? issues.length : 0,
      }),
  );

  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <TasksTab agent={agent} />
    </QueryClientProvider>,
  );
}

describe("TasksTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("uses workspace-scoped issue detail paths when issue data is loaded", async () => {
    renderTasksTab(
      [
        {
          id: "task-1",
          agent_id: "agent-1",
          runtime_id: "runtime-1",
          issue_id: "issue-1",
          status: "queued",
          priority: 1,
          dispatched_at: null,
          started_at: null,
          completed_at: null,
          result: null,
          error: null,
          created_at: "2026-04-16T00:00:00Z",
        },
      ],
      [
        {
          id: "issue-1",
          workspace_id: "ws-1",
          number: 1,
          identifier: "MUL-1",
          title: "Fix agent task routing",
          description: "",
          status: "todo",
          priority: "medium",
          assignee_type: null,
          assignee_id: null,
          creator_type: "member",
          creator_id: "user-1",
          parent_issue_id: null,
          project_id: null,
          position: 1,
          due_date: null,
          created_at: "2026-04-16T00:00:00Z",
          updated_at: "2026-04-16T00:00:00Z",
        },
      ],
    );

    const title = await screen.findByText("Fix agent task routing");
    const link = title.closest("a");

    expect(link?.getAttribute("href")).toBe("/test/issues/issue-1");
  });

  it("keeps task rows clickable when the issue is missing from the list query", async () => {
    renderTasksTab(
      [
        {
          id: "task-2",
          agent_id: "agent-1",
          runtime_id: "runtime-1",
          issue_id: "12345678-fallback",
          status: "completed",
          priority: 1,
          dispatched_at: null,
          started_at: null,
          completed_at: "2026-04-16T01:00:00Z",
          result: null,
          error: null,
          created_at: "2026-04-16T00:00:00Z",
        },
      ],
      [],
    );

    await waitFor(() => {
      expect(mockListAgentTasks).toHaveBeenCalledWith("agent-1");
    });

    const title = await screen.findByText("Issue 12345678...");
    const link = title.closest("a");

    expect(link?.getAttribute("href")).toBe("/test/issues/12345678-fallback");
  });
});
