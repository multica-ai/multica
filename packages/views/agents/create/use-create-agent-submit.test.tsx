// @vitest-environment jsdom

import type { ReactNode } from "react";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { EMPTY_AGENT_DRAFT } from "@multica/core/agents";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent } from "@multica/core/types";
import { workspaceKeys } from "@multica/core/workspace/queries";
import enAgents from "../../locales/en/agents.json";

const mockCreateAgent = vi.hoisted(() => vi.fn());
const mockPush = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    agentDetail: (agentId: string) => `/acme/agents/${agentId}`,
    squadDetail: (squadId: string) => `/acme/squads/${squadId}`,
  }),
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: mockPush }),
}));

vi.mock("@multica/core/api", () => {
  class ApiError extends Error {
    status: number;
    constructor(message: string, status: number) {
      super(message);
      this.status = status;
    }
  }
  return {
    api: {
      createAgent: mockCreateAgent,
      addSquadMember: vi.fn(),
    },
    ApiError,
  };
});

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), warning: vi.fn() },
}));

import { useCreateAgentSubmit } from "./use-create-agent-submit";

const CREATED_AGENT: Agent = {
  id: "agent-new",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Blank agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "workspace",
  permission_mode: "public_to",
  invocation_targets: [{ target_type: "workspace", target_id: null }],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-08-15T00:00:00Z",
  updated_at: "2026-08-15T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function wrapper(queryClient: QueryClient) {
  return function TestWrapper({ children }: { children: ReactNode }) {
    return (
      <I18nProvider locale="en" resources={{ en: { agents: enAgents } }}>
        <QueryClientProvider client={queryClient}>
          {children}
        </QueryClientProvider>
      </I18nProvider>
    );
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mockCreateAgent.mockResolvedValue(CREATED_AGENT);
});

describe("useCreateAgentSubmit cache handoff", () => {
  it("hydrates list and detail before navigating without awaiting reconciliation", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    queryClient.setQueryData<Agent[]>(workspaceKeys.agents("ws-1"), []);

    // A slow list reconciliation must not gate the detail route.
    vi.spyOn(queryClient, "invalidateQueries").mockReturnValue(
      new Promise<void>(() => {}),
    );
    let navigationSnapshot:
      | { path: string; list: Agent[] | undefined; detail: Agent | undefined }
      | undefined;
    mockPush.mockImplementation((path: string) => {
      navigationSnapshot = {
        path,
        list: queryClient.getQueryData<Agent[]>(
          workspaceKeys.agents("ws-1"),
        ),
        detail: queryClient.getQueryData<Agent>(
          workspaceKeys.agent("ws-1", CREATED_AGENT.id),
        ),
      };
    });

    const { result } = renderHook(
      () =>
        useCreateAgentSubmit({
          draft: {
            ...EMPTY_AGENT_DRAFT,
            name: CREATED_AGENT.name,
            runtimeId: CREATED_AGENT.runtime_id,
          },
          runtimeId: CREATED_AGENT.runtime_id,
          squadId: null,
        }),
      { wrapper: wrapper(queryClient) },
    );

    await act(async () => {
      await result.current.create();
    });

    expect(navigationSnapshot).toEqual({
      path: "/acme/agents/agent-new",
      list: [CREATED_AGENT],
      detail: CREATED_AGENT,
    });
    expect(queryClient.invalidateQueries).toHaveBeenCalledWith({
      queryKey: workspaceKeys.agents("ws-1"),
    });
  });
});
