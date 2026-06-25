// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { ExecutionPanoramaPage } from "./execution-panorama-page";

// ---------------------------------------------------------------------------
// Hoisted mock state — lets each test control query behaviour
// ---------------------------------------------------------------------------
const mocks = vi.hoisted(() => ({
  workflowData: undefined as unknown,
  stagesData: undefined as unknown as unknown[],
  nodesData: undefined as unknown as unknown[],
  edgesData: undefined as unknown as unknown[],
  nodeRunsData: undefined as unknown as unknown[],
  agentsData: undefined as unknown as unknown[],
  pluginsData: undefined as unknown,
  isLoading: true,
  onNodeClick: vi.fn(),
}));

// ---------------------------------------------------------------------------
// Mock @tanstack/react-query — check query keys to route data
// ---------------------------------------------------------------------------
vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual("@tanstack/react-query");
  return {
    ...(actual as object),
    useQuery: (opts: { queryKey?: unknown[]; enabled?: boolean }) => {
      const key = opts.queryKey ?? [];
      const enabled = opts.enabled !== false;
      if (!enabled) return { data: undefined, isLoading: false };

      if (Array.isArray(key)) {
        if (key.includes("stages"))
          return { data: mocks.stagesData, isLoading: mocks.isLoading };
        if (key.includes("nodes"))
          return { data: mocks.nodesData, isLoading: mocks.isLoading };
        if (key.includes("edges"))
          return { data: mocks.edgesData, isLoading: mocks.isLoading };
        if (key.includes("node-runs"))
          return { data: mocks.nodeRunsData, isLoading: false };
        if (key.includes("agents"))
          return { data: mocks.agentsData, isLoading: false };
        if (key.includes("plugins"))
          return { data: mocks.pluginsData, isLoading: false };
        return { data: mocks.workflowData, isLoading: mocks.isLoading };
      }
      return { data: undefined, isLoading: true };
    },
    useMutation: () => ({
      mutateAsync: vi.fn(),
      mutate: vi.fn(),
      isPending: false,
    }),
    useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  };
});

