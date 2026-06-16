import { describe, it, expect, beforeEach, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";

// The viewing timezone flows: auth store `user.timezone` → useViewingTimezone()
// → every dashboard query key. This test pins that chain: when the stored
// timezone changes, the dashboard report query keys must change, which is
// what makes TanStack Query refetch under the new tz.

// Capture every queryKey passed to useQuery. queryOptions() inside the
// dashboard options builders runs for real, so the key is the production key.
const queryKeys = vi.hoisted(() => [] as unknown[][]);
const queryState = vi.hoisted(() => ({
  loading: true,
  daily: [] as Record<string, unknown>[],
  byAgent: [] as Record<string, unknown>[],
  agentRuntime: [] as Record<string, unknown>[],
  runtimeDaily: [] as Record<string, unknown>[],
}));

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQuery: (opts: { queryKey: unknown[] }) => {
      queryKeys.push(opts.queryKey);
      if (queryState.loading) return { data: undefined, isLoading: true };
      if (opts.queryKey[0] === "dashboard") {
        switch (opts.queryKey[2]) {
          case "daily":
            return { data: queryState.daily, isLoading: false };
          case "by-agent":
            return { data: queryState.byAgent, isLoading: false };
          case "agent-runtime":
            return { data: queryState.agentRuntime, isLoading: false };
          case "runtime-daily":
            return { data: queryState.runtimeDaily, isLoading: false };
        }
      }
      return { data: [], isLoading: false };
    },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const tzRef = vi.hoisted(() => ({ current: "UTC" as string | null }));

vi.mock("@multica/core/auth", () => {
  type AuthState = { user: { timezone: string | null } | null };
  const state = (): AuthState => ({ user: { timezone: tzRef.current } });
  const useAuthStore = Object.assign(
    (sel?: (s: AuthState) => unknown) => (sel ? sel(state()) : state()),
    { getState: state },
  );
  return { useAuthStore };
});

vi.mock("@multica/core/runtimes/custom-pricing-store", () => {
  const state = () => ({ pricings: {} });
  const useCustomPricingStore = Object.assign(
    (sel?: (s: ReturnType<typeof state>) => unknown) =>
      sel ? sel(state()) : state(),
    { getState: state },
  );
  return { useCustomPricingStore, getCustomPricing: () => undefined };
});

vi.mock("../../runtimes/components/charts", () => ({
  DailyCostChart: () => null,
  DailyTokensChart: () => null,
  DailyTimeChart: () => null,
  DailyTasksChart: () => null,
  WeeklyCostChart: () => null,
  WeeklyTokensChart: () => null,
  WeeklyTimeChart: () => null,
  WeeklyTasksChart: () => null,
}));

vi.mock("../../runtimes/components/custom-pricing-dialog", () => ({
  CustomPricingDialog: () => null,
}));

import { DashboardPage } from "./dashboard-page";

describe("DashboardPage — viewing timezone drives the query key", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    queryState.loading = true;
    queryState.daily = [];
    queryState.byAgent = [];
    queryState.agentRuntime = [];
    queryState.runtimeDaily = [];
    cleanup();
  });

  // The `tz` segment is the last element of every dashboard key
  // (see dashboardKeys in @multica/core/dashboard/queries).
  function tzSegments(): unknown[] {
    return queryKeys
      .filter((k) => k[0] === "dashboard")
      .map((k) => k[k.length - 1]);
  }

  it("uses the stored timezone in every dashboard query key", () => {
    tzRef.current = "UTC";
    renderWithI18n(<DashboardPage />);

    const tzs = tzSegments();
    expect(tzs.length).toBeGreaterThan(0);
    expect(tzs.every((tz) => tz === "UTC")).toBe(true);
  });

  it("flips the query key when the stored timezone changes", () => {
    tzRef.current = "UTC";
    renderWithI18n(<DashboardPage />);
    const utcKeys = queryKeys.filter((k) => k[0] === "dashboard");

    queryKeys.length = 0;
    cleanup();

    tzRef.current = "Asia/Tokyo";
    renderWithI18n(<DashboardPage />);
    const tokyoKeys = queryKeys.filter((k) => k[0] === "dashboard");

    expect(utcKeys.length).toBe(tokyoKeys.length);
    expect(utcKeys.length).toBeGreaterThan(0);
    // Same number of dashboard queries, but no key is shared between the
    // two timezones — so TanStack Query treats every series as a fresh
    // fetch and refetches under the new tz.
    for (let i = 0; i < utcKeys.length; i++) {
      expect(utcKeys[i]).not.toEqual(tokyoKeys[i]);
    }
  });

  it("does not show a pricing gap for codex auto-review usage", () => {
    queryState.loading = false;
    queryState.daily = [usageRow("codex-auto-review")];

    renderWithI18n(<DashboardPage />, { locale: "zh-Hans" });

    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.queryByText(/没有维护价格/)).not.toBeInTheDocument();
  });

  it("still shows the pricing gap banner for truly unmapped models", () => {
    queryState.loading = false;
    queryState.daily = [usageRow("unknown-review-model")];

    renderWithI18n(<DashboardPage />, { locale: "zh-Hans" });

    expect(screen.getByRole("alert")).toHaveTextContent(
      "有 1 个模型没有维护价格",
    );
    expect(screen.getByText("unknown-review-model")).toBeInTheDocument();
  });
});

function usageRow(model: string): Record<string, unknown> {
  return {
    date: new Date().toISOString().slice(0, 10),
    model,
    input_tokens: 1_000,
    output_tokens: 250,
    cache_read_tokens: 100,
    cache_write_tokens: 50,
    task_count: 1,
  };
}
