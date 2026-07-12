import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { CerebroAccount } from "@multica/core/api";

const mockUseAccount = vi.hoisted(() => vi.fn());
const mockUseUsageHistory = vi.hoisted(() => vi.fn());

vi.mock("./use-cerebro-accounts", () => ({
  useCerebroAccount: mockUseAccount,
  useCerebroAccountUsageHistory: mockUseUsageHistory,
}));

vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useWorkspacePaths: () => actual.paths.workspace("acme"),
  };
});

vi.mock("@multica/views/navigation", () => ({
  AppLink: ({
    href,
    children,
    className,
  }: {
    href: string;
    children: ReactNode;
    className?: string;
  }) => (
    <a href={href} className={className}>
      {children}
    </a>
  ),
}));

// recharts' ResponsiveContainer measures the DOM, which jsdom can't do —
// flatten the chart to inspectable stubs.
vi.mock("recharts", () => ({
  ResponsiveContainer: ({ children }: { children: ReactNode }) => (
    <div data-testid="chart">{children}</div>
  ),
  BarChart: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  Bar: () => null,
  XAxis: () => null,
  YAxis: () => null,
  CartesianGrid: () => null,
  Tooltip: () => null,
}));

import { AccountDetailPage } from "./account-detail-page";

const account: CerebroAccount = {
  id: "acc-1",
  workspace_id: "ws-1",
  provider: "claude",
  login_identity: "user-a@example.com",
  usage_window_pct: null,
  throttled_until: null,
  usage_5h_pct: 40,
  usage_5h_resets_at: null,
  usage_7d_pct: 15,
  usage_7d_resets_at: null,
  tokens_5h: 1_500,
  tokens_7d: 2_400_000,
  extra_spend_on: false,
  paused_manual: false,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  runtime_count: 2,
  available_runtime_count: 1,
  nearest_unpause_at: null,
  status: "available",
};

beforeEach(() => {
  mockUseAccount.mockReset();
  mockUseUsageHistory.mockReset();
  mockUseUsageHistory.mockReturnValue({ data: [], isLoading: false });
});

describe("AccountDetailPage", () => {
  it("shows identity, remaining usage for both windows, and details", () => {
    mockUseAccount.mockReturnValue({ account, isLoading: false });

    render(<AccountDetailPage accountId="acc-1" />);

    expect(screen.getByText("user-a@example.com")).toBeInTheDocument();
    expect(screen.getByText("claude")).toBeInTheDocument();
    expect(screen.getByText("Available")).toBeInTheDocument();

    expect(screen.getByText("Next 5 hours")).toBeInTheDocument();
    expect(screen.getByText("60% left")).toBeInTheDocument();
    expect(screen.getByText("This week")).toBeInTheDocument();
    expect(screen.getByText("85% left")).toBeInTheDocument();
    expect(screen.getByText(/1\.5k tokens used last 5 hours/)).toBeInTheDocument();
    expect(screen.getByText(/2\.4M tokens used last 7 days/)).toBeInTheDocument();

    expect(screen.getByText("1 of 2 available")).toBeInTheDocument();
    expect(screen.getByText("Not throttled")).toBeInTheDocument();
  });

  it("shows reset countdowns when the provider reports them", () => {
    const inTwoHours = new Date(Date.now() + 2 * 60 * 60 * 1000 + 60_000).toISOString();
    mockUseAccount.mockReturnValue({
      account: { ...account, usage_5h_resets_at: inTwoHours },
      isLoading: false,
    });

    render(<AccountDetailPage accountId="acc-1" />);

    expect(screen.getByText(/resets in 2h/)).toBeInTheDocument();
  });

  it("renders the usage chart when history buckets exist", () => {
    mockUseAccount.mockReturnValue({ account, isLoading: false });
    mockUseUsageHistory.mockReturnValue({
      data: [
        { bucket: "2026-07-11T10:00:00Z", tokens: 1000 },
        { bucket: "2026-07-11T11:00:00Z", tokens: 2500 },
      ],
      isLoading: false,
    });

    render(<AccountDetailPage accountId="acc-1" />);

    expect(screen.getByTestId("chart")).toBeInTheDocument();
    expect(screen.getByText("3.5k total")).toBeInTheDocument();
  });

  it("shows an empty chart state without history", () => {
    mockUseAccount.mockReturnValue({ account, isLoading: false });

    render(<AccountDetailPage accountId="acc-1" />);

    expect(screen.getByText("No usage recorded yet.")).toBeInTheDocument();
  });

  it("shows 'No data yet' when the provider has not reported a window", () => {
    mockUseAccount.mockReturnValue({
      account: { ...account, usage_5h_pct: null, usage_window_pct: null, usage_7d_pct: null },
      isLoading: false,
    });

    render(<AccountDetailPage accountId="acc-1" />);

    expect(screen.getAllByText("No data yet")).toHaveLength(2);
  });

  it("shows a not-found state for an unknown account id", () => {
    mockUseAccount.mockReturnValue({ account: null, isLoading: false });

    render(<AccountDetailPage accountId="nope" />);

    expect(screen.getByText("Account not found")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Accounts/ })).toHaveAttribute(
      "href",
      "/acme/settings?tab=accounts",
    );
  });
});
