import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Rock, VisionPlanPage, VisionPlanSection } from "../core/types";
import { StrategyPage } from "./strategy-page";

const state = vi.hoisted(() => ({
  enabled: true,
  loading: false,
  error: false,
  pages: [] as VisionPlanPage[],
  sections: [] as VisionPlanSection[],
  rocks: [] as Rock[],
  createItem: vi.fn(),
  updateItem: vi.fn(),
  deleteItem: vi.fn(),
  createSection: vi.fn(),
  updateSection: vi.fn(),
  deleteSection: vi.fn(),
  createPage: vi.fn(),
  updatePage: vi.fn(),
  deletePage: vi.fn(),
  createConnection: vi.fn(),
  deleteConnection: vi.fn(),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => state.enabled }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));
vi.mock("@multica/core/projects", () => ({ projectListOptions: () => ({ queryKey: ["projects"] }) }));
vi.mock("@multica/core/issues/queries", () => ({ issueListOptions: () => ({ queryKey: ["issues"] }) }));
vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return { ...actual, useQuery: (options: { queryKey: readonly string[] }) => {
    if (options.queryKey.includes("settings")) return { data: { terminology: { strategy: "Strategy", rock: "Goal", rocks: "Goals", vision_plan: "Vision Plan", meetings: "Cycles", org_chart: "Roles", scorecard: "Scorecard", issues_list: "Issues List", strategy_map: "Strategy Map" } } };
    if (options.queryKey.includes("vision-plan")) return { data: { pages: state.pages, sections: state.sections }, isLoading: state.loading, isError: state.error };
    if (options.queryKey.includes("rocks")) return { data: { rocks: state.rocks } };
    if (options.queryKey.includes("periods")) return { data: { periods: [] } };
    if (options.queryKey.includes("members")) return { data: [{ id: "member-1", name: "Maja" }] };
    if (options.queryKey.includes("agents")) return { data: [{ id: "agent-1", name: "Lone" }] };
    if (options.queryKey.includes("projects")) return { data: [{ id: "project-1", title: "Nordic launch" }] };
    if (options.queryKey.includes("issues")) return { data: [{ id: "issue-1", identifier: "FIR-42", title: "Ship pricing page" }] };
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
    useCreateVisionPlanPage: () => mutation(state.createPage),
    useUpdateVisionPlanPage: () => mutation(state.updatePage),
    useDeleteVisionPlanPage: () => mutation(state.deletePage),
    useCreateConnection: () => mutation(state.createConnection),
    useDeleteConnection: () => mutation(state.deleteConnection),
    useSaveRock: () => mutation(vi.fn()),
  };
});

const page = (partial: Partial<VisionPlanPage>): VisionPlanPage => ({
  id: partial.id ?? "vision", workspace_id: "workspace-1", key: partial.key ?? partial.id ?? "vision",
  name: partial.name ?? "Vision", column_count: partial.column_count ?? 2, position: partial.position ?? 0,
  created_at: "2026-07-01T00:00:00Z", updated_at: "2026-07-01T00:00:00Z",
});

const section = (partial: Partial<VisionPlanSection>): VisionPlanSection => ({
  id: partial.id ?? "section-1", workspace_id: "workspace-1", key: partial.key ?? "core-values",
  name: partial.name ?? "Core Values", section_type: partial.section_type ?? "list", position: partial.position ?? 0,
  page_id: partial.page_id ?? "vision", column_index: partial.column_index ?? 0,
  items: partial.items ?? [], created_at: "2026-07-01T00:00:00Z", updated_at: "2026-07-01T00:00:00Z",
});

