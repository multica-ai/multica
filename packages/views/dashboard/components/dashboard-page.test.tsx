import { describe, it, expect, beforeEach, vi } from "vitest";
import { cleanup } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";

// The viewing timezone flows: auth store `user.timezone` → useViewingTimezone()
// → every dashboard query key. This test pins that chain: when the stored
// timezone changes, the dashboard report query keys must change, which is
// what makes TanStack Query refetch under the new tz.

// Capture every queryKey passed to useQuery. queryOptions() inside the
// dashboard options builders runs for real, so the key is the production key.
const queryKeys = vi.hoisted(() => [] as unknown[][]);
const dashboardDataRef = vi.hoisted(() => ({ current: false }));

function todayIso() {
  return new Date().toISOString().slice(0, 10);
}

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQuery: (opts: { queryKey: unknown[] }) => {
      queryKeys.push(opts.queryKey);
      if (dashboardDataRef.current) {
        const kind = opts.queryKey[2];
        const data =
          kind === "daily"
            ? [
                {
                  date: todayIso(),
                  provider: "anthropic",
                  model: "claude-sonnet-4-6",
                  input_tokens: 1_000,
                  output_tokens: 2_000,
                  cache_read_tokens: 0,
                  cache_write_tokens: 0,
                  task_count: 2,
                },
              ]
            : kind === "agent-runtime"
              ? [
                  {
                    agent_id: "agent-1",
                    total_seconds: 3 * 3_600 + 17 * 60,
                    task_count: 12,
                    failed_count: 1,
                  },
                ]
              : kind === "runtime-daily"
                ? [
                    {
                      date: todayIso(),
                      total_seconds: 3 * 3_600 + 17 * 60,
                      task_count: 12,
                      failed_count: 1,
                    },
                  ]
                : [];
        return { data, isLoading: false, isSuccess: true };
      }
      return { data: undefined, isLoading: true };
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
  return { useCustomPricingStore };
});

import { DashboardPage } from "./dashboard-page";

describe("DashboardPage — viewing timezone drives the query key", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    dashboardDataRef.current = false;
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

  it("renders every workspace KPI as an animated number", () => {
    dashboardDataRef.current = true;
    tzRef.current = "UTC";

    const { container } = renderWithI18n(<DashboardPage />);
    const flows = Array.from(
      container.querySelectorAll("number-flow-react"),
    );

    expect(flows).toHaveLength(5);
    expect(flows.map((flow) => flow.getAttribute("aria-label"))).toEqual(
      expect.arrayContaining(["$0.03", "3K", "12"]),
    );
    expect(container).toHaveTextContent("3h 17m");
    expect(
      flows.every(
        (flow) =>
          (flow as HTMLElement & { respectMotionPreference?: boolean })
            .respectMotionPreference === true,
      ),
    ).toBe(true);
  });
});
