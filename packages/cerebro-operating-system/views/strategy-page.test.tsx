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
    if (options.queryKey.includes("settings")) return { data: { terminology: { strategy: "Strategy", rock: "Goal", rocks: "Goals", vision_plan: "Vision Plan", meetings: "Cycles", org_chart: "Roles", scorecard: "Scorecard", issues_list: "Issues List", strategy_map: "Strategy Map" } } };
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
  const renderEdit = () => render(<StrategyPage />);
  const openTraction = () => fireEvent.click(screen.getByRole("tab", { name: "Traction" }));

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
      section({ id: "one-year", key: "one-year-plan", name: "One-Year Plan", position: 3, items: [{
        id: "annual-goal", workspace_id: "workspace-1", section_id: "one-year", title: "Reach 100m revenue", description: "",
        position: 0, state: "active", goal_connections: [], created_at: "", updated_at: "",
      }] }),
    ];
    for (const fn of [state.createItem, state.updateItem, state.deleteItem, state.createSection, state.updateSection, state.deleteSection, state.createConnection, state.deleteConnection]) fn.mockReset();
  });

  it("lays Vision out as labelled rows with the 3-Year Picture beside them", () => {
    state.sections = [...state.sections, section({ id: "picture", key: "three-year-picture", name: "Three-Year Picture", position: 4 })];

    renderEdit();

    expect(screen.getByRole("heading", { name: "Strategy Map" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Core Values" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Marketing Strategy" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Three-Year Picture" })).toBeInTheDocument();
    expect(screen.getByDisplayValue("Own the outcome")).toBeInTheDocument();
    // The named blanks the paper organiser asks for are offered as one-click chips.
    expect(screen.getByRole("button", { name: /Target Market \/ The List/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add section" })).toBeInTheDocument();
  }, 30_000);

  it("lays Traction out as 1-Year Plan, Goals with a Who column, and Issues List", () => {
    state.sections = [...state.sections, section({ id: "issues", key: "issues-list", name: "Issues List", position: 5 })];
    state.rocks = [{ id: "rock-1", title: "Launch Denmark", owner_name: "Maja" } as Rock];

    renderEdit();
    openTraction();

    expect(screen.getByRole("region", { name: "One-Year Plan" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Goals" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Issues List" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Who" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "Maja" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Launch Denmark" })).toBeInTheDocument();
  }, 30_000);

  it("keeps sections outside the organiser visible instead of dropping them", () => {
    renderEdit();
    // Core Processes is not one of the six V/TO slots.
    expect(screen.getByRole("region", { name: "Other sections" })).toBeInTheDocument();
    expect(screen.getByDisplayValue("Core Processes")).toBeInTheDocument();
  });

  it("adds and updates inline content without opening a modal", () => {
    renderEdit();
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

  it("deletes an extra section via an inline confirm instead of a window dialog", () => {
    renderEdit();
    fireEvent.click(screen.getByRole("button", { name: "Delete Core Processes section" }));
    // First click only arms the confirm — nothing is deleted yet.
    expect(state.deleteSection).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Confirm delete Core Processes section" }));
    expect(state.deleteSection).toHaveBeenCalledWith("processes");
  });

  it("cancels a section delete without removing it", () => {
    renderEdit();
    fireEvent.click(screen.getByRole("button", { name: "Delete Core Processes section" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel delete Core Processes section" }));
    expect(state.deleteSection).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Delete Core Processes section" })).toBeInTheDocument();
  });

  it("renames an extra section inline", () => {
    renderEdit();
    fireEvent.change(screen.getByDisplayValue("Core Processes"), { target: { value: "Our Processes" } });
    fireEvent.blur(screen.getByDisplayValue("Our Processes"));
    expect(state.updateSection).toHaveBeenCalledWith(expect.objectContaining({ id: "processes", input: expect.objectContaining({ name: "Our Processes" }) }));
  });

  it("renders drag grips so cards and extra columns can be reordered", () => {
    renderEdit();
    expect(screen.getByRole("button", { name: "Reorder Own the outcome" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reorder Core Processes" })).toBeInTheDocument();
    // The six fixed V/TO slots are part of the organiser and cannot be dragged away.
    expect(screen.queryByRole("button", { name: "Reorder Core Values" })).not.toBeInTheDocument();
  });

  it("offers Goal connections only for One-Year Plan items", () => {
    renderEdit();
    expect(screen.queryByRole("button", { name: "Own the outcome Goals" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Care deeply Goals" })).not.toBeInTheDocument();

    openTraction();
    expect(screen.getByRole("button", { name: "Reach 100m revenue Goals" })).toBeInTheDocument();
  });
});
