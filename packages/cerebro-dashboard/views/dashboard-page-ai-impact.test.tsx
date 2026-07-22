// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { useQuery } = vi.hoisted(() => ({ useQuery: vi.fn() }));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery,
}));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "workspace-1", slug: "firtal" }),
}));
vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => true }));
vi.mock("@multica/views/layout/page-header", () => ({
  PageHeader: ({ children }: { children: React.ReactNode }) => <header>{children}</header>,
}));
vi.mock("./components/dashboard-tab-bar", () => ({ DashboardTabBar: () => null }));
vi.mock("./components/analytics-dashboard", () => ({ AnalyticsDashboard: () => null }));
vi.mock("./components/messages-control-room", () => ({ MessagesControlRoom: () => null }));
vi.mock("./components/overview-control-room", () => ({ OverviewControlRoom: () => null }));
vi.mock("./components/runs-control-room", () => ({ RunsControlRoom: () => null }));
vi.mock("./components/ai-impact-control-room", () => ({
  AIImpactControlRoom: ({
    overview,
    functions,
    qualityRisk,
  }: {
    overview?: { families: unknown[] };
    functions?: { functions: unknown[] };
    qualityRisk?: { decisions: unknown[] };
  }) => (
    <div>
      AI Impact read models: {overview?.families.length ?? 0}/
      {functions?.functions.length ?? 0}/{qualityRisk?.decisions.length ?? 0}
    </div>
  ),
}));

import { useDashboardStore } from "../core/store";
import { DashboardPage } from "./dashboard-page";

afterEach(cleanup);

describe("DashboardPage AI Impact integration", () => {
  beforeEach(() => {
    useDashboardStore.setState({ tab: "ai-impact" });
    useQuery.mockImplementation(({ queryKey }: { queryKey: readonly string[] }) => {
      const view = queryKey.at(-1);
      if (view === "overview" && queryKey.includes("ai-impact")) {
        return { data: { families: [{ family: "Outcome", evidence: [] }] }, isLoading: false };
      }
      if (view === "functions") {
        return { data: { functions: [{ id: "function-1" }] }, isLoading: false };
      }
      if (view === "quality-risk") {
        return { data: { decisions: [{ operating_loop_id: "loop-1" }] }, isLoading: false };
      }
      return {
        data: { period_start: "2026-07-01T00:00:00Z", period_end: "2026-07-22T00:00:00Z" },
        isLoading: false,
        isError: false,
      };
    });
  });

  it("passes the Overview, Functions, and Quality & Risk query results to the control room", () => {
    render(<DashboardPage />);

    expect(screen.getByText("AI Impact read models: 1/1/1")).toBeTruthy();
  });
});
