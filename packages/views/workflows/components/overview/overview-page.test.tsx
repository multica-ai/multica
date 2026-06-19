// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, cleanup } from "@testing-library/react";
import { renderWithI18n } from "../../../test/i18n";

// ── Mock data ─────────────────────────────────────────────────

const MOCK_WORKFLOW = {
  id: "wf-1",
  title: "Test Workflow",
};

const MOCK_STAGES = [
  { id: "stage-1", workflowId: "wf-1", name: "需求", description: "", sortOrder: 0, nodeCount: 2, createdAt: "", updatedAt: "" },
  { id: "stage-2", workflowId: "wf-1", name: "设计", description: "", sortOrder: 1, nodeCount: 3, createdAt: "", updatedAt: "" },
  { id: "stage-3", workflowId: "wf-1", name: "编码", description: "", sortOrder: 2, nodeCount: 0, createdAt: "", updatedAt: "" },
];

const MOCK_NODES = [
  { id: "n1", workflowId: "wf-1", stageId: "stage-1", title: "需求收集", description: "", sortOrder: 0 },
  { id: "n2", workflowId: "wf-1", stageId: "stage-1", title: "需求分析", description: "", sortOrder: 1 },
  { id: "n3", workflowId: "wf-1", stageId: "stage-2", title: "UI设计", description: "", sortOrder: 0 },
  { id: "n4", workflowId: "wf-1", stageId: "stage-2", title: "架构设计", description: "", sortOrder: 1 },
  { id: "n5", workflowId: "wf-1", stageId: "stage-2", title: "数据库设计", description: "", sortOrder: 2 },
];

const MOCK_EDGES = [
  { id: "e1", workflowId: "wf-1", sourceNodeId: "n1", targetNodeId: "n2" },
  { id: "e2", workflowId: "wf-1", sourceNodeId: "n2", targetNodeId: "n3" },
];

// ── Hoisted mocks ──────────────────────────────────────────

const mocks = vi.hoisted(() => ({
  workflowData: undefined as unknown,
  stagesData: undefined as unknown,
  nodesData: undefined as unknown as unknown[],
  edgesData: undefined as unknown as unknown[],
  isLoading: false,
  isError: false,
  navigationPush: vi.fn(),
  onStageSelect: vi.fn(),
  onNodeSelect: vi.fn(),
  onClose: vi.fn(),
}));

// ── Mock @tanstack/react-query ─────────────────────────────

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey?: unknown[] }) => {
    const key = opts.queryKey ?? [];
    if (Array.isArray(key) && key.includes("stages")) {
      return { data: mocks.stagesData, isLoading: mocks.isLoading, isError: mocks.isError };
    }
    if (Array.isArray(key) && key.includes("nodes")) {
      return { data: mocks.nodesData, isLoading: false };
    }
    if (Array.isArray(key) && key.includes("edges")) {
      return { data: mocks.edgesData, isLoading: false };
    }
    // Default: workflow detail
    return { data: mocks.workflowData, isLoading: mocks.isLoading, isError: mocks.isError };
  },
  useMutation: () => ({ mutateAsync: vi.fn(), mutate: vi.fn(), isPending: false }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));

// ── Mock external packages ─────────────────────────────────

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/workflows/queries", () => ({
  workflowOverviewOptions: (wsId: string, id: string) => ({ queryKey: ["workflows", wsId, "detail", id] }),
  workflowStagesOptions: (wsId: string, workflowId: string) => ({ queryKey: ["workflows", wsId, workflowId, "stages"] }),
  workflowNodesOptions: (wsId: string, workflowId: string) => ({ queryKey: ["workflows", wsId, workflowId, "nodes"] }),
  workflowEdgesOptions: (wsId: string, workflowId: string) => ({ queryKey: ["workflows", wsId, workflowId, "edges"] }),
  useCreateStage: () => ({ mutateAsync: vi.fn(), mutate: vi.fn(), isPending: false }),
  useUpdateStage: () => ({ mutateAsync: vi.fn(), mutate: vi.fn(), isPending: false }),
  useDeleteStage: () => ({ mutateAsync: vi.fn(), mutate: vi.fn(), isPending: false }),
  useReorderStages: () => ({ mutateAsync: vi.fn(), mutate: vi.fn(), isPending: false }),
  useAssignNodeToStage: () => ({ mutateAsync: vi.fn(), mutate: vi.fn(), isPending: false }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    workflowDetail: (id: string) => `/ws-1/workflows/${id}`,
    workflows: () => "/ws-1/workflows",
  }),
}));

