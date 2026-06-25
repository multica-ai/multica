// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@testing-library/react";
import { ArchitectureDetailPanel } from "./architecture-detail-panel";
import type { ArchitectureDetailPanelData } from "./architecture-detail-panel";
import type { WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";

vi.mock("../../../i18n", () => ({
  useT: () => ({
    t: (key: unknown) => {
      if (typeof key === "function") {
        const resources = {
          overview: {
            detail_panel: {
              title: "Node Details",
              inspector: "Inspector",
              plugins: "Plugin Info",
              worker: "Associated Agent",
              critic: "Critic",
              skills: "Skills",
              open_in_editor: "Open in Editor",
              bundle: "Bundle",
              skills_count: "Skills count",
              skills_list: "Skills list",
              agents_count: "Agents count",
              agent_name: "Name",
              agent_description: "Description",
              agent_avatar: "Avatar",
              agent_runtime_mode: "Runtime mode",
              agent_runtime: "Runtime",
              agent_status: "Status",
              agent_model: "Model",
              agent_thinking_level: "Thinking level",
              agent_visibility: "Visibility",
              agent_max_concurrent: "Max concurrent",
              agent_instructions: "Instructions",
              agent_custom_env: "Custom env",
              agent_custom_args: "Custom args",
              agent_builtin: "Built-in",
              agent_skills: "Skills",
              plugin_name: "Name",
              plugin_slug: "Slug",
              plugin_version: "Version",
              plugin_category: "Category",
              plugin_description: "Description",
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
  title: "Requirement Analysis",
  description: "",
  position_x: 0,
  position_y: 0,
  format_schema: null,
  worker_type: "agent",
  worker_id: "agent-1",
  critic_type: "human",
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
  name: "Requirements Agent",
  description: "Analyzes requirements",
  instructions: "Analyze the document",
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
  name: "Requirements Plugin",
  description: "Requirement analysis plugin",
  slug: "requirements-plugin",
  version: "1.0.0",
  category: "engineering",
  metadata: {
    bundle: {
      skills_count: 9,
      agents_count: 0,
      commands_count: 0,
      hooks_count: 0,
      skills_namespaces: [
        "cospowers-requirements:requirements-intake",
        "cospowers-requirements:requirement-analysis",
        "cospowers-requirements:system-requirement-analysis",
      ],
    },
  },
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
    expect(screen.getAllByText("Requirements Plugin").length).toBeGreaterThan(0);
    expect(screen.getByText("requirements-plugin")).toBeTruthy();
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
    expect(screen.getByText("Requirements Agent")).toBeTruthy();
    expect(screen.getByText(/claude-sonnet-4-6/)).toBeTruthy();
    expect(screen.getByText(/cloud/)).toBeTruthy();
  });

  it("shows agent skills names", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(
      <ArchitectureDetailPanel
        data={MOCK_DATA}
        onClose={onClose}
        onOpenInEditor={onOpenInEditor}
      />,
    );
    expect(screen.getByText(/brainstorming/)).toBeTruthy();
    expect(screen.getByText(/session-context/)).toBeTruthy();
  });

  it("shows bundle info with skills count", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(
      <ArchitectureDetailPanel
        data={MOCK_DATA}
        onClose={onClose}
        onOpenInEditor={onOpenInEditor}
      />,
    );
    expect(screen.getByText("9")).toBeTruthy();
  });

  it("shows skills namespaces list", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(
      <ArchitectureDetailPanel
        data={MOCK_DATA}
        onClose={onClose}
        onOpenInEditor={onOpenInEditor}
      />,
    );
    expect(screen.getByText("requirements-intake")).toBeTruthy();
    expect(screen.getByText("requirement-analysis")).toBeTruthy();
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
    fireEvent.click(screen.getByTestId("detail-panel-close"));
    expect(onClose).toHaveBeenCalled();
  });

  it("calls onClose when escape is pressed", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(
      <ArchitectureDetailPanel
        data={MOCK_DATA}
        onClose={onClose}
        onOpenInEditor={onOpenInEditor}
      />,
    );
    fireEvent.keyDown(window, { key: "Escape" });
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
    expect(screen.getAllByText("Requirement Analysis").length).toBeGreaterThan(0);
  });

  it("handles plugin without metadata gracefully", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(
      <ArchitectureDetailPanel
        data={{
          ...MOCK_DATA,
          plugin: {
            id: "p-min",
            name: "Minimal Plugin",
            description: "",
            slug: "minimal",
            version: "0.0.1",
            category: "test",
          },
        }}
        onClose={onClose}
        onOpenInEditor={onOpenInEditor}
      />,
    );
    expect(screen.getByText("Minimal Plugin")).toBeTruthy();
  });
});
