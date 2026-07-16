import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Rock, StrategyItem } from "../core/types";
import { StrategyPage } from "./strategy-page";

const state = vi.hoisted(() => ({
  enabled: true,
  strategyLoading: false,
  strategyError: false,
  strategy: [] as StrategyItem[],
  rocks: [] as Rock[],
  terminology: { strategy: "Strategy", rock: "Rock", rocks: "Rocks" },
  createItem: vi.fn(),
  updateItem: vi.fn(),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => state.enabled }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return { ...actual, useQuery: (options: { queryKey: readonly string[] }) => {
    if (options.queryKey.includes("settings")) return { data: { terminology: state.terminology } };
    if (options.queryKey.includes("strategy-history")) return { data: { history: state.strategy.map((item) => ({ id: `history-${item.id}`, strategy_item_id: item.id, action: "updated", title: item.title, snapshot: item, changed_at: item.updated_at })) } };
    if (options.queryKey.includes("strategy")) return { data: { strategy_items: state.strategy }, isLoading: state.strategyLoading, isError: state.strategyError };
    if (options.queryKey.includes("rocks")) return { data: { rocks: state.rocks } };
    return { data: undefined };
  } };
});
vi.mock("../core/queries", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../core/queries")>();
  return { ...actual,
    useCreateStrategyItem: () => ({ mutate: state.createItem, isPending: false }),
    useUpdateStrategyItem: () => ({ mutate: state.updateItem, isPending: false }),
  };
});

const item = (partial: Partial<StrategyItem>): StrategyItem => ({
  id: partial.id ?? "strategy-1", workspace_id: "workspace-1", kind: partial.kind ?? "horizon_goal",
  title: partial.title ?? "Become the most trusted operator", description: partial.description ?? "",
  horizon_count: partial.horizon_count, horizon_unit: partial.horizon_unit, horizon_label: partial.horizon_label,
  position: partial.position ?? 0, state: partial.state ?? "active",
  created_at: "2026-07-01T00:00:00Z", updated_at: "2026-07-01T00:00:00Z",
});

const rock: Rock = {
  id: "rock-1", workspace_id: "workspace-1", title: "Win Norway", owner_type: "agent", owner_id: "agent-1", owner_name: "Sara",
  period_id: "period-1", period_name: "Q3 2026", period_start: "2026-07-01", period_end: "2026-09-30",
  confidence: 75, reported_health: "on_track", derived_health: { state: "at_risk", reason: "Execution is in progress", calculated_at: "2026-07-13T00:00:00Z" },
  health_score: 63, issue_count: 4, done_issue_count: 1, blocked_issue_count: 0, project_count: 0, projects: [], issues: [], check_ins: [],
  strategy_item_id: "one", strategy_item_title: "Win the Nordic market", created_at: "2026-07-01T00:00:00Z", updated_at: "2026-07-01T00:00:00Z",
};

describe("StrategyPage", () => {
  beforeEach(() => { state.enabled = true; state.strategyLoading = false; state.strategyError = false; state.strategy = []; state.rocks = []; state.createItem.mockReset(); state.updateItem.mockReset(); });

  it("renders the v4 Strategy shell without mixing in Settings", () => {
    render(<StrategyPage />);
    expect(screen.getByRole("heading", { name: "Strategy" })).toBeInTheDocument();
    expect(screen.getByText(/Vision \/ Traction Organizer/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "History" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Edit map" })).toBeInTheDocument();
    expect(screen.queryByText("Customize labels")).not.toBeInTheDocument();
    expect(screen.getByText(/Built on Multica/)).toBeInTheDocument();
  });

  it("renders the four-column strategy cascade and live Rock health", () => {
    state.strategy = [
      item({ id: "value", kind: "core_value", title: "Own the outcome" }),
      item({ id: "focus", kind: "core_focus", title: "AI-native commerce" }),
      item({ id: "ten", title: "Nordic category leader", horizon_count: 10, horizon_unit: "year", horizon_label: "10-Year Target" }),
      item({ id: "three", title: "Build the platform", horizon_count: 3, horizon_unit: "year", horizon_label: "3-Year Picture" }),
      item({ id: "one", title: "Win the Nordic market", horizon_count: 1, horizon_unit: "year", horizon_label: "1-Year Plan" }),
    ];
    state.rocks = [rock];
    render(<StrategyPage />);
    expect(screen.getByRole("heading", { name: "Core Values" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Core Focus" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "10-Year Target" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "3-Year Picture" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "1-Year Plan" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Quarterly Rocks" })).toBeInTheDocument();
    expect(screen.getByText("Win Norway")).toBeInTheDocument();
    expect(screen.getByText("At risk")).toBeInTheDocument();
  });

  it("opens Edit map and saves a named horizon", () => {
    render(<StrategyPage />);
    fireEvent.click(screen.getByRole("button", { name: "Edit map" }));
    fireEvent.click(screen.getByRole("button", { name: "+ Add item" }));
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Category leader" } });
    fireEvent.change(screen.getByLabelText("Horizon name"), { target: { value: "10-Year Target" } });
    fireEvent.change(screen.getByLabelText("Horizon count"), { target: { value: "10" } });
    fireEvent.click(screen.getByRole("button", { name: "Save Strategy item" }));
    expect(state.createItem).toHaveBeenCalledWith(expect.objectContaining({ title: "Category leader", horizon_label: "10-Year Target", horizon_count: 10 }), expect.any(Object));
  });

  it("renders a custom named horizon as its own cascade column", () => {
    state.strategy = [item({ title: "Expand Europe", horizon_count: 18, horizon_unit: "month", horizon_label: "FY27 North Star" })];
    render(<StrategyPage />);
    expect(screen.getByRole("heading", { name: "FY27 North Star" })).toBeInTheDocument();
    expect(screen.getByText("Expand Europe")).toBeInTheDocument();
  });

  it("shows a useful History panel", () => {
    state.strategy = [item({ title: "Updated target" })];
    render(<StrategyPage />);
    fireEvent.click(screen.getByRole("button", { name: "History" }));
    expect(screen.getByRole("heading", { name: "Strategy history" })).toBeInTheDocument();
    expect(screen.getAllByText("Updated target")).toHaveLength(2);
  });
});
