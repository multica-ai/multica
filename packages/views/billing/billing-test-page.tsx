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
// will live elsewhere and this whole page can be deleted. Strings
// are still routed through useT() so the package-wide
// i18next/no-literal-string lint rule passes; the namespace is
// `billing` and the en/zh-Hans bundles live alongside the other
// namespaces. When the real UI lands, both the namespace and this
// file get deleted together.

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
import { useT } from "../i18n";
import { useNavigation } from "../navigation";

// 1 credit = 1_000_000 micro-credit; cents → dollars factor for the
// Stripe-side display column. Documented at the top of the cloud
// billing.md so we don't sprinkle magic numbers through the UI.
const MICRO_PER_CREDIT = 1_000_000;
const CENTS_PER_DOLLAR = 100;

export function BillingTestPage() {
  const { t } = useT("billing");
  const { searchParams, replace, pathname } = useNavigation();

  // The Stripe success URL on the cloud side has the literal
  // {CHECKOUT_SESSION_ID} placeholder which Stripe substitutes before
  // redirecting the browser. So when we land here, the param is real.
  const sessionId = searchParams.get("session_id") ?? "";

  return (
    <div className="space-y-6 p-6">
      <header>
        <h1 className="text-xl font-semibold">{t(($) => $.title)}</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          {t(($) => $.subtitle)}
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
  const { t } = useT("billing");
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
  // Dep list `terminal`: terminal only flips from false→true once
  // per session-id, so the invalidation fires exactly once. If the
  // caller mounts this banner with a session that is already in a
  // terminal state (e.g. user revisits the success URL after closing
  // and reopening the tab), terminal flips false→true on the first
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
          {t(($) => $.checkout.session_label, { prefix: sessionId.slice(0, 16) })}
        </CardTitle>
        <CardDescription className="text-xs">
          {isLoading
            ? t(($) => $.checkout.loading)
            : isError
              ? t(($) => $.checkout.fetch_failed, {
                  error:
                    error instanceof Error
                      ? error.message
                      : t(($) => $.checkout.fetch_failed_unknown),
                })
              : terminal
                ? t(($) => $.checkout.final_status, { status })
                : t(($) => $.checkout.polling_status, {
                    status: status || t(($) => $.checkout.status_unknown),
                  })}
        </CardDescription>
      </CardHeader>
      {data && (
        <CardContent className="text-xs">
          <dl className="grid grid-cols-[120px_1fr] gap-y-1">
            <dt className="text-muted-foreground">{t(($) => $.checkout.label_order)}</dt>
            <dd className="font-mono">{data.order_id}</dd>
            <dt className="text-muted-foreground">{t(($) => $.checkout.label_tier)}</dt>
            <dd>{data.tier_id}</dd>
            <dt className="text-muted-foreground">{t(($) => $.checkout.label_charged)}</dt>
            <dd>
              {data.bonus_credits > 0
                ? t(($) => $.checkout.charged_with_bonus, {
                    money: formatMoney(data.amount_cents, data.currency),
                    credits: data.credits.toLocaleString(),
                    bonus: data.bonus_credits.toLocaleString(),
                  })
                : t(($) => $.checkout.charged_value, {
                    money: formatMoney(data.amount_cents, data.currency),
                    credits: data.credits.toLocaleString(),
                  })}
            </dd>
          </dl>
          {terminal && (
            <Button
              variant="outline"
              size="sm"
              className="mt-3"
              onClick={onDismiss}
            >
              {t(($) => $.checkout.clear_url)}
            </Button>
          )}
        </CardContent>
      )}
    </Card>
  );
}

// ─── Balance ─────────────────────────────────────────────────────────

