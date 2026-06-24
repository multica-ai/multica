// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, cleanup, screen, within } from "@testing-library/react";
import { renderWithI18n } from "../../../test/i18n";

const MOCK_WORKFLOW = { id: "wf-1", title: "Test Workflow" };

const MOCK_STAGES = [
  { id: "stage-1", workflow_id: "wf-1", name: "Intake", description: "", sort_order: 0, node_count: 2, created_at: "", updated_at: "" },
  { id: "stage-2", workflow_id: "wf-1", name: "Analysis", description: "", sort_order: 1, node_count: 1, created_at: "", updated_at: "" },
];

const MOCK_NODES = [
  { id: "n1", workflow_id: "wf-1", title: "brainstorming", description: "", position_x: 0, position_y: 0, format_schema: null, worker_type: "agent" as const, worker_id: "agent-1", critic_type: "agent" as const, critic_id: "agent-2", critic_api_url: null, sort_order: 0, stage_id: "stage-1", created_at: "", updated_at: "" },
  { id: "n2", workflow_id: "wf-1", title: "session-context", description: "", position_x: 0, position_y: 0, format_schema: null, worker_type: "agent" as const, worker_id: "agent-2", critic_type: "human", critic_id: null, critic_api_url: null, sort_order: 1, stage_id: "stage-1", created_at: "", updated_at: "" },
  { id: "n3", workflow_id: "wf-1", title: "requirement-analysis", description: "", position_x: 0, position_y: 0, format_schema: null, worker_type: "agent" as const, worker_id: "agent-3", critic_type: "human", critic_id: null, critic_api_url: null, sort_order: 0, stage_id: "stage-2", created_at: "", updated_at: "" },
];

const MOCK_EDGES = [
  { id: "e1", workflow_id: "wf-1", source_node_id: "n1", target_node_id: "n2", condition: null, created_at: "" },
  { id: "e2", workflow_id: "wf-1", source_node_id: "n2", target_node_id: "n3", condition: null, created_at: "" },
];

const MOCK_AGENTS = [
  { id: "agent-1", workspace_id: "ws-1", runtime_id: "rt-1", name: "Brainstorming Agent", description: "Brainstorms", instructions: "", avatar_url: null, runtime_mode: "cloud" as const, runtime_config: {}, custom_env: {}, custom_args: [], custom_env_redacted: false, visibility: "workspace" as const, status: "idle" as const, max_concurrent_tasks: 1, model: "claude-sonnet-4-6", thinking_level: "medium", plugin_id: "plugin-1", is_builtin: false, owner_id: null, skills: [], created_at: "", updated_at: "", archived_at: null, archived_by: null },
  { id: "agent-2", workspace_id: "ws-1", runtime_id: "rt-1", name: "Session Agent", description: "Session context", instructions: "", avatar_url: null, runtime_mode: "cloud" as const, runtime_config: {}, custom_env: {}, custom_args: [], custom_env_redacted: false, visibility: "workspace" as const, status: "working" as const, max_concurrent_tasks: 1, model: "claude-opus-4-8", thinking_level: "", plugin_id: "plugin-2", is_builtin: false, owner_id: null, skills: [], created_at: "", updated_at: "", archived_at: null, archived_by: null },
  { id: "agent-3", workspace_id: "ws-1", runtime_id: "rt-1", name: "Analysis Agent", description: "Requirements analysis", instructions: "", avatar_url: null, runtime_mode: "local" as const, runtime_config: {}, custom_env: {}, custom_args: [], custom_env_redacted: false, visibility: "workspace" as const, status: "idle" as const, max_concurrent_tasks: 2, model: "claude-haiku-4-5-20251001", thinking_level: "", plugin_id: null, is_builtin: false, owner_id: null, skills: [], created_at: "", updated_at: "", archived_at: null, archived_by: null },
];

const MOCK_PLUGINS = {
  items: [
    { id: "plugin-1", name: "Cospowers Brainstorming", description: "Brainstorming plugin", slug: "cospowers-brainstorming", version: "1.0.0", category: "engineering" },
    { id: "plugin-2", name: "Cospowers Session", description: "Session context plugin", slug: "cospowers-session", version: "1.0.0", category: "engineering" },
  ],
  total: 2, page: 1, pageSize: 100, hasMore: false,
};

const mocks = vi.hoisted(() => ({
  workflowData: undefined as unknown,
  stagesData: undefined as unknown as unknown[],
  nodesData: undefined as unknown as unknown[],
  edgesData: undefined as unknown as unknown[],
  agentsData: undefined as unknown as unknown[],
  pluginsData: undefined as unknown,
  isLoading: false,
  isError: false,
  navigationPush: vi.fn(),
  setViewMode: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey?: unknown[] }) => {
    const key = opts.queryKey ?? [];
    if (Array.isArray(key) && key.includes("stages")) return { data: mocks.stagesData, isLoading: mocks.isLoading, isError: false };
    if (Array.isArray(key) && key.includes("nodes")) return { data: mocks.nodesData, isLoading: false };
    if (Array.isArray(key) && key.includes("edges")) return { data: mocks.edgesData, isLoading: false };
    if (Array.isArray(key) && key.includes("agents") && !key.includes("plugins")) return { data: mocks.agentsData, isLoading: false };
    if (Array.isArray(key) && key.includes("plugins")) return { data: mocks.pluginsData, isLoading: false };
    return { data: mocks.workflowData, isLoading: mocks.isLoading, isError: mocks.isError, refetch: vi.fn() };
  },
  useMutation: () => ({ mutateAsync: vi.fn(), mutate: vi.fn(), isPending: false }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/workflows/queries", () => ({
  workflowOverviewOptions: (wsId: string, id: string) => ({ queryKey: ["workflows", wsId, "detail", id] }),
  workflowStagesOptions: (wsId: string, workflowId: string) => ({ queryKey: ["workflows", wsId, workflowId, "stages"] }),
  workflowNodesOptions: (wsId: string, workflowId: string) => ({ queryKey: ["workflows", wsId, workflowId, "nodes"] }),
  workflowEdgesOptions: (wsId: string, workflowId: string) => ({ queryKey: ["workflows", wsId, workflowId, "edges"] }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({ queryKey: ["workspaces", wsId, "agents"] }),
  builtinPluginListOptions: () => ({ queryKey: ["plugins", "builtin"] }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ workflowDetail: (id: string) => `/ws-1/workflows/${id}`, workflows: () => "/ws-1/workflows" }),
}));

