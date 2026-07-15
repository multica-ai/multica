// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ActivityHeatmap, Donut, PanelFooterPager, PanelPager, PeopleTable, TimeSeries } from "./runs-control-room";

afterEach(cleanup);

describe("ActivityHeatmap", () => {
  it("keeps sparse activity in fixed-width time buckets", () => {
    render(
      <ActivityHeatmap
        result={{ columns: ["time", "runs"], rows: [{ time: "2026-07-13T18:00:00Z", runs: 1 }] }}
        onFilter={vi.fn()}
      />,
    );

    expect(screen.getByRole("grid", { name: "Activity by time bucket" }).style.gridTemplateColumns).toBe(
      "repeat(42, minmax(0, 1fr))",
    );
    expect(screen.getByTitle(/1 runs/).className).toContain("bg-[#6557d8]");
  });

  it("renders the mockup configure and pagination controls", () => {
    const onPrevious = vi.fn();
    const onNext = vi.fn();
    const onConfigure = vi.fn();
    render(
      <PanelPager
        page={1}
        canPrevious={false}
        canNext
        onPrevious={onPrevious}
        onNext={onNext}
        onConfigure={onConfigure}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Configure Activity grid" }));
    fireEvent.click(screen.getByRole("button", { name: "Next Activity grid page" }));
    expect(onConfigure).toHaveBeenCalledOnce();
    expect(onNext).toHaveBeenCalledOnce();
    expect(screen.getByText("Page 1")).toBeTruthy();
  });

  it("renders the activity range explanation beside its footer pager", () => {
    render(
      <PanelFooterPager
        label="Activity grid"
        page={1}
        canPrevious={false}
        canNext
        onPrevious={vi.fn()}
        onNext={vi.fn()}
        summary="Showing hours 00–23 across 30 days"
      />,
    );

    expect(screen.getByText("Showing hours 00–23 across 30 days")).toBeTruthy();
  });

  it("renders a visible three-color judge outcome ring", () => {
    render(<Donut value={0.82} />);

    const ring = screen.getByLabelText("Judge gate outcome 82%");
    expect([...ring.querySelectorAll("circle")].map((circle) => circle.getAttribute("stroke"))).toEqual([
      "#ff3b68", "#f0b429", "#00a56a",
    ]);
  });

  it("renders the mockup line-chart legend instead of raw axis labels", () => {
    render(
      <TimeSeries
        result={{
          columns: ["time", "runs", "saved_cents"],
          rows: [
            { time: "2026-07-12T00:00:00Z", runs: 4, saved_cents: 25 },
            { time: "2026-07-13T00:00:00Z", runs: 8, saved_cents: 50 },
          ],
        }}
      />,
    );

    expect(screen.getByText("Runs")).toBeTruthy();
    expect(screen.getByText("Savings")).toBeTruthy();
    expect(screen.getByLabelText("Runs and savings trend")).toBeTruthy();
  });

  it("shows a colored initial before each person", () => {
    render(
      <PeopleTable
        result={{
          columns: ["person", "project", "runs", "cost_cents", "quality_pass_rate"],
          rows: [{ person: "Lone", project: "Dashboard", runs: 3, cost_cents: 42, quality_pass_rate: 1 }],
        }}
        onFilter={vi.fn()}
      />,
    );

    expect(screen.getByText("L", { selector: "span" }).className).toContain("rounded-full");
  });
});
