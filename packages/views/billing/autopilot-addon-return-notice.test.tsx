import { cleanup, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enBilling from "../locales/en/billing.json";

const apiMock = vi.hoisted(() => ({
  getWorkspaceAutopilotAddonCheckout: vi.fn(),
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: apiMock,
}));

import { AutopilotAddonReturnNotice } from "./autopilot-addon-return-notice";

const session = (status: string) => ({
  sessionId: "cs_test_1",
  status,
  packs: 3,
  runs: 3000,
  currency: "usd",
  amount: 3000,
});

function renderNotice(sessionId: string | null) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidate = vi.spyOn(client, "invalidateQueries");
  render(
    <I18nProvider locale="en" resources={{ en: { billing: enBilling } }}>
      <QueryClientProvider client={client}>
        <AutopilotAddonReturnNotice wsId="ws-1" sessionId={sessionId} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { invalidate };
}

describe("AutopilotAddonReturnNotice", () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(cleanup);

  it("confirms a paid session and refreshes the limit and usage", async () => {
    apiMock.getWorkspaceAutopilotAddonCheckout.mockResolvedValue(session("paid"));
    const { invalidate } = renderNotice("cs_test_1");

    expect(await screen.findByText("Runs added")).toBeInTheDocument();
    expect(screen.getByText(/3,000 runs were added/)).toBeInTheDocument();
    expect(apiMock.getWorkspaceAutopilotAddonCheckout).toHaveBeenCalledWith("cs_test_1");
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["workspace-subscriptions", "ws-1"] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["autopilots", "ws-1", "usage"] });
  });

  it("says nothing was charged for an expired session", async () => {
    apiMock.getWorkspaceAutopilotAddonCheckout.mockResolvedValue(session("expired"));
    renderNotice("cs_test_1");
    expect(await screen.findByText("Payment not completed")).toBeInTheDocument();
  });

  it("claims no outcome without a session to read", () => {
    renderNotice("not-a-session");
    expect(screen.getByText("Confirming the payment")).toBeInTheDocument();
    expect(apiMock.getWorkspaceAutopilotAddonCheckout).not.toHaveBeenCalled();
  });
});
