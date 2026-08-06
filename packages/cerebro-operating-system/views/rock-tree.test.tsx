import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Rock } from "../core/types";
import { RockTree } from "./rock-tree";

const terminology = { strategy: "Strategy", rock: "Rock", rocks: "Rocks", vision_plan: "Vision Plan", meetings: "Meetings", org_chart: "Org Chart", scorecard: "Scorecard", issues_list: "Issues List", strategy_map: "Strategy Map" };

const rock = (over: Partial<Rock> = {}): Rock => ({
  id: "rock-1", workspace_id: "workspace-1", title: "Cut fulfilment cost 12%", description: "",
  period_id: "period-1", period_name: "Q3 2026", period_start: "2026-07-01", period_end: "2026-09-30",
  confidence: 72, reported_health: "at_risk", derived_health: { state: "on_track", reason: "", calculated_at: "" },
  health_score: 80, issue_count: 0, done_issue_count: 0, blocked_issue_count: 0, project_count: 0,
  projects: [], issues: [], check_ins: [], created_at: "", updated_at: "", ...over,
});

describe("RockTree", () => {
  it("tells the user when nothing is connected", () => {
    render(<RockTree rock={rock()} terminology={terminology} />);
    expect(screen.getByText(/currently stands on its own/)).toBeInTheDocument();
  });

  it("renders projects as branches with their issues nested underneath", () => {
    render(<RockTree rock={rock({
      projects: [{ id: "p1", title: "Warehouse", issue_count: 2, done_issue_count: 1 }],
      issues: [
        { id: "i1", identifier: "FIR-1", title: "Pick faster", status: "in_progress", project_id: "p1" },
        { id: "i2", identifier: "FIR-2", title: "Sub task", status: "done", project_id: "p1", parent_id: "i1" },
      ],
    })} terminology={terminology} />);

    expect(screen.getByRole("tree", { name: "Connected execution" })).toBeInTheDocument();
    const items = screen.getAllByRole("treeitem");
    expect(items.map((item) => item.getAttribute("aria-level"))).toEqual(["1", "2", "3"]);
    expect(screen.getByText("Warehouse")).toBeInTheDocument();
    expect(screen.getByText("Sub task")).toBeInTheDocument();
    expect(screen.getByText("1/2 done")).toBeInTheDocument();
  });

  it("collapses and re-expands a branch", () => {
    render(<RockTree rock={rock({
      projects: [{ id: "p1", title: "Warehouse", issue_count: 1, done_issue_count: 0 }],
      issues: [{ id: "i1", identifier: "FIR-1", title: "Pick faster", status: "todo", project_id: "p1" }],
    })} terminology={terminology} />);

    expect(screen.getByText("Pick faster")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Collapse Warehouse" }));
    expect(screen.queryByText("Pick faster")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Expand Warehouse" }));
    expect(screen.getByText("Pick faster")).toBeInTheDocument();
  });

  it("shows an issue without a connected project at the root", () => {
    render(<RockTree rock={rock({ issues: [{ id: "i9", identifier: "FIR-9", title: "Standalone", status: "todo" }] })} terminology={terminology} />);
    const items = screen.getAllByRole("treeitem");
    expect(items).toHaveLength(1);
    expect(items[0]).toHaveAttribute("aria-level", "1");
    expect(screen.getByText("FIR-9")).toBeInTheDocument();
  });
});
