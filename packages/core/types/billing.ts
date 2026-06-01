// Mirrors the multica-cloud Billing module response shapes
// (multica-cloud/docs/api/billing.md). These types are the contract our
// frontend consumes via /api/cloud-billing/* — multica-api itself does
// not own the schema, it just proxies bytes. Keep field names verbatim
// with what the cloud sends.
//
// Unit convention (from the cloud doc):
//   - micro-credit (BIGINT): internal storage unit; 1 credit = 1_000_000 micro
//   - credit:                user-facing display unit; 1 USD = 1000 credit
// Always show users `*_credit` fields when present; only do math on
// `*_micro` to avoid float drift.

// GET /balance
export interface BillingBalance {
  owner_id: string;
  balance_micro: number;
  balance_credit: number;
  updated_at: string;
}

// `tx_type` values per the cloud doc's enum. Exported as a union for
// reference / display switches; the actual interface field below is
// typed as plain `string` so a future cloud-side enum widening doesn't
// crash the parser. Frontend should switch on these values when known
// and fall back to a generic display otherwise.
export type BillingTxType =
  | "topup"
  | "deduction"
  | "refund"
  | "expire"
  | "adjustment";

// `source` values per the cloud doc's enum. Same loosening rationale
// as BillingTxType above.
export type BillingTxSource =
  | "gateway"
  | "fleet"
  | "topup"
  | "refund"
  | "admin"
  | "system";

export interface BillingTransaction {
  id: string;
  owner_id: string;
  idempotency_key: string;
  // tx_type / source are widened to string here even though the cloud
  // doc enumerates a fixed set; see comment on BillingTxType. UIs that
  // care should switch on the value and gracefully default.
  tx_type: string;
  source: string;
  // amount_micro is positive for credits, negative for deductions.
  amount_micro: number;
  balance_after: number;
  reference_id: string;
  description: string;
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface BillingTransactionsPage {
  items: BillingTransaction[];
  total: number;
  page: number;
  page_size: number;
}

// `source_type` of a credit batch. `purchase` = paid topup,
// `bonus`   = subscription / promo gift, `adjustment` = ops fix.
// Exported as a union for documentation; the field is widened to
// string in BillingBatch (same rationale as BillingTxType).
export type BillingBatchSourceType = "purchase" | "bonus" | "adjustment";

export interface BillingBatch {
  id: string;
  owner_id: string;
  source_tx_id: string;
  // Widened to string; see BillingBatchSourceType comment above.
  source_type: string;
  total_micro: number;
  remaining_micro: number;
  // expires_at omitted means the batch never expires. Bonus batches
  // typically carry an expiry; purchase batches typically don't.
  expires_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface BillingBatchesPage {
  items: BillingBatch[];
  total: number;
  page: number;
  page_size: number;
}

// Topup order lifecycle. `pending` = checkout open, `paid` = Stripe
// confirmed payment but credit not yet booked, `credited` = wallet
// updated, `failed`/`canceled` = terminal failures. Exported as a
// union for documentation; the field is widened to string in
// BillingTopup / BillingCheckoutSessionStatus.
export type BillingTopupStatus =
  | "pending"
  | "paid"
  | "credited"
  | "failed"
  | "canceled";

export interface BillingTopup {
  id: string;
  owner_id: string;
  amount_cents: number;
  currency: string;
  credits: number;
  bonus_credits: number;
  // Widened to string; see BillingTopupStatus comment.
  status: string;
  tier_id: string;
  stripe_checkout_id: string;
  // Set when status reaches `credited` and the purchase batch was
  // minted; before that the field is empty.
  purchase_batch_id?: string;
  created_at: string;
  updated_at: string;
}

export interface BillingTopupsPage {
  items: BillingTopup[];
  total: number;
  page: number;
  page_size: number;
}

// GET /price-tiers — returns server-authoritative purchasable tiers.
// Frontend should NEVER hard-code amount/credits; the upstream is the
// source of truth so prices can be updated without a frontend ship.
export interface BillingPriceTier {
  id: string;
  display_name: string;
  amount_cents: number;
  credits: number;
  // Both bonus fields are optional: when omitted, no bonus is granted.
  // When `bonus_expires_in` is omitted but `bonus_credits` is present,
  // the bonus credits never expire.
  bonus_credits?: number;
  // Go time.Duration string, e.g. "720h0m0s" = 30 days.
  bonus_expires_in?: string;
}

// POST /checkout-sessions
export interface CreateBillingCheckoutSessionRequest {
  tier_id: string;
  // Optional caller-provided email; only used by Stripe Checkout when
  // the owner doesn't yet have a Stripe customer record. Pass it when
  // we already know the user's email and want it pre-filled.
  customer_email?: string;
}

export interface CreateBillingCheckoutSessionResponse {
  order_id: string;
  session_id: string;
  // Stripe-hosted Checkout URL. Frontend redirects the browser here.
  url: string;
}

// GET /checkout-sessions/{session_id} — frontend polls this after
// returning from Stripe with `?session_id=...` to surface the credit
// status before the user navigates away.
export interface BillingCheckoutSessionStatus {
  order_id: string;
  // Widened to string; see BillingTopupStatus comment.
  status: string;
  amount_cents: number;
  credits: number;
  bonus_credits: number;
  currency: string;
  tier_id: string;
}

// POST /portal-sessions
export interface CreateBillingPortalSessionResponse {
  url: string;
}
