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
    render(<AnalyticsWorkbench visuals={DEFAULT_ANALYTICS_VISUALS} results={results} filters={[]} onFilter={onFilter} onNext={onNext} onAddVisual={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Include provider openai" }));
    expect(onFilter).toHaveBeenCalledWith("provider", "openai", "in");
    fireEvent.click(screen.getByRole("button", { name: "Next Activity page" }));
    expect(onNext).toHaveBeenCalledWith("activity", "2026-07-11T00:00:00Z");
  });

  it("opens New visual and creates a visual from catalog fields", () => {
    const onAddVisual = vi.fn();
    render(<AnalyticsWorkbench visuals={DEFAULT_ANALYTICS_VISUALS} results={results} filters={[]} onFilter={vi.fn()} onNext={vi.fn()} onAddVisual={onAddVisual} />);
    fireEvent.click(screen.getByRole("button", { name: "New visual" }));
    fireEvent.change(screen.getByLabelText("Visual title"), { target: { value: "Models by cost" } });
    fireEvent.click(screen.getByRole("button", { name: "Create visual" }));
    expect(onAddVisual).toHaveBeenCalledWith(expect.objectContaining({ title: "Models by cost", metrics: ["runs"], dimensions: ["time"] }));
  });
});
