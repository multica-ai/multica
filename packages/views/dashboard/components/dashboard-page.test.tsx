import { useMemo, useState } from "react";
import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
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
// Swaps the per-agent fixtures for ones with enough agents to exercise the
// top-offenders and leaderboard caps. Kept off by default so the other tests
// keep their exact 4-of-10 arithmetic.
const manyAgentsRef = vi.hoisted(() => ({ current: false }));
// Appends the server's `__restricted_agents__` bucket to the per-agent rollups
// — what a plain member actually receives once the backend folds the agents
// they may not view (MUL-5409).
const restrictedBucketRef = vi.hoisted(() => ({ current: false }));

// Kept out of the fixture ternary so the sentinel's shape reads at a glance.
// Unlike the deleted-agents bucket this one carries real seconds / tasks: the
// agents behind it are alive and ran.
const RESTRICTED_BUCKET_ROWS = vi.hoisted(
  () =>
    ({
      "by-agent": [
        {
          agent_id: "__restricted_agents__",
          provider: "anthropic",
          model: "claude-sonnet-4-6",
          input_tokens: 500,
          output_tokens: 500,
          cache_read_tokens: 0,
          cache_write_tokens: 0,
          task_count: 4,
        },
      ],
      "agent-runtime": [
        {
          agent_id: "__restricted_agents__",
          total_seconds: 2 * 3_600,
          task_count: 4,
          failed_count: 2,
        },
      ],
    }) as Record<string, unknown[]>,
);

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
    // The page reads the client only to invalidate the dashboard keys from
    // the refresh button; there is no provider in these renders.
    useQueryClient: () => ({ invalidateQueries: vi.fn() }),
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
        // Bulk fixture: 12 agents, every per-agent metric strictly descending
        // so both caps (leaderboard top 10, offenders top 8) are testable by
        // rank without the ties an equal-valued fixture would create. The
        // date-bucketed series stay on the small fixture below — the caps are
        // a property of the per-agent lists only.
        const bulkRows =
          !manyAgentsRef.current
            ? null
            : kind === "by-agent"
              ? Array.from({ length: 12 }, (_, i) => ({
                  agent_id: `bulk-${i}`,
                  provider: "anthropic",
                  model: "claude-sonnet-4-6",
                  input_tokens: (12 - i) * 1_000,
                  output_tokens: 0,
                  cache_read_tokens: 0,
                  cache_write_tokens: 0,
                  task_count: 12 - i,
                }))
              : kind === "agent-runtime"
                ? Array.from({ length: 12 }, (_, i) => ({
                    agent_id: `bulk-${i}`,
                    total_seconds: (12 - i) * 600,
                    task_count: 12 - i,
                    failed_count: 12 - i,
                  }))
                : kind === "failures-by-agent"
                  ? Array.from({ length: 12 }, (_, i) => [
                      {
                        agent_id: `bulk-${i}`,
                        failure_reason: "",
                        task_count: 100,
                      },
                      {
                        agent_id: `bulk-${i}`,
                        failure_reason: "timeout",
                        task_count: 12 - i,
                      },
                    ]).flat()
                  : null;
        if (bulkRows) {
          return { data: bulkRows, isLoading: false, isSuccess: true };
        }
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
                    ? [
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
        return {
          data: restrictedBucketRef.current
            ? [...data, ...(RESTRICTED_BUCKET_ROWS[kind as string] ?? [])]
            : data,
          isLoading: false,
          isSuccess: true,
        };
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

const replaceSpy = vi.fn();

/**
 * A navigation adapter that actually holds the query string, because the tab
 * IS the URL: the page reads `?tab=` and writes it back through `replace`. A
 * spy-only adapter would swallow the write and the tab could never change,
 * which would make every Errors assertion below test a screen the user cannot
 * reach.
 *
 * The adapter also backs the offender drill-down's <AppLink>. Asserting on the
 * rendered href (rather than a push spy) keeps that test on the contract that
 * matters: the row points at the agent's Overview, whose ActivityTab lists its
 * runs and each failure's reason.
 */
function DashboardHarness({ initialSearch = "" }: { initialSearch?: string }) {
  const [search, setSearch] = useState(initialSearch);
  const adapter = useMemo<NavigationAdapter>(
    () => ({
      push: vi.fn(),
      replace: (path: string) => {
        replaceSpy(path);
        setSearch(path.split("?")[1] ?? "");
      },
      back: vi.fn(),
      pathname: "/acme/usage",
      searchParams: new URLSearchParams(search),
      hash: "",
      getShareableUrl: (path: string) => `https://example.test${path}`,
    }),
    [search],
  );
  return (
    <NavigationProvider value={adapter}>
      <DashboardPage />
    </NavigationProvider>
  );
}

function renderDashboard(initialSearch = "") {
  return renderWithI18n(<DashboardHarness initialSearch={initialSearch} />);
}

/** Everything about failures now lives behind its own tab, so a test that
 *  wants it has to go there first — exactly as a reader does. */
async function openErrorsTab(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("tab", { name: /Errors/ }));
}

