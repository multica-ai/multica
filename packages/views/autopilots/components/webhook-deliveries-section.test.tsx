import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { screen, fireEvent, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import { EMPTY_WEBHOOK_DELIVERY } from "@multica/core/api/schemas";
import type { WebhookDelivery } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { WebhookDeliveriesSection } from "./webhook-deliveries-section";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/api", () => ({ api: { listAutopilotDeliveries: vi.fn(), getAutopilotDelivery: vi.fn() } }));

function renderDelivery(delivery: WebhookDelivery) {
  vi.mocked(api.listAutopilotDeliveries).mockResolvedValue({ deliveries: [delivery], total: 1 });
  vi.mocked(api.getAutopilotDelivery).mockResolvedValue(delivery);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <WebhookDeliveriesSection autopilotId="ap" hasWebhookTrigger />
    </QueryClientProvider>,
  );
}

describe("WebhookDeliveriesSection", () => {
  it("shows the failed field in the list and expected/actual typed values in a neutral detail section", async () => {
    renderDelivery({
      ...EMPTY_WEBHOOK_DELIVERY, id: "d", status: "ignored", reason_code: "payload_filtered", error: "payload_filtered: pull_request.base.ref",
      payload_condition_failure: {
        filter_index: 0, condition_index: 0,
        condition: { path: "pull_request.base.ref", op: "equals", value: "main" },
        actual_values: ['"dev"'], missing: false, truncated: false,
      },
    });
    fireEvent.click(await screen.findByRole("button", { name: /Payload conditions not met · pull_request.base.ref/ }));
    const reason = await screen.findByRole("region", { name: "Ignore reason" });
    expect(reason).toHaveTextContent('pull_request.base.ref is "main"');
    expect(reason).toHaveTextContent('Actual value"dev"');
    expect(screen.queryByText("Error", { exact: true })).not.toBeInTheDocument();
  });

  it("supports an older server's event-filtered reason without a condition snapshot", async () => {
    renderDelivery({ ...EMPTY_WEBHOOK_DELIVERY, id: "old", status: "ignored", error: "event_filtered" });
    fireEvent.click(await screen.findByRole("button", { name: /Event or action did not match/ }));
    expect(await screen.findByRole("region", { name: "Ignore reason" })).toHaveTextContent("Event or action did not match");
  });

  it("keeps real failures in the error section", async () => {
    renderDelivery({ ...EMPTY_WEBHOOK_DELIVERY, id: "failed", status: "failed", error: "dispatch failed" });
    fireEvent.click(await screen.findByRole("button", { name: /Failed/ }));
    expect(await screen.findByText("dispatch failed")).toBeInTheDocument();
    expect(within(screen.getByRole("dialog")).getByText("Error", { exact: true })).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Ignore reason" })).not.toBeInTheDocument();
  });
});
