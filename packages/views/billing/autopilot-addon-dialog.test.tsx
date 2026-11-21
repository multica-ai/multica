import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enBilling from "../locales/en/billing.json";

const apiMock = vi.hoisted(() => ({
  getWorkspaceSubscriptionPrices: vi.fn(),
  createWorkspaceAutopilotAddonCheckout: vi.fn(),
  getAutopilotQuotaUsage: vi.fn(),
}));
const mockOpenExternal = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: apiMock,
}));

vi.mock("../platform", () => ({ openExternal: mockOpenExternal }));

import { ApiError } from "@multica/core/api";
import { AutopilotAddonDialog } from "./autopilot-addon-dialog";

const recurring = (unitAmount: number, interval: "month" | "year") => ({
  currency: "usd",
  unitAmount,
  interval,
  intervalCount: 1,
});

const prices = (addon: unknown = { currency: "usd", unitAmount: 1000, runsPerPack: 1000 }) => ({
  pro: { month: recurring(1500, "month"), year: recurring(14400, "year") },
  business: { month: recurring(3000, "month"), year: recurring(28800, "year") },
  autopilotAddon: addon,
});

function renderDialog() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <I18nProvider locale="en" resources={{ en: { billing: enBilling } }}>
      <QueryClientProvider client={client}>
        <AutopilotAddonDialog wsId="ws-1" open onOpenChange={vi.fn()} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("AutopilotAddonDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiMock.getWorkspaceSubscriptionPrices.mockResolvedValue(prices());
    apiMock.getAutopilotQuotaUsage.mockResolvedValue({
      action: "enforce",
      used: 12000,
      reserved: 0,
      total: 12000,
      limit: 12000,
      reached: true,
      period_start: "2026-11-01T00:00:00Z",
      period_end: "2026-12-01T00:00:00Z",
      reset_at: "2026-12-01T00:00:00Z",
      blocked_counts: {},
    });
  });

  afterEach(cleanup);

  it("sends the chosen packs at the shown price to Stripe Checkout", async () => {
    apiMock.createWorkspaceAutopilotAddonCheckout.mockResolvedValue({
      sessionId: "cs_test_1",
      url: "https://checkout.stripe.com/c/pay/cs_test_1",
    });
    const user = userEvent.setup();
    renderDialog();

    expect(await screen.findByRole("radio", { name: /1,000 runs/ })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByText(/12,000 \/ 12,000 used this month/)).toBeInTheDocument();
    // No auto top-up and no in-app card charge in the first release.
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();

    await user.click(screen.getByRole("radio", { name: /3,000 runs/ }));
    await user.click(screen.getByRole("button", { name: "Continue to pay $30.00" }));

    await waitFor(() =>
      expect(mockOpenExternal).toHaveBeenCalledWith("https://checkout.stripe.com/c/pay/cs_test_1", {
        webTarget: "same-tab",
      }),
    );
    const request = apiMock.createWorkspaceAutopilotAddonCheckout.mock.calls[0]?.[0];
    expect(request).toMatchObject({ packs: 3, expectedUnitAmount: 1000, currency: "usd" });
    expect(request.idempotencyKey).toMatch(/^autopilot-addon-ws-1-/);
  });

  it("retries a failed request with the same key so it returns the same session", async () => {
    apiMock.createWorkspaceAutopilotAddonCheckout
      .mockRejectedValueOnce(new ApiError("unavailable", 503, "Service Unavailable"))
      .mockResolvedValueOnce({ sessionId: "cs_test_1", url: "https://checkout.stripe.com/c/pay/cs_test_1" });
    const user = userEvent.setup();
    renderDialog();

    await user.click(await screen.findByRole("button", { name: "Continue to pay $10.00" }));
    expect(await screen.findByText("The payment page couldn't be opened. Try again.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Continue to pay $10.00" }));
    await waitFor(() => expect(mockOpenExternal).toHaveBeenCalledTimes(1));

    const [first, second] = apiMock.createWorkspaceAutopilotAddonCheckout.mock.calls.map(
      (call) => call[0].idempotencyKey,
    );
    expect(second).toBe(first);
  });

  it("explains a refused checkout and starts over with a new key", async () => {
    apiMock.createWorkspaceAutopilotAddonCheckout
      .mockRejectedValueOnce(new ApiError("not purchasable", 409, "Conflict"))
      .mockResolvedValueOnce({ sessionId: "cs_test_2", url: "https://checkout.stripe.com/c/pay/cs_test_2" });
    const user = userEvent.setup();
    renderDialog();

    await user.click(await screen.findByRole("button", { name: "Continue to pay $10.00" }));
    expect(await screen.findByText(/can't be bought right now, or the price changed/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Continue to pay $10.00" }));
    await waitFor(() => expect(mockOpenExternal).toHaveBeenCalledTimes(1));

    const [first, second] = apiMock.createWorkspaceAutopilotAddonCheckout.mock.calls.map(
      (call) => call[0].idempotencyKey,
    );
    expect(second).not.toBe(first);
  });

  it("offers no purchase when Cloud sells no add-on", async () => {
    apiMock.getWorkspaceSubscriptionPrices.mockResolvedValue(prices(null));
    renderDialog();

    expect(
      await screen.findByText("This workspace can't buy add-on runs right now."),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Continue to payment" })).toBeDisabled();
  });
});
