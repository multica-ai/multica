// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@testing-library/react";
import { CompactNodeCard } from "./compact-node-card";
import type { WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";

const MOCK_NODE: WorkflowNode = {
  id: "node-1",
  workflow_id: "wf-1",
  title: "brainstorming",
  description: "Brainstorming session",
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
  name: "Brainstorm Agent",
  description: "Runs brainstorming",
  instructions: "",
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
  plugin_id: "plugin-1",
  is_builtin: false,
  owner_id: null,
  skills: [],
  created_at: "", updated_at: "",
  archived_at: null, archived_by: null,
};

const MOCK_PLUGIN: BuiltinPlugin = {
  id: "plugin-1",
  name: "Cospowers Brainstorming",
  description: "Brainstorming plugin",
  slug: "cospowers-brainstorming",
  version: "1.0.0",
  category: "engineering",
};

describe("CompactNodeCard", () => {
  it("renders plugin name from plugin lookup", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.getByText("Cospowers Brainstorming")).toBeInTheDocument();
  });

  it("falls back to node title when plugin is null", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={null} onClick={onClick} />);
    expect(screen.getByText("brainstorming")).toBeInTheDocument();
  });

  it("does not render plugin description text", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.queryByText("Brainstorming plugin")).not.toBeInTheDocument();
  });

  it("renders agent name and status dot", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.getByText("Brainstorm Agent")).toBeInTheDocument();
  });

  it("does not render agent model text", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.queryByText(/claude-sonnet/)).not.toBeInTheDocument();
  });

  it("fires onClick with node id and 'worker' focus when clicked", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    fireEvent.click(screen.getByTestId("compact-node-card-node-1"));
    expect(onClick).toHaveBeenCalledWith("node-1", "worker");
  });

  it("renders without agent info when agent is null", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={null} plugin={null} onClick={onClick} />);
    expect(screen.getByText("brainstorming")).toBeInTheDocument();
    expect(screen.getByTestId("compact-node-card-node-1")).toBeInTheDocument();
  });

  it("applies selected styling when isSelected is true", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} isSelected />);
    const card = screen.getByTestId("compact-node-card-node-1");
    expect(card.getAttribute("aria-pressed")).toBe("true");
  });

  it("uses a wide fixed card width for readable plugin names", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.getByTestId("compact-node-card-node-1").className).toContain("w-56");
  });

  it("calls elementRef callback with the DOM element", () => {
    const onClick = vi.fn();
    const refs: (HTMLButtonElement | null)[] = [];
    render(<CompactNodeCard node={MOCK_NODE} agent={null} plugin={null} onClick={onClick} elementRef={(el) => refs.push(el)} />);
    expect(refs.length).toBeGreaterThan(0);
    expect(refs[0]).toBeInstanceOf(HTMLButtonElement);
  });
});
