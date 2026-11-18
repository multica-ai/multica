// @vitest-environment node
import { describe, expect, it } from "vitest";
import { AppConfigSchema, EMPTY_WEBHOOK_DELIVERY, ListWebhookDeliveriesResponseSchema, WebhookDeliveryResponseSchema } from "./schemas";

describe("webhook delivery filter diagnostics", () => {
  it("requires explicit backend support before enabling payload conditions", () => {
    expect(AppConfigSchema.parse({}).webhook_payload_conditions_supported).toBe(false);
    expect(AppConfigSchema.parse({ webhook_payload_conditions_supported: "yes" }).webhook_payload_conditions_supported).toBe(false);
    expect(AppConfigSchema.parse({ webhook_payload_conditions_supported: true }).webhook_payload_conditions_supported).toBe(true);
  });
  const failure = {
    filter_index: 0, condition_index: 1,
    condition: { path: "pull_request.base.ref", op: "equals", value: "main" },
    actual_values: ['"dev"'], missing: false, truncated: false,
  };

  it("preserves the admission snapshot in list and detail responses", () => {
    const delivery = { ...EMPTY_WEBHOOK_DELIVERY, status: "ignored", reason_code: "payload_filtered", payload_condition_failure: failure };
    expect(WebhookDeliveryResponseSchema.parse(delivery).payload_condition_failure).toEqual(failure);
    expect(ListWebhookDeliveriesResponseSchema.parse({ deliveries: [delivery] }).deliveries[0]?.payload_condition_failure).toEqual(failure);
  });

  it("reads older servers and drops malformed diagnostics without losing the delivery", () => {
    expect(WebhookDeliveryResponseSchema.parse(EMPTY_WEBHOOK_DELIVERY).payload_condition_failure).toBeUndefined();
    for (const malformed of [{}, { ...failure, actual_values: [42] }, { ...failure, condition: { path: "x", op: "future_operator" } }]) {
      const parsed = WebhookDeliveryResponseSchema.parse({ ...EMPTY_WEBHOOK_DELIVERY, id: "kept", payload_condition_failure: malformed });
      expect(parsed.id).toBe("kept");
      expect(parsed.payload_condition_failure).toBeNull();
    }
  });
});