vi.mock("../../../navigation", () => ({
  useNavigation: () => ({ push: mocks.navigationPush, replace: mocks.navigationPush }),
}));

vi.mock("@multica/core/workflows/stores/view-store", () => ({
  useWorkflowViewStore: (selector: (s: unknown) => unknown) =>
    selector({ viewMode: "panorama", setViewMode: mocks.setViewMode }),
}));

// Mock ResizeObserver for tests
class ResizeObserverMock {
  observe() {}
  disconnect() {}
  unobserve() {}
}
globalThis.ResizeObserver = ResizeObserverMock as typeof ResizeObserver;

// Provide minimal bounding rects for all nodes so SVG overlay can compute edges
const originalGetBoundingClientRect = HTMLElement.prototype.getBoundingClientRect;
HTMLElement.prototype.getBoundingClientRect = function mockRect() {
  const testId = (this as HTMLElement).getAttribute?.("data-testid") ?? "";
  if (testId.includes("compact-node-card-n1")) {
    return { x: 12, y: 62, left: 12, top: 62, right: 132, bottom: 134, width: 120, height: 72, toJSON() { return this; } };
  }
  if (testId.includes("compact-node-card-n2")) {
    return { x: 142, y: 62, left: 142, top: 62, right: 262, bottom: 134, width: 120, height: 72, toJSON() { return this; } };
  }
  if (testId.includes("compact-node-card-n3")) {
    return { x: 12, y: 218, left: 12, top: 218, right: 132, bottom: 290, width: 120, height: 72, toJSON() { return this; } };
  }
  if (testId.includes("critic-badge-n1")) {
    return { x: 24, y: 142, left: 24, top: 142, right: 144, bottom: 206, width: 120, height: 64, toJSON() { return this; } };
  }
  return originalGetBoundingClientRect.call(this);
};

import { WorkflowPanoramaPage } from "./workflow-panorama-page";

describe("WorkflowPanoramaPage", () => {
  beforeEach(() => {
    mocks.workflowData = MOCK_WORKFLOW;
    mocks.stagesData = MOCK_STAGES;
    mocks.nodesData = MOCK_NODES;
    mocks.edgesData = MOCK_EDGES;
    mocks.agentsData = MOCK_AGENTS;
    mocks.pluginsData = MOCK_PLUGINS;
    mocks.isLoading = false;
    mocks.isError = false;
    mocks.navigationPush = vi.fn();
    mocks.setViewMode = vi.fn();
    cleanup();
  });

  it("renders workflow title in header", () => {
    const { container } = renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(container.querySelector("h1")?.textContent).toBe("Test Workflow");
  });

  it("renders stage lanes for each stage", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(screen.getByTestId("stage-lane-stage-1")).toBeTruthy();
    expect(screen.getByTestId("stage-lane-stage-2")).toBeTruthy();
  });

  it("renders compact node cards with resolved names", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(screen.getByTestId("compact-node-card-n1")).toBeTruthy();
    expect(screen.getByTestId("compact-node-card-n2")).toBeTruthy();
    expect(screen.getByTestId("compact-node-card-n3")).toBeTruthy();
  });

  it("no longer renders DataFlowArrow between stages", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(screen.queryByTestId("data-flow-arrow")).toBeNull();
  });

  it("renders stage transition gradient between stages", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(screen.queryAllByTestId("stage-transition-gradient").length).toBeGreaterThan(0);
  });

  it("opens detail panel on node card click", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    fireEvent.click(screen.getByTestId("compact-node-card-n1"));
    expect(screen.getByTestId("architecture-detail-panel")).toBeTruthy();
  });

  it("opens critic detail panel on critic badge click", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    fireEvent.click(screen.getByTestId("critic-badge-n1"));
    const panel = screen.getByTestId("architecture-detail-panel");
    expect(panel).toBeTruthy();
    expect(within(panel).getAllByText("Critic").length).toBeGreaterThan(0);
  });

  it("shows loading skeleton when loading", () => {
    mocks.isLoading = true;
    const { container } = renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(container.querySelector('[data-testid="panorama-skeleton"]')).toBeTruthy();
  });

  it("shows error alert when workflow fails", () => {
    mocks.workflowData = undefined;
    mocks.isError = true;
    const { container } = renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(container.querySelector('[role="alert"]')).toBeTruthy();
  });
});
