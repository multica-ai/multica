// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { DerivedInsights } from "./derived-insights";
import type { DerivedInsights as DerivedInsightsData } from "@/lib/types";

describe("DerivedInsights", () => {
  it("renders the health label with a hover trigger explaining what it means", () => {
    const insights: DerivedInsightsData = { successRate: 82, health: "warning" };
    render(<DerivedInsights insights={insights} />);
    expect(screen.getByText("Needs attention")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: 'What does "Needs attention" mean?' }),
    ).toBeInTheDocument();
  });

  it("renders a distinct explanation trigger for each health tier", () => {
    const good: DerivedInsightsData = { successRate: 95, health: "good" };
    render(<DerivedInsights insights={good} />);
    expect(screen.getByRole("button", { name: 'What does "Good" mean?' })).toBeInTheDocument();
  });

  it("renders the not-enough-data state alongside the health badge", () => {
    const insights: DerivedInsightsData = { successRate: null, health: "good" };
    render(<DerivedInsights insights={insights} />);
    expect(screen.getByText("Not enough data")).toBeInTheDocument();
  });
});
