// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { IssueMetricsSection } from "./issue-metrics";

describe("IssueMetricsSection", () => {
  it("renders the label breakdown chips (plan §2.2E severity-by-label)", () => {
    render(
      <IssueMetricsSection
        issues={{
          openIssues: 3,
          closedLast7d: 1,
          avgResolutionHours: 4.5,
          dailyOpenCounts: [],
          labelBreakdown: [
            { name: "bug", color: "#c96442", count: 2 },
            { name: "feature", color: "#5e5d59", count: 1 },
          ],
        }}
      />,
    );
    expect(screen.getByText("bug · 2")).toBeInTheDocument();
    expect(screen.getByText("feature · 1")).toBeInTheDocument();
  });

  it("omits the breakdown list entirely when there are no labels", () => {
    render(
      <IssueMetricsSection
        issues={{
          openIssues: 0,
          closedLast7d: 0,
          avgResolutionHours: null,
          dailyOpenCounts: [],
          labelBreakdown: [],
        }}
      />,
    );
    expect(screen.queryByRole("list")).not.toBeInTheDocument();
  });
});