// ---------------------------------------------------------------------------
// Mock query-option modules (return keys so useQuery mock can route)
// ---------------------------------------------------------------------------
vi.mock("@multica/core/workflows/queries", () => ({
  workflowDetailOptions: (wsId: string, id: string) => ({
    queryKey: ["workflows", wsId, "detail", id],
  }),
  workflowStagesOptions: (wsId: string, workflowId: string) => ({
    queryKey: ["workflows", wsId, workflowId, "stages"],
  }),
  workflowNodesOptions: (wsId: string, workflowId: string) => ({
    queryKey: ["workflows", wsId, workflowId, "nodes"],
  }),
  workflowEdgesOptions: (wsId: string, workflowId: string) => ({
    queryKey: ["workflows", wsId, workflowId, "edges"],
  }),
  workflowNodeRunsOptions: (wsId: string, workflowId: string, runId: string) => ({
    queryKey: ["workflows", wsId, workflowId, runId, "node-runs"],
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({
    queryKey: ["workspaces", wsId, "agents"],
  }),
  builtinPluginListOptions: () => ({
    queryKey: ["plugins", "builtin"],
  }),
}));

// ---------------------------------------------------------------------------
// Mock child components
// ---------------------------------------------------------------------------
vi.mock("../../../workflows/components/overview/stage-lane", () => ({
  StageLane: ({
    stage,
    nodeIds,
  }: {
    stage: { id: string; name: string };
    nodeIds: unknown[];
  }) => <div data-testid={`stage-lane-${stage.id}`}>{stage.name}</div>,
}));

vi.mock("../../../workflows/components/overview/panorama-svg-overlay", () => ({
  PanoramaSvgOverlay: () => <svg data-testid="panorama-svg-overlay" />,
}));

vi.mock("./execution-detail-panel", () => ({
  ExecutionDetailPanel: ({ onClose }: { onClose: () => void }) => (
    <div data-testid="execution-detail-panel">
      <button onClick={onClose}>Close</button>
    </div>
  ),
}));

// ---------------------------------------------------------------------------
// Test wrapper
// ---------------------------------------------------------------------------
function Wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
const STAGE = {
  id: "stage-1",
  workflow_id: "wf-1",
  name: "Intake",
  description: "",
  sort_order: 0,
  node_count: 0,
  created_at: "",
  updated_at: "",
};

const NODE = {
  id: "n1",
  workflow_id: "wf-1",
  title: "brainstorming",
  description: "",
  position_x: 0,
  position_y: 0,
  format_schema: null,
  worker_type: "agent" as const,
  worker_id: "agent-1",
  critic_type: "human" as const,
  critic_id: null,
  critic_api_url: null,
  sort_order: 0,
  stage_id: "stage-1",
  created_at: "",
  updated_at: "",
};

const AGENT = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "rt-1",
  name: "Brainstorming Agent",
  description: "Brainstorms",
  instructions: "",
  avatar_url: null,
  runtime_mode: "cloud" as const,
  runtime_config: {},
  custom_env: {},
  custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace" as const,
  status: "idle" as const,
  max_concurrent_tasks: 1,
  model: "claude-sonnet-4-6",
  thinking_level: "medium",
  plugin_id: null,
  is_builtin: false,
  owner_id: null,
  skills: [],
  created_at: "",
  updated_at: "",
  archived_at: null,
  archived_by: null,
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------
describe("ExecutionPanoramaPage", () => {
  beforeEach(() => {
    mocks.isLoading = true;
    mocks.workflowData = undefined;
    mocks.stagesData = [];
    mocks.nodesData = [];
    mocks.edgesData = [];
    mocks.nodeRunsData = [];
    mocks.agentsData = [];
    mocks.pluginsData = {
      items: [],
      total: 0,
      page: 1,
      pageSize: 100,
      hasMore: false,
    };
  });

  it("renders loading state when data is loading", () => {
    mocks.isLoading = true;

    render(
      <Wrapper>
        <ExecutionPanoramaPage workflowId="wf-1" runId={null} wsId="ws-1" />
      </Wrapper>,
    );

    expect(screen.getByRole("status")).toBeInTheDocument();
  });

  it("renders unassigned lane when no stages defined", () => {
    mocks.isLoading = false;
    mocks.workflowData = { id: "wf-1", title: "Test Workflow" };
    mocks.stagesData = [];
    mocks.nodesData = [NODE];
    mocks.agentsData = [AGENT];
    mocks.pluginsData = {
      items: [],
      total: 0,
      page: 1,
      pageSize: 100,
      hasMore: false,
    };

    render(
      <Wrapper>
        <ExecutionPanoramaPage workflowId="wf-1" runId={null} wsId="ws-1" />
      </Wrapper>,
    );

    expect(screen.getByTestId("execution-panorama")).toBeInTheDocument();
    expect(screen.getByTestId("panorama-canvas")).toBeInTheDocument();
  });

  it("renders stage lanes when stages exist", () => {
    mocks.isLoading = false;
    mocks.workflowData = { id: "wf-1", title: "Test Workflow" };
    mocks.stagesData = [STAGE];
    mocks.nodesData = [NODE];
    mocks.agentsData = [AGENT];
    mocks.pluginsData = {
      items: [],
      total: 0,
      page: 1,
      pageSize: 100,
      hasMore: false,
    };

    render(
      <Wrapper>
        <ExecutionPanoramaPage workflowId="wf-1" runId={null} wsId="ws-1" />
      </Wrapper>,
    );

    expect(screen.getByTestId("stage-lane-stage-1")).toBeInTheDocument();
  });

  it("does not render detail panel initially", () => {
    mocks.isLoading = false;
    mocks.workflowData = { id: "wf-1", title: "Test Workflow" };
    mocks.stagesData = [STAGE];
    mocks.nodesData = [NODE];
    mocks.agentsData = [AGENT];
    mocks.pluginsData = {
      items: [],
      total: 0,
      page: 1,
      pageSize: 100,
      hasMore: false,
    };

    // Simulate state change by clicking — we pass the onNodeClick
    // via StageLane mock already, but since the component uses
    // internal state, we can't easily trigger it from the test.
    // This test verifies the component renders without detail panel initially.
    render(
      <Wrapper>
        <ExecutionPanoramaPage workflowId="wf-1" runId={null} wsId="ws-1" />
      </Wrapper>,
    );

    expect(screen.queryByTestId("execution-detail-panel")).not.toBeInTheDocument();
  });

  it("renders SVG overlay when runId is provided", () => {
    mocks.isLoading = false;
    mocks.workflowData = { id: "wf-1", title: "Test Workflow" };
    mocks.stagesData = [STAGE];
    mocks.nodesData = [NODE];
    mocks.edgesData = [
      {
        id: "e1",
        workflow_id: "wf-1",
        source_node_id: "n1",
        target_node_id: "n1",
        condition: null,
        created_at: "",
      },
    ];
    mocks.nodeRunsData = [
      {
        id: "run-1",
        workflow_run_id: "run-1",
        workflow_node_id: "n1",
        node_title: "brainstorming",
        status: "completed",
        retry_count: 0,
        worker_type: "agent",
        worker_id: "agent-1",
        worker_output: null,
        worker_agent_task_id: null,
        critic_type: "human",
        critic_id: null,
        critic_output: null,
        critic_comment: "",
        critic_agent_task_id: null,
        agent_task_id: null,
        session_id: null,
        runtime_id: null,
        device_id: null,
        started_at: null,
        completed_at: null,
        created_at: "",
        updated_at: "",
      },
    ];
    mocks.agentsData = [AGENT];
    mocks.pluginsData = {
      items: [],
      total: 0,
      page: 1,
      pageSize: 100,
      hasMore: false,
    };

    render(
      <Wrapper>
        <ExecutionPanoramaPage workflowId="wf-1" runId="run-1" wsId="ws-1" />
      </Wrapper>,
    );

    expect(screen.getByTestId("panorama-svg-overlay")).toBeInTheDocument();
  });
});