describe("Vision Plan", () => {
  const renderEdit = () => render(<StrategyPage />);
  const openTraction = () => fireEvent.click(screen.getByRole("tab", { name: "Traction" }));

  beforeEach(() => {
    state.enabled = true; state.loading = false; state.error = false; state.rocks = [];
    state.pages = [
      page({ id: "vision", name: "Vision", column_count: 2, position: 0 }),
      page({ id: "traction", key: "traction", name: "Traction", column_count: 3, position: 1 }),
    ];
    state.sections = [
      section({ id: "values", key: "core-values", name: "Core Values", position: 0, items: [{
        id: "item-1", workspace_id: "workspace-1", section_id: "values", title: "Own the outcome", description: "",
        position: 0, state: "active", goal_connections: [], links: [], created_at: "", updated_at: "",
      }, {
        id: "item-2", workspace_id: "workspace-1", section_id: "values", title: "Care deeply", description: "",
        position: 1, state: "active", goal_connections: [], links: [], created_at: "", updated_at: "",
      }] }),
      section({ id: "marketing", key: "marketing-strategy", name: "Marketing Strategy", section_type: "structured", position: 1 }),
      section({ id: "picture", key: "three-year-picture", name: "Three-Year Picture", position: 0, column_index: 1 }),
      section({ id: "processes", key: "core-processes", name: "Core Processes", section_type: "process", position: 1, column_index: 1 }),
      section({ id: "one-year", key: "one-year-plan", name: "One-Year Plan", position: 0, page_id: "traction", column_index: 0, items: [{
        id: "annual-goal", workspace_id: "workspace-1", section_id: "one-year", title: "Reach 100m revenue", description: "",
        position: 0, state: "active", goal_connections: [], links: [], created_at: "", updated_at: "",
      }] }),
      section({ id: "goals-board", key: "goals-board", name: "Goals", section_type: "goals", position: 0, page_id: "traction", column_index: 1 }),
      section({ id: "issues", key: "issues-list", name: "Issues List", position: 0, page_id: "traction", column_index: 2 }),
    ];
    for (const fn of [state.createItem, state.updateItem, state.deleteItem, state.createSection, state.updateSection, state.deleteSection, state.createPage, state.updatePage, state.deletePage, state.createConnection, state.deleteConnection]) fn.mockReset();
  });

  it("draws one tab per page from the data, not from hard-coded layouts", () => {
    state.pages = [...state.pages, page({ id: "accountability", key: "accountability", name: "Accountability", column_count: 1, position: 2 })];

    renderEdit();

    expect(screen.getByRole("heading", { name: "Strategy Map" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Vision" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Traction" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Accountability" })).toBeInTheDocument();
  }, 30_000);

  it("shows the open page's blocks in their own columns", () => {
    renderEdit();

    expect(screen.getByRole("region", { name: "Core Values" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Three-Year Picture" })).toBeInTheDocument();
    expect(screen.getByDisplayValue("Own the outcome")).toBeInTheDocument();
    // The named blanks the paper organiser asks for are offered as one-click chips.
    expect(screen.getByRole("button", { name: /Target Market \/ The List/ })).toBeInTheDocument();
    // Two columns on Vision, three on Traction — both come from the page record.
    expect(screen.getAllByLabelText(/^Column /)).toHaveLength(2);
    openTraction();
    expect(screen.getAllByLabelText(/^Column /)).toHaveLength(3);
  }, 30_000);

  it("renders a Goals block as the current period's goals", () => {
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

  it("adds a page", () => {
    renderEdit();
    fireEvent.click(screen.getByRole("button", { name: "Add page" }));
    fireEvent.change(screen.getByLabelText("New page name"), { target: { value: "Accountability" } });
    fireEvent.keyDown(screen.getByLabelText("New page name"), { key: "Enter" });
    expect(state.createPage).toHaveBeenCalledWith(expect.objectContaining({ name: "Accountability", position: 2 }));
  });

  it("renames the open page and changes its column count", () => {
    renderEdit();

    fireEvent.change(screen.getByLabelText("Vision page name"), { target: { value: "Our Vision" } });
    fireEvent.blur(screen.getByDisplayValue("Our Vision"));
    expect(state.updatePage).toHaveBeenCalledWith(expect.objectContaining({ id: "vision", input: expect.objectContaining({ name: "Our Vision" }) }));

    fireEvent.click(screen.getByRole("button", { name: "3 column layout" }));
    expect(state.updatePage).toHaveBeenCalledWith(expect.objectContaining({ id: "vision", input: expect.objectContaining({ column_count: 3 }) }));
  });

  it("deletes a page behind an inline confirm, and never the last one", () => {
    renderEdit();
    fireEvent.click(screen.getByRole("button", { name: "Delete Vision page" }));
    expect(state.deletePage).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Confirm delete Vision page" }));
    expect(state.deletePage).toHaveBeenCalledWith("vision");

    state.pages = [page({ id: "vision", name: "Vision" })];
    renderEdit();
    expect(screen.queryByRole("button", { name: "Delete Vision page" })).not.toBeInTheDocument();
  });

  it("adds a block to a chosen column of the open page", () => {
    renderEdit();
    const input = screen.getByLabelText("Add block to column 2");
    fireEvent.change(input, { target: { value: "Customer Promise" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(state.createSection).toHaveBeenCalledWith(expect.objectContaining({
      name: "Customer Promise", page_id: "vision", column_index: 1, position: 2,
    }));
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

  it("deletes a block via an inline confirm instead of a window dialog", () => {
    renderEdit();
    fireEvent.click(screen.getByRole("button", { name: "Delete Core Processes block" }));
    // First click only arms the confirm — nothing is deleted yet.
    expect(state.deleteSection).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Confirm delete Core Processes block" }));
    expect(state.deleteSection).toHaveBeenCalledWith("processes");
  });

  it("cancels a block delete without removing it", () => {
    renderEdit();
    fireEvent.click(screen.getByRole("button", { name: "Delete Core Processes block" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel delete Core Processes block" }));
    expect(state.deleteSection).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Delete Core Processes block" })).toBeInTheDocument();
  });

  it("renames a block inline and keeps it on its page and column", () => {
    renderEdit();
    fireEvent.change(screen.getByDisplayValue("Core Processes"), { target: { value: "Our Processes" } });
    fireEvent.blur(screen.getByDisplayValue("Our Processes"));
    expect(state.updateSection).toHaveBeenCalledWith(expect.objectContaining({
      id: "processes", input: expect.objectContaining({ name: "Our Processes", page_id: "vision", column_index: 1 }),
    }));
  });

  it("moves a block up its column", () => {
    renderEdit();
    fireEvent.click(screen.getByRole("button", { name: "Move Core Processes up" }));
    expect(state.updateSection).toHaveBeenCalledWith(expect.objectContaining({
      id: "processes", input: expect.objectContaining({ position: 0, column_index: 1 }),
    }));
  });

  it("gives every block a drag grip so any block can be rearranged", () => {
    renderEdit();
    expect(screen.getByRole("button", { name: "Reorder Own the outcome" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reorder Core Processes" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reorder Core Values" })).toBeInTheDocument();
  });

  it("couples an item to a Project and to an Issue", () => {
    renderEdit();

    fireEvent.click(screen.getByRole("button", { name: "Own the outcome Projects and Issues" }));
    fireEvent.click(screen.getByRole("option", { name: "Nordic launch" }));
    expect(state.createConnection).toHaveBeenCalledWith(expect.objectContaining({
      source_type: "strategy_item", source_id: "item-1", target_type: "project", target_id: "project-1",
    }));

    fireEvent.click(screen.getByRole("option", { name: "FIR-42 · Ship pricing page" }));
    expect(state.createConnection).toHaveBeenCalledWith(expect.objectContaining({
      source_type: "strategy_item", source_id: "item-1", target_type: "issue", target_id: "issue-1",
    }));
  });

  it("shows a coupled Project on the item and disconnects it again", () => {
    state.sections[0]!.items[0]!.links = [
      { connection_id: "connection-1", target_type: "project", target_id: "project-1", title: "Nordic launch", identifier: "" },
      { connection_id: "connection-2", target_type: "issue", target_id: "issue-1", title: "Ship pricing page", identifier: "FIR-42" },
    ];

    renderEdit();

    const linked = screen.getByRole("list", { name: "Own the outcome connected Projects and Issues" });
    expect(linked).toHaveTextContent("Nordic launch");
    expect(linked).toHaveTextContent("FIR-42 · Ship pricing page");

    fireEvent.click(screen.getByRole("button", { name: "Disconnect Nordic launch" }));
    expect(state.deleteConnection).toHaveBeenCalledWith("connection-1");
    expect(state.deleteConnection).not.toHaveBeenCalledWith("connection-2");
  });

  it("offers Goal connections only for One-Year Plan items", () => {
    renderEdit();
    expect(screen.queryByRole("button", { name: "Own the outcome Goals" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Care deeply Goals" })).not.toBeInTheDocument();

    openTraction();
    expect(screen.getByRole("button", { name: "Reach 100m revenue Goals" })).toBeInTheDocument();
  });
});
