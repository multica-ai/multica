import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Rock } from "../core/types";
import { RocksPage } from "./rocks-page";

const state = vi.hoisted(() => ({
  enabled: true,
  rocks: [] as Rock[],
  terminology: { strategy: "Strategy", rock: "Rock", rocks: "Rocks", vision_plan: "Vision Plan", meetings: "Meetings", org_chart: "Org Chart", scorecard: "Scorecard", issues_list: "Issues List", strategy_map: "Strategy Map" },
  goalTypes: [] as Array<{ id: string; workspace_id: string; name: string; color: string; scope_label: string; position: number; created_at: string; updated_at: string }>,
  rocksLoading: false,
  rocksError: false,
  periods: [{ id: "period-1", workspace_id: "workspace-1", name: "Q3 2026", unit: "quarter", starts_on: "2026-07-01", ends_on: "2026-09-30" }],
  projects: [{ id: "project-1", title: "Carrier migration", status: "in_progress" }],
  issues: [{ id: "issue-1", identifier: "FIR-2711", title: "Carrier API migration", status: "blocked" }],
  members: [{ id: "member-1", name: "Jesper" }],
  agents: [{ id: "agent-1", name: "Sara" }],
  strategy: [{ id: "strategy-1", title: "Best-in-class logistics cost", kind: "horizon_goal", position: 0 }],
  mutate: vi.fn(),
  checkIn: vi.fn(),
  savePeriod: vi.fn(),
  isMobile: false,
}));

vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => state.enabled }));
vi.mock("@multica/ui/hooks/use-mobile", () => ({ useIsMobile: () => state.isMobile }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/projects", () => ({ projectListOptions: () => ({ queryKey: ["projects"] }) }));
vi.mock("@multica/core/issues/queries", () => ({ issueListOptions: () => ({ queryKey: ["issues"] }) }));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: (options: { queryKey: readonly string[] }) => {
      if (options.queryKey.includes("settings")) return { data: { terminology: state.terminology } };
      if (options.queryKey.includes("rocks")) return { data: { rocks: state.rocks }, isLoading: state.rocksLoading, isError: state.rocksError };
      if (options.queryKey.includes("periods")) return { data: { periods: state.periods } };
      if (options.queryKey.includes("goal-types")) return { data: { goal_types: state.goalTypes } };
      if (options.queryKey.includes("strategy")) return { data: { strategy_items: state.strategy } };
      if (options.queryKey.includes("projects")) return { data: state.projects };
      if (options.queryKey.includes("issues")) return { data: state.issues };
      if (options.queryKey.includes("members")) return { data: state.members };
      if (options.queryKey.includes("agents")) return { data: state.agents };
      return { data: [] };
    },
  };
});

vi.mock("../core/queries", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../core/queries")>();
  return {
    ...actual,
    useSaveRock: () => ({ mutate: state.mutate, isPending: false }),
    useRockCheckIn: () => ({ mutate: state.checkIn, isPending: false }),
    useSavePeriod: () => ({ mutate: state.savePeriod, isPending: false }),
  };
});

const rock: Rock = {
  id: "rock-1", workspace_id: "workspace-1", title: "Cut fulfilment cost 12%", description: "Best-in-class logistics",
  owner_type: "agent", owner_id: "agent-1", owner_name: "Sara", period_id: "period-1", period_name: "Q3 2026",
  period_start: "2026-07-01", period_end: "2026-09-30", confidence: 72, reported_health: "at_risk",
  derived_health: { state: "off_track", reason: "1 of 3 Issues are blocked", calculated_at: "2026-07-13T18:00:00Z" },
  health_score: 36, issue_count: 3, done_issue_count: 1, blocked_issue_count: 1, project_count: 1,
  projects: [{ id: "project-1", title: "Carrier migration", issue_count: 3, done_issue_count: 1 }],
  issues: [{ id: "issue-1", identifier: "FIR-2711", title: "Carrier API migration", status: "blocked", project_id: "project-1", project_title: "Carrier migration" }],
  check_ins: [{ id: "check-1", confidence: 36, reported_health: "off_track", note: "Carrier migration blocked", created_by_type: "member", created_by_id: "member-1", created_at: "2026-07-07T12:00:00Z" }],
  strategy_item_id: "strategy-1", strategy_item_title: "Best-in-class logistics cost", created_at: "", updated_at: "",
};

