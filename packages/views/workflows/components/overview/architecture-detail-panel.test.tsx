// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@testing-library/react";
import { ArchitectureDetailPanel } from "./architecture-detail-panel";
import type { ArchitectureDetailPanelData } from "./architecture-detail-panel";
import type { WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";

// ── Hoisted mocks ──
vi.mock("../../../i18n", () => ({
  useT: () => ({
    t: (key: unknown) => {
      if (typeof key === "function") {
        const resources = {
          overview: {
            detail_panel: {
              title: "Node Details",
              plugin_info: "Plugin Info",
              agent_info: "Associated Agent",
              critic: "Critic",
              skills: "Skills",
              open_in_editor: "Open in Editor",
            },
          },
        };
        return key(resources);
      }
      return String(key);
    },
  }),
}));

const MOCK_NODE: WorkflowNode = {
  id: "node-1",
  workflow_id: "wf-1",
  title: "需求分析",
  description: "",
  position_x: 0,
  position_y: 0,
  format_schema: null,
  worker_type: "agent",
  worker_id: "agent-1",
  critic_type: "",
  critic_id: null,
  critic_api_url: null,
  sort_order: 0,
  stage_id: "stage-1",
  created_at: "",
  updated_at: "",
};

const MOCK_AGENT: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "rt-1",
  name: "需求分析 Agent",
  description: "负责需求分析",
  instructions: "请分析需求文档",
  avatar_url: null,
  runtime_mode: "cloud",
  runtime_config: {},
  custom_env: { NODE_ENV: "production" },
  custom_args: ["--verbose"],
  custom_env_redacted: false,
  visibility: "workspace",
  status: "idle",
  max_concurrent_tasks: 1,
  model: "claude-sonnet-4-6",
  thinking_level: "medium",
  plugin_id: "plugin-uuid-1",
  is_builtin: false,
  owner_id: null,
  skills: [
    { id: "s1", name: "brainstorming", description: "Brainstorming skill" },
    { id: "s2", name: "session-context", description: "Session context" },
  ],
  created_at: "",
  updated_at: "",
  archived_at: null,
  archived_by: null,
};

const MOCK_PLUGIN: BuiltinPlugin = {
  id: "plugin-uuid-1",
  name: "Cospowers Requirements",
  description: "需求分析插件",
  slug: "cospowers-requirements",
  version: "1.0.0",
  category: "engineering",
};

const MOCK_DATA: ArchitectureDetailPanelData = {
  node: MOCK_NODE,
  agent: MOCK_AGENT,
  plugin: MOCK_PLUGIN,
  criticAgent: null,
};

describe("ArchitectureDetailPanel", () => {
  it("renders plugin section with name and slug", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(
      <ArchitectureDetailPanel
        data={MOCK_DATA}
        onClose={onClose}
        onOpenInEditor={onOpenInEditor}
      />,
    );
    expect(screen.getByText("Cospowers Requirements")).toBeTruthy();
    expect(screen.getByText("cospowers-requirements")).toBeTruthy();
  });

  it("renders agent section with full info", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(
      <ArchitectureDetailPanel
        data={MOCK_DATA}
        onClose={onClose}
        onOpenInEditor={onOpenInEditor}
      />,
    );
    expect(screen.getByText("需求分析 Agent")).toBeTruthy();
    expect(screen.getByText(/claude-sonnet-4-6/)).toBeTruthy();
    expect(screen.getByText(/cloud/)).toBeTruthy();
  });

  it("shows skills count", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(
      <ArchitectureDetailPanel
        data={MOCK_DATA}
        onClose={onClose}
        onOpenInEditor={onOpenInEditor}
      />,
    );
    expect(screen.getByText("2")).toBeTruthy();
  });

  it("renders the Skills label with i18n key", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(
      <ArchitectureDetailPanel
        data={MOCK_DATA}
        onClose={onClose}
        onOpenInEditor={onOpenInEditor}
      />,
    );
    expect(screen.getByText("Skills")).toBeTruthy();
  });

  it("calls onClose when close button clicked", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(
      <ArchitectureDetailPanel
        data={MOCK_DATA}
        onClose={onClose}
        onOpenInEditor={onOpenInEditor}
      />,
    );
    fireEvent.click(screen.getByText("×"));
    expect(onClose).toHaveBeenCalled();
  });

  it("calls onOpenInEditor when editor button clicked", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(
      <ArchitectureDetailPanel
        data={MOCK_DATA}
        onClose={onClose}
        onOpenInEditor={onOpenInEditor}
      />,
    );
    fireEvent.click(screen.getByText(/Open in Editor/));
    expect(onOpenInEditor).toHaveBeenCalled();
  });

  it("handles null agent and plugin gracefully", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(
      <ArchitectureDetailPanel
        data={{ ...MOCK_DATA, agent: null, plugin: null }}
        onClose={onClose}
        onOpenInEditor={onOpenInEditor}
      />,
    );
    expect(screen.getByText("需求分析")).toBeTruthy(); // node title as fallback
  });
});
