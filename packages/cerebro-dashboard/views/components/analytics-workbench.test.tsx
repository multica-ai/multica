// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AnalyticsWorkbench } from "./analytics-workbench";
import { DEFAULT_ANALYTICS_VISUALS } from "../../core/analytics";

const results = {
  activity: { columns: ["time", "runs", "cost_cents", "saved_cents"], rows: [{ time: "2026-07-12T00:00:00Z", runs: 8, cost_cents: 42, saved_cents: 12 }], next_cursor: "2026-07-11T00:00:00Z" },
  people: { columns: ["person", "project", "runs"], rows: [{ person: "Lone", project: "Multica", runs: 8 }] },
  models: { columns: ["provider", "model", "skill", "runs"], rows: [{ provider: "openai", model: "gpt-5", skill: "TDD", runs: 8 }] },
  quality: { columns: ["quality_category", "quality_type", "source", "context", "runs"], rows: [{ quality_category: "correctness", quality_type: "review", source: "issue", context: "issue", runs: 8 }] },
};

afterEach(cleanup);

describe("AnalyticsWorkbench", () => {
  it("uses data cells as shared filters and paginates each block", () => {
    const onFilter = vi.fn();
    const onNext = vi.fn();
    render(<AnalyticsWorkbench visuals={DEFAULT_ANALYTICS_VISUALS} results={results} filters={[]} onFilter={onFilter} onRemoveFilter={vi.fn()} onNext={onNext} onAddVisual={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Include provider openai" }));
    expect(onFilter).toHaveBeenCalledWith("provider", "openai", "in");
    fireEvent.click(screen.getByRole("button", { name: "Next Activity page" }));
    expect(onNext).toHaveBeenCalledWith("activity", "2026-07-11T00:00:00Z");
  });

  it("opens New visual and creates a visual from the mockup builder", () => {
    const onAddVisual = vi.fn();
    render(<AnalyticsWorkbench visuals={DEFAULT_ANALYTICS_VISUALS} results={results} filters={[]} onFilter={vi.fn()} onRemoveFilter={vi.fn()} onNext={vi.fn()} onAddVisual={onAddVisual} />);
    fireEvent.click(screen.getByRole("button", { name: "New visual" }));
    fireEvent.click(screen.getByRole("button", { name: "Add visual to Dashboard" }));
    expect(onAddVisual).toHaveBeenCalledWith(expect.objectContaining({ title: "Project runs by person", metrics: ["runs"], dimensions: ["project", "person"] }));
  });

  it("renders the controlled mockup builder rail without a duplicate toolbar", () => {
    const onAddVisual = vi.fn();
    const onBuilderOpenChange = vi.fn();
    render(
      <AnalyticsWorkbench
        visuals={[]}
        results={{}}
        filters={[]}
        onFilter={vi.fn()}
        onRemoveFilter={vi.fn()}
        onNext={vi.fn()}
        onAddVisual={onAddVisual}
        showToolbar={false}
        builderOpen
        onBuilderOpenChange={onBuilderOpenChange}
      />,
    );
    expect(screen.queryByRole("button", { name: "New visual" })).toBeNull();
    expect(screen.getByRole("complementary", { name: "New visual" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Add visual to Dashboard" }));
    expect(onAddVisual).toHaveBeenCalledWith(expect.objectContaining({ presentation: "line" }));
    expect(onBuilderOpenChange).toHaveBeenCalledWith(false);
  });

  it("renders saved line and single-metric presentations", () => {
    render(
      <AnalyticsWorkbench
        visuals={[
          { id: "trend", title: "Runs trend", kind: "bars", presentation: "line", metrics: ["runs"], dimensions: ["time"], grain: "day", limit: 12 },
          { id: "total", title: "Total runs", kind: "table", presentation: "metric", metrics: ["runs"], dimensions: [], grain: "none", limit: 1 },
        ]}
        results={{
          trend: { columns: ["time", "runs"], rows: [{ time: "2026-07-12", runs: 3 }, { time: "2026-07-13", runs: 8 }] },
          total: { columns: ["runs"], rows: [{ runs: 11 }] },
        }}
        filters={[]}
        onFilter={vi.fn()}
        onRemoveFilter={vi.fn()}
        onNext={vi.fn()}
        onAddVisual={vi.fn()}
        showToolbar={false}
      />,
    );
    expect(screen.getByRole("img", { name: "Runs trend chart" })).toBeTruthy();
    expect(screen.getByText("11")).toBeTruthy();
  });

  it("removes active range filters from filter chips", () => {
    const onRemoveFilter = vi.fn();
    render(
      <AnalyticsWorkbench
        visuals={DEFAULT_ANALYTICS_VISUALS}
        results={results}
        filters={[{ dimension: "time", operator: "gte", values: ["2026-07-13T00:00:00.000Z"] }]}
        onFilter={vi.fn()}
        onRemoveFilter={onRemoveFilter}
        onNext={vi.fn()}
        onAddVisual={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /time = 2026-07-13T00:00:00.000Z/ }));
    expect(onRemoveFilter).toHaveBeenCalledWith("time", "2026-07-13T00:00:00.000Z", "gte");
  });
});
