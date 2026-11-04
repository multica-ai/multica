/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { HealthReport } from "@multica/core/governance";
import { renderWithI18n } from "../test/i18n";
import { GovernanceHealthSection } from "./governance-health-section";

const api = vi.hoisted(() => ({ getGovernanceHealth: vi.fn() }));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

const HEALTHY: HealthReport = {
  checked_at: "2026-11-03T10:00:00Z",
  jobs: [
    {
      name: "brief_catchup",
      label: "Brief catch-up",
      cadence_seconds: 3600,
      last_success_at: "2026-11-03T09:30:00Z",
      last_failed_at: null,
      lag_seconds: 1800,
      stuck: false,
      stuck_reason: "",
    },
    {
      name: "squad_attention",
      label: "Attention recompute",
      cadence_seconds: 86400,
      last_success_at: "2026-10-20T09:00:00Z",
      last_failed_at: null,
      lag_seconds: 1209600,
      stuck: true,
      stuck_reason: "no success for more than 3 cadences",
    },
  ],
  delivery_failures: 2,
  knowledge: { offered: true, state: "offline", reason: "" },
  budget_gate: { reported: true, cache_age_seconds: 42 },
  risks: ["Scheduled job Attention recompute is stuck: no success for more than 3 cadences."],
};

function renderSection() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <GovernanceHealthSection />
    </QueryClientProvider>,
  );
}

describe("GovernanceHealthSection", () => {
  beforeEach(() => {
    api.getGovernanceHealth.mockReset();
  });
  afterEach(cleanup);

  it("names each risk and marks the stuck job", async () => {
    api.getGovernanceHealth.mockResolvedValue(HEALTHY);
    renderSection();

    expect(await screen.findByText(/Attention recompute is stuck/)).toBeTruthy();
    const stuck = document.querySelector('[data-job="squad_attention"]');
    expect(stuck?.getAttribute("data-stuck")).toBe("true");
    expect(document.querySelector('[data-job="brief_catchup"]')?.getAttribute("data-stuck")).toBe("false");
  });

  it("reports the delivery failures, WeKnora state and the budget gate's cache age", async () => {
    api.getGovernanceHealth.mockResolvedValue(HEALTHY);
    renderSection();

    expect(await screen.findByText("Offline")).toBeTruthy();
    expect(screen.getByText("Counters refreshed 42 s ago")).toBeTruthy();
    expect(screen.getByText("2")).toBeTruthy();
  });

  it("says a missing budget gate reading is not reported, not healthy", async () => {
    api.getGovernanceHealth.mockResolvedValue({ ...HEALTHY, budget_gate: { reported: false, cache_age_seconds: null } });
    renderSection();

    expect(await screen.findByText("Not reported yet")).toBeTruthy();
  });

  it("says when the health cannot be read", async () => {
    api.getGovernanceHealth.mockRejectedValue(new Error("boom"));
    renderSection();

    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByText("The system health could not be read.")).toBeTruthy();
  });
});
