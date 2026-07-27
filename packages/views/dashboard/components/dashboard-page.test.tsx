import { describe, it, expect, beforeEach, vi } from "vitest";
import { cleanup, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";

// The viewing timezone flows: auth store `user.timezone` → useViewingTimezone()
// → every dashboard query key. This test pins that chain: when the stored
// timezone changes, the dashboard report query keys must change, which is
// what makes TanStack Query refetch under the new tz.

// Capture every queryKey passed to useQuery. queryOptions() inside the
// dashboard options builders runs for real, so the key is the production key.
const queryKeys = vi.hoisted(() => [] as unknown[][]);
const dashboardDataRef = vi.hoisted(() => ({ current: false }));
// Swaps the by-agent fixture for one with enough agents to exercise the
// top-offenders cap. Kept off by default so the other tests keep their exact
// 4-of-10 arithmetic.
const manyAgentsRef = vi.hoisted(() => ({ current: false }));

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
        // ["workspaces", wsId, "agents"] — needed so the Errors breakdown can
        // resolve agent-1 to a name and render its drill-down link.
        if (opts.queryKey[0] === "workspaces" && opts.queryKey[2] === "agents") {
          return {
            data: manyAgentsRef.current
              ? Array.from({ length: 12 }, (_, i) => ({
                  id: `bulk-${i}`,
                  name: `Bulk Agent ${i}`,
                }))
              : [{ id: "agent-1", name: "Agent One" }],
            isLoading: false,
            isSuccess: true,
          };
        }
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
                : // `failure_reason: ""` is the succeeded bucket — the
                  // denominator behind every rate the Errors surface shows.
                  kind === "failures-daily"
                  ? [
                      { date: todayIso(), failure_reason: "", task_count: 6 },
                      {
                        date: todayIso(),
                        failure_reason: "agent_error.provider_auth_or_access",
                        task_count: 3,
                      },
                      { date: todayIso(), failure_reason: "timeout", task_count: 1 },
                    ]
                  : kind === "failures-by-agent"
                    ? manyAgentsRef.current
                      ? Array.from({ length: 12 }, (_, i) => [
                          {
                            agent_id: `bulk-${i}`,
                            failure_reason: "",
                            task_count: 100,
                          },
                          {
                            agent_id: `bulk-${i}`,
                            failure_reason: "timeout",
                            // Descending so rank order is unambiguous.
                            task_count: 12 - i,
                          },
                        ]).flat()
                      : [
                        { agent_id: "agent-1", failure_reason: "", task_count: 6 },
                        {
                          agent_id: "agent-1",
                          failure_reason: "agent_error.provider_auth_or_access",
                          task_count: 3,
                        },
                        {
                          agent_id: "agent-1",
                          failure_reason: "timeout",
                          task_count: 1,
                        },
                        // Not in the agent list below — a private agent this
                        // member cannot see, or a deleted one. The rollup
                        // still returns it.
                        {
                          agent_id: "0f9d1c2e-private-agent-uuid",
                          failure_reason: "agent_error.provider_auth_or_access",
                          task_count: 2,
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

// The leaderboard renders ActorAvatar, which resolves avatar URLs through
// the api singleton. Only the base-URL read is exercised here.
vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: () => "https://example.test" },
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    agentDetail: (id: string) => `/acme/agents/${id}`,
  }),
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

// A minimal adapter — the Errors breakdown's drill-down renders an <AppLink>,
// which reads the navigation context. Asserting on the rendered href (rather
// than a push spy) keeps the test on the contract that matters: the row points
// at the agent's Overview, whose ActivityTab lists its runs and each failure's
// reason.
const navAdapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/usage",
  searchParams: new URLSearchParams(),
  getShareableUrl: (path: string) => `https://example.test${path}`,
};

function renderDashboard() {
  return renderWithI18n(
    <NavigationProvider value={navAdapter}>
      <DashboardPage />
    </NavigationProvider>,
  );
}

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

    const { container } = renderDashboard();
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

