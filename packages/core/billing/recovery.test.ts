import { describe, expect, it } from "vitest";
import { resolveBillingRecovery } from "./recovery";

const actions = (
  overrides: Partial<{
    checkout: boolean;
    portal: boolean;
    purchaseSeats: boolean;
  }> = {},
) => ({ checkout: false, portal: false, purchaseSeats: false, ...overrides });

describe("resolveBillingRecovery", () => {
  it("fails closed when workspace billing is disabled", () => {
    expect(
      resolveBillingRecovery({
        actions: actions({ checkout: true }),
        billingEnabled: false,
        loading: false,
      }),
    ).toBe("billing_disabled");
  });

  it.each([
    [actions({ checkout: true }), "checkout"],
    [actions({ portal: true }), "portal"],
    [actions({ purchaseSeats: true }), "billing"],
    [actions(), "contact_admin"],
  ] as const)("uses Cloud actions %# as %s", (availableActions, expected) => {
    expect(
      resolveBillingRecovery({
        actions: availableActions,
        billingEnabled: true,
        loading: false,
      }),
    ).toBe(expected);
  });

  it("distinguishes a pending read from an unavailable one", () => {
    expect(
      resolveBillingRecovery({
        actions: undefined,
        billingEnabled: true,
        loading: true,
      }),
    ).toBe("checking");
    expect(
      resolveBillingRecovery({
        actions: undefined,
        billingEnabled: true,
        loading: false,
      }),
    ).toBe("billing_unavailable");
  });

  it("falls back to Billing after a portal launch fails", () => {
    expect(
      resolveBillingRecovery({
        actions: actions({ portal: true }),
        billingEnabled: true,
        loading: false,
        portalFailed: true,
      }),
    ).toBe("billing_unavailable");
  });
});
