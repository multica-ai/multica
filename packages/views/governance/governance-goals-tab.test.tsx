/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Goal, Metric } from "@multica/core/governance";
import { renderWithI18n } from "../test/i18n";
import { GovernanceGoalsTab } from "./governance-goals-tab";

const api = vi.hoisted(() => ({
  listGoals: vi.fn(),
  getGoal: vi.fn(),
  listMetrics: vi.fn(),
  listMetricReadings: vi.fn(),
  reportMetricReading: vi.fn(),
  listOfficeFloors: vi.fn(),
  listSquads: vi.fn(),
  listInitiatives: vi.fn(),
}));
const nav = vi.hoisted(() => ({
  pathname: "/acme/governance",
  searchParams: new URLSearchParams(),
  replace: vi.fn(),
  push: vi.fn(),
}));
const role = vi.hoisted(() => ({ value: "admin" as "owner" | "admin" | "member" }));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/permissions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/permissions")>()),
  useCurrentMember: () => ({ userId: "u-1", role: role.value, member: null, isLoading: false }),
}));
vi.mock("../navigation", () => ({
  AppLink: ({ children, ...props }: React.ComponentProps<"a">) => <a {...props}>{children}</a>,
  useNavigation: () => nav,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function goal(over: Partial<Goal> = {}): Goal {
  return {
    id: "g-1",
    workspace_id: "ws-1",
    scope_type: "company",
    scope_id: null,
    parent_goal_id: null,
    title: "Grow revenue",
    why: "",
    period_start: "2026-10-01",
    period_end: "2026-12-31",
    owner_type: "member",
    owner_id: "u-1",
    status: "active",
    confidence: "at_risk",
    derived_confidence: "at_risk",
    confidence_override: null,
    confidence_note: "",
    confidence_changed_at: "2026-11-02T00:00:00Z",
    progress: 0.4,
    readings_missing: 0,
    source_proposal_id: null,
    key_results: [
      {
        id: "kr-1",
        goal_id: "g-1",
        title: "Monthly recurring revenue",
        metric_key: "mrr",
        scope_type: "company",
        scope_id: null,
        start_value: 0,
        target_value: 1000,
        weight: 1,
        current_value: 400,
        observed_at: "2026-11-02T00:00:00Z",
        progress: 0.4,
        series: [],
      },
    ],
    created_by_type: "member",
    created_by_id: "u-1",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-11-02T00:00:00Z",
    ...over,
  };
}

const metric: Metric = {
  key: "mrr",
  name: "Monthly recurring revenue",
  unit: "USD",
  source: "manual",
  direction: "increase",
  description: "",
  created_at: "",
};

function renderTab() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <GovernanceGoalsTab />
    </QueryClientProvider>,
  );
}

describe("GovernanceGoalsTab", () => {
  beforeEach(() => {
    nav.searchParams = new URLSearchParams();
    nav.replace.mockClear();
    role.value = "admin";
    api.listGoals.mockResolvedValue([goal()]);
    api.listOfficeFloors.mockResolvedValue([]);
    api.listSquads.mockResolvedValue([]);
    api.listInitiatives.mockResolvedValue([]);
    api.listMetrics.mockResolvedValue([metric]);
    api.listMetricReadings.mockResolvedValue([]);
    api.getGoal.mockResolvedValue({ goal: goal(), child_goals: [] });
    api.reportMetricReading.mockResolvedValue({
      id: "r-1",
      metric_key: "mrr",
      scope_type: "company",
      scope_id: null,
      value: 42,
      observed_at: "2026-11-03T00:00:00Z",
      actor_type: "member",
      actor_id: "u-1",
      note: "",
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("draws the company section with each goal's progress and confidence", async () => {
    renderTab();
    const section = await screen.findByRole("region", { name: "Company" });
    expect(section).toHaveTextContent("Grow revenue");
    expect(section).toHaveTextContent("At risk");
    expect(section).toHaveTextContent("40%");
  });

  it("selects a goal in the URL when its row is pressed, and opens the goal below the tree", async () => {
    renderTab();
    fireEvent.click(await screen.findByRole("button", { name: /Grow revenue/ }));
    expect(nav.replace).toHaveBeenCalledWith("/acme/governance?goal=g-1");
  });

  it("adds a reading to the goal's first key result's metric at the company scope", async () => {
    nav.searchParams = new URLSearchParams("goal=g-1");
    renderTab();
    const value = await screen.findByLabelText("Value");
    fireEvent.change(value, { target: { value: "42" } });
    fireEvent.click(screen.getByRole("button", { name: "Add reading" }));
    await waitFor(() =>
      expect(api.reportMetricReading).toHaveBeenCalledWith("mrr", {
        scope_type: "company",
        scope_id: null,
        value: 42,
        note: "",
      }),
    );
  });

  it("does not offer the owner's confidence override to a member who cannot govern", async () => {
    role.value = "member";
    nav.searchParams = new URLSearchParams("goal=g-1");
    renderTab();
    await screen.findByLabelText("Value");
    expect(screen.queryByRole("button", { name: "Save confidence" })).toBeNull();
  });

  it("says so when there are no goals yet", async () => {
    api.listGoals.mockResolvedValue([]);
    renderTab();
    expect(await screen.findByText(/Nothing to show yet/)).toBeInTheDocument();
  });

  it("offers New goal to an owner or admin, and not to a member", async () => {
    renderTab();
    expect(await screen.findByRole("button", { name: "New goal" })).toBeTruthy();

    role.value = "member";
    cleanup();
    renderTab();
    await screen.findByRole("region", { name: "Company" });
    expect(screen.queryByRole("button", { name: "New goal" })).toBeNull();
  });
});

