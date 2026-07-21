import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { DEFAULT_TERMINOLOGY } from "../core/api-schemas";
import type { Rock, VisionPlanSection } from "../core/types";
import { StrategyMap } from "./strategy-map";

const section = (key: string, name: string, title?: string, goalId?: string): VisionPlanSection => ({
  id: key, workspace_id: "workspace-1", key, name, section_type: "list", position: 0,
  items: title ? [{ id: `${key}-item`, workspace_id: "workspace-1", section_id: key, title, description: "", position: 0, state: "active", goal_connections: goalId ? [{ connection_id: "connection-1", goal_id: goalId }] : [], created_at: "", updated_at: "" }] : [],
  created_at: "", updated_at: "",
});

const rock: Rock = {
  id: "rock-1", workspace_id: "workspace-1", title: "Launch Denmark", period_id: "period-1", period_name: "Q3", period_start: "", period_end: "",
  confidence: 70, reported_health: "at_risk", derived_health: { state: "at_risk", reason: "Late", calculated_at: "" }, health_score: 60,
  issue_count: 3, done_issue_count: 1, blocked_issue_count: 0, project_count: 2, projects: [], issues: [], check_ins: [], created_at: "", updated_at: "",
};

describe("StrategyMap", () => {
  it("renders direction through execution in order and opens canonical records", () => {
    const edit = vi.fn();
    const openRock = vi.fn();
    render(<StrategyMap sections={[
      section("ten-year-target", "Long-term direction", "Lead Scandinavia"),
      section("three-year-picture", "Mid-term picture", "Three profitable markets"),
      section("one-year-plan", "One-Year Plan", "Launch Denmark", "rock-1"),
    ]} rocks={[rock]} terminology={DEFAULT_TERMINOLOGY} onEditSection={edit} onOpenRock={openRock} />);

    const bands = screen.getAllByRole("region");
    expect(bands.slice(1).map((band) => band.getAttribute("aria-label"))).toEqual(["Long-term direction", "Mid-term picture", "One-Year Plan", "Goals"]);
    expect(screen.getByRole("button", { name: "Open Goal Launch Denmark" })).toBeInTheDocument();
    expect(screen.getByText("2 projects · 3 issues")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Edit Mid-term picture" }));
    fireEvent.click(screen.getByRole("button", { name: "Open Goal Launch Denmark" }));
    expect(edit).toHaveBeenCalledWith("three-year-picture");
    expect(openRock).toHaveBeenCalledWith("rock-1");
  });

  it("shows actionable gaps without inventing records", () => {
    const edit = vi.fn();
    render(<StrategyMap sections={[]} rocks={[]} terminology={{ ...DEFAULT_TERMINOLOGY, rocks: "Priorities" }} onEditSection={edit} onOpenRock={vi.fn()} />);
    expect(screen.getAllByText("Add direction")).toHaveLength(3);
    expect(screen.getByText("No priorities connected yet")).toBeInTheDocument();
    fireEvent.click(screen.getAllByRole("button", { name: /Add .* direction/i })[0]!);
    expect(edit).toHaveBeenCalled();
  });
});
