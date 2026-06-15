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
  dailyRows: [] as unknown[],
}));
const pricingState = vi.hoisted(() => ({
  pricings: {} as Record<
    string,
    { input: number; output: number; cacheRead: number; cacheWrite: number }
  >,
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
      const key = opts.queryKey;
      const kind = key[0] === "dashboard" ? key[2] : null;
      if (kind === "daily") {
        return { data: queryState.dailyRows, isLoading: queryState.loading };
      }
      if (kind === "by-agent" || kind === "agent-runtime" || kind === "runtime-daily") {
        return { data: [], isLoading: queryState.loading };
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
  const useCustomPricingStore = Object.assign(
    (sel?: (s: typeof pricingState) => unknown) =>
      sel ? sel(pricingState) : pricingState,
    { getState: () => pricingState },
  );
  return {
    useCustomPricingStore,
    getCustomPricing: (model: string) => pricingState.pricings[model],
  };
});

vi.mock("../../runtimes/components/charts", () => ({
  DailyCostChart: () => <div data-testid="daily-cost-chart" />,
  DailyTokensChart: () => <div data-testid="daily-tokens-chart" />,
  DailyTimeChart: () => <div data-testid="daily-time-chart" />,
  DailyTasksChart: () => <div data-testid="daily-tasks-chart" />,
  WeeklyCostChart: () => <div data-testid="weekly-cost-chart" />,
  WeeklyTokensChart: () => <div data-testid="weekly-tokens-chart" />,
  WeeklyTimeChart: () => <div data-testid="weekly-time-chart" />,
  WeeklyTasksChart: () => <div data-testid="weekly-tasks-chart" />,
}));

vi.mock("../../runtimes/components/custom-pricing-dialog", () => ({
  CustomPricingDialog: ({
    open,
    unmappedModels,
  }: {
    open: boolean;
    unmappedModels: string[];
  }) =>
    open ? (
      <div data-testid="custom-pricing-dialog">{unmappedModels.join(", ")}</div>
    ) : null,
}));

import { DashboardPage } from "./dashboard-page";

describe("DashboardPage — viewing timezone drives the query key", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    queryState.loading = true;
    queryState.dailyRows = [];
    pricingState.pricings = {};
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

  it("surfaces an unmapped-pricing notice when dashboard token rows cannot be priced", () => {
    tzRef.current = "UTC";
    queryState.loading = false;
    queryState.dailyRows = [
      {
        date: "2026-06-15",
        model: "self-hosted-model-x",
        input_tokens: 1_000,
        output_tokens: 250,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 1,
      },
    ];

    renderWithI18n(<DashboardPage />);

    expect(screen.getByRole("alert")).toHaveTextContent(
      "1 model has no maintained price",
    );
    expect(screen.getByText("self-hosted-model-x")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Set custom prices" }),
    ).toBeInTheDocument();
  });

  it("re-prices dashboard cost totals when custom pricing changes", () => {
    tzRef.current = "UTC";
    queryState.loading = false;
    queryState.dailyRows = [
      {
        date: "2026-06-15",
        model: "self-hosted-model-x",
        input_tokens: 1_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 1,
      },
    ];

    const { rerender } = renderWithI18n(<DashboardPage />);
    expect(screen.getByText("$0.00")).toBeInTheDocument();

    pricingState.pricings = {
      "self-hosted-model-x": {
        input: 2,
        output: 0,
        cacheRead: 0,
        cacheWrite: 0,
      },
    };

    rerender(<DashboardPage />);

    expect(screen.getByText("$2.00")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
