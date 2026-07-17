import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Rock } from "../core/types";
import { RocksPage } from "./rocks-page";

const state = vi.hoisted(() => ({
  enabled: true,
  rocks: [] as Rock[],
  terminology: { strategy: "Strategy", rock: "Rock", rocks: "Rocks", vision_plan: "Vision Plan", meetings: "Meetings", org_chart: "Org Chart", scorecard: "Scorecard", issues_list: "Issues List", strategy_map: "Strategy Map" },
  goalTypes: [] as Array<{ id: string; workspace_id: string; name: string; color: string; scope_label: string; position: number; created_at: string; updated_at: string }>,
  rocksLoading: false,
  rocksError: false,
  periods: [{ id: "period-1", workspace_id: "workspace-1", name: "Q3 2026", starts_on: "2026-07-01", ends_on: "2026-09-30" }],
  projects: [{ id: "project-1", title: "Carrier migration", status: "in_progress" }],
  issues: [{ id: "issue-1", identifier: "FIR-2711", title: "Carrier API migration", status: "blocked" }],
  members: [{ id: "member-1", name: "Jesper" }],
  agents: [{ id: "agent-1", name: "Sara" }],
  strategy: [{ id: "strategy-1", title: "Best-in-class logistics cost", kind: "horizon_goal", position: 0 }],
  mutate: vi.fn(),
  checkIn: vi.fn(),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => state.enabled }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/projects", () => ({ projectListOptions: () => ({ queryKey: ["projects"] }) }));
vi.mock("@multica/core/issues/queries", () => ({ issueListOptions: () => ({ queryKey: ["issues"] }) }));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: (options: { queryKey: readonly string[] }) => {
      if (options.queryKey.includes("settings")) return { data: { terminology: state.terminology } };
      if (options.queryKey.includes("rocks")) return { data: { rocks: state.rocks }, isLoading: state.rocksLoading, isError: state.rocksError };
      if (options.queryKey.includes("periods")) return { data: { periods: state.periods } };
      if (options.queryKey.includes("goal-types")) return { data: { goal_types: state.goalTypes } };
      if (options.queryKey.includes("strategy")) return { data: { strategy_items: state.strategy } };
      if (options.queryKey.includes("projects")) return { data: state.projects };
      if (options.queryKey.includes("issues")) return { data: state.issues };
      if (options.queryKey.includes("members")) return { data: state.members };
      if (options.queryKey.includes("agents")) return { data: state.agents };
      return { data: [] };
    },
  };
});

vi.mock("../core/queries", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../core/queries")>();
  return {
    ...actual,
    useSaveRock: () => ({ mutate: state.mutate, isPending: false }),
    useRockCheckIn: () => ({ mutate: state.checkIn, isPending: false }),
  };
});

const rock: Rock = {
  id: "rock-1", workspace_id: "workspace-1", title: "Cut fulfilment cost 12%", description: "Best-in-class logistics",
  owner_type: "agent", owner_id: "agent-1", owner_name: "Sara", period_id: "period-1", period_name: "Q3 2026",
  period_start: "2026-07-01", period_end: "2026-09-30", confidence: 72, reported_health: "at_risk",
  derived_health: { state: "off_track", reason: "1 of 3 Issues are blocked", calculated_at: "2026-07-13T18:00:00Z" },
  health_score: 36, issue_count: 3, done_issue_count: 1, blocked_issue_count: 1, project_count: 1,
  projects: [{ id: "project-1", title: "Carrier migration", issue_count: 3, done_issue_count: 1 }],
  issues: [{ id: "issue-1", identifier: "FIR-2711", title: "Carrier API migration", status: "blocked", project_id: "project-1", project_title: "Carrier migration" }],
  check_ins: [{ id: "check-1", confidence: 36, reported_health: "off_track", note: "Carrier migration blocked", created_by_type: "member", created_by_id: "member-1", created_at: "2026-07-07T12:00:00Z" }],
  strategy_item_id: "strategy-1", strategy_item_title: "Best-in-class logistics cost", created_at: "", updated_at: "",
};

