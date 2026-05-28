"use client";

// Test-quality billing page. Stuffs every /api/cloud-billing/* surface
// onto a single screen so we can verify the proxy + Stripe flow
// end-to-end without a designed UI. Sections:
//
//   1. Balance card
//   2. Stripe-success banner (visible only when the URL carries a
//      ?session_id=... — the user just came back from Stripe Checkout
//      and we poll the upstream until the topup is terminal).
//   3. Buy section: server-authoritative price tier buttons that POST
//      a checkout-session and redirect.window.location.href = url.
//   4. Billing Portal button.
//   5. Three lists: transactions / batches / topups.
//
// Anything past "make the API talk to Stripe and surface results" is
// out of scope here on purpose — when the real billing UI ships it
// will live elsewhere and this whole page can be deleted. Until then,
// the page intentionally hard-codes English labels rather than going
// through useT(): it is dev-only, not localized, and slated for
// deletion — so it is exempt from the package-wide i18next/no-literal-string
// rule.
/* eslint-disable i18next/no-literal-string */

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Loader2, RefreshCw, ExternalLink } from "lucide-react";
import { toast } from "sonner";
import {
  billingBalanceOptions,
  billingBatchesOptions,
  billingCheckoutSessionOptions,
  billingPriceTiersOptions,
  billingTopupsOptions,
  billingTransactionsOptions,
  useCreateCloudBillingCheckoutSession,
  useCreateCloudBillingPortalSession,
  useInvalidateBillingDataAfterCredit,
} from "@multica/core/billing";
import type {
  BillingBatch,
  BillingPriceTier,
  BillingTopup,
  BillingTransaction,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@multica/ui/components/ui/card";
import { useNavigation } from "../navigation";

// 1 credit = 1_000_000 micro-credit; cents → dollars factor for the
// Stripe-side display column. Documented at the top of the cloud
// billing.md so we don't sprinkle magic numbers through the UI.
const MICRO_PER_CREDIT = 1_000_000;
const CENTS_PER_DOLLAR = 100;

export function BillingTestPage() {
  const { searchParams, replace, pathname } = useNavigation();

  // The Stripe success URL on the cloud side has the literal
  // {CHECKOUT_SESSION_ID} placeholder which Stripe substitutes before
  // redirecting the browser. So when we land here, the param is real.
  const sessionId = searchParams.get("session_id") ?? "";

  return (
    <div className="space-y-6 p-6">
      <header>
        <h1 className="text-xl font-semibold">Billing (test page)</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Direct passthrough to multica-cloud&apos;s /api/v1/billing/*. Not
          a finished UI — just every endpoint stuffed onto one page so
          we can verify the proxy + Stripe flow.
        </p>
      </header>

      {sessionId && (
        <CheckoutSessionStatusBanner
          sessionId={sessionId}
          onDismiss={() => {
            // After we've shown the terminal status we strip
            // session_id from the URL so a refresh doesn't re-poll a
            // stale order. `replace` keeps the browser at the same
            // pathname without adding history.
            replace(pathname);
          }}
        />
      )}

      <BalanceCard />

      <BuyAndPortalSection />

      <TransactionsCard />

      <BatchesCard />

      <TopupsCard />
    </div>
  );
}

// ─── Stripe-success banner ───────────────────────────────────────────

// Polls /checkout-sessions/{id} every 2s until the order reaches a
// terminal state. Mounted only when ?session_id is in the URL.
function CheckoutSessionStatusBanner({
  sessionId,
  onDismiss,
}: {
  sessionId: string;
  onDismiss: () => void;
}) {
  const { data, isLoading, isError, error } = useQuery(
    billingCheckoutSessionOptions(sessionId),
  );

  const status = data?.status ?? (isLoading ? "loading" : "");
  const terminal =
    status === "credited" || status === "failed" || status === "canceled";

  // When the polling reaches a terminal state, the rest of the page
  // (balance, transactions, batches, topups) is still showing the
  // pre-checkout snapshot. Without this effect the user would see
  // "Final status: credited" up here while the balance card still
  // displays the old number — the only signal that things were stale
  // would be a manual refresh click. Invalidate the dependent
  // queries so they re-fetch in the background.
  //
  // Dep list `[terminal, ...]`: `terminal` only flips from false→true
  // once per session-id, so the invalidation fires exactly once. If
  // the caller mounts this banner with a session that is already in a
  // terminal state (e.g. user revisits the success URL after closing
  // and reopening the tab), `terminal` flips false→true on the first
  // data load and we still re-fetch — which is what we want, because
  // the cached snapshot is just as stale in that case.
  const invalidateBillingDataAfterCredit = useInvalidateBillingDataAfterCredit();
  useEffect(() => {
    if (terminal) invalidateBillingDataAfterCredit();
  }, [terminal, invalidateBillingDataAfterCredit]);

  return (
    <Card className="border-primary/40 bg-primary/5">
      <CardHeader>
        <CardTitle className="text-sm">
          Checkout session {sessionId.slice(0, 16)}…
        </CardTitle>
        <CardDescription className="text-xs">
          {isLoading
            ? "Loading order status…"
            : isError
              ? `Failed to fetch status: ${error instanceof Error ? error.message : "unknown error"}`
              : terminal
                ? `Final status: ${status}`
                : `Polling status… current: ${status || "unknown"}`}
        </CardDescription>
      </CardHeader>
      {data && (
        <CardContent className="text-xs">
          <dl className="grid grid-cols-[120px_1fr] gap-y-1">
            <dt className="text-muted-foreground">Order</dt>
            <dd className="font-mono">{data.order_id}</dd>
            <dt className="text-muted-foreground">Tier</dt>
            <dd>{data.tier_id}</dd>
            <dt className="text-muted-foreground">Charged</dt>
            <dd>
              {formatMoney(data.amount_cents, data.currency)} ·{" "}
              {data.credits.toLocaleString()} credits
              {data.bonus_credits > 0 &&
                ` + ${data.bonus_credits.toLocaleString()} bonus`}
            </dd>
          </dl>
          {terminal && (
            <Button
              variant="outline"
              size="sm"
              className="mt-3"
              onClick={onDismiss}
            >
              Clear from URL
            </Button>
          )}
        </CardContent>
      )}
    </Card>
  );
}

// ─── Balance ─────────────────────────────────────────────────────────

function BalanceCard() {
  const balance = useQuery(billingBalanceOptions());

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-sm">Balance</CardTitle>
          <CardDescription className="text-xs">
            GET /api/cloud-billing/balance
          </CardDescription>
        </div>
        <RefreshButton
          isLoading={balance.isFetching}
          onClick={() => void balance.refetch()}
        />
      </CardHeader>
      <CardContent>
        {balance.isLoading ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : balance.isError ? (
          <ErrorText error={balance.error} />
        ) : (
          <div className="space-y-1 text-sm">
            <div className="text-2xl font-semibold tabular-nums">
              {balance.data?.balance_credit.toLocaleString() ?? 0}
              <span className="ml-1 text-sm font-normal text-muted-foreground">
                credits
              </span>
            </div>
            <div className="text-xs text-muted-foreground">
              raw micro: {balance.data?.balance_micro.toLocaleString() ?? 0} ·
              owner: {balance.data?.owner_id.slice(0, 8) ?? ""}… · updated{" "}
              {formatDate(balance.data?.updated_at)}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// ─── Buy + Portal ────────────────────────────────────────────────────

function BuyAndPortalSection() {
  const tiers = useQuery(billingPriceTiersOptions());
  const createCheckout = useCreateCloudBillingCheckoutSession();
  const createPortal = useCreateCloudBillingPortalSession();
  const [busyTier, setBusyTier] = useState<string | null>(null);

  const handleBuy = async (tier: BillingPriceTier) => {
    setBusyTier(tier.id);
    try {
      const { url } = await createCheckout.mutateAsync({ tier_id: tier.id });
      if (!url) {
        toast.error("Cloud returned no checkout URL");
        return;
      }
      // Redirect via window.location instead of window.open so the
      // browser back button returns the user to this page after
      // Stripe redirects out. Stripe-hosted pages handle their own
      // SPA-like behaviour from there.
      window.location.href = url;
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Checkout failed");
    } finally {
      setBusyTier(null);
    }
  };

  const handlePortal = async () => {
    try {
      const { url } = await createPortal.mutateAsync();
      if (!url) {
        toast.error("No portal URL returned");
        return;
      }
      // Open in a new tab — the portal is a customer self-service
      // surface and keeping our session in this tab makes it easy to
      // come back and verify the resulting state via this same page.
      window.open(url, "_blank", "noopener,noreferrer");
    } catch (err) {
      // 400 is the documented "no Stripe customer yet" case from
      // upstream. Surface the body verbatim — it's the most useful
      // signal during testing.
      toast.error(err instanceof Error ? err.message : "Portal failed");
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">Buy credits / Manage billing</CardTitle>
        <CardDescription className="text-xs">
          GET /price-tiers · POST /checkout-sessions · POST /portal-sessions
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {tiers.isLoading ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : tiers.isError ? (
          <ErrorText error={tiers.error} />
        ) : tiers.data?.length ? (
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {tiers.data.map((tier) => (
              <TierButton
                key={tier.id}
                tier={tier}
                busy={busyTier === tier.id}
                disabled={busyTier !== null}
                onClick={() => void handleBuy(tier)}
              />
            ))}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">
            No price tiers configured. Cloud probably has no Stripe key set —
            checkout will return 503.
          </p>
        )}

        <div className="border-t pt-4">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={createPortal.isPending}
            onClick={() => void handlePortal()}
          >
            {createPortal.isPending ? (
              <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
            ) : (
              <ExternalLink className="mr-1.5 h-3.5 w-3.5" />
            )}
            Open Stripe Billing Portal
          </Button>
          <p className="mt-1 text-xs text-muted-foreground">
            400 is expected for users who haven&apos;t paid yet (no Stripe
            customer record exists).
          </p>
        </div>
      </CardContent>
    </Card>
  );
}

function TierButton({
  tier,
  busy,
  disabled,
  onClick,
}: {
  tier: BillingPriceTier;
  busy: boolean;
  disabled: boolean;
  onClick: () => void;
}) {
  const display = tier.display_name || tier.id;
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className="rounded-md border bg-background p-3 text-left transition hover:border-primary disabled:cursor-not-allowed disabled:opacity-50"
    >
      <div className="flex items-center justify-between">
        <div className="text-sm font-medium">{display}</div>
        {busy && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
      </div>
      <div className="mt-1 text-xs text-muted-foreground">
        {formatMoney(tier.amount_cents, "usd")} →{" "}
        {tier.credits.toLocaleString()} credits
        {tier.bonus_credits ? (
          <>
            {" "}
            + {tier.bonus_credits.toLocaleString()} bonus
            {tier.bonus_expires_in ? ` (${tier.bonus_expires_in})` : ""}
          </>
        ) : null}
      </div>
      <div className="mt-1 font-mono text-[10px] text-muted-foreground/70">
        id: {tier.id}
      </div>
    </button>
  );
}

// ─── Lists ───────────────────────────────────────────────────────────

function TransactionsCard() {
  const txs = useQuery(billingTransactionsOptions({ page: 1, page_size: 20 }));
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-sm">Transactions</CardTitle>
          <CardDescription className="text-xs">
            GET /api/cloud-billing/transactions
          </CardDescription>
        </div>
        <RefreshButton
          isLoading={txs.isFetching}
          onClick={() => void txs.refetch()}
        />
      </CardHeader>
      <CardContent>
        {txs.isLoading ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : txs.isError ? (
          <ErrorText error={txs.error} />
        ) : txs.data?.items.length ? (
          <ul className="space-y-2 text-xs">
            {txs.data.items.map((row) => (
              <TransactionRow key={row.id} row={row} />
            ))}
          </ul>
        ) : (
          <EmptyText>No transactions yet.</EmptyText>
        )}
        <PagingFooter
          page={txs.data?.page ?? 1}
          pageSize={txs.data?.page_size ?? 20}
          total={txs.data?.total ?? 0}
        />
      </CardContent>
    </Card>
  );
}

function TransactionRow({ row }: { row: BillingTransaction }) {
  const credit = row.amount_micro / MICRO_PER_CREDIT;
  return (
    <li className="rounded-md border bg-background p-2.5">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium">
          {row.tx_type}
          <span className="ml-1.5 rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
            {row.source}
          </span>
        </span>
        <span
          className={`text-sm tabular-nums ${
            credit >= 0 ? "text-green-700 dark:text-green-400" : "text-red-700 dark:text-red-400"
          }`}
        >
          {credit >= 0 ? "+" : ""}
          {credit.toLocaleString()} credits
        </span>
      </div>
      {row.description && (
        <div className="mt-1 text-xs text-muted-foreground">{row.description}</div>
      )}
      <div className="mt-1 font-mono text-[10px] text-muted-foreground/70">
        {formatDate(row.created_at)} · balance after:{" "}
        {(row.balance_after / MICRO_PER_CREDIT).toLocaleString()} credits · ref:{" "}
        {row.reference_id || "—"}
      </div>
    </li>
  );
}

function BatchesCard() {
  const batches = useQuery(billingBatchesOptions({ page: 1, page_size: 20 }));
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-sm">Credit batches</CardTitle>
          <CardDescription className="text-xs">
            GET /api/cloud-billing/batches
          </CardDescription>
        </div>
        <RefreshButton
          isLoading={batches.isFetching}
          onClick={() => void batches.refetch()}
        />
      </CardHeader>
      <CardContent>
        {batches.isLoading ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : batches.isError ? (
          <ErrorText error={batches.error} />
        ) : batches.data?.items.length ? (
          <ul className="space-y-2 text-xs">
            {batches.data.items.map((row) => (
              <BatchRow key={row.id} row={row} />
            ))}
          </ul>
        ) : (
          <EmptyText>No batches yet.</EmptyText>
        )}
        <PagingFooter
          page={batches.data?.page ?? 1}
          pageSize={batches.data?.page_size ?? 20}
          total={batches.data?.total ?? 0}
        />
      </CardContent>
    </Card>
  );
}

function BatchRow({ row }: { row: BillingBatch }) {
  const total = row.total_micro / MICRO_PER_CREDIT;
  const remaining = row.remaining_micro / MICRO_PER_CREDIT;
  const consumed = total - remaining;
  return (
    <li className="rounded-md border bg-background p-2.5">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium">
          {row.source_type}
          <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">
            {row.id.slice(0, 8)}…
          </span>
        </span>
        <span className="text-sm tabular-nums">
          {remaining.toLocaleString()} / {total.toLocaleString()} credits
        </span>
      </div>
      <div className="mt-1 text-xs text-muted-foreground">
        Consumed: {consumed.toLocaleString()} credits
        {row.expires_at ? ` · expires ${formatDate(row.expires_at)}` : " · never expires"}
      </div>
    </li>
  );
}

function TopupsCard() {
  const topups = useQuery(billingTopupsOptions({ page: 1, page_size: 20 }));
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-sm">Topup orders</CardTitle>
          <CardDescription className="text-xs">
            GET /api/cloud-billing/topups
          </CardDescription>
        </div>
        <RefreshButton
          isLoading={topups.isFetching}
          onClick={() => void topups.refetch()}
        />
      </CardHeader>
      <CardContent>
        {topups.isLoading ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : topups.isError ? (
          <ErrorText error={topups.error} />
        ) : topups.data?.items.length ? (
          <ul className="space-y-2 text-xs">
            {topups.data.items.map((row) => (
              <TopupRow key={row.id} row={row} />
            ))}
          </ul>
        ) : (
          <EmptyText>No topup orders yet.</EmptyText>
        )}
        <PagingFooter
          page={topups.data?.page ?? 1}
          pageSize={topups.data?.page_size ?? 20}
          total={topups.data?.total ?? 0}
        />
      </CardContent>
    </Card>
  );
}

function TopupRow({ row }: { row: BillingTopup }) {
  return (
    <li className="rounded-md border bg-background p-2.5">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium">
          {row.tier_id || row.id.slice(0, 8)}
          <span
            className={`ml-1.5 rounded px-1.5 py-0.5 font-mono text-[10px] ${
              row.status === "credited"
                ? "bg-green-500/10 text-green-700 dark:text-green-400"
                : row.status === "failed" || row.status === "canceled"
                  ? "bg-red-500/10 text-red-700 dark:text-red-400"
                  : "bg-amber-500/10 text-amber-700 dark:text-amber-400"
            }`}
          >
            {row.status}
          </span>
        </span>
        <span className="text-sm tabular-nums">
          {formatMoney(row.amount_cents, row.currency)} →{" "}
          {row.credits.toLocaleString()} credits
          {row.bonus_credits > 0 ? ` + ${row.bonus_credits} bonus` : ""}
        </span>
      </div>
      <div className="mt-1 font-mono text-[10px] text-muted-foreground/70">
        {formatDate(row.created_at)} · stripe: {row.stripe_checkout_id || "—"}
      </div>
    </li>
  );
}

// ─── Shared bits ─────────────────────────────────────────────────────

function PagingFooter({
  page,
  pageSize,
  total,
}: {
  page: number;
  pageSize: number;
  total: number;
}) {
  if (total === 0) return null;
  return (
    <div className="mt-3 text-[10px] text-muted-foreground">
      page {page} / {Math.max(1, Math.ceil(total / pageSize))} · {total} total
    </div>
  );
}

function RefreshButton({
  isLoading,
  onClick,
}: {
  isLoading: boolean;
  onClick: () => void;
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      className="h-7 w-7 p-0"
      onClick={onClick}
      disabled={isLoading}
      aria-label="Refresh"
    >
      <RefreshCw className={`h-3.5 w-3.5 ${isLoading ? "animate-spin" : ""}`} />
    </Button>
  );
}

function ErrorText({ error }: { error: unknown }) {
  return (
    <p className="text-xs text-destructive">
      {error instanceof Error ? error.message : "Request failed"}
    </p>
  );
}

function EmptyText({ children }: { children: React.ReactNode }) {
  return <p className="text-xs text-muted-foreground">{children}</p>;
}

function formatMoney(amountCents: number, currency: string): string {
  // Intl is fine here — no currency conversion happening, just
  // canonical display. Defaults to en-US to match the rest of the
  // dev UI.
  try {
    return new Intl.NumberFormat("en-US", {
      style: "currency",
      currency: currency.toUpperCase(),
    }).format(amountCents / CENTS_PER_DOLLAR);
  } catch {
    return `${(amountCents / CENTS_PER_DOLLAR).toFixed(2)} ${currency.toUpperCase()}`;
  }
}

function formatDate(value: string | undefined): string {
  if (!value) return "—";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  return d.toLocaleString();
}
