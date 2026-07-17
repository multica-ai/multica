import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { OperatingPeriod, Rock } from "../core/types";
import { RocksTimeline } from "./rocks-timeline";

const terminology = { strategy: "Strategy", rock: "Rock", rocks: "Rocks", vision_plan: "Vision Plan", meetings: "Meetings", org_chart: "Org Chart", scorecard: "Scorecard", issues_list: "Issues List", strategy_map: "Strategy Map" };

const periods: OperatingPeriod[] = [
  { id: "period-2", workspace_id: "workspace-1", name: "Q4 2026", unit: "quarter", starts_on: "2026-10-01", ends_on: "2026-12-31" },
  { id: "period-1", workspace_id: "workspace-1", name: "Q3 2026", unit: "quarter", starts_on: "2026-07-01", ends_on: "2026-09-30" },
];

const baseRock: Rock = {
  id: "rock-1", workspace_id: "workspace-1", title: "Cut fulfilment cost 12%", description: "",
  owner_type: "agent", owner_id: "agent-1", owner_name: "Sara", period_id: "period-1", period_name: "Q3 2026",
  period_start: "2026-07-01", period_end: "2026-09-30", confidence: 72, reported_health: "at_risk",
  derived_health: { state: "on_track", reason: "", calculated_at: "" }, health_score: 80,
  issue_count: 0, done_issue_count: 0, blocked_issue_count: 0, project_count: 0,
  projects: [], issues: [], check_ins: [], created_at: "", updated_at: "",
};

describe("RocksTimeline", () => {
  it("renders period columns chronologically with empty future periods visible", () => {
    render(<RocksTimeline rocks={[baseRock]} periods={periods} terminology={terminology} groupBy="owner" onSelect={vi.fn()} />);
    const headers = screen.getAllByText(/Q[34] 2026/);
    expect(headers[0]).toHaveTextContent("Q3 2026");
    expect(headers[1]).toHaveTextContent("Q4 2026");
    expect(screen.getByText("Sara")).toBeInTheDocument();
    expect(screen.getByText("Cut fulfilment cost 12%")).toBeInTheDocument();
  });

  it("groups by type and opens a rock on click", () => {
    const onSelect = vi.fn();
    render(<RocksTimeline rocks={[{ ...baseRock, goal_type_name: "Company" }, { ...baseRock, id: "rock-2", title: "Second goal", period_id: "period-2" }]} periods={periods} terminology={terminology} groupBy="type" onSelect={onSelect} />);
    expect(screen.getByText("Company")).toBeInTheDocument();
    expect(screen.getByText("No type")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open Cut fulfilment cost 12%" }));
    expect(onSelect).toHaveBeenCalledWith("rock-1");
  });

  it("shows an empty state when no periods exist", () => {
    render(<RocksTimeline rocks={[]} periods={[]} terminology={terminology} groupBy="owner" onSelect={vi.fn()} />);
    expect(screen.getByText(/No periods defined yet/)).toBeInTheDocument();
  });
});
