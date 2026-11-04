/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import userEvent from "@testing-library/user-event";
import { ApiError } from "@multica/core/api";
import { renderWithI18n } from "../test/i18n";
import { GovernanceBudgetCreateDialog } from "./governance-budget-create-dialog";

const api = vi.hoisted(() => ({
  createBudget: vi.fn(),
  listCompanyFloors: vi.fn(),
  listInitiatives: vi.fn(),
  listSquads: vi.fn(),
}));
const toasts = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));

vi.mock("sonner", () => ({ toast: toasts }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

function renderDialog(onOpenChange = vi.fn()) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <GovernanceBudgetCreateDialog open onOpenChange={onOpenChange} />
    </QueryClientProvider>,
  );
  return { onOpenChange };
}

async function pick(user: ReturnType<typeof userEvent.setup>, name: string, option: string) {
  await user.click(screen.getByRole("combobox", { name }));
  await user.click(await screen.findByRole("option", { name: option }, { timeout: 3000 }));
  // The list closes before the next select can take pointer input.
  await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
}

describe("GovernanceBudgetCreateDialog", { timeout: 30_000 }, () => {
  beforeEach(() => {
    api.createBudget.mockReset();
    toasts.success.mockReset();
    toasts.error.mockReset();
    api.listCompanyFloors.mockResolvedValue([]);
    api.listInitiatives.mockResolvedValue([]);
    api.listSquads.mockResolvedValue([
      { id: "sq-1", name: "Research", archived_at: null },
      { id: "sq-2", name: "Old team", archived_at: "2026-01-01T00:00:00Z" },
    ]);
  });
  afterEach(cleanup);

  it("creates a company budget with the defaults and closes once the server keeps it", async () => {
    const user = userEvent.setup({ delay: null });
    api.createBudget.mockResolvedValue({ id: "b-1" });
    const { onOpenChange } = renderDialog();

    fireEvent.change(screen.getByLabelText("Amount per period, in dollars"), { target: { value: "1200" } });
    await user.click(screen.getByRole("button", { name: "Create budget" }));

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(api.createBudget).toHaveBeenCalledWith(
      expect.objectContaining({
        scope_type: "company",
        period: "month",
        amount_micros: 1_200_000_000,
        soft_percent: 80,
        mode: "hold",
        classes: ["routine", "delegated"],
      }),
    );
    expect(toasts.success).toHaveBeenCalledWith("Budget created");
  });

  it("sends the chosen squad, period, mode and run classes", async () => {
    const user = userEvent.setup({ delay: null });
    api.createBudget.mockResolvedValue({ id: "b-2" });
    renderDialog();

    await pick(user, "Scope", "Squad");
    await pick(user, "Squad", "Research");
    await pick(user, "Period", "Weekly");
    await pick(user, "When the limit is reached", "holds runs at the limit");
    await user.click(screen.getByRole("checkbox", { name: "Delegated" }));
    fireEvent.change(screen.getByLabelText("Amount per period, in dollars"), { target: { value: "0.5" } });
    await user.click(screen.getByRole("button", { name: "Create budget" }));

    await waitFor(() => expect(api.createBudget).toHaveBeenCalledTimes(1));
    expect(api.createBudget).toHaveBeenCalledWith(
      expect.objectContaining({
        scope_type: "squad",
        scope_id: "sq-1",
        period: "week",
        amount_micros: 500_000,
        mode: "hold",
        classes: ["routine"],
      }),
    );
  });

  it("asks for a scope target and an amount before it sends anything", async () => {
    const user = userEvent.setup({ delay: null });
    renderDialog();

    await pick(user, "Scope", "Squad");
    await user.click(screen.getByRole("button", { name: "Create budget" }));

    expect(await screen.findByText("Choose the Squad this budget covers.")).toBeTruthy();
    expect(screen.getByText("Enter an amount of zero or more.")).toBeTruthy();
    expect(api.createBudget).not.toHaveBeenCalled();
  });

  it("says a budget already exists for the scope and period", async () => {
    const user = userEvent.setup({ delay: null });
    api.createBudget.mockRejectedValue(new ApiError("exists", 409, "Conflict"));
    const { onOpenChange } = renderDialog();

    fireEvent.change(screen.getByLabelText("Amount per period, in dollars"), { target: { value: "10" } });
    await user.click(screen.getByRole("button", { name: "Create budget" }));

    expect(await screen.findByText("A budget already exists for this scope and period.")).toBeTruthy();
    expect(onOpenChange).not.toHaveBeenCalled();
  });
});
