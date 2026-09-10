import { describe, expect, it } from "vitest";
import type {
  WorkspaceSeatPurchasePreview,
  WorkspaceSubscriptionSummary,
} from "@multica/core/types";
import {
  isSingleSeatInvitePreview,
  purchasedSeatIsReadyForInvitation,
  seatInvitationCanRetryAfterPurchase,
  seatInvitationCapacityFailure,
  seatPurchaseCanRetryWithSameQuote,
  seatPurchaseMatchesPreview,
} from "./seat-invite-purchase";

const preview: WorkspaceSeatPurchasePreview = {
  currentSeats: 4,
  additionalSeats: 1,
  resultingSeats: 5,
  purchaseVersion: 7,
  currency: "usd",
  prorationAmount: 425,
  nextInvoiceAmount: 5_000,
  quotedAt: "2026-08-25T00:00:00Z",
};

function summary(purchased: number, available: number): WorkspaceSubscriptionSummary {
  return {
    entitlement: {
      workspaceId: "workspace-1",
      plan: "pro",
      status: "active",
      seats: purchased,
      limits: {
        issueCount: { mode: "unlimited", limit: null },
        autopilotRuns: { mode: "unlimited", limit: null },
      },
      currentPeriodEnd: null,
      snapshotExpiresAt: null,
      version: 1,
    },
    billingInterval: "month",
    humanMembers: 4,
    seatCapacity: {
      purchased,
      used: 4,
      reserved: purchased - 4 - available,
      available,
      overcommitted: false,
      version: 8,
      pendingQuantity: null,
      activePurchase: null,
    },
    cancelAtPeriodEnd: false,
    graceUntil: null,
    hasStripeCustomer: true,
    availableActions: {
      checkout: false,
      portal: true,
      purchaseSeats: true,
    },
  };
}

describe("invite seat purchase validation", () => {
  it("accepts only a coherent one-seat server preview", () => {
    expect(isSingleSeatInvitePreview(preview)).toBe(true);
    expect(
      isSingleSeatInvitePreview({ ...preview, resultingSeats: 6 }),
    ).toBe(false);
    expect(
      isSingleSeatInvitePreview({ ...preview, prorationAmount: -1 }),
    ).toBe(false);
  });

  it("requires the purchase response to match the accepted quote", () => {
    const response = {
      requestId: "request-1",
      currentSeats: 4,
      additionalSeats: 1,
      resultingSeats: 5,
      currency: "usd",
      prorationAmount: 425,
      nextInvoiceAmount: 5_000,
      status: "submitted" as const,
    };
    expect(seatPurchaseMatchesPreview(response, preview)).toBe(true);
    expect(
      seatPurchaseMatchesPreview({ ...response, resultingSeats: 6 }, preview),
    ).toBe(false);
  });

  it("reuses the idempotent quote only for retry-safe failures", () => {
    expect(seatPurchaseCanRetryWithSameQuote(undefined)).toBe(true);
    expect(seatPurchaseCanRetryWithSameQuote("seat_purchase_rate_limited")).toBe(
      true,
    );
    expect(seatPurchaseCanRetryWithSameQuote("seat_purchase_payment_failed")).toBe(
      false,
    );
    expect(seatPurchaseCanRetryWithSameQuote("seat_quote_changed")).toBe(false);
    expect(seatPurchaseCanRetryWithSameQuote("seat_capacity_changed")).toBe(false);
    expect(seatPurchaseCanRetryWithSameQuote("seat_purchase_in_progress")).toBe(
      false,
    );
  });

  it("retries the invitation only from a fresh summary with free capacity", () => {
    expect(purchasedSeatIsReadyForInvitation(summary(5, 1), preview, 100, 101)).toBe(true);
    expect(purchasedSeatIsReadyForInvitation(summary(5, 1), preview, 100, 99)).toBe(false);
    expect(purchasedSeatIsReadyForInvitation(summary(4, 0), preview, 100, 101)).toBe(false);
    expect(purchasedSeatIsReadyForInvitation(summary(5, 0), preview, 100, 101)).toBe(false);
  });

  it("classifies capacity toast branches without confusing purchase limits", () => {
    expect(seatInvitationCapacityFailure("seat_capacity_full")).toBe("full");
    expect(seatInvitationCapacityFailure("seat_capacity_unavailable")).toBe(
      "unavailable",
    );
    expect(seatInvitationCapacityFailure("seat_capacity_rate_limited")).toBe(
      "rate_limited",
    );
    expect(
      seatInvitationCapacityFailure("seat_purchase_rate_limited"),
    ).toBeUndefined();
  });

  it("allows transient post-purchase invitation failures to retry idempotently", () => {
    expect(
      seatInvitationCanRetryAfterPurchase("seat_capacity_rate_limited"),
    ).toBe(true);
    expect(
      seatInvitationCanRetryAfterPurchase("seat_capacity_unavailable"),
    ).toBe(true);
    expect(seatInvitationCanRetryAfterPurchase("seat_capacity_full")).toBe(
      false,
    );
  });
});