function BalanceCard() {
  const { t } = useT("billing");
  const balance = useQuery(billingBalanceOptions());

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-sm">{t(($) => $.balance.title)}</CardTitle>
          <CardDescription className="text-xs">
            {t(($) => $.endpoints.balance)}
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
                {t(($) => $.balance.credits_suffix)}
              </span>
            </div>
            <div className="text-xs text-muted-foreground">
              {t(($) => $.balance.meta, {
                micro: balance.data?.balance_micro.toLocaleString() ?? 0,
                owner: balance.data?.owner_id.slice(0, 8) ?? "",
                updated: formatDate(balance.data?.updated_at, t),
              })}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// ─── Buy + Portal ────────────────────────────────────────────────────

function BuyAndPortalSection() {
  const { t } = useT("billing");
  const tiers = useQuery(billingPriceTiersOptions());
  const createCheckout = useCreateCloudBillingCheckoutSession();
  const createPortal = useCreateCloudBillingPortalSession();
  const [busyTier, setBusyTier] = useState<string | null>(null);

  const handleBuy = async (tier: BillingPriceTier) => {
    setBusyTier(tier.id);
    try {
      const { url } = await createCheckout.mutateAsync({ tier_id: tier.id });
      if (!url) {
        toast.error(t(($) => $.buy.toast_no_url));
        return;
      }
      // Redirect via window.location instead of window.open so the
      // browser back button returns the user to this page after
      // Stripe redirects out. Stripe-hosted pages handle their own
      // SPA-like behaviour from there.
      window.location.href = url;
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : t(($) => $.buy.toast_checkout_failed),
      );
    } finally {
      setBusyTier(null);
    }
  };

  const handlePortal = async () => {
    try {
      const { url } = await createPortal.mutateAsync();
      if (!url) {
        toast.error(t(($) => $.buy.toast_no_portal_url));
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
      toast.error(
        err instanceof Error ? err.message : t(($) => $.buy.toast_portal_failed),
      );
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">{t(($) => $.buy.title)}</CardTitle>
        <CardDescription className="text-xs">
          {t(($) => $.endpoints.buy)}
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
          <p className="text-xs text-muted-foreground">{t(($) => $.buy.no_tiers)}</p>
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
            {t(($) => $.buy.open_portal)}
          </Button>
          <p className="mt-1 text-xs text-muted-foreground">
            {t(($) => $.buy.portal_hint)}
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
  const { t } = useT("billing");
  const display = tier.display_name || tier.id;
  const baseLine = t(($) => $.buy.tier_money_to_credits, {
    money: formatMoney(tier.amount_cents, "usd"),
    credits: tier.credits.toLocaleString(),
  });
  const bonusLine = tier.bonus_credits
    ? tier.bonus_expires_in
      ? t(($) => $.buy.tier_bonus_with_expiry, {
          credits: tier.bonus_credits.toLocaleString(),
          expiry: tier.bonus_expires_in,
        })
      : t(($) => $.buy.tier_bonus, {
          credits: tier.bonus_credits.toLocaleString(),
        })
    : "";
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
        {baseLine}
        {bonusLine}
      </div>
      <div className="mt-1 font-mono text-[10px] text-muted-foreground/70">
        {t(($) => $.buy.tier_id, { id: tier.id })}
      </div>
    </button>
  );
}

// ─── Lists ───────────────────────────────────────────────────────────

function TransactionsCard() {
  const { t } = useT("billing");
  const txs = useQuery(billingTransactionsOptions({ page: 1, page_size: 20 }));
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-sm">{t(($) => $.transactions.title)}</CardTitle>
          <CardDescription className="text-xs">
            {t(($) => $.endpoints.transactions)}
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
          <EmptyText>{t(($) => $.transactions.empty)}</EmptyText>
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
  const { t } = useT("billing");
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
            credit >= 0
              ? "text-green-700 dark:text-green-400"
              : "text-red-700 dark:text-red-400"
          }`}
        >
          {t(($) => $.transactions.credits_value, {
            value: `${credit >= 0 ? "+" : ""}${credit.toLocaleString()}`,
          })}
        </span>
      </div>
      {row.description && (
        <div className="mt-1 text-xs text-muted-foreground">{row.description}</div>
      )}
      <div className="mt-1 font-mono text-[10px] text-muted-foreground/70">
        {t(($) => $.transactions.row_meta, {
          date: formatDate(row.created_at, t),
          balance: (row.balance_after / MICRO_PER_CREDIT).toLocaleString(),
          ref: row.reference_id || t(($) => $.transactions.ref_empty),
        })}
      </div>
    </li>
  );
}

function BatchesCard() {
  const { t } = useT("billing");
  const batches = useQuery(billingBatchesOptions({ page: 1, page_size: 20 }));
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-sm">{t(($) => $.batches.title)}</CardTitle>
          <CardDescription className="text-xs">
            {t(($) => $.endpoints.batches)}
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
          <EmptyText>{t(($) => $.batches.empty)}</EmptyText>
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
  const { t } = useT("billing");
  const total = row.total_micro / MICRO_PER_CREDIT;
  const remaining = row.remaining_micro / MICRO_PER_CREDIT;
  const consumed = total - remaining;
  return (
    <li className="rounded-md border bg-background p-2.5">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium">
          {row.source_type}
          <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">
            {t(($) => $.batches.id_suffix, { id: row.id.slice(0, 8) })}
          </span>
        </span>
        <span className="text-sm tabular-nums">
          {t(($) => $.batches.remaining_over_total, {
            remaining: remaining.toLocaleString(),
            total: total.toLocaleString(),
          })}
        </span>
      </div>
      <div className="mt-1 text-xs text-muted-foreground">
        {t(($) => $.batches.consumed, { value: consumed.toLocaleString() })}
        {row.expires_at
          ? t(($) => $.batches.expires_suffix, { value: formatDate(row.expires_at, t) })
          : t(($) => $.batches.never_expires_suffix)}
      </div>
    </li>
  );
}

function TopupsCard() {
  const { t } = useT("billing");
  const topups = useQuery(billingTopupsOptions({ page: 1, page_size: 20 }));
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-sm">{t(($) => $.topups.title)}</CardTitle>
          <CardDescription className="text-xs">
            {t(($) => $.endpoints.topups)}
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
          <EmptyText>{t(($) => $.topups.empty)}</EmptyText>
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
  const { t } = useT("billing");
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
          {row.bonus_credits > 0
            ? t(($) => $.topups.amount_to_credits_with_bonus, {
                money: formatMoney(row.amount_cents, row.currency),
                credits: row.credits.toLocaleString(),
                bonus: row.bonus_credits,
              })
            : t(($) => $.topups.amount_to_credits, {
                money: formatMoney(row.amount_cents, row.currency),
                credits: row.credits.toLocaleString(),
              })}
        </span>
      </div>
      <div className="mt-1 font-mono text-[10px] text-muted-foreground/70">
        {t(($) => $.topups.row_meta, {
          date: formatDate(row.created_at, t),
          checkout: row.stripe_checkout_id || t(($) => $.topups.stripe_empty),
        })}
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
  const { t } = useT("billing");
  if (total === 0) return null;
  return (
    <div className="mt-3 text-[10px] text-muted-foreground">
      {t(($) => $.shared.paging, {
        page,
        totalPages: Math.max(1, Math.ceil(total / pageSize)),
        total,
      })}
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
  const { t } = useT("billing");
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      className="h-7 w-7 p-0"
      onClick={onClick}
      disabled={isLoading}
      aria-label={t(($) => $.shared.refresh)}
    >
      <RefreshCw className={`h-3.5 w-3.5 ${isLoading ? "animate-spin" : ""}`} />
    </Button>
  );
}

function ErrorText({ error }: { error: unknown }) {
  const { t } = useT("billing");
  return (
    <p className="text-xs text-destructive">
      {error instanceof Error ? error.message : t(($) => $.shared.request_failed)}
    </p>
  );
}

function EmptyText({ children }: { children: React.ReactNode }) {
  return <p className="text-xs text-muted-foreground">{children}</p>;
}

function formatMoney(amountCents: number, currency: string): string {
  // Intl is fine here — no currency conversion happening, just
  // canonical display. Defaults to en-US to match the rest of the
  // dev UI; the produced string is then passed into a t() interpolation
  // so the surrounding sentence still gets translated.
  try {
    return new Intl.NumberFormat("en-US", {
      style: "currency",
      currency: currency.toUpperCase(),
    }).format(amountCents / CENTS_PER_DOLLAR);
  } catch {
    return `${(amountCents / CENTS_PER_DOLLAR).toFixed(2)} ${currency.toUpperCase()}`;
  }
}

// formatDate returns an ISO-ish localized string or the locale's "—"
// dash for missing/invalid input. The dash itself goes through the
// translation bundle so it stays consistent if either locale ever
// wants to swap it for a localized placeholder. Caller passes in `t`
// so we don't run useT() inside a util (the rule of hooks would
// trip on the conditional path).
function formatDate(
  value: string | undefined,
  t: ReturnType<typeof useT<"billing">>["t"],
): string {
  if (!value) return t(($) => $.shared.date_dash);
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  return d.toLocaleString();
}
