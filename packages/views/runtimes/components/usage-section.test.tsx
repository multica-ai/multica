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
// callable and expose getState(), mirroring a real Zustand store.
vi.mock("@multica/core/runtimes/custom-pricing-store", () => {
  const state = { pricings: {} as Record<string, unknown> };
  const useCustomPricingStore = Object.assign(
    (sel?: (s: typeof state) => unknown) => (sel ? sel(state) : state),
    { getState: () => state },
  );
  return { useCustomPricingStore, getCustomPricing: () => undefined };
});

// useQuery is mocked so the component renders synchronously with canned
// data — the `kind` tag on each query-options object routes the response.
vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  const usageRows = [
    {
      runtime_id: "r-1",
      date: "2026-05-19",
      provider: "anthropic",
      model: "claude-sonnet-4-6",
      input_tokens: 1_000,
      output_tokens: 0,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
    },
  ];
  return {
    ...actual,
    useQuery: (opts: { kind?: string }) => ({
      data: opts?.kind === "usage" ? usageRows : [],
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
});
