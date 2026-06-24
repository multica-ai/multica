// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@testing-library/react";
import { PluginCard } from "./plugin-card";
import type { WorkflowNode } from "@multica/core/types";
import type { Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";

const MOCK_NODE: WorkflowNode = {
  id: "node-1",
  workflow_id: "wf-1",
  title: "需求分析",
  description: "分析产品需求文档",
  position_x: 0, position_y: 0,
  format_schema: null,
  worker_type: "agent",
  worker_id: "agent-1",
  critic_type: "human",
  critic_id: null,
  critic_api_url: null,
  sort_order: 0,
  stage_id: "stage-1",
  created_at: "", updated_at: "",
};

const MOCK_AGENT: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "rt-1",
  name: "需求分析 Agent",
  description: "负责需求分析",
  instructions: "...",
  avatar_url: null,
  runtime_mode: "cloud",
  runtime_config: {},
  custom_env: {},
  custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace",
  status: "idle",
  max_concurrent_tasks: 1,
  model: "claude-sonnet-4-6",
  thinking_level: "medium",
  plugin_id: "plugin-uuid-1",
  is_builtin: false,
  owner_id: null,
  skills: [],
  created_at: "", updated_at: "",
  archived_at: null, archived_by: null,
};

const MOCK_PLUGIN: BuiltinPlugin = {
  id: "plugin-uuid-1",
  name: "Cospowers Requirements",
  description: "需求分析插件",
  slug: "cospowers-requirements",
  version: "1.0.0",
  category: "engineering",
};

describe("PluginCard", () => {
  it("renders plugin name from plugin lookup", () => {
    const onClick = vi.fn();
    render(<PluginCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.getByText("Cospowers Requirements")).toBeInTheDocument();
  });

  it("falls back to node title when plugin is null", () => {
    const onClick = vi.fn();
    render(<PluginCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={null} onClick={onClick} />);
    expect(screen.getByText("需求分析")).toBeInTheDocument();
  });

  it("renders plugin description when available", () => {
    const onClick = vi.fn();
    render(<PluginCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.getByText("需求分析插件")).toBeInTheDocument();
  });

  it("shows agent status dot and model", () => {
    const onClick = vi.fn();
    render(<PluginCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.getByText(/claude-sonnet-4-6/)).toBeInTheDocument();
    expect(screen.getByText("需求分析 Agent")).toBeInTheDocument();
  });

  it("fires onClick with node id when clicked", () => {
    const onClick = vi.fn();
    render(<PluginCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    fireEvent.click(screen.getByTestId("plugin-card-node-1"));
    expect(onClick).toHaveBeenCalledWith("node-1", "worker");
  });

  it("renders without agent info when agent is null", () => {
    const onClick = vi.fn();
    render(<PluginCard node={MOCK_NODE} agent={null} plugin={null} onClick={onClick} />);
    expect(screen.getByText("需求分析")).toBeInTheDocument();
    expect(screen.getByTestId("plugin-card-node-1")).toBeInTheDocument();
  });
});
