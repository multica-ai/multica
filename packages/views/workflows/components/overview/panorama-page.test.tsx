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
    // Stages: data only available when NOT loading (mimics real API behavior)
    if (Array.isArray(key) && key.includes("stages")) return { data: mocks.isLoading ? [] : mocks.stagesData, isLoading: mocks.isLoading, isError: false };
    if (Array.isArray(key) && key.includes("nodes")) return { data: mocks.isLoading ? [] : mocks.nodesData, isLoading: mocks.isLoading };
    if (Array.isArray(key) && key.includes("edges")) return { data: mocks.edgesData, isLoading: false };
    if (Array.isArray(key) && key.includes("agents") && !key.includes("plugins")) return { data: mocks.agentsData, isLoading: false };
    if (Array.isArray(key) && key.includes("plugins")) return { data: mocks.pluginsData, isLoading: false };
    if (Array.isArray(key) && key.includes("members")) return { data: [], isLoading: false };
    if (Array.isArray(key) && key.includes("squads")) return { data: [], isLoading: false };
    if (Array.isArray(key) && key.includes("list")) return { data: { workflows: [] }, isLoading: false };
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
  workflowListOptions: (wsId: string) => ({ queryKey: ["workflows", wsId, "list"] }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({ queryKey: ["workspaces", wsId, "agents"] }),
  memberListOptions: (wsId: string) => ({ queryKey: ["workspaces", wsId, "members"] }),
  squadListOptions: (wsId: string) => ({ queryKey: ["workspaces", wsId, "squads"] }),
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

  it("renders stage transition gradient between stages", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(screen.queryAllByTestId("stage-transition-gradient").length).toBeGreaterThan(0);
  });

  it("renders a left-aligned full-width panorama process rail on a distinct canvas background", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    const canvas = screen.getByTestId("workflow-panorama-canvas");
    const rail = screen.getByTestId("workflow-panorama-rail");
    expect(canvas.className).toContain("bg-slate-100/70");
    expect(rail.className).toContain("ml-0");
    expect(rail.className).not.toContain("mx-auto");
    expect(rail.className).not.toContain("border ");
    expect(rail.className).not.toContain("border-slate");
    expect(rail.className).toContain("w-full");
    expect(rail.className).toContain("min-w-[1320px]");
  });

  it("keeps the SVG connector layer above stage backgrounds", () => {
    const { container } = renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    const svg = container.querySelector("svg.absolute");
    expect(svg?.getAttribute("class")).toContain("z-10");
    expect(screen.getByTestId("stage-lane-stage-1").className).toContain("z-0");
    expect(screen.getByTestId("stage-lane-shell-stage-1").className).toContain("z-20");
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

  describe("incremental data loading (first-entry simulation)", () => {
    it("renders SVG overlay when edges arrive after nodes/stages", () => {
      // Simulate first entry: nodes/stages loaded, but edges NOT yet loaded
      mocks.edgesData = [];

      const { container, rerender } = renderWithI18n(
        <WorkflowPanoramaPage workflowId="wf-1" />,
      );

      // After initial render: cards visible, SVG should be absent (no edges yet)
      expect(screen.getByTestId("compact-node-card-n1")).toBeTruthy();
      const svgBefore = container.querySelector("svg.absolute");
      console.log("[DIAG] after initial render (edges=[]): svg present =", !!svgBefore);
      console.log("[DIAG] container query for svg:", container.querySelectorAll("svg").length, "svg elements total");

      // Simulate edges arriving asynchronously
      mocks.edgesData = MOCK_EDGES;
      rerender(<WorkflowPanoramaPage workflowId="wf-1" />);

      // After edges arrive: SVG overlay should render with connection lines
      const svgAfter = container.querySelector("svg.absolute");
      console.log("[DIAG] after edges arrive: svg present =", !!svgAfter);
      if (svgAfter) {
        const paths = svgAfter.querySelectorAll("path");
        console.log("[DIAG] svg path count:", paths.length);
        for (let i = 0; i < paths.length; i++) {
          const p = paths[i]!;
          console.log(`[DIAG] path[${i}]: d="${p.getAttribute("d")}", marker-end="${p.getAttribute("marker-end")}"`);
        }
      }

      // Also check for any path elements in the container
      const allPaths = container.querySelectorAll("path");
      console.log("[DIAG] total path elements in container:", allPaths.length);

      expect(svgAfter).toBeTruthy();
      expect(svgAfter!.querySelectorAll("path").length).toBeGreaterThan(0);
    });

    it("renders SVG overlay when edges and positions arrive in separate renders", () => {
      // Simulate: edges arrive BEFORE positions are measured
      // (edges=[] on initial render, edges arrive on re-render)
      mocks.edgesData = [];

      const { container, rerender } = renderWithI18n(
        <WorkflowPanoramaPage workflowId="wf-1" />,
      );

      // Verify initial state: no SVG
      const svgBefore = container.querySelector("svg.absolute");
      console.log("[DIAG-separate] before edges: svg =", !!svgBefore);

      // Now edges arrive
      mocks.edgesData = MOCK_EDGES;
      rerender(<WorkflowPanoramaPage workflowId="wf-1" />);

      const svgAfter = container.querySelector("svg.absolute");
      console.log("[DIAG-separate] after edges: svg =", !!svgAfter);
      if (svgAfter) {
        console.log("[DIAG-separate] svg path count:", svgAfter.querySelectorAll("path").length);
      }

      expect(svgAfter).toBeTruthy();
    });

    it("handles skeleton-to-panorama transition with delayed edges", () => {
      // Simulate FULL first-entry flow:
      // Step 1: loading skeleton
      mocks.isLoading = true;
      mocks.edgesData = [];

      const { container, rerender } = renderWithI18n(
        <WorkflowPanoramaPage workflowId="wf-1" />,
      );

      // Verify skeleton
      const skeleton = container.querySelector('[data-testid="panorama-skeleton"]');
      console.log("[DIAG-transition] step1 skeleton present:", !!skeleton);

      // Step 2: nodes/stages arrive, but edges still loading
      mocks.isLoading = false;
      // edgesData stays []
      rerender(<WorkflowPanoramaPage workflowId="wf-1" />);

      // Check state after skeleton→panorama transition
      expect(screen.getByTestId("compact-node-card-n1")).toBeTruthy();
      let svgAfterLoad = container.querySelector("svg.absolute");
      console.log("[DIAG-transition] step2a (after skeleton→panorama): svg present =", !!svgAfterLoad);

      // Step 2b: ANOTHER rerender to flush any pending useLayoutEffect state updates
      rerender(<WorkflowPanoramaPage workflowId="wf-1" />);
      svgAfterLoad = container.querySelector("svg.absolute");
      console.log("[DIAG-transition] step2b (after extra rerender): svg present =", !!svgAfterLoad);
      if (svgAfterLoad) {
        const paths = svgAfterLoad.querySelectorAll("path");
        console.log("[DIAG-transition] step2b path count:", paths.length);
        paths.forEach((p, i) => {
          console.log(`[DIAG-transition] step2b path[${i}]: marker="${p.getAttribute("marker-end")}", d="${p.getAttribute("d")?.substring(0, 60)}"`);
        });
      }

      // Step 3: edges finally arrive
      mocks.edgesData = MOCK_EDGES;
      rerender(<WorkflowPanoramaPage workflowId="wf-1" />);

      const svgAfterEdges = container.querySelector("svg.absolute");
      console.log("[DIAG-transition] step3 (edges arrived): svg present =", !!svgAfterEdges);
      if (svgAfterEdges) {
        const paths = svgAfterEdges.querySelectorAll("path");
        console.log("[DIAG-transition] step3 path count:", paths.length);
        const edgePaths = Array.from(paths).filter(p => p.getAttribute("marker-end"));
        console.log("[DIAG-transition] step3 edge paths:", edgePaths.length);
      }

      // Check PanoramaSvgOverlay's SVG
      const panoramaSvg = container.querySelector("svg.absolute.z-10");
      console.log("[DIAG-transition] final panorama svg:", !!panoramaSvg);

      expect(panoramaSvg).toBeTruthy();
      const edgePaths = panoramaSvg!.querySelectorAll('path[marker-end]');
      console.log("[DIAG-transition] final edge paths count:", edgePaths.length);
      expect(edgePaths.length).toBeGreaterThan(0);
    });
  });
});