describe("RocksPage", () => {
  beforeEach(() => { state.enabled = true; state.rocks = []; state.goalTypes = []; state.rocksLoading = false; state.rocksError = false; state.mutate.mockReset(); state.checkIn.mockReset(); });

  it("stays hidden when the feature flag is off", () => {
    state.enabled = false;
    expect(render(<RocksPage />).container).toBeEmptyDOMElement();
  });

  it("renders the v4 header, KPI strip and empty state", () => {
    render(<RocksPage />);
    expect(screen.getByRole("heading", { name: "Rocks" })).toBeInTheDocument();
    expect(screen.getByText(/Quarterly priorities · Q3 2026/)).toBeInTheDocument();
    expect(screen.getByText("OVERALL HEALTH")).toBeInTheDocument();
    expect(screen.getByText("ON TRACK")).toBeInTheDocument();
    expect(screen.getByText("NEED ATTENTION")).toBeInTheDocument();
    expect(screen.getByText("DAYS LEFT IN Q3")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "+ New Rock" })).toBeInTheDocument();
  });

  it("communicates loading and error states", () => {
    state.rocksLoading = true;
    const { rerender } = render(<RocksPage />);
    expect(screen.getByText("Loading Rocks…")).toBeInTheDocument();
    state.rocksLoading = false;
    state.rocksError = true;
    rerender(<RocksPage />);
    expect(screen.getByRole("alert")).toHaveTextContent("Rocks could not be loaded.");
  });

  it("renders the Rock table and connected execution details", () => {
    state.rocks = [rock];
    render(<RocksPage />);
    expect(screen.getByText("Cut fulfilment cost 12%")).toBeInTheDocument();
    expect(screen.getByText("↳ 1-Year Plan · Best-in-class logistics cost")).toBeInTheDocument();
    expect(screen.getByText("Sara · agent")).toBeInTheDocument();
    expect(screen.getByText("1/3 issues · 1 project")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "View Cut fulfilment cost 12%" }));
    expect(screen.getByText("Under this Rock — Cut fulfilment cost 12%")).toBeInTheDocument();
    expect(screen.getByText("FIR-2711")).toBeInTheDocument();
    expect(screen.getByText("Weekly check-in history")).toBeInTheDocument();
    expect(screen.getByText(/Carrier migration blocked/)).toBeInTheDocument();
  });

  it("creates a standalone Rock and allows optional Project and Issue links", () => {
    render(<RocksPage />);
    fireEvent.click(screen.getByRole("button", { name: "+ New Rock" }));
    fireEvent.change(screen.getByLabelText("Rock title"), { target: { value: "Independent Rock" } });
    fireEvent.change(screen.getByLabelText("Period"), { target: { value: "period-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Save Rock" }));
    expect(state.mutate).toHaveBeenCalledWith(expect.objectContaining({ input: expect.objectContaining({ title: "Independent Rock", project_ids: [], issue_ids: [] }) }), expect.any(Object));
  });

  it("badges and filters Rocks by their configurable type", () => {
    state.goalTypes = [
      { id: "type-1", workspace_id: "workspace-1", name: "Company", color: "#22C55E", scope_label: "company-wide", position: 0, created_at: "", updated_at: "" },
      { id: "type-2", workspace_id: "workspace-1", name: "Team", color: "#6366F1", scope_label: "", position: 1, created_at: "", updated_at: "" },
    ];
    state.rocks = [{ ...rock, goal_type_id: "type-1", goal_type_name: "Company", goal_type_color: "#22C55E" }, { ...rock, id: "rock-2", title: "Untyped goal", strategy_item_id: undefined, strategy_item_title: undefined }];
    render(<RocksPage />);
    expect(screen.getByRole("group", { name: "Filter by rock type" })).toBeInTheDocument();
    expect(screen.getByText("Untyped goal")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Company/ }));
    expect(screen.queryByText("Untyped goal")).not.toBeInTheDocument();
    expect(screen.getByText("Cut fulfilment cost 12%")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "All" }));
    expect(screen.getByText("Untyped goal")).toBeInTheDocument();
  });

  it("submits the selected type when saving a Rock", () => {
    state.goalTypes = [{ id: "type-1", workspace_id: "workspace-1", name: "Company", color: "#22C55E", scope_label: "", position: 0, created_at: "", updated_at: "" }];
    render(<RocksPage />);
    fireEvent.click(screen.getByRole("button", { name: "+ New Rock" }));
    fireEvent.change(screen.getByLabelText("Rock title"), { target: { value: "Typed goal" } });
    fireEvent.change(screen.getByLabelText("Type"), { target: { value: "type-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Save Rock" }));
    expect(state.mutate).toHaveBeenCalledWith(expect.objectContaining({ input: expect.objectContaining({ title: "Typed goal", goal_type_id: "type-1" }) }), expect.any(Object));
  });

  it("records a weekly check-in directly from the selected Rock", () => {
    state.rocks = [rock];
    render(<RocksPage />);
    fireEvent.click(screen.getByRole("button", { name: "View Cut fulfilment cost 12%" }));
    fireEvent.change(screen.getByLabelText("Check-in note"), { target: { value: "Decision needed" } });
    fireEvent.click(screen.getByRole("button", { name: "Save check-in" }));
    expect(state.checkIn).toHaveBeenCalledWith(expect.objectContaining({ id: "rock-1", input: expect.objectContaining({ note: "Decision needed" }) }), expect.any(Object));
  });
});