// The offender ranking control. Named rather than reached through the DOM
// tree so it survives the card's internal layout changing.
function offenderSort(): HTMLElement {
  return screen.getByRole("group", { name: "Rank offenders by" });
}

// The filled part of one offender row's bar. `role="img"` is on the fill (it
// carries the class-split label), and its inline width is the metric the list
// is currently ranked by.
function offenderBar(index: number): HTMLElement {
  const rows = within(
    screen.getByRole("list", { name: "Top offenders" }),
  ).getAllByRole("listitem");
  return within(rows[index] as HTMLElement).getByRole("img");
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
    renderDashboard();

    const tzs = tzSegments();
    expect(tzs.length).toBeGreaterThan(0);
    expect(tzs.every((tz) => tz === "UTC")).toBe(true);
  });

  it("flips the query key when the stored timezone changes", () => {
    tzRef.current = "UTC";
    renderDashboard();
    const utcKeys = queryKeys.filter((k) => k[0] === "dashboard");

    queryKeys.length = 0;
    cleanup();

    tzRef.current = "Asia/Tokyo";
    renderDashboard();
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
    replaceSpy.mockClear();
    cleanup();
  });

  it("states the error rate with its denominator spelled out", async () => {
    const user = userEvent.setup();
    renderDashboard();

    // The Tasks tile on the Usage tab keeps its own started-tasks-only figure.
    expect(screen.getByText("1 failed")).toBeInTheDocument();

    await openErrorsTab(user);

    // The run-time rollup sees 1 failure out of 12 tasks — it only counts
    // tasks that actually started. The failure rollup also sees tasks that
    // never started, so it reports 4 out of 10. The rate tile quotes the
    // latter *with* its denominator, which is what keeps it from reading as a
    // contradiction of the Tasks tile on the other tab.
    expect(screen.getByText("4 of 10 runs failed · 40%")).toBeInTheDocument();
  });

  it("breaks failures down by class and links the offending agent to its runs", async () => {
    const user = userEvent.setup();
    renderDashboard();
    await openErrorsTab(user);

    // Auth (3) outranks Timeout (1), and both are named by class rather than
    // by the raw failure_reason enum.
    const byClass = within(screen.getByRole("list", { name: "Failure mix" }));
    expect(byClass.getAllByRole("listitem").map((li) => li.textContent)).toEqual([
      "Auth3",
      "Timeout1",
    ]);
    // The section names its own denominator: the rate tile above quotes a
    // rate over all 10 runs, this one splits the 4 failures.
    expect(screen.getByText("Failure mix · 4")).toBeInTheDocument();

    const byAgent = within(screen.getByRole("list", { name: "Top offenders" }));
    const link = byAgent.getByRole("link", { name: /Agent One/ });
    // Overview, NOT ?view=work: Work lists the issues assigned to the agent,
    // while its recent runs (and each failure's reason) live in the Overview
    // pane's ActivityTab.
    expect(link).toHaveAttribute("href", "/acme/agents/agent-1?view=overview");
    // One labelled column per number instead of the old unlabelled
    // `4 / 10 · 40%` blob.
    const row = byAgent.getAllByRole("listitem")[0] as HTMLElement;
    expect(within(row).getByText("4")).toBeInTheDocument();
    expect(within(row).getByText("10")).toBeInTheDocument();
    expect(within(row).getByText("40%")).toBeInTheDocument();
    // The bar is the only place the class split lives now that the dominant-
    // class badge is gone, so it has to name that split for screen readers.
    expect(within(row).getByRole("img")).toHaveAccessibleName("Auth 3 · Timeout 1");
  });

  it("moves the bar onto whichever metric the list is ranked by", async () => {
    const user = userEvent.setup();
    renderDashboard();
    await openErrorsTab(user);

    // Agent One failed 4 of 10; the anonymous bucket failed 2 of 2. Under
    // Failures the bucket is half the leader's bar. Under Rate it is 100% —
    // a bar that stayed on the failure count while the row shouted "100%" is
    // exactly the mismatch this control exists to remove.
    expect(offenderBar(1).style.width).toBe("50%");

    await user.click(within(offenderSort()).getByRole("button", { name: "Rate" }));

    expect(offenderBar(1).style.width).toBe("100%");
  });

  it("says which metric the list is ranked by without relying on colour", async () => {
    const user = userEvent.setup();
    renderDashboard();
    await openErrorsTab(user);

    // The active option used to be a colour swap and nothing else, which is
    // invisible to a screen reader. The group is named too — "Rate, pressed"
    // means nothing until you know the group ranks the offender list.
    const group = offenderSort();
    expect(
      within(group).getByRole("button", { name: "Failures" }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(within(group).getByRole("button", { name: "Rate" })).toHaveAttribute(
      "aria-pressed",
      "false",
    );

    await user.click(within(group).getByRole("button", { name: "Rate" }));

    expect(within(group).getByRole("button", { name: "Rate" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("keeps a two-run agent from hijacking the rate ranking", async () => {
    const user = userEvent.setup();
    renderDashboard();
    await openErrorsTab(user);

    await user.click(within(offenderSort()).getByRole("button", { name: "Rate" }));

    // 2/2 is a 100% rate and 4/10 is 40%, but two runs is not evidence. The
    // small-sample row is demoted, not dropped — the list still has to
    // reconcile with the workspace failure count above it.
    const rows = within(screen.getByRole("list", { name: "Top offenders" }))
      .getAllByRole("listitem")
      .map((li) => li.textContent);
    expect(rows[0]).toMatch(/Agent One/);
    expect(rows[1]).toMatch(/Other agents/);
  });

  it("reveals the raw failure_reason values behind the class summary", async () => {
    const user = userEvent.setup();
    renderDashboard();
    await openErrorsTab(user);

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

  it("gives failures their own chart instead of a slot on the spend toggle", async () => {
    const user = userEvent.setup();
    const { container } = renderDashboard();

    // Charting failures used to mean hiding spend: Errors was the fifth option
    // of the single trend toggle, so "what did it cost" and "what broke" could
    // not be on screen in the same breath.
    const metrics = within(screen.getByRole("group", { name: "Metric" }));
    expect(
      metrics.queryByRole("button", { name: "Errors" }),
    ).not.toBeInTheDocument();
    expect(metrics.getByRole("button", { name: "Tokens" })).toBeInTheDocument();

    await openErrorsTab(user);

    expect(container).toHaveTextContent("Daily errors");
  });
});

describe("DashboardPage — the Errors list never exposes an agent the viewer can't see", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    dashboardDataRef.current = true;
    tzRef.current = "UTC";
    replaceSpy.mockClear();
    cleanup();
  });

  it("folds an unresolvable agent into an anonymous row instead of printing its UUID", async () => {
    // The failure rollups are workspace-scoped and deliberately skip
    // per-agent visibility, but the agent list does not: a private agent
    // this member can't see never appears there. Rendering `row.agentId`
    // would leak its existence, failure count and failure rate.
    const user = userEvent.setup();
    const { container } = renderDashboard();
    await openErrorsTab(user);

    expect(container).not.toHaveTextContent("0f9d1c2e-private-agent-uuid");

    const byAgent = within(screen.getByRole("list", { name: "Top offenders" }));
    expect(byAgent.getByText("Other agents")).toBeInTheDocument();
    // Anonymous, and therefore not a link — there is no page to open.
    expect(
      byAgent.queryByRole("link", { name: /Other agents/ }),
    ).not.toBeInTheDocument();
  });
});

// The page answers two questions — "what did this cost" and "what broke" —
// and used to answer both on one scroll, where the failure breakdown sat
// below a leaderboard that can itself run to thirty rows (MUL-5759).
describe("DashboardPage — the two questions are separate tabs", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    dashboardDataRef.current = true;
    manyAgentsRef.current = false;
    tzRef.current = "UTC";
    replaceSpy.mockClear();
    cleanup();
  });

  it("keeps failures off the Usage tab and reaches them in one click", async () => {
    const user = userEvent.setup();
    renderDashboard();

    expect(
      screen.queryByRole("list", { name: "Top offenders" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("list", { name: "Leaderboard" })).toBeInTheDocument();

    await openErrorsTab(user);

    expect(screen.getByRole("list", { name: "Top offenders" })).toBeInTheDocument();
    // The two per-agent rankings had near-identical shapes and used to stack
    // one above the other; each now belongs to the question it answers.
    expect(
      screen.queryByRole("list", { name: "Leaderboard" }),
    ).not.toBeInTheDocument();
  });

  it("puts the tab in the URL so an Errors view can be linked", async () => {
    const user = userEvent.setup();
    renderDashboard();

    await openErrorsTab(user);
    expect(replaceSpy).toHaveBeenLastCalledWith("/acme/usage?tab=errors");

    // Returning to the default view drops the param instead of pinning
    // ?tab=usage onto every link out of this page.
    await user.click(screen.getByRole("tab", { name: "Usage" }));
    expect(replaceSpy).toHaveBeenLastCalledWith("/acme/usage");
  });

  it("opens straight onto Errors when the URL asks for it", () => {
    renderDashboard("tab=errors");

    expect(screen.getByRole("list", { name: "Top offenders" })).toBeInTheDocument();
  });

  it("falls back to Usage for a tab value it does not recognise", () => {
    renderDashboard("tab=nonsense");

    expect(screen.getByRole("list", { name: "Leaderboard" })).toBeInTheDocument();
  });

  it("caps the offender list and expands it on demand", async () => {
    manyAgentsRef.current = true;
    const user = userEvent.setup();
    renderDashboard();
    await openErrorsTab(user);

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

  it("shows no expand affordance when every offender already fits", async () => {
    const user = userEvent.setup();
    renderDashboard();
    await openErrorsTab(user);

    expect(
      screen.queryByRole("button", { name: /Show all/ }),
    ).not.toBeInTheDocument();
  });
});

// MUL-5409. The server folds every agent it won't name — those the viewer may
// not see, plus the hidden system carriers behind agent-builder sessions — onto
// one sentinel row. The leaderboard used to have a single synthetic row,
// labelled "Deleted agents" with a bin icon and dashed-out Time/Tasks, so the
// user was told "N agents were deleted" about agents that are alive and running.
describe("DashboardPage — the leaderboard tells the truth about the server's bucket", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    dashboardDataRef.current = true;
    manyAgentsRef.current = false;
    restrictedBucketRef.current = true;
    tzRef.current = "UTC";
    cleanup();
  });

  afterEach(() => {
    restrictedBucketRef.current = false;
  });

  it("labels the bucket neutrally, never as deleted", () => {
    const { container } = renderDashboard();

    const list = within(screen.getByRole("list", { name: "Leaderboard" }));
    expect(list.getByText("Other agents")).toBeInTheDocument();
    expect(list.queryByText("Deleted agents")).not.toBeInTheDocument();
    // The sentinel is a placeholder, not an id to render.
    expect(container).not.toHaveTextContent("__restricted_agents__");
  });

  it("counts it as neither an agent nor a deletion in the caption", () => {
    const { container } = renderDashboard();

    // One real agent in the fixture. The bucket is a row, not an agent, and
    // nothing here was deleted — so no "· N deleted" suffix.
    expect(container).toHaveTextContent("1 agents");
    expect(container).not.toHaveTextContent("deleted");
  });

  it("keeps the bucket's run time and task count instead of dashing them out", () => {
    // Those agents really ran; the server merely merged them. Blanking these
    // columns (which is right for a hard-deleted agent) would under-report the
    // workspace's run time against the Time / Tasks KPIs above.
    renderDashboard();

    const rows = within(
      screen.getByRole("list", { name: "Leaderboard" }),
    ).getAllByRole("listitem");
    const bucket = rows.find((r) => r.textContent?.includes("Other agents"));
    expect(bucket).toBeDefined();
    expect(bucket).toHaveTextContent("2h");
    expect(bucket).toHaveTextContent("4");
  });
});

describe("DashboardPage — leaderboard density", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    dashboardDataRef.current = true;
    manyAgentsRef.current = false;
    tzRef.current = "UTC";
    cleanup();
  });

  it("ranks the top 10 agents and keeps the tail behind a toggle", async () => {
    manyAgentsRef.current = true;
    const user = userEvent.setup();
    renderDashboard();

    const list = () => within(screen.getByRole("list", { name: "Leaderboard" }));
    // 12 agents have usage. Flattening all of them is what pushed the Errors
    // card a full screen below the fold (MUL-5388).
    expect(list().getAllByRole("listitem")).toHaveLength(10);
    // Ranked by tokens desc, so the two smallest spenders are the ones cut.
    expect(list().getByText("Bulk Agent 0")).toBeInTheDocument();
    expect(list().queryByText("Bulk Agent 10")).not.toBeInTheDocument();
    expect(list().queryByText("Bulk Agent 11")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Show all" }));
    expect(list().getAllByRole("listitem")).toHaveLength(12);
    expect(list().getByText("Bulk Agent 11")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Show top 10" }));
    expect(list().getAllByRole("listitem")).toHaveLength(10);
  });

  it("keeps the cap honest when the ranking metric changes", async () => {
    manyAgentsRef.current = true;
    const user = userEvent.setup();
    renderDashboard();

    // Re-ranking must not quietly reveal the tail: the cap belongs to the
    // list, not to one metric.
    const sortControls = within(
      screen.getByRole("group", { name: "Rank agents by" }),
    );
    await user.click(sortControls.getByRole("button", { name: "Time" }));

    const list = within(screen.getByRole("list", { name: "Leaderboard" }));
    expect(list.getAllByRole("listitem")).toHaveLength(10);
  });

  it("shows no expand affordance when every agent already fits", () => {
    renderDashboard();

    const list = within(screen.getByRole("list", { name: "Leaderboard" }));
    expect(list.getAllByRole("listitem")).toHaveLength(1);
    expect(
      screen.queryByRole("button", { name: "Show all" }),
    ).not.toBeInTheDocument();
  });

  it("exposes wide columns through a named keyboard-focusable local scroller", () => {
    renderDashboard();

    const scroller = screen.getByRole("region", { name: "Leaderboard" });
    const list = within(scroller).getByRole("list", { name: "Leaderboard" });
    const row = within(list).getByRole("listitem");

    expect(scroller).toHaveAttribute("tabindex", "0");
    scroller.focus();
    expect(scroller).toHaveFocus();
    expect(scroller).toHaveClass(
      "overflow-x-auto",
      "overscroll-x-contain",
      "[-webkit-overflow-scrolling:touch]",
    );
    expect(row).toHaveStyle({
      minWidth: "fit-content",
      gridTemplateColumns:
        "minmax(10rem, 1.6fr) minmax(6rem, 1fr) 5rem 5rem 5rem 4rem",
    });
  });
});
