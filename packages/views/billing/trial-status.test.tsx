import { screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceTrialView } from "@multica/core/billing";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { renderWithI18n } from "../test/i18n";
import { TrialStatus } from "./trial-status";

const h = vi.hoisted(() => ({
  enabled: true,
  trial: { kind: "none" } as WorkspaceTrialView,
  calls: [] as Array<{ wsId: string; enabled: boolean }>,
}));

vi.mock("@multica/core/billing", () => ({
  useWorkspaceTrial: (wsId: string, enabled: boolean) => {
    h.calls.push({ wsId, enabled });
    return h.trial;
  },
}));
vi.mock("@multica/core/config", () => ({ useFeatureEnabled: () => h.enabled }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ settings: () => "/acme/settings" }),
}));

function renderStatus() {
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/issues",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
  return renderWithI18n(
    <NavigationProvider value={adapter}>
      <TrialStatus wsId="ws-1" />
    </NavigationProvider>,
  );
}

function active(daysLeft: number, warning: boolean): WorkspaceTrialView {
  return {
    kind: "active",
    plan: "business",
    startedAt: 0,
    endsAt: 14,
    daysLeft,
    day: 15 - daysLeft,
    totalDays: 14,
    warning,
    reminder: null,
  };
}

beforeEach(() => {
  h.enabled = true;
  h.trial = { kind: "none" };
  h.calls = [];
});

describe("TrialStatus", () => {
  it("counts down to Billing while the trial runs", () => {
    h.trial = active(9, false);
    renderStatus();

    const link = screen.getByRole("link", { name: "Business trial, 9 days left. Open Billing." });
    expect(link).toHaveAttribute("href", "/acme/settings?tab=billing");
    expect(link).toHaveTextContent("Business trial9 days left");
    expect(link).not.toHaveAttribute("data-warning");
  });

  it("warns for the last three days", () => {
    h.trial = active(1, true);
    renderStatus();

    const link = screen.getByRole("link", { name: "Business trial, 1 day left. Open Billing." });
    expect(link).toHaveAttribute("data-warning");
  });

  it("offers an upgrade for a week after the trial, then disappears", () => {
    h.trial = { kind: "ended", plan: "business", endsAt: 0, recent: true, confirmed: true, settledAt: 70_000, settled: true };
    const { unmount } = renderStatus();
    expect(screen.getByRole("link")).toHaveTextContent("FreeUpgrade");
    unmount();

    h.trial = { kind: "ended", plan: "business", endsAt: 0, recent: false, confirmed: true, settledAt: 70_000, settled: true };
    renderStatus();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("says the trial is ending, not that the workspace is on Free, until Cloud confirms", () => {
    h.trial = {
      kind: "ended",
      plan: "business",
      endsAt: 0,
      recent: true,
      confirmed: false,
      settledAt: null,
      settled: false,
    };
    renderStatus();

    const link = screen.getByRole("link", { name: "Business trial ending. Open Billing." });
    expect(link).toHaveTextContent("Business trialending");
    expect(link).not.toHaveTextContent("Free");
  });

  it("does not read billing where workspace subscriptions are off", () => {
    h.enabled = false;
    renderStatus();
    expect(h.calls).toEqual([{ wsId: "ws-1", enabled: false }]);
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });
});