describe("DashboardPage — failure visibility", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    dashboardDataRef.current = true;
    tzRef.current = "UTC";
    cleanup();
  });

  it("states the error rate with its denominator spelled out", () => {
    // The run-time rollup sees 1 failure out of 12 tasks — it only counts
    // tasks that actually started. The failure rollup also sees tasks that
    // never started, so it reports 4 out of 10. The Errors card quotes the
    // latter *with* its denominator, which is what keeps it from reading as
    // a contradiction of the Tasks tile above.
    renderDashboard();

    expect(screen.getByText("4 of 10 runs failed · 40%")).toBeInTheDocument();
    // The Tasks tile keeps its own started-tasks-only figure.
    expect(screen.getByText("1 failed")).toBeInTheDocument();
  });

  it("breaks failures down by class and links the offending agent to its runs", () => {
    renderDashboard();

    // Auth (3) outranks Timeout (1), and both are named by class rather than
    // by the raw failure_reason enum.
    const byClass = within(screen.getByRole("list", { name: "By class" }));
    expect(byClass.getAllByRole("listitem").map((li) => li.textContent)).toEqual([
      "Auth3",
      "Timeout1",
    ]);

    const byAgent = within(screen.getByRole("list", { name: "Top offenders" }));
    const link = byAgent.getByRole("link", { name: /Agent One/ });
    // Overview, NOT ?view=work: Work lists the issues assigned to the agent,
    // while its recent runs (and each failure's reason) live in the Overview
    // pane's ActivityTab.
    expect(link).toHaveAttribute("href", "/acme/agents/agent-1?view=overview");
    // 4 of that agent's 10 terminal runs failed, and Auth is what dominates.
    expect(byAgent.getByText("4 / 10 · 40%")).toBeInTheDocument();
  });

  it("reveals the raw failure_reason values behind the class summary", async () => {
    const user = userEvent.setup();
    renderDashboard();

    expect(
      screen.queryByText("agent_error.provider_auth_or_access"),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Show error codes" }));

    // Raw and unlocalised: an operator pastes this exact string into a log
    // search, so it must not be translated or prettified.
    expect(
      screen.getByText("agent_error.provider_auth_or_access"),
    ).toBeInTheDocument();
  });

  it("adds Errors to the trend toggle without disturbing the other metrics", async () => {
    const user = userEvent.setup();
    const { container } = renderDashboard();

    await user.click(screen.getByRole("button", { name: "Errors" }));

    expect(container).toHaveTextContent("Daily errors");
  });
});

describe("DashboardPage — the Errors list never exposes an agent the viewer can't see", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    dashboardDataRef.current = true;
    tzRef.current = "UTC";
    cleanup();
  });

  it("folds an unresolvable agent into an anonymous row instead of printing its UUID", () => {
    // The failure rollups are workspace-scoped and deliberately skip
    // per-agent visibility, but the agent list does not: a private agent
    // this member can't see never appears there. Rendering `row.agentId`
    // would leak its existence, failure count and failure rate.
    const { container } = renderDashboard();

    expect(container).not.toHaveTextContent("0f9d1c2e-private-agent-uuid");

    const byAgent = within(screen.getByRole("list", { name: "Top offenders" }));
    expect(byAgent.getByText("Other agents")).toBeInTheDocument();
    // Anonymous, and therefore not a link — there is no page to open.
    expect(
      byAgent.queryByRole("link", { name: /Other agents/ }),
    ).not.toBeInTheDocument();
  });
});

describe("DashboardPage — Errors card placement and density", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    dashboardDataRef.current = true;
    manyAgentsRef.current = false;
    tzRef.current = "UTC";
    cleanup();
  });

  it("renders the Errors card after the leaderboard, at the bottom of the page", () => {
    // Spend is the headline; failures are the follow-up question you ask
    // after seeing who is spending. Asserting document order rather than a
    // class name keeps this about the reading sequence.
    renderDashboard();

    // By heading, not by text: "Errors" also names the trend-chart toggle.
    const leaderboard = screen.getByRole("heading", { name: "Leaderboard" });
    const errors = screen.getByRole("heading", { name: "Errors" });

    expect(
      leaderboard.compareDocumentPosition(errors) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("caps the offender list and expands it on demand", async () => {
    manyAgentsRef.current = true;
    const user = userEvent.setup();
    renderDashboard();

    const list = () => screen.getByRole("list", { name: "Top offenders" });
    // 12 agents have failures, but an unbounded list is what made this card
    // taller than the rest of the page put together.
    expect(within(list()).getAllByRole("listitem")).toHaveLength(8);
    // The toggle is the truncation signal — its label carries the full count,
    // so the cap is never silent.
    await user.click(screen.getByRole("button", { name: "Show all 12" }));
    expect(within(list()).getAllByRole("listitem")).toHaveLength(12);

    await user.click(screen.getByRole("button", { name: "Show top 8" }));
    expect(within(list()).getAllByRole("listitem")).toHaveLength(8);
  });

  it("shows no expand affordance when every offender already fits", () => {
    renderDashboard();

    expect(
      screen.queryByRole("button", { name: /Show all/ }),
    ).not.toBeInTheDocument();
  });
});
