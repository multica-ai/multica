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
  createConnection: vi.fn(),
  updateSettings: vi.fn(),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => state.enabled }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: (options: { queryKey: readonly string[] }) => {
      if (options.queryKey.includes("settings")) return { data: { terminology: state.terminology } };
      if (options.queryKey.includes("strategy")) return { data: { strategy_items: state.strategy }, isLoading: state.strategyLoading, isError: state.strategyError };
      if (options.queryKey.includes("rocks")) return { data: { rocks: state.rocks } };
      return { data: { connections: [{ source_id: "ten", target_type: "rock", target_id: "project-1" }] } };
    },
  };
});

vi.mock("../core/queries", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../core/queries")>();
  return {
    ...actual,
    useCreateStrategyItem: () => ({ mutateAsync: state.createItem, isPending: false }),
    useCreateConnection: () => ({ mutateAsync: state.createConnection, isPending: false }),
    useUpdateSettings: () => ({ mutate: state.updateSettings, isPending: false }),
  };
});

const item = (partial: Partial<StrategyItem>): StrategyItem => ({
  id: partial.id ?? "strategy-1",
  workspace_id: "workspace-1",
  kind: partial.kind ?? "horizon_goal",
  title: partial.title ?? "Become the most trusted operator",
  description: partial.description ?? "",
  horizon_count: partial.horizon_count,
  horizon_unit: partial.horizon_unit,
  position: partial.position ?? 0,
  state: partial.state ?? "active",
  created_at: "2026-07-01T00:00:00Z",
  updated_at: "2026-07-01T00:00:00Z",
});

const rock: Rock = {
  project_id: "project-1", workspace_id: "workspace-1", project_title: "Win Norway",
  project_status: "active", period_start: "2026-07-01", period_end: "2026-09-30",
  confidence: 75, reported_health: "on_track",
  derived_health: { state: "at_risk", reason: "Execution is in progress", calculated_at: "2026-07-13T00:00:00Z" },
  issue_count: 4, done_issue_count: 1, blocked_issue_count: 0,
  created_at: "2026-07-01T00:00:00Z", updated_at: "2026-07-01T00:00:00Z",
};

describe("StrategyPage", () => {
  beforeEach(() => {
    state.enabled = true;
    state.strategyLoading = false;
    state.strategyError = false;
    state.strategy = [];
    state.rocks = [];
    state.terminology = { strategy: "Strategy", rock: "Rock", rocks: "Rocks" };
    state.createItem.mockReset();
    state.createConnection.mockReset();
    state.updateSettings.mockReset();
  });

  it("renders flag, loading, error, and custom terminology states", () => {
    state.enabled = false;
    const { container, rerender } = render(<StrategyPage />);
    expect(container).toBeEmptyDOMElement();

    state.enabled = true;
    state.strategyLoading = true;
    rerender(<StrategyPage />);
    expect(screen.getByText("Loading Strategy…")).toBeInTheDocument();

    state.strategyLoading = false;
    state.strategyError = true;
    rerender(<StrategyPage />);
    expect(screen.getByRole("alert")).toHaveTextContent("Strategy could not be loaded");

    state.strategyError = false;
    state.terminology = { strategy: "Direction", rock: "Priority", rocks: "Priorities" };
    rerender(<StrategyPage />);
    expect(screen.getAllByText("Shape your Direction")).toHaveLength(2);
    expect(screen.getByText("Connected Priorities")).toBeInTheDocument();
  });

  it("orders Core Values, Core Focus, arbitrary horizons, and current Rocks", () => {
    state.strategy = [
      item({ id: "value", kind: "core_value", title: "Own the outcome", position: 0 }),
      item({ id: "focus", kind: "core_focus", title: "AI-native commerce", position: 1 }),
      item({ id: "ten", title: "Nordic category leader", horizon_count: 10, horizon_unit: "year", position: 2 }),
      item({ id: "weeks", title: "Prove the loop", horizon_count: 6, horizon_unit: "week", position: 3 }),
      item({ id: "unknown", kind: "unknown", title: "Future server value", position: 4 }),
    ];
    state.rocks = [rock];
    render(<StrategyPage />);

    expect(screen.getByRole("heading", { name: "Core Values" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Core Focus" })).toBeInTheDocument();
    expect(screen.getByText("10 years")).toBeInTheDocument();
    expect(screen.getByText("6 weeks")).toBeInTheDocument();
    expect(screen.getAllByText("Win Norway").length).toBeGreaterThan(0);
    expect(screen.getByText("Future server value")).toBeInTheDocument();
  });

  it("creates an arbitrary horizon and connects it to an existing Rock", async () => {
    state.rocks = [rock];
    state.createItem.mockResolvedValue(item({ id: "new-strategy" }));
    state.createConnection.mockResolvedValue(undefined);
    render(<StrategyPage />);

    fireEvent.click(screen.getByRole("button", { name: "Add Strategy item" }));
    fireEvent.change(screen.getByLabelText("Type"), { target: { value: "horizon_goal" } });
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Build the operating advantage" } });
    fireEvent.change(screen.getByLabelText("Horizon count"), { target: { value: "18" } });
    fireEvent.change(screen.getByLabelText("Horizon unit"), { target: { value: "month" } });
    fireEvent.change(screen.getByLabelText("Connected Rock"), { target: { value: "project-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Save Strategy item" }));

    expect(state.createItem).toHaveBeenCalledWith(expect.objectContaining({
      kind: "horizon_goal", title: "Build the operating advantage", horizon_count: 18, horizon_unit: "month",
    }));
    await vi.waitFor(() => expect(state.createConnection).toHaveBeenCalledWith({
      source_type: "strategy_item",
      source_id: "new-strategy",
      target_type: "rock",
      target_id: "project-1",
      relationship_type: "supports",
      provenance: "manual",
    }));
  });

  it("updates customer terminology without changing canonical data names", () => {
    render(<StrategyPage />);
    fireEvent.click(screen.getByRole("button", { name: "Customize labels" }));
    fireEvent.change(screen.getByLabelText("Strategy label"), { target: { value: "Direction" } });
    fireEvent.change(screen.getByLabelText("Rock label"), { target: { value: "Priority" } });
    fireEvent.change(screen.getByLabelText("Rocks label"), { target: { value: "Priorities" } });
    fireEvent.click(screen.getByRole("button", { name: "Save labels" }));
    expect(state.updateSettings).toHaveBeenCalledWith(
      { strategy: "Direction", rock: "Priority", rocks: "Priorities" },
      expect.any(Object),
    );
  });
});
