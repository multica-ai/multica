/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { CompanyPulse } from "@multica/core/governance";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { CompanyPulseStrip } from "./company-pulse-strip";

const api = vi.hoisted(() => ({ getCompanyPulse: vi.fn() }));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    governance: (tab?: string) => (tab ? `/acme/governance?tab=${tab}` : "/acme/governance"),
  }),
}));

const QUIET: CompanyPulse = {
  state: "running",
  decisions_waiting: 0,
  oldest_decision_age_seconds: -1,
  goals_at_risk: 0,
  budgets_above_soft_line: 0,
  budget_holds: 0,
  stalled_initiatives: 0,
  routines_late: 0,
  knowledge_retrieval_hit_rate: null,
  runtimes_offline: 0,
  generated_at: "2026-11-03T10:00:00Z",
};

const adapter: NavigationAdapter = {
  pathname: "/acme/company",
  searchParams: new URLSearchParams(),
  hash: "",
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  getShareableUrl: (path) => path,
};

function renderStrip() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <NavigationProvider value={adapter}>
      <QueryClientProvider client={client}>
        <CompanyPulseStrip />
      </QueryClientProvider>
    </NavigationProvider>,
  );
}

describe("CompanyPulseStrip", () => {
  beforeEach(() => {
    api.getCompanyPulse.mockReset();
  });
  afterEach(cleanup);

  it("shows nothing until the pulse is read", () => {
    api.getCompanyPulse.mockReturnValue(new Promise(() => {}));
    renderStrip();

    expect(screen.queryByRole("group", { name: "Company pulse" })).toBeNull();
    expect(screen.queryByText("Nothing needs you")).toBeNull();
  });

  it("says so when nothing waits", async () => {
    api.getCompanyPulse.mockResolvedValue(QUIET);
    renderStrip();

    expect(await screen.findByText("Nothing needs you")).toBeTruthy();
    expect(screen.queryByRole("group")).toBeNull();
  });

  it("opens the Governance tab that answers each reading", async () => {
    api.getCompanyPulse.mockResolvedValue({
      ...QUIET,
      goals_at_risk: 2,
      budget_holds: 1,
      runtimes_offline: 3,
      decisions_waiting: 4,
    });
    renderStrip();

    const goals = await screen.findByText("2 goals at risk");
    expect(goals.closest("a")?.getAttribute("href")).toBe("/acme/governance?tab=goals");
    expect(screen.getByText("1 budget holds").closest("a")?.getAttribute("href")).toBe("/acme/governance?tab=budgets");
    expect(screen.getByText("3 runtimes offline").closest("a")?.getAttribute("href")).toBe("/acme/governance?tab=controls");
    // Decisions have no Governance tab: the chip is shown, not linked.
    const decisions = screen.getByText("4 waiting for you");
    expect(decisions.closest("a")).toBeNull();
  });

  it("reads the pulse of the workspace", async () => {
    api.getCompanyPulse.mockResolvedValue(QUIET);
    renderStrip();

    await waitFor(() => expect(api.getCompanyPulse).toHaveBeenCalled());
  });
});
