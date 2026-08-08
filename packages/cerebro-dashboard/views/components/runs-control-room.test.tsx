// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AnalyticsFilter } from "../../core/analytics";
import {
  ActivityHeatmap,
  Donut,
  PanelFooterPager,
  PanelPager,
  PeopleTable,
  RunsControlRoom,
  TimeSeries,
  TimeSeriesPoint,
  withJudgeGateVerdictFilter,
} from "./runs-control-room";

const { useQueriesMock } = vi.hoisted(() => ({
  useQueriesMock: vi.fn(),
}));

vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
    "@tanstack/react-query",
  );
  return { ...actual, useQueries: useQueriesMock };
});

vi.mock("recharts", async () => {
  const actual = await vi.importActual<typeof import("recharts")>("recharts");
  const React = await import("react");
  return {
    ...actual,
    ResponsiveContainer: ({ children }: { children: React.ReactElement }) =>
      React.cloneElement(
        children as React.ReactElement<{ height?: number; width?: number }>,
        { width: 800, height: 176 },
      ),
  };
});

afterEach(cleanup);

describe("ActivityHeatmap", () => {
  it("lays out day buckets as a GitHub-style weekday-by-week grid", () => {
    const onFilter = vi.fn();
    render(
      <ActivityHeatmap
        grain="day"
        result={{
          columns: ["time", "runs"],
          rows: [
            { time: "2026-07-13T00:00:00Z", runs: 1 },
            { time: "2026-07-15T00:00:00Z", runs: 4 },
          ],
        }}
        onFilter={onFilter}
      />,
    );

    expect(screen.getByRole("grid", { name: "Activity by day" })).toBeTruthy();
    expect(screen.getByText("Mon")).toBeTruthy();
    expect(screen.getByText("Fri")).toBeTruthy();
    expect(screen.getByText("Less")).toBeTruthy();
    expect(screen.getByText("More")).toBeTruthy();
    // 2026-07-14 has no data but stays in the grid as an empty calendar cell.
    expect(screen.getByTitle("Jul 14 · 0 runs")).toBeTruthy();
    expect(screen.getByTitle("Jul 15 · 4 runs").className).toContain("bg-[#6557d8]");
    fireEvent.click(screen.getByTitle("Jul 15 · 4 runs"));
    expect(onFilter).toHaveBeenCalledWith("2026-07-15T00:00:00.000Z");
  });

  it("lays out hour buckets as hour rows under day columns", () => {
    render(
      <ActivityHeatmap
        grain="hour"
        result={{ columns: ["time", "runs"], rows: [{ time: "2026-07-13T18:00:00Z", runs: 2 }] }}
        onFilter={vi.fn()}
      />,
    );

    expect(screen.getByRole("grid", { name: "Activity by hour" })).toBeTruthy();
    expect(screen.getByText("00")).toBeTruthy();
    expect(screen.getByText("23")).toBeTruthy();
    expect(screen.getByTitle(/2 runs/).className).toContain("bg-[#6557d8]");
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

  it("renders accessible judge outcome controls backed by stored verdict values", () => {
    const onFilter = vi.fn();
    render(<Donut value={0.82} onFilter={onFilter} />);

    const ring = screen.getByLabelText("Judge gate outcome 82%");
    expect([...ring.querySelectorAll("circle")].map((circle) => circle.getAttribute("stroke"))).toEqual([
      "#ff3b68", "#f0b429", "#00a56a",
    ]);
    fireEvent.click(screen.getByRole("button", { name: "Filter runs by passing judge gates" }));
    expect(onFilter).toHaveBeenCalledWith("pass");
    fireEvent.click(screen.getByRole("button", { name: "Filter runs by failed judge gates" }));
    expect(onFilter).toHaveBeenLastCalledWith("fail");
  });

  it("scopes judge outcome controls to judge gates without disturbing other filters", () => {
    expect(withJudgeGateVerdictFilter([
      { dimension: "provider", operator: "in", values: ["openai"] },
      { dimension: "quality_type", operator: "in", values: ["skill_observation"] },
      { dimension: "quality_verdict", operator: "in", values: ["success"] },
    ], "fail")).toEqual([
      { dimension: "provider", operator: "in", values: ["openai"] },
      { dimension: "quality_type", operator: "in", values: ["judge_gate"] },
      { dimension: "quality_verdict", operator: "in", values: ["fail"] },
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

  it("turns each visible time-series point into a mouse and keyboard control", () => {
    const onTimeFilter = vi.fn();
    render(
      <svg>
        <TimeSeriesPoint
          cx={20}
          cy={30}
          rawTime="2026-07-13T00:00:00Z"
          series="Runs"
          value={8}
          color="#8b7cf6"
          onTimeFilter={onTimeFilter}
        />
      </svg>,
    );

    const point = screen.getByRole("button", {
      name: "Filter Runs & cost over time by 13 Jul 2026, Runs 8",
    });
    fireEvent.click(point);
    fireEvent.keyDown(point, { key: "Enter" });
    fireEvent.keyDown(point, { key: " " });
    expect(onTimeFilter).toHaveBeenNthCalledWith(1, "2026-07-13T00:00:00Z");
    expect(onTimeFilter).toHaveBeenNthCalledWith(2, "2026-07-13T00:00:00Z");
    expect(onTimeFilter).toHaveBeenNthCalledWith(3, "2026-07-13T00:00:00Z");
  });

  it("updates shared filters and resets Runs pagination after a point click", () => {
    const emptyResult = { columns: [], rows: [] };
    useQueriesMock.mockReturnValue(
      Array.from({ length: 13 }, (_, index) => ({
        data: index === 3
          ? {
              columns: ["time", "runs", "saved_cents"],
              rows: [{ time: "2026-07-13T00:00:00Z", runs: 8, saved_cents: 50 }],
              next_cursor: "next-page",
            }
          : emptyResult,
        isLoading: false,
      })),
    );

    function Harness() {
      const [filters, setFilters] = useState<AnalyticsFilter[]>([
        { dimension: "provider", operator: "in", values: ["openai"] },
        { dimension: "time", operator: "gte", values: ["2026-07-01T00:00:00.000Z"] },
        { dimension: "time", operator: "lte", values: ["2026-07-31T00:00:00.000Z"] },
      ]);
      return (
        <>
          <RunsControlRoom
            workspaceId="workspace-1"
            filters={filters}
            onFiltersChange={setFilters}
            onNewVisual={vi.fn()}
          />
          <output aria-label="Shared filters">{JSON.stringify(filters)}</output>
        </>
      );
    }

    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <Harness />
      </QueryClientProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Next Runs and cost page" }));
    expect(screen.getByText("Page 2")).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", {
        name: "Filter Runs & cost over time by 13 Jul 2026, Runs 8",
      }),
    );

    expect(screen.queryByText("Page 2")).toBeNull();
    const filters = screen.getByLabelText("Shared filters").textContent ?? "";
    expect(filters).toContain('"provider"');
    expect(filters).not.toContain("2026-07-01T00:00:00.000Z");
    expect(filters).not.toContain("2026-07-31T00:00:00.000Z");
    expect(filters).toContain(new Date(2026, 6, 13).toISOString());
    expect(filters).toContain(new Date(2026, 6, 14).toISOString());
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
