import type {
  PurchaseWorkspaceSeatsResponse,
  WorkspaceSeatPurchasePreview,
  WorkspaceSubscriptionSummary,
} from "@multica/core/types";

export function isSingleSeatInvitePreview(
  preview: WorkspaceSeatPurchasePreview | null | undefined,
): preview is WorkspaceSeatPurchasePreview {
  return Boolean(
    preview &&
      preview.additionalSeats === 1 &&
      preview.resultingSeats === preview.currentSeats + 1 &&
      Number.isSafeInteger(preview.prorationAmount) &&
      preview.prorationAmount >= 0 &&
      Number.isSafeInteger(preview.nextInvoiceAmount) &&
      preview.nextInvoiceAmount >= 0,
  );
}

export function seatPurchaseMatchesPreview(
  response: PurchaseWorkspaceSeatsResponse | null | undefined,
  preview: WorkspaceSeatPurchasePreview,
): response is PurchaseWorkspaceSeatsResponse {
  return Boolean(
    response &&
      response.currentSeats === preview.currentSeats &&
      response.additionalSeats === preview.additionalSeats &&
      response.resultingSeats === preview.resultingSeats &&
      response.currency === preview.currency,
  );
}

export function seatPurchaseCanRetryWithSameQuote(
  errorCode: string | undefined,
): boolean {
  return ![
    "seat_purchase_payment_failed",
    "seat_purchase_in_progress",
    "seat_quote_changed",
    "seat_capacity_changed",
  ].includes(errorCode ?? "");
}

export function purchasedSeatIsReadyForInvitation(
  summary: WorkspaceSubscriptionSummary | null | undefined,
  preview: WorkspaceSeatPurchasePreview,
  submittedAt: number,
  summaryUpdatedAt: number,
): boolean {
  const capacity = summary?.seatCapacity;
  return Boolean(
    summaryUpdatedAt >= submittedAt &&
      capacity &&
      capacity.purchased >= preview.resultingSeats &&
      capacity.available >= 1,
  );
}
