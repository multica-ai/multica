import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Rock, VisionPlanSection } from "../core/types";
import { StrategyPage } from "./strategy-page";

const state = vi.hoisted(() => ({
  enabled: true,
  loading: false,
  error: false,
  sections: [] as VisionPlanSection[],
  rocks: [] as Rock[],
  createItem: vi.fn(),
  updateItem: vi.fn(),
  deleteItem: vi.fn(),
  createSection: vi.fn(),
  updateSection: vi.fn(),
  deleteSection: vi.fn(),
  createConnection: vi.fn(),
  deleteConnection: vi.fn(),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => state.enabled }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return { ...actual, useQuery: (options: { queryKey: readonly string[] }) => {
    if (options.queryKey.includes("settings")) return { data: { terminology: { strategy: "Strategy", rock: "Goal", rocks: "Goals", vision_plan: "Vision Plan" } } };
    if (options.queryKey.includes("vision-plan")) return { data: { sections: state.sections }, isLoading: state.loading, isError: state.error };
    if (options.queryKey.includes("rocks")) return { data: { rocks: state.rocks } };
    if (options.queryKey.includes("periods")) return { data: { periods: [] } };
    if (options.queryKey.includes("members")) return { data: [{ id: "member-1", name: "Maja" }] };
    if (options.queryKey.includes("agents")) return { data: [{ id: "agent-1", name: "Lone" }] };
    return { data: undefined };
  } };
});
vi.mock("../core/queries", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../core/queries")>();
  const mutation = (fn: ReturnType<typeof vi.fn>) => ({ mutate: fn, isPending: false });
  return { ...actual,
    useCreateVisionPlanItem: () => mutation(state.createItem),
    useUpdateVisionPlanItem: () => mutation(state.updateItem),
    useDeleteVisionPlanItem: () => mutation(state.deleteItem),
    useCreateVisionPlanSection: () => mutation(state.createSection),
    useUpdateVisionPlanSection: () => mutation(state.updateSection),
    useDeleteVisionPlanSection: () => mutation(state.deleteSection),
    useCreateConnection: () => mutation(state.createConnection),
    useDeleteConnection: () => mutation(state.deleteConnection),
    useSaveRock: () => mutation(vi.fn()),
  };
});

const section = (partial: Partial<VisionPlanSection>): VisionPlanSection => ({
  id: partial.id ?? "section-1", workspace_id: "workspace-1", key: partial.key ?? "core-values",
  name: partial.name ?? "Core Values", section_type: partial.section_type ?? "list", position: partial.position ?? 0,
  items: partial.items ?? [], created_at: "2026-07-01T00:00:00Z", updated_at: "2026-07-01T00:00:00Z",
});

describe("Vision Plan", () => {
  beforeEach(() => {
    state.enabled = true; state.loading = false; state.error = false; state.rocks = [];
    state.sections = [
      section({ id: "values", key: "core-values", name: "Core Values", position: 0, items: [{
        id: "item-1", workspace_id: "workspace-1", section_id: "values", title: "Own the outcome", description: "",
        position: 0, state: "active", goal_connections: [], created_at: "", updated_at: "",
      }, {
        id: "item-2", workspace_id: "workspace-1", section_id: "values", title: "Care deeply", description: "",
        position: 1, state: "active", goal_connections: [], created_at: "", updated_at: "",
      }] }),
      section({ id: "marketing", key: "marketing-strategy", name: "Marketing Strategy", section_type: "structured", position: 1 }),
      section({ id: "processes", key: "core-processes", name: "Core Processes", section_type: "process", position: 2 }),
    ];
    for (const fn of [state.createItem, state.updateItem, state.deleteItem, state.createSection, state.updateSection, state.deleteSection, state.createConnection, state.deleteConnection]) fn.mockReset();
  });

  it("renders the editable Vision Plan as an ordered strategy canvas", () => {
    render(<StrategyPage />);
    expect(screen.getByRole("heading", { name: "Vision Plan" })).toBeInTheDocument();
    expect(screen.getByDisplayValue("Core Values")).toBeInTheDocument();
    expect(screen.getByDisplayValue("Own the outcome")).toBeInTheDocument();
    expect(screen.getByText(/Target market/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add section" })).toBeInTheDocument();
  });

  it("adds and updates inline content without opening a modal", () => {
    render(<StrategyPage />);
    const input = screen.getByLabelText("Add item to Core Values");
    fireEvent.change(input, { target: { value: "Care deeply" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(state.createItem).toHaveBeenCalledWith(expect.objectContaining({ section_id: "values", title: "Care deeply", position: 2 }));

    fireEvent.change(screen.getByDisplayValue("Own the outcome"), { target: { value: "Own every outcome" } });
    fireEvent.blur(screen.getByDisplayValue("Own every outcome"));
    expect(state.updateItem).toHaveBeenCalledWith(expect.objectContaining({ id: "item-1", input: expect.objectContaining({ title: "Own every outcome" }) }));

    fireEvent.click(screen.getByRole("button", { name: "Move Own the outcome down" }));
    expect(state.updateItem).toHaveBeenCalledWith(expect.objectContaining({ id: "item-1", input: expect.objectContaining({ position: 1 }) }));
  });

  it("renames and reorders sections inline", () => {
    render(<StrategyPage />);
    fireEvent.change(screen.getByDisplayValue("Core Values"), { target: { value: "Operating Principles" } });
    fireEvent.blur(screen.getByDisplayValue("Operating Principles"));
    expect(state.updateSection).toHaveBeenCalledWith(expect.objectContaining({ id: "values", input: expect.objectContaining({ name: "Operating Principles" }) }));

    fireEvent.click(screen.getByRole("button", { name: "Move Core Values down" }));
    expect(state.updateSection).toHaveBeenCalledWith(expect.objectContaining({ id: "values", input: expect.objectContaining({ position: 1 }) }));
  });
});
