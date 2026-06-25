// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@testing-library/react";
import { CompactNodeCard } from "./compact-node-card";
import type { WorkflowNode } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";

vi.mock("../../../i18n", () => ({
  useT: () => ({
    t: (key: unknown) => {
      if (typeof key === "function") {
        const resources: Record<string, unknown> = {
          node: {
            worker_type_human: "Human",
            worker_type_agent: "Agent",
            worker_type_squad: "Squad",
          },
          overview: {
            detail_panel: {
              not_configured: "Not configured",
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

const MOCK_PLUGIN: BuiltinPlugin = {
  id: "plugin-1",
  name: "Cospowers Brainstorming",
  description: "Brainstorming plugin",
  slug: "cospowers-brainstorming",
  version: "1.0.0",
  category: "engineering",
};

const WORKER_NAME = "Brainstorm Agent";

describe("CompactNodeCard", () => {
  it("renders plugin name from plugin lookup", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} workerName={WORKER_NAME} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.getByText("Cospowers Brainstorming")).toBeInTheDocument();
  });

  it("falls back to node title when plugin is null", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} workerName={WORKER_NAME} plugin={null} onClick={onClick} />);
    expect(screen.getByText("brainstorming")).toBeInTheDocument();
  });

  it("does not render plugin description text", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} workerName={WORKER_NAME} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.queryByText("Brainstorming plugin")).not.toBeInTheDocument();
  });

  it("renders worker name with success dot when worker is assigned", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} workerName={WORKER_NAME} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.getByText("Brainstorm Agent")).toBeInTheDocument();
  });

  it("renders fallback label when workerName is null (agent type)", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} workerName={null} plugin={null} onClick={onClick} />);
    expect(screen.getByText("brainstorming")).toBeInTheDocument();
    expect(screen.getByText(/Agent/)).toBeInTheDocument();
    expect(screen.getByText(/Not configured/)).toBeInTheDocument();
  });

  it("renders fallback label for human worker type", () => {
    const onClick = vi.fn();
    const humanNode = { ...MOCK_NODE, worker_type: "human" as const, worker_id: null };
    render(<CompactNodeCard node={humanNode} workerName={null} plugin={null} onClick={onClick} />);
    expect(screen.getByText(/Human/)).toBeInTheDocument();
    expect(screen.getByText(/Not configured/)).toBeInTheDocument();
  });

  it("renders fallback label for squad worker type", () => {
    const onClick = vi.fn();
    const squadNode = { ...MOCK_NODE, worker_type: "squad" as const, worker_id: null };
    render(<CompactNodeCard node={squadNode} workerName={null} plugin={null} onClick={onClick} />);
    expect(screen.getByText(/Squad/)).toBeInTheDocument();
    expect(screen.getByText(/Not configured/)).toBeInTheDocument();
  });

  it("fires onClick with node id and 'worker' focus when clicked", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} workerName={WORKER_NAME} plugin={MOCK_PLUGIN} onClick={onClick} />);
    fireEvent.click(screen.getByTestId("compact-node-card-node-1"));
    expect(onClick).toHaveBeenCalledWith("node-1", "worker");
  });

  it("applies selected styling when isSelected is true", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} workerName={WORKER_NAME} plugin={MOCK_PLUGIN} onClick={onClick} isSelected />);
    const card = screen.getByTestId("compact-node-card-node-1");
    expect(card.getAttribute("aria-pressed")).toBe("true");
  });

  it("uses a wide fixed card width for readable plugin names", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} workerName={WORKER_NAME} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.getByTestId("compact-node-card-node-1").className).toContain("w-56");
  });

  it("calls elementRef callback with the DOM element", () => {
    const onClick = vi.fn();
    const refs: (HTMLButtonElement | null)[] = [];
    render(<CompactNodeCard node={MOCK_NODE} workerName={null} plugin={null} onClick={onClick} elementRef={(el) => refs.push(el)} />);
    expect(refs.length).toBeGreaterThan(0);
    expect(refs[0]).toBeInstanceOf(HTMLButtonElement);
  });
});
