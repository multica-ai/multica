// @vitest-environment jsdom

import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { AgentRuntime } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

// The viewer's tz (Viewing layer) drives both the trend and the heatmap.
const VIEWER_TZ = "Asia/Tokyo";

// runtimeUsageOptions is the trend-fetch query. Capture its args so the
// test can assert which tz the trend was wired with.
const runtimeUsageOptions = vi.hoisted(() =>
  vi.fn((..._args: unknown[]) => ({ kind: "usage" as const })),
);
const runtimeUsageByAgentOptions = vi.hoisted(() =>
  vi.fn((..._args: unknown[]) => ({ kind: "by-agent" as const })),
);

vi.mock("../../common/use-viewing-timezone", () => ({
  useViewingTimezone: () => VIEWER_TZ,
}));

vi.mock("@multica/core/runtimes/queries", () => ({
  runtimeUsageOptions,
  runtimeUsageByAgentOptions,
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ kind: "agents" as const }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// custom-pricing-store is consumed two ways: usage-section reads the store
// hook, and runtimes/utils reads getCustomPricing(). The hook must be both
// callable and expose getState(), mirroring a real Zustand store. Backed by
// a mutable holder so a test can seed saved overrides — with a hard-coded
// empty store, `collectUnmappedModels` can never see an override and the
// "saved rates stay editable" path below would be untestable.
const pricingState = vi.hoisted(() => ({
  pricings: {} as Record<string, unknown>,
}));

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

// Lets a test swap in its own usage rows (e.g. an unpriced model) without
// re-mocking the whole query layer. `null` keeps the default fixture.
const usageOverride = vi.hoisted(() => ({ rows: null as unknown[] | null }));

// useQuery is mocked so the component renders synchronously with canned
// data — the `kind` tag on each query-options object routes the response.
vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  const dateDaysAgo = (days: number) => {
    const date = new Date();
    date.setUTCDate(date.getUTCDate() - days);
    return date.toISOString().slice(0, 10);
  };
  const usageRows = [
    {
      runtime_id: "r-1",
      date: dateDaysAgo(0),
      provider: "anthropic",
      model: "claude-sonnet-4-6",
      input_tokens: 1_000,
      output_tokens: 0,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
    },
    {
      runtime_id: "r-1",
      date: dateDaysAgo(15),
      provider: "anthropic",
      model: "claude-sonnet-4-6",
      input_tokens: 2_000,
      output_tokens: 0,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
    },
  ];
  return {
    ...actual,
    useQuery: (opts: { kind?: string }) => ({
      data: opts?.kind === "usage" ? (usageOverride.rows ?? usageRows) : [],
      isLoading: false,
    }),
  };
});

// Charts are recharts-heavy; stub them. ActivityHeatmap echoes its `tz`
// prop so the test can read which tz the heatmap was wired with.
vi.mock("./charts", () => ({
  DailyCostChart: () => <div data-testid="daily-cost-chart" />,
  DailyTokensChart: () => <div data-testid="daily-tokens-chart" />,
  WeeklyCostChart: () => <div data-testid="weekly-cost-chart" />,
  WeeklyTokensChart: () => <div data-testid="weekly-tokens-chart" />,
  ActivityHeatmap: ({ tz }: { tz: string }) => (
    <div data-testid="heatmap-tz">{tz}</div>
  ),
}));

vi.mock("./custom-pricing-dialog", () => ({
  CustomPricingDialog: () => null,
}));

import { UsageSection } from "./usage-section";

const RUNTIME: AgentRuntime = {
  id: "r-1",
  workspace_id: "ws-1",
  daemon_id: null,
  name: "test-runtime",
  runtime_mode: "cloud",
  provider: "claude",
  launch_header: "",
  status: "online",
  device_info: "",
  metadata: {},
  owner_id: null,
  visibility: "private",
  last_seen_at: null,
  created_at: "2026-05-01T00:00:00Z",
  updated_at: "2026-05-01T00:00:00Z",
};

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

describe("UsageSection — Viewing timezone wiring", () => {
  beforeEach(() => {
    runtimeUsageOptions.mockClear();
    runtimeUsageByAgentOptions.mockClear();
  });

  it("fetches the trend in the viewer's tz", () => {
    render(<UsageSection runtime={RUNTIME} />, { wrapper: Wrapper });

    expect(runtimeUsageOptions).toHaveBeenCalled();
    const [, days, tz] = runtimeUsageOptions.mock.calls[0]!;
    expect(days).toBe(180);
    expect(tz).toBe(VIEWER_TZ);
  });

  it("renders the heatmap in the viewer's tz", () => {
    render(<UsageSection runtime={RUNTIME} />, { wrapper: Wrapper });

    // The heatmap is an opt-in toggle inside the "When" card.
    fireEvent.click(screen.getByRole("button", { name: "Heatmap" }));

    expect(screen.getByTestId("heatmap-tz").textContent).toBe(VIEWER_TZ);
  });

  it("renders KPI values with NumberFlow and updates them when the period changes", () => {
    render(<UsageSection runtime={RUNTIME} />, { wrapper: Wrapper });

    const flows = Array.from(document.querySelectorAll("number-flow-react"));
    expect(flows).toHaveLength(3);
    expect(flows.at(-1)).toHaveAttribute("aria-label", "3K");
    expect(
      flows.every(
        (flow) =>
          (flow as HTMLElement & { respectMotionPreference?: boolean })
            .respectMotionPreference === true,
      ),
    ).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "7d" }));

    expect(flows.at(-1)).toHaveAttribute("aria-label", "1K");
  });
});

describe("UsageSection — custom-pricing entry point", () => {
  // A model that no maintained row prices, so it lands in the unmapped
  // diagnostic. `collectUnmappedModels` keys it by provider, so the saved
  // override below must use the same `acme/…` key the dialog would store.
  const UNPRICED_KEY = "acme/made-up-model-9";
  const unpricedRows = [
    {
      runtime_id: "r-1",
      date: new Date().toISOString().slice(0, 10),
      provider: "acme",
      model: "made-up-model-9",
      input_tokens: 1_000,
      output_tokens: 500,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
    },
  ];

  beforeEach(() => {
    usageOverride.rows = null;
    pricingState.pricings = {};
  });

  it("stays hidden when every model resolves and nothing is overridden", () => {
    render(<UsageSection runtime={RUNTIME} />, { wrapper: Wrapper });

    expect(
      screen.queryByRole("button", { name: "Set custom prices" }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Edit custom prices" }),
    ).toBeNull();
  });

  it("warns and offers the dialog while a model is unpriced", () => {
    usageOverride.rows = unpricedRows;

    render(<UsageSection runtime={RUNTIME} />, { wrapper: Wrapper });

    expect(screen.getByRole("alert")).toHaveTextContent(UNPRICED_KEY);
    expect(
      screen.getByRole("button", { name: "Set custom prices" }),
    ).toBeInTheDocument();
  });

  it("keeps the dialog reachable after the last override is saved", () => {
    // Regression: a saved override makes the model resolve, so the window
    // has nothing unmapped left. Gating the bar on "something is unmapped"
    // used to remove the only entry point here, stranding the user with
    // rates they could no longer edit or delete.
    usageOverride.rows = unpricedRows;
    pricingState.pricings = {
      [UNPRICED_KEY]: { input: 1, output: 2, cacheRead: 0.1, cacheWrite: 1 },
    };

    render(<UsageSection runtime={RUNTIME} />, { wrapper: Wrapper });

    expect(screen.queryByRole("alert")).toBeNull();
    expect(
      screen.getByRole("button", { name: "Edit custom prices" }),
    ).toBeInTheDocument();
  });
});
