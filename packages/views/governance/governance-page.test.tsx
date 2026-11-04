/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../test/i18n";
import { GovernancePage } from "./governance-page";
import { RUNNING } from "./governance-test-mocks";

const api = vi.hoisted(() => ({
  getCompanyState: vi.fn(),
  setCompanyState: vi.fn(),
  setQuietHours: vi.fn(),
  listGovernanceEvents: vi.fn(),
  exportGovernanceEventsCsv: vi.fn(),
  listBudgets: vi.fn(),
  listBudgetHolds: vi.fn(),
  listGoals: vi.fn(),
}));
const role = vi.hoisted(() => ({ value: "admin" as "owner" | "admin" | "member" }));
const nav = vi.hoisted(() => ({
  pathname: "/acme/governance",
  searchParams: new URLSearchParams(),
  replace: vi.fn(),
  push: vi.fn(),
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/permissions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/permissions")>()),
  useCurrentMember: () => ({ userId: "u-1", role: role.value, member: null, isLoading: false }),
}));
vi.mock("@multica/core/workspace/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/workspace/hooks")>()),
  useActorName: () => ({ getActorName: (type: string, id: string) => `${type} ${id}` }),
}));
vi.mock("../navigation", () => ({
  AppLink: ({ children, ...props }: React.ComponentProps<"a">) => <a {...props}>{children}</a>,
  useNavigation: () => nav,
}));

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <GovernancePage />
    </QueryClientProvider>,
  );
}

describe("GovernancePage", () => {
  beforeEach(() => {
    role.value = "admin";
    nav.searchParams = new URLSearchParams();
    nav.replace.mockReset();
    api.getCompanyState.mockResolvedValue(RUNNING);
    api.listGovernanceEvents.mockResolvedValue({ events: [], next_cursor: null });
    api.listBudgets.mockResolvedValue({ budgets: [] });
    api.listBudgetHolds.mockResolvedValue([]);
    api.listGoals.mockResolvedValue([]);
  });
  afterEach(cleanup);

  it("renders all six tabs and opens on Goals by default", async () => {
    renderPage();

    for (const name of ["Goals", "Budgets", "Autonomy", "Reviews", "Log", "Controls"]) {
      expect(screen.getByRole("tab", { name })).toBeTruthy();
    }
    expect(screen.getByRole("tab", { name: "Goals" }).getAttribute("aria-selected")).toBe("true");
    // The Budgets tab reads the budget list; with none it shows the empty state.
    await waitFor(() => expect(screen.getAllByText("Nothing to show yet").length).toBeGreaterThan(0));
  });

  it("selects the tab named by the URL", async () => {
    nav.searchParams = new URLSearchParams("tab=log");
    renderPage();

    expect(screen.getByRole("tab", { name: "Log" }).getAttribute("aria-selected")).toBe("true");
    expect(await screen.findByText("No governance events match these filters.")).toBeTruthy();
  });

  it("opens the Controls tab from a deep link", async () => {
    nav.searchParams = new URLSearchParams("tab=controls");
    renderPage();

    expect(await screen.findByRole("heading", { name: "Company state" })).toBeTruthy();
  });

  it("writes the chosen tab to the URL and keeps the other parameters", async () => {
    nav.searchParams = new URLSearchParams("kind=budget_hold");
    renderPage();

    fireEvent.click(screen.getByRole("tab", { name: "Budgets" }));
    await waitFor(() => expect(nav.replace).toHaveBeenCalled());
    const target = String(nav.replace.mock.calls[0]?.[0]);
    expect(target.startsWith("/acme/governance?")).toBe(true);
    expect(new URLSearchParams(target.split("?")[1]).get("tab")).toBe("budgets");
    expect(new URLSearchParams(target.split("?")[1]).get("kind")).toBe("budget_hold");
  });
});
