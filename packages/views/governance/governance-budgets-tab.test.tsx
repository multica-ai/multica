/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../test/i18n";
import { GovernanceBudgetsTab, formatBudgetUsd } from "./governance-budgets-tab";

const api = vi.hoisted(() => ({
  listBudgets: vi.fn(),
  listBudgetHolds: vi.fn(),
  listBudgetPeriods: vi.fn(),
  releaseBudgetHolds: vi.fn(),
  extendBudget: vi.fn(),
  updateBudget: vi.fn(),
}));
const role = vi.hoisted(() => ({ value: "admin" as "owner" | "admin" | "member" }));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/permissions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/permissions")>()),
  useCurrentMember: () => ({ userId: "u-1", role: role.value, member: null, isLoading: false }),
}));

const monthStart = "2026-11-01T00:00:00.000Z";
const monthEnd = "2026-12-01T00:00:00.000Z";

function budget(over: Record<string, unknown> = {}) {
  return {
    id: "b-company",
    scope_type: "company",
    scope_id: null,
    period: "month",
    amount_micros: 100_000_000,
    soft_percent: 80,
    mode: "hold",
    classes: ["routine", "delegated"],
    status: "active",
    spent_micros: 85_000_000,
    extended_micros: 0,
    limit_micros: 100_000_000,
    runs: 12,
    state: "soft",
    period_start: monthStart,
    period_end: monthEnd,
    created_at: "2026-10-01T00:00:00.000Z",
    updated_at: "2026-10-01T00:00:00.000Z",
    ...over,
  };
}

function renderTab() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <GovernanceBudgetsTab />
    </QueryClientProvider>,
  );
}

describe("GovernanceBudgetsTab", () => {
  beforeEach(() => {
    role.value = "admin";
    for (const fn of Object.values(api)) fn.mockReset();
    api.listBudgetPeriods.mockResolvedValue([]);
  });
  afterEach(cleanup);

  it("shows the spend against the limit with the state the server reported", async () => {
    api.listBudgets.mockResolvedValue({ budgets: [budget()] });
    api.listBudgetHolds.mockResolvedValue([]);
    renderTab();

    await waitFor(() => expect(screen.getByTestId("budget-state").textContent).toBe("Near the limit"));
    expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe("85");
    expect(screen.getByText(/of \$100\.00 spent/)).toBeTruthy();
    expect(screen.getByTestId("budget-elapsed")).toBeTruthy();
  });

  it("names the holds in force and releases them once", async () => {
    api.listBudgets.mockResolvedValue({ budgets: [budget({ spent_micros: 100_000_000, state: "hard" })] });
    api.listBudgetHolds.mockResolvedValue([
      {
        id: "h1", budget_id: "b-company", task_id: "t1", agent_id: null, squad_id: null,
        run_class: "routine", scope_type: "company", scope_id: null, period: "month", created_at: monthStart,
      },
      {
        id: "h2", budget_id: "b-company", task_id: "t2", agent_id: null, squad_id: null,
        run_class: "delegated", scope_type: "company", scope_id: null, period: "month", created_at: monthStart,
      },
    ]);
    api.releaseBudgetHolds.mockResolvedValue({ released: 2 });
    renderTab();

    await waitFor(() => expect(screen.getByText("2 held")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Release holds" }));
    await waitFor(() => expect(api.releaseBudgetHolds).toHaveBeenCalledWith("b-company"));
  });

  it("extends the period by the amount typed, in dollars", async () => {
    api.listBudgets.mockResolvedValue({ budgets: [budget({ spent_micros: 100_000_000, state: "hard" })] });
    api.listBudgetHolds.mockResolvedValue([
      {
        id: "h1", budget_id: "b-company", task_id: "t1", agent_id: null, squad_id: null,
        run_class: "routine", scope_type: "company", scope_id: null, period: "month", created_at: monthStart,
      },
    ]);
    api.extendBudget.mockResolvedValue(budget());
    renderTab();

    await waitFor(() => expect(screen.getByRole("button", { name: "Extend" })).toBeTruthy());
    fireEvent.change(screen.getByLabelText("Amount to add to this period, in dollars"), { target: { value: "25" } });
    fireEvent.click(screen.getByRole("button", { name: "Extend" }));
    await waitFor(() => expect(api.extendBudget).toHaveBeenCalledWith("b-company", 25_000_000));
  });

  it("keeps writes from a member, who reads the same figures", async () => {
    role.value = "member";
    api.listBudgets.mockResolvedValue({ budgets: [budget()] });
    api.listBudgetHolds.mockResolvedValue([
      {
        id: "h1", budget_id: "b-company", task_id: "t1", agent_id: null, squad_id: null,
        run_class: "routine", scope_type: "company", scope_id: null, period: "month", created_at: monthStart,
      },
    ]);
    renderTab();

    await waitFor(() => expect(screen.getByText(/Only an owner or an admin/)).toBeTruthy());
    expect(screen.queryByRole("button", { name: "Release holds" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Extend" })).toBeNull();
  });

  it("shows the empty state when no budget exists yet", async () => {
    api.listBudgets.mockResolvedValue({ budgets: [] });
    api.listBudgetHolds.mockResolvedValue([]);
    renderTab();

    await waitFor(() => expect(screen.getByText("Nothing to show yet")).toBeTruthy());
  });

  it("offers New budget to an owner or admin, and not to a member", async () => {
    api.listBudgets.mockResolvedValue({ budgets: [budget()] });
    api.listBudgetHolds.mockResolvedValue([]);
    renderTab();
    expect(await screen.findByRole("button", { name: "New budget" })).toBeTruthy();

    role.value = "member";
    cleanup();
    renderTab();
    await waitFor(() => expect(screen.getByText(/Only an owner or an admin/)).toBeTruthy());
    expect(screen.queryByRole("button", { name: "New budget" })).toBeNull();
  });
});

describe("formatBudgetUsd", () => {
  it("formats millionths of a dollar as dollars", () => {
    expect(formatBudgetUsd(1_500_000)).toBe("$1.50");
    expect(formatBudgetUsd(0)).toBe("$0.00");
  });
});