vi.mock("../../../navigation", () => ({
  useNavigation: () => ({ push: mocks.navigationPush, replace: mocks.navigationPush }),
}));

// ── Mock sub-components (don't exist yet; created in Tasks 7-10) ──

vi.mock("./stage-canvas", () => ({
  StageCanvas: ({ stages, selectedStageId, onStageSelect }: {
    stages: { id: string; name: string; nodeCount: number }[];
    selectedStageId: string | null;
    onStageSelect: (id: string) => void;
  }) => (
    <div data-testid="stage-canvas">
      {stages.map((s) => (
        <button
          key={s.id}
          data-testid={`stage-card-${s.id}`}
          data-selected={selectedStageId === s.id ? "true" : "false"}
          onClick={() => onStageSelect(s.id)}
        >
          {s.name} ({s.nodeCount})
        </button>
      ))}
    </div>
  ),
}));

vi.mock("./stage-node-dag", () => ({
  StageNodeDag: ({ stageId, nodes, onNodeSelect }: {
    stageId: string | null;
    nodes: { id: string; stageId?: string }[];
    onNodeSelect: (id: string) => void;
  }) => {
    if (!stageId) return <div data-testid="stage-node-dag-empty">Select a stage</div>;
    const stageNodes = nodes.filter((n) => n.stageId === stageId);
    if (stageNodes.length === 0) return <div data-testid="empty-nodes-state">No nodes in this stage</div>;
    return (
      <div data-testid="stage-node-dag">
        {stageNodes.map((n) => (
          <button key={n.id} data-testid={`dag-node-${n.id}`} onClick={() => onNodeSelect(n.id)}>
            Node {n.id}
          </button>
        ))}
      </div>
    );
  },
}));

vi.mock("./node-detail-panel", () => ({
  NodeDetailPanel: ({ nodeId, onClose }: {
    nodeId: string;
    onClose: () => void;
  }) => (
    <div data-testid="node-detail-panel">
      <span data-testid="node-detail-id">{nodeId}</span>
      <button data-testid="node-detail-close" onClick={onClose}>Close</button>
    </div>
  ),
}));

vi.mock("./stage-create-dialog", () => ({
  StageCreateDialog: ({ open, onClose }: { open: boolean; onClose: () => void }) =>
    open ? <div data-testid="stage-create-dialog"><button onClick={onClose}>Cancel</button></div> : null,
}));

// ── Import after mocks ─────────────────────────────────────

import { WorkflowOverviewPage } from "./workflow-overview-page";

