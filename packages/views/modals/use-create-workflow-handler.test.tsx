import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  agentListOptions,
  memberListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { issueWorkflowListOptions } from "@multica/core/issue-workflows/queries";
import type { Agent, IssueWorkflowHandler } from "@multica/core/types";
import { useCreateWorkflowHandler } from "./use-create-workflow-handler";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (state: { user: { id: string } }) => unknown) =>
    select({ user: { id: "user-1" } }),
}));

let client: QueryClient;
const agent: Agent = {
  workspace_id: "ws-test",
  name: "Website agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "private",
  invocation_targets: [],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  skills: [],
  created_at: "",
  updated_at: "",
  archived_by: null,
  id: "agent-1",
  owner_id: "user-1",
  runtime_id: "runtime-1",
  archived_at: null,
  permission_mode: "private",
};

function handlerResult(
  handler: IssueWorkflowHandler,
  projectId: string | null = "proj-1",
  status = "todo",
) {
  client.setQueryData(issueWorkflowListOptions("ws-test").queryKey, {
    workflows: [
      {
        id: "workflow-1",
        workspace_id: "ws-test",
        name: "Delivery",
        description: "",
        is_default: false,
        owner_id: null,
        project_ids: ["proj-1"],
        created_at: "",
        updated_at: "",
        steps: [
          {
            status_key: "todo",
            handler,
            instructions: "Follow the project brief",
          },
        ],
      },
    ],
    total: 1,
  });
  return renderHook(() => useCreateWorkflowHandler(projectId, status), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  }).result.current;
}

describe("create workflow defaults", () => {
  beforeEach(() => {
    client = new QueryClient({
      defaultOptions: { queries: { staleTime: Infinity, retry: false } },
    });
    client.setQueryData(agentListOptions("ws-test").queryKey, [agent]);
    client.setQueryData(memberListOptions("ws-test").queryKey, [
      {
        id: "member-1",
        workspace_id: "ws-test",
        user_id: "user-1",
        role: "member",
        name: "User",
        email: "dev@localhost",
        avatar_url: null,
        created_at: "",
      },
    ]);
    client.setQueryData(squadListOptions("ws-test").queryKey, [
      {
        id: "squad-1",
        workspace_id: "ws-test",
        name: "Website",
        description: "",
        instructions: "",
        avatar_url: null,
        creator_id: "user-1",
        created_at: "",
        updated_at: "",
        archived_by: null,
        leader_id: "agent-1",
        archived_at: null,
      },
    ]);
    client.setQueryData(projectListOptions("ws-test").queryKey, {
      projects: [
        {
          id: "proj-1",
          workspace_id: "ws-test",
          title: "Website",
          description: null,
          icon: null,
          status: "planned",
          priority: "none",
          start_date: null,
          due_date: null,
          created_at: "",
          updated_at: "",
          issue_count: 0,
          done_count: 0,
          resource_count: 0,
          archived_at: null,
          workflow_id: "workflow-1",
          lead_type: "agent",
          lead_id: "agent-1",
        },
      ],
      total: 1,
    });
  });

  it.each([
    [
      { type: "agent", id: "agent-1" },
      { type: "agent", id: "agent-1" },
    ],
    [
      { type: "squad", id: "squad-1" },
      { type: "squad", id: "squad-1" },
    ],
    [{ type: "project_lead" }, { type: "agent", id: "agent-1" }],
    [
      { type: "member", id: "user-1" },
      { type: "member", id: "user-1" },
    ],
    [{ type: "creator" }, { type: "member", id: "user-1" }],
    [{ type: "none" }, null],
    [{ type: "agent", id: "deleted" }, null],
    [{ type: "member", id: "deleted" }, null],
  ] as const)("resolves %j to %j", (handler, expected) => {
    expect(handlerResult(handler)).toEqual(expected);
  });

  it.each([
    { ...agent, archived_at: "2026-11-17" },
    { ...agent, runtime_id: "" },
    { ...agent, owner_id: "another-user" },
  ])(
    "ignores an unavailable or unauthorized agent and its squad: %j",
    (unavailable) => {
      client.setQueryData(agentListOptions("ws-test").queryKey, [unavailable]);
      expect(handlerResult({ type: "agent", id: "agent-1" })).toBeNull();
      expect(handlerResult({ type: "squad", id: "squad-1" })).toBeNull();
    },
  );

  it("does not use a lead without a handoff, outside a project, or on another status", () => {
    expect(handlerResult({ type: "none" })).toBeNull();
    expect(handlerResult({ type: "project_lead" }, null)).toBeNull();
    expect(
      handlerResult({ type: "project_lead" }, "proj-1", "backlog"),
    ).toBeNull();
  });
});
