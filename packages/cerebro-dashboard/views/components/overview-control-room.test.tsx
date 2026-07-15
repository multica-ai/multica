// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DashboardOverview } from "../../core/api";
import { OverviewControlRoom } from "./overview-control-room";

vi.mock("./analytics-dashboard", () => ({
  AnalyticsDashboard: () => <div>Analytics visuals</div>,
}));

afterEach(cleanup);

const overview = {
  issues_created: { value: 12, prior: 8 },
  issues_completed: { value: 9, prior: 7 },
  tasks_completed: { value: 20, prior: 16 },
  tasks_failed: { value: 2, prior: 3 },
  agents_active: { value: 4, prior: 3 },
  members_active: { value: 6, prior: 5 },
  spend_cents: { value: 1234, prior: 1000 },
  issues_by_status: [{ key: "done", count: 9 }],
  issues_by_priority: [{ key: "high", count: 3 }],
  top_agents: [{ id: "agent-1", name: "Lone", count: 7 }],
  top_members: [{ id: "member-1", name: "Jesper", count: 5 }],
  timeline: [{ day: "2026-07-15", issues_created: 2, issues_done: 1, tasks_completed: 4, tasks_failed: 0 }],
  activity_feed: [],
  recent_tasks: [],
} as unknown as DashboardOverview;

describe("OverviewControlRoom", () => {
  it("renders the new operational overview without legacy section labels", () => {
    render(
      <OverviewControlRoom
        workspaceId="workspace-1"
        data={overview}
        isLoading={false}
        filters={[]}
        onFiltersChange={vi.fn()}
        onNewVisual={vi.fn()}
        onSelectActor={vi.fn()}
      />,
    );

    expect(screen.getByRole("region", { name: "Workspace pulse" })).toBeTruthy();
    expect(screen.getByText("Work throughput")).toBeTruthy();
    expect(screen.getByText("People driving work")).toBeTruthy();
    expect(screen.queryByLabelText("KPIs")).toBeNull();
    expect(screen.queryByLabelText("Breakdown")).toBeNull();
  });

  it("turns a person click into a shared Dashboard filter", () => {
    const onSelectActor = vi.fn();
    render(
      <OverviewControlRoom
        workspaceId="workspace-1"
        data={overview}
        isLoading={false}
        filters={[]}
        onFiltersChange={vi.fn()}
        onNewVisual={vi.fn()}
        onSelectActor={onSelectActor}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Filter Dashboard by Lone" }));
    expect(onSelectActor).toHaveBeenCalledWith("agent-1", "Lone");
  });
});