describe("WorkflowOverviewPage", () => {
  beforeEach(() => {
    // Reset all mocks to default data
    mocks.workflowData = MOCK_WORKFLOW;
    mocks.stagesData = MOCK_STAGES;
    mocks.nodesData = MOCK_NODES;
    mocks.edgesData = MOCK_EDGES;
    mocks.isLoading = false;
    mocks.isError = false;
    mocks.navigationPush = vi.fn();
    cleanup();
  });

  describe("Loading state", () => {
    it("shows skeleton while loading", () => {
      mocks.isLoading = true;
      const { container } = renderWithI18n(<WorkflowOverviewPage workflowId="wf-1" />);
      const skeleton = container.querySelector('[data-testid="stage-canvas-skeleton"]');
      expect(skeleton).toBeTruthy();
    });
  });

  describe("Error state", () => {
    it("shows error alert when workflow fetch fails", () => {
      mocks.workflowData = undefined;
      mocks.isError = true;
      const { container } = renderWithI18n(<WorkflowOverviewPage workflowId="wf-1" />);
      expect(container.querySelector('[role="alert"]')).toBeTruthy();
    });
  });

  describe("Empty state", () => {
    it("shows empty state when workflow has no stages", () => {
      mocks.stagesData = [];
      const { container } = renderWithI18n(<WorkflowOverviewPage workflowId="wf-1" />);
      // Should show empty stage state — StageCanvas should handle rendering empty state
      expect(container.querySelector('[data-testid="stage-canvas"]')).toBeTruthy();
    });
  });

  describe("Stage canvas rendering", () => {
    it("renders stage cards with correct data", () => {
      const { getByTestId } = renderWithI18n(<WorkflowOverviewPage workflowId="wf-1" />);
      expect(getByTestId("stage-canvas")).toBeTruthy();
      expect(getByTestId("stage-card-stage-1")).toBeTruthy();
      expect(getByTestId("stage-card-stage-2")).toBeTruthy();
      expect(getByTestId("stage-card-stage-3")).toBeTruthy();
    });
  });

  describe("Stage selection", () => {
    it("selects a stage and shows its DAG", () => {
      const { getByTestId, queryByTestId } = renderWithI18n(<WorkflowOverviewPage workflowId="wf-1" />);

      // Initially no stage selected — DAG not rendered
      expect(queryByTestId("stage-node-dag")).toBeNull();

      // Click stage 2
      fireEvent.click(getByTestId("stage-card-stage-2"));

      // DAG should now show stage 2's nodes (n3, n4, n5)
      expect(getByTestId("stage-node-dag")).toBeTruthy();
      expect(getByTestId("dag-node-n3")).toBeTruthy();
      expect(getByTestId("dag-node-n4")).toBeTruthy();
      expect(getByTestId("dag-node-n5")).toBeTruthy();
    });

    it("switches DAG content when another stage is selected", () => {
      const { getByTestId, queryByTestId } = renderWithI18n(<WorkflowOverviewPage workflowId="wf-1" />);

      // Select stage 2, then stage 1
      fireEvent.click(getByTestId("stage-card-stage-2"));
      expect(getByTestId("dag-node-n3")).toBeTruthy();

      fireEvent.click(getByTestId("stage-card-stage-1"));
      expect(getByTestId("dag-node-n1")).toBeTruthy();
      expect(getByTestId("dag-node-n2")).toBeTruthy();
      // Stage 2 nodes should not be in DAG
      expect(queryByTestId("dag-node-n3")).toBeNull();
    });

    it("shows empty nodes state for stage with zero nodes", () => {
      const { getByTestId } = renderWithI18n(<WorkflowOverviewPage workflowId="wf-1" />);

      // Stage 3 has 0 nodes
      fireEvent.click(getByTestId("stage-card-stage-3"));
      expect(getByTestId("empty-nodes-state")).toBeTruthy();
    });
  });

  describe("Node selection", () => {
    it("opens detail panel when a node is clicked", () => {
      const { getByTestId } = renderWithI18n(<WorkflowOverviewPage workflowId="wf-1" />);

      // Select stage 2 first
      fireEvent.click(getByTestId("stage-card-stage-2"));
      // Click node n3
      fireEvent.click(getByTestId("dag-node-n3"));

      // Detail panel should open showing node n3
      expect(getByTestId("node-detail-panel")).toBeTruthy();
      expect(getByTestId("node-detail-id").textContent).toBe("n3");
    });

    it("closes detail panel when close button is clicked", () => {
      const { getByTestId, queryByTestId } = renderWithI18n(<WorkflowOverviewPage workflowId="wf-1" />);

      // Open detail panel
      fireEvent.click(getByTestId("stage-card-stage-2"));
      fireEvent.click(getByTestId("dag-node-n3"));
      expect(getByTestId("node-detail-panel")).toBeTruthy();

      // Close it
      fireEvent.click(getByTestId("node-detail-close"));
      expect(queryByTestId("node-detail-panel")).toBeNull();
    });

    it("switches detail panel content when clicking another node", () => {
      const { getByTestId } = renderWithI18n(<WorkflowOverviewPage workflowId="wf-1" />);

      // Select stage 2
      fireEvent.click(getByTestId("stage-card-stage-2"));
      // Click node n3
      fireEvent.click(getByTestId("dag-node-n3"));
      expect(getByTestId("node-detail-id").textContent).toBe("n3");

      // Click node n4 — panel should update to n4
      fireEvent.click(getByTestId("dag-node-n4"));
      expect(getByTestId("node-detail-id").textContent).toBe("n4");
    });

    it("closes detail panel when selecting a different stage", () => {
      const { getByTestId, queryByTestId } = renderWithI18n(<WorkflowOverviewPage workflowId="wf-1" />);

      // Open detail panel for node in stage 2
      fireEvent.click(getByTestId("stage-card-stage-2"));
      fireEvent.click(getByTestId("dag-node-n3"));
      expect(getByTestId("node-detail-panel")).toBeTruthy();

      // Switch to stage 1 — panel should close
      fireEvent.click(getByTestId("stage-card-stage-1"));
      expect(queryByTestId("node-detail-panel")).toBeNull();
    });
  });
});
