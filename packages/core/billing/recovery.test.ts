import { describe, expect, it } from "vitest";
import { resolveBillingRecovery } from "./recovery";

const actions = (
  overrides: Partial<{
    checkout: boolean;
    portal: boolean;
    purchaseSeats: boolean;
    changePlan: boolean;
    checkPlanChange: boolean;
    purchaseAutopilotRuns: boolean;
  }> = {},
) => ({
  checkout: false,
  portal: false,
  purchaseSeats: false,
  changePlan: false,
  checkPlanChange: false,
  purchaseAutopilotRuns: false,
  ...overrides,
});

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

  it("offers add-on runs to a paying workspace only on autopilot surfaces", () => {
    // Pro/Business manager: Cloud allows both the add-on and the portal.
    const paidManager = actions({
      portal: true,
      purchaseSeats: true,
      changePlan: true,
      purchaseAutopilotRuns: true,
    });
    expect(
      resolveBillingRecovery({
        actions: paidManager,
        billingEnabled: true,
        loading: false,
        autopilotAddon: true,
      }),
    ).toBe("autopilot_addon");
    // Other limit surfaces keep their previous recovery.
    expect(
      resolveBillingRecovery({
        actions: paidManager,
        billingEnabled: true,
        loading: false,
      }),
    ).toBe("portal");
    // Free or a no-card trial: upgrading still comes first.
    expect(
      resolveBillingRecovery({
        actions: actions({ checkout: true, purchaseAutopilotRuns: true }),
        billingEnabled: true,
        loading: false,
        autopilotAddon: true,
      }),
    ).toBe("checkout");
    // past_due: Cloud withholds the add-on, so the portal is the way back.
    expect(
      resolveBillingRecovery({
        actions: actions({ portal: true }),
        billingEnabled: true,
        loading: false,
        autopilotAddon: true,
      }),
    ).toBe("portal");
    // A member: nothing to buy.
    expect(
      resolveBillingRecovery({
        actions: actions(),
        billingEnabled: true,
        loading: false,
        autopilotAddon: true,
      }),
    ).toBe("contact_admin");
  });
});
