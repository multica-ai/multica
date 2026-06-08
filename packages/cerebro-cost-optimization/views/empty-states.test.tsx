import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { CostOptimizationAnalyticsTab } from "./analytics-tab";
import { CostOptimizationInspectorTab } from "./inspector-tab";

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({
    repos: [{ url: "https://github.com/firtal-group/firtal-cerebro" }],
  }),
}));

vi.mock("./dashboard", () => ({
  CostOptimizationDashboard: () => <div>holdout-dashboard</div>,
}));

vi.mock("./api", () => ({
  useCostOptimizationAnalyticsSummaryQuery: () => ({
    data: [],
    isError: false,
  }),
  useCostOptimizationAnalyticsIssuesQuery: () => ({
    data: [],
    isLoading: false,
    isError: false,
  }),
  useCostOptimizationAnalyticsRunsQuery: () => ({
    data: [],
    isLoading: false,
    isError: false,
  }),
  useCostOptimizationPromptInspectorQuery: () => ({
    data: {
      inspection: {
        sections: [],
        duplication: {
          totalTokens: 0,
          duplicateTokens: 0,
          duplicatePct: 0,
          topOverlapSections: [],
        },
      },
    },
    isLoading: false,
    isError: false,
  }),
}));

describe("Cost optimization empty states", () => {
  it("shows analytics rows from the registry and an empty issue/run state", () => {
    render(<CostOptimizationAnalyticsTab />);

    expect(screen.getByText("holdout-dashboard")).toBeInTheDocument();
    expect(screen.getByText("Snapshot in start prompt")).toBeInTheDocument();
    expect(screen.getByText("Context duplication")).toBeInTheDocument();
    expect(
      screen.getByText("No measured runs for this saving in the last 30 days."),
    ).toBeInTheDocument();
    expect(screen.getByText("Select an issue to list runs.")).toBeInTheDocument();
  });

  it("shows inspector empty state and keeps copy disabled without layers", () => {
    render(<CostOptimizationInspectorTab />);

    expect(
      screen.getByText("No layers returned for this workspace."),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Copy as markdown" }),
    ).toBeDisabled();
  });
});
