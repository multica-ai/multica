// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@testing-library/react";
import { CriticBadge } from "./critic-badge";
import type { WorkflowNode, Agent } from "@multica/core/types";

const MOCK_CRITIC_NODE: WorkflowNode = {
  id: "critic-1",
  workflow_id: "wf-1",
  title: "评估器",
  description: "",
  position_x: 0, position_y: 0,
  format_schema: null,
  worker_type: "agent",
  worker_id: "agent-critic-1",
  critic_type: "agent",
  critic_id: "agent-critic-2",
  critic_api_url: null,
  sort_order: 0,
  stage_id: "stage-1",
  created_at: "", updated_at: "",
};

const MOCK_CRITIC_AGENT: Agent = {
  id: "agent-critic-1",
  workspace_id: "ws-1",
  runtime_id: "rt-1",
  name: "审核师",
  description: "负责代码审查",
  instructions: "", avatar_url: null,
  runtime_mode: "cloud", runtime_config: {},
  custom_env: {}, custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace", status: "idle",
  max_concurrent_tasks: 1,
  model: "claude-sonnet-4-6",
  thinking_level: "",
  plugin_id: null,
  is_builtin: true,
  owner_id: null, skills: [],
  created_at: "", updated_at: "",
  archived_at: null, archived_by: null,
};

describe("CriticBadge", () => {
  it("renders with dashed border style", () => {
    const onClick = vi.fn();
    render(<CriticBadge node={MOCK_CRITIC_NODE} criticAgent={MOCK_CRITIC_AGENT} onClick={onClick} />);
    const btn = screen.getByTestId("critic-badge-critic-1");
    expect(btn.className).toContain("border-dashed");
  });

  it("renders critic agent name", () => {
    const onClick = vi.fn();
    render(<CriticBadge node={MOCK_CRITIC_NODE} criticAgent={MOCK_CRITIC_AGENT} onClick={onClick} />);
    expect(screen.getByText("审核师")).toBeInTheDocument();
  });

  it("falls back to node title when critic agent is null", () => {
    const onClick = vi.fn();
    render(<CriticBadge node={MOCK_CRITIC_NODE} criticAgent={null} onClick={onClick} />);
    expect(screen.getByText("评估器")).toBeInTheDocument();
  });

  it("renders agent model when criticAgent is provided", () => {
    const onClick = vi.fn();
    render(<CriticBadge node={MOCK_CRITIC_NODE} criticAgent={MOCK_CRITIC_AGENT} onClick={onClick} />);
    expect(screen.getByText("claude-sonnet-4-6")).toBeInTheDocument();
  });

  it("fires onClick when clicked", () => {
    const onClick = vi.fn();
    render(<CriticBadge node={MOCK_CRITIC_NODE} criticAgent={MOCK_CRITIC_AGENT} onClick={onClick} />);
    fireEvent.click(screen.getByTestId("critic-badge-critic-1"));
    expect(onClick).toHaveBeenCalledWith("critic-1", "critic");
  });
});
