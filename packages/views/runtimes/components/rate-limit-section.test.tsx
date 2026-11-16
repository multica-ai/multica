// @vitest-environment jsdom

import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentRuntime, RuntimeRateLimitsResponse } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

const getRuntimeRateLimits = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({
  api: { getRuntimeRateLimits },
}));

vi.mock("../../common/use-viewing-timezone", () => ({
  useViewingTimezone: () => "UTC",
}));

import { RateLimitCell, RateLimitSection } from "./rate-limit-section";

// Mon 2026-11-16 11:46 UTC: 2h46m into the 09:00-14:00 session window.
const NOW = Date.parse("2026-11-16T11:46:00Z");

const RUNTIME: AgentRuntime = {
  id: "r-1",
  workspace_id: "ws-1",
  daemon_id: null,
  name: "Claude Code",
  runtime_mode: "local",
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

const SNAPSHOT: RuntimeRateLimitsResponse = {
  rate_limits: {
    plan_type: "pro",
    status: "warning",
    windows: [
      { kind: "session", used_percent: 72, window_minutes: 300, resets_at: "2026-11-16T14:00:00Z", status: "warning" },
      { kind: "weekly", used_percent: 41, window_minutes: 10080, resets_at: "2026-11-19T09:00:00Z", status: "allowed" },
    ],
    credits: { has_credits: true, unlimited: false, balance: "240" },
    observed_at: "2026-11-16T11:44:00Z",
  },
};

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        {children}
      </I18nProvider>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(NOW);
  getRuntimeRateLimits.mockReset();
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("RateLimitSection", () => {
  it("shows each window's usage, pace and reset", async () => {
    getRuntimeRateLimits.mockResolvedValue(SNAPSHOT);
    render(<RateLimitSection runtime={RUNTIME} />, { wrapper: Wrapper });

    expect(await screen.findByText("5-hour session")).toBeInTheDocument();
    expect(getRuntimeRateLimits).toHaveBeenCalledWith("r-1");
    expect(screen.getByText("Pro")).toBeInTheDocument();
    expect(screen.getByText("Updated 2m ago")).toBeInTheDocument();

    // 72% at 54.7% elapsed (reported 11:44): ahead of pace, out ~12:48.
    expect(screen.getByText("72% used")).toBeInTheDocument();
    expect(screen.getByText("17% ahead of pace · runs out in about 1h 2m")).toBeInTheDocument();
    expect(screen.getByText("Resets in 2h 14m")).toBeInTheDocument();

    // The weekly window is behind pace and resets more than a day out.
    expect(screen.getByText("Weekly")).toBeInTheDocument();
    expect(screen.getByText("41% used")).toBeInTheDocument();
    expect(screen.getByText("On pace to last until reset")).toBeInTheDocument();
    expect(screen.getByText("Resets Thu 09:00")).toBeInTheDocument();

    expect(screen.getByText("240 left")).toBeInTheDocument();
    const meters = screen.getAllByRole("meter");
    expect(meters.map((m) => m.getAttribute("aria-valuenow"))).toEqual(["72", "41"]);
  });

  it("explains an empty runtime instead of showing zeros", async () => {
    getRuntimeRateLimits.mockResolvedValue({ rate_limits: null });
    render(<RateLimitSection runtime={RUNTIME} />, { wrapper: Wrapper });
    expect(await screen.findByText("No limit data yet")).toBeInTheDocument();
    expect(screen.queryByRole("meter")).not.toBeInTheDocument();
  });

  it("marks a reached limit and does not project a pace", async () => {
    getRuntimeRateLimits.mockResolvedValue({
      rate_limits: {
        ...SNAPSHOT.rate_limits!,
        credits: undefined,
        windows: [{ kind: "session", used_percent: 100, window_minutes: 300, resets_at: "2026-11-16T14:20:00Z", status: "rejected" }],
      },
    });
    render(<RateLimitSection runtime={RUNTIME} />, { wrapper: Wrapper });
    expect(await screen.findByText("Limit reached")).toBeInTheDocument();
    expect(screen.getByText("Resumes after the reset")).toBeInTheDocument();
    expect(screen.queryByText(/ahead of pace/)).not.toBeInTheDocument();
  });

  it("drops the usage of a window that has reset since the report", async () => {
    getRuntimeRateLimits.mockResolvedValue({
      rate_limits: {
        ...SNAPSHOT.rate_limits!,
        credits: undefined,
        windows: [{ kind: "session", used_percent: 95, window_minutes: 300, resets_at: "2026-11-16T09:00:00Z", status: "warning" }],
        observed_at: "2026-11-16T08:30:00Z",
      },
    });
    render(<RateLimitSection runtime={RUNTIME} />, { wrapper: Wrapper });
    expect(await screen.findByText("Reset since last report")).toBeInTheDocument();
    expect(screen.queryByText("95% used")).not.toBeInTheDocument();
  });
});

describe("RateLimitCell", () => {
  it("summarizes the window closest to its limit", async () => {
    getRuntimeRateLimits.mockResolvedValue({
      rate_limits: {
        ...SNAPSHOT.rate_limits!,
        windows: [
          { kind: "session", used_percent: 18, window_minutes: 300, resets_at: "2026-11-16T15:32:00Z", status: "" },
          { kind: "weekly", used_percent: 93, window_minutes: 10080, resets_at: "2026-11-18T08:00:00Z", status: "" },
        ],
      },
    });
    render(<RateLimitCell runtime={RUNTIME} enabled />, { wrapper: Wrapper });
    expect(await screen.findByText("93%")).toBeInTheDocument();
  });

  it("shows a rejected window as full even without a reported share", async () => {
    getRuntimeRateLimits.mockResolvedValue({
      rate_limits: {
        ...SNAPSHOT.rate_limits!,
        windows: [{ kind: "session", window_minutes: 300, resets_at: "2026-11-16T14:20:00Z", status: "rejected" }],
      },
    });
    render(<RateLimitCell runtime={RUNTIME} enabled />, { wrapper: Wrapper });
    expect(await screen.findByText("100%")).toHaveClass("text-destructive");
  });

  it("does not fetch for a runtime whose CLI reports no limits", () => {
    render(<RateLimitCell runtime={{ ...RUNTIME, provider: "cursor" }} enabled />, { wrapper: Wrapper });
    expect(getRuntimeRateLimits).not.toHaveBeenCalled();
  });

  it("does not fetch for a viewer who cannot read the runtime", () => {
    render(<RateLimitCell runtime={RUNTIME} enabled={false} />, { wrapper: Wrapper });
    expect(getRuntimeRateLimits).not.toHaveBeenCalled();
  });
});