describe("RocksPage", () => {
  beforeEach(() => { state.enabled = true; state.rocks = []; state.goalTypes = []; state.rocksLoading = false; state.rocksError = false; state.isMobile = false; state.mutate.mockReset(); state.checkIn.mockReset(); state.savePeriod.mockReset(); });

  it("stays hidden when the feature flag is off", () => {
    state.enabled = false;
    expect(render(<RocksPage />).container).toBeEmptyDOMElement();
  });

  it("defaults to the timeline view", () => {
    state.rocks = [rock];
    render(<RocksPage />);
    expect(screen.getByLabelText("Rocks timeline")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Open Cut fulfilment cost 12%" })).toBeInTheDocument();
  });

  it("renders the header, KPI strip, inline add row and empty state", () => {
    render(<RocksPage />);
    fireEvent.click(screen.getByRole("button", { name: "List view" }));
    expect(screen.getByRole("heading", { name: "Rocks" })).toBeInTheDocument();
    expect(screen.getByText(/Priorities · Q3 2026/)).toBeInTheDocument();
    expect(screen.getByText("OVERALL HEALTH")).toBeInTheDocument();
    expect(screen.getByText("ON TRACK")).toBeInTheDocument();
    expect(screen.getByText("NEED ATTENTION")).toBeInTheDocument();
    expect(screen.getByText("DAYS LEFT IN Q3")).toBeInTheDocument();
    expect(screen.getByLabelText("New Rock title")).toBeInTheDocument();
    expect(screen.getByText("No Rocks yet")).toBeInTheDocument();
  });

  it("communicates loading and error states", () => {
    state.rocksLoading = true;
    const { rerender } = render(<RocksPage />);
    fireEvent.click(screen.getByRole("button", { name: "List view" }));
    expect(screen.getByText("Loading Rocks…")).toBeInTheDocument();
    state.rocksLoading = false;
    state.rocksError = true;
    rerender(<RocksPage />);
    expect(screen.getByRole("alert")).toHaveTextContent("Rocks could not be loaded.");
  });

  it("renders the Rock table and connected execution details", () => {
    state.rocks = [rock];
    render(<RocksPage />);
    fireEvent.click(screen.getByRole("button", { name: "List view" }));
    expect(screen.getByRole("button", { name: "View Cut fulfilment cost 12%" })).toBeInTheDocument();
    expect(screen.getByText("↳ 1-Year Plan · Best-in-class logistics cost")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cut fulfilment cost 12% owner" })).toHaveTextContent("Sara");
    expect(screen.getByRole("button", { name: "Cut fulfilment cost 12% period" })).toHaveTextContent("Q3 2026");
    expect(screen.getByText("1/3 issues · 1 project")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "View Cut fulfilment cost 12%" }));
    expect(screen.getByText("Under this Rock — Cut fulfilment cost 12%")).toBeInTheDocument();
    expect(screen.getByText("FIR-2711")).toBeInTheDocument();
    expect(screen.getByText("Weekly check-in history")).toBeInTheDocument();
    expect(screen.getByText(/Carrier migration blocked/)).toBeInTheDocument();
  });

  it("creates a Rock inline from the add row with Enter", () => {
    render(<RocksPage />);
    fireEvent.click(screen.getByRole("button", { name: "List view" }));
    fireEvent.change(screen.getByLabelText("New Rock title"), { target: { value: "Independent Rock" } });
    fireEvent.keyDown(screen.getByLabelText("New Rock title"), { key: "Enter" });
    expect(state.mutate).toHaveBeenCalledWith(
      expect.objectContaining({ input: expect.objectContaining({ title: "Independent Rock", period_id: "period-1", project_ids: [], issue_ids: [] }) }),
      expect.any(Object),
    );
  });

  it("creates a typed Rock via the inline type picker", () => {
    state.goalTypes = [{ id: "type-1", workspace_id: "workspace-1", name: "Company", color: "#22C55E", scope_label: "", position: 0, created_at: "", updated_at: "" }];
    render(<RocksPage />);
    fireEvent.click(screen.getByRole("button", { name: "List view" }));
    fireEvent.change(screen.getByLabelText("New Rock title"), { target: { value: "Typed goal" } });
    fireEvent.click(screen.getByRole("button", { name: "New Rock type" }));
    fireEvent.click(screen.getByRole("option", { name: /Company/ }));
    fireEvent.click(screen.getByRole("button", { name: "Add Rock" }));
    expect(state.mutate).toHaveBeenCalledWith(
      expect.objectContaining({ input: expect.objectContaining({ title: "Typed goal", goal_type_id: "type-1" }) }),
      expect.any(Object),
    );
  });

  it("badges and filters Rocks by their configurable type", () => {
    state.goalTypes = [
      { id: "type-1", workspace_id: "workspace-1", name: "Company", color: "#22C55E", scope_label: "company-wide", position: 0, created_at: "", updated_at: "" },
      { id: "type-2", workspace_id: "workspace-1", name: "Team", color: "#6366F1", scope_label: "", position: 1, created_at: "", updated_at: "" },
    ];
    state.rocks = [{ ...rock, goal_type_id: "type-1", goal_type_name: "Company", goal_type_color: "#22C55E" }, { ...rock, id: "rock-2", title: "Untyped goal", strategy_item_id: undefined, strategy_item_title: undefined }];
    render(<RocksPage />);
    const filterGroup = screen.getByRole("group", { name: "Filter by rock type" });
    expect(filterGroup).toBeInTheDocument();
    expect(screen.getByText("Untyped goal")).toBeInTheDocument();
    fireEvent.click(screen.getAllByRole("button", { name: /Company/ })[0]!);
    expect(screen.queryByText("Untyped goal")).not.toBeInTheDocument();
    expect(screen.getByText("Cut fulfilment cost 12%")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "All" }));
    expect(screen.getByText("Untyped goal")).toBeInTheDocument();
  });

  it("changes a Rock owner inline from the list", () => {
    state.rocks = [rock];
    render(<RocksPage />);
    fireEvent.click(screen.getByRole("button", { name: "List view" }));
    fireEvent.click(screen.getByRole("button", { name: "Cut fulfilment cost 12% owner" }));
    fireEvent.click(screen.getByRole("option", { name: "Jesper" }));
    expect(state.mutate).toHaveBeenCalledWith(expect.objectContaining({
      id: "rock-1",
      input: expect.objectContaining({ owner_type: "member", owner_id: "member-1", title: "Cut fulfilment cost 12%", period_id: "period-1" }),
    }));
  });

  it("renames a Rock inline", () => {
    state.rocks = [rock];
    render(<RocksPage />);
    fireEvent.click(screen.getByRole("button", { name: "Rename Cut fulfilment cost 12%" }));
    const input = screen.getByLabelText("Cut fulfilment cost 12% new title");
    fireEvent.change(input, { target: { value: "Cut fulfilment cost 15%" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(state.mutate).toHaveBeenCalledWith(expect.objectContaining({ id: "rock-1", input: expect.objectContaining({ title: "Cut fulfilment cost 15%" }) }));
  });

  it("plans the next period from the period filter", () => {
    render(<RocksPage />);
    fireEvent.click(screen.getByRole("button", { name: "Period filter" }));
    fireEvent.click(screen.getByRole("button", { name: /Plan next period/ }));
    expect(state.savePeriod).toHaveBeenCalledWith(
      expect.objectContaining({ input: expect.objectContaining({ unit: "quarter", starts_on: "2026-10-01", ends_on: "2026-12-31", name: "Q4 2026" }) }),
      expect.any(Object),
    );
  });

  it("switches to the timeline view grouped by owner", () => {
    state.rocks = [rock];
    render(<RocksPage />);
    fireEvent.click(screen.getByRole("button", { name: "Timeline view" }));
    expect(screen.getByLabelText("Rocks timeline")).toBeInTheDocument();
    expect(screen.getByText("Sara")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Open Cut fulfilment cost 12%" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Group by type" }));
    expect(screen.getByText("No type")).toBeInTheDocument();
  });

  it("records a weekly check-in directly from the selected Rock", () => {
    state.rocks = [rock];
    render(<RocksPage />);
    fireEvent.click(screen.getByRole("button", { name: "List view" }));
    fireEvent.click(screen.getByRole("button", { name: "View Cut fulfilment cost 12%" }));
    fireEvent.change(screen.getByLabelText("Check-in note"), { target: { value: "Decision needed" } });
    fireEvent.click(screen.getByRole("button", { name: "Save check-in" }));
    expect(state.checkIn).toHaveBeenCalledWith(expect.objectContaining({ id: "rock-1", input: expect.objectContaining({ note: "Decision needed" }) }), expect.any(Object));
  });

  it("opens the edit panel as a modal on desktop with the form, linked-project chip and check-in history", () => {
    state.rocks = [rock];
    render(<RocksPage />);
    fireEvent.click(screen.getByRole("button", { name: "Edit Cut fulfilment cost 12%" }));
    const panel = screen.getByRole("dialog", { name: "Edit Cut fulfilment cost 12%" });
    expect(panel).toHaveAttribute("data-slot", "dialog-content");
    expect(within(panel).getByRole("heading", { name: "Edit Cut fulfilment cost 12%" })).toBeInTheDocument();
    expect(within(panel).getByLabelText("Rock title")).toHaveValue("Cut fulfilment cost 12%");
    expect(within(panel).getByRole("button", { name: "Remove project Carrier migration" })).toBeInTheDocument();
    expect(within(panel).getByText("Under this Rock — Cut fulfilment cost 12%")).toBeInTheDocument();
    expect(within(panel).getByText("Weekly check-in history")).toBeInTheDocument();
    fireEvent.click(within(panel).getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("dialog", { name: "Edit Cut fulfilment cost 12%" })).not.toBeInTheDocument();
  });

  it("opens the edit panel as a bottom sheet on mobile", () => {
    state.rocks = [rock];
    state.isMobile = true;
    render(<RocksPage />);
    fireEvent.click(screen.getByRole("button", { name: "Edit Cut fulfilment cost 12%" }));
    const sheet = screen.getByRole("dialog", { name: "Edit Cut fulfilment cost 12%" });
    expect(sheet).toHaveAttribute("data-slot", "drawer-content");
    expect(sheet).toHaveAttribute("data-vaul-drawer-direction", "bottom");
    expect(within(sheet).getByLabelText("Rock title")).toHaveValue("Cut fulfilment cost 12%");
    expect(within(sheet).getByText("Weekly check-in history")).toBeInTheDocument();
  });
});
