"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, ExternalLink, Loader2 } from "@multica/ui/icons";
import { ApiError } from "@multica/core/api";
import { autopilotQuotaUsageOptions } from "@multica/core/autopilots";
import {
  useCreateWorkspaceAutopilotAddonCheckout,
  workspaceSubscriptionPricesOptions,
} from "@multica/core/billing";
import { Alert, AlertDescription } from "@multica/ui/components/ui/alert";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useLocale, useT } from "../i18n";
import { openExternal } from "../platform";
import { formatStripeMinorAmount } from "../settings/components/billing-format";

/** Pack counts the dialog offers; Cloud accepts up to ten. */
export const AUTOPILOT_ADDON_PACK_OPTIONS = [1, 3, 5] as const;

function createAddonIdempotencyKey(wsId: string): string {
  const suffix =
    globalThis.crypto?.randomUUID?.() ??
    `${Date.now()}-${Math.random().toString(36).slice(2)}`;
  return `autopilot-addon-${wsId}-${suffix}`.slice(0, 200);
}

/**
 * Pick autopilot run add-on packs for the current month, then pay for them on
 * Stripe Checkout. Stripe handles the card, authentication and declines;
 * nothing is charged until the payer completes Stripe's page, and the Billing
 * return confirms the runs.
 *
 * Only rendered for managers whose summary allows `purchaseAutopilotRuns`;
 * Cloud authorizes the purchase again.
 */
export function AutopilotAddonDialog({
  wsId,
  open,
  onOpenChange,
}: {
  wsId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("billing");
  const locale = useLocale();
  const pricesQuery = useQuery({
    ...workspaceSubscriptionPricesOptions(wsId),
    enabled: open && wsId.length > 0,
  });
  const usageQuery = useQuery({
    ...autopilotQuotaUsageOptions(wsId),
    enabled: open && wsId.length > 0,
  });
  const checkout = useCreateWorkspaceAutopilotAddonCheckout();
  const [packs, setPacks] = useState<number>(AUTOPILOT_ADDON_PACK_OPTIONS[0]);
  const [error, setError] = useState<string | null>(null);
  // One key per pack choice while the dialog is open, so a double click or a
  // retry after a lost response returns the same Checkout Session.
  const intentRef = useRef<{ packs: number; key: string } | null>(null);

  useEffect(() => {
    if (!open) return;
    setPacks(AUTOPILOT_ADDON_PACK_OPTIONS[0]);
    setError(null);
    intentRef.current = null;
  }, [open, wsId]);

  const price = pricesQuery.data?.autopilotAddon ?? null;
  const amountFor = (count: number) =>
    price ? formatStripeMinorAmount(price.unitAmount * count, price.currency, locale) : null;
  const selectedAmount = amountFor(packs);
  const numberFormatter = new Intl.NumberFormat(locale);
  const usage = usageQuery.data;
  const resetDate = usage?.reset_at
    ? new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(new Date(usage.reset_at))
    : null;
  const usedLine =
    usage && usage.action !== "off" && usage.total !== null && usage.limit !== null && resetDate
      ? t(($) => $.autopilot_addon.usage_summary, {
          used: numberFormatter.format(usage.total),
          limit: numberFormatter.format(usage.limit),
          date: resetDate,
        })
      : null;

  const submit = async () => {
    if (!price) return;
    if (intentRef.current?.packs !== packs) {
      intentRef.current = { packs, key: createAddonIdempotencyKey(wsId) };
    }
    setError(null);
    try {
      const session = await checkout.mutateAsync({
        packs,
        expectedUnitAmount: price.unitAmount,
        currency: price.currency,
        idempotencyKey: intentRef.current.key,
      });
      if (!session?.url) {
        setError(t(($) => $.autopilot_addon.error));
        return;
      }
      openExternal(session.url, { webTarget: "same-tab" });
    } catch (err) {
      if (err instanceof ApiError && (err.status === 409 || err.status === 403)) {
        intentRef.current = null;
        void pricesQuery.refetch();
        setError(t(($) => $.autopilot_addon.conflict));
        return;
      }
      setError(t(($) => $.autopilot_addon.error));
    }
  };

  const busy = checkout.isPending;

  return (
    <Dialog open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(($) => $.autopilot_addon.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.autopilot_addon.description)}</DialogDescription>
        </DialogHeader>

        {pricesQuery.isPending ? (
          <p className="flex items-center gap-2 text-body text-muted-foreground" role="status">
            <Loader2 className="size-4 animate-spin motion-reduce:animate-none" />
            {t(($) => $.autopilot_addon.loading)}
          </p>
        ) : !price ? (
          <Alert variant="destructive">
            <AlertCircle />
            <AlertDescription>{t(($) => $.autopilot_addon.unavailable)}</AlertDescription>
          </Alert>
        ) : (
          <div className="space-y-4">
            <div
              role="radiogroup"
              aria-label={t(($) => $.autopilot_addon.options_label)}
              className="space-y-2"
            >
              {AUTOPILOT_ADDON_PACK_OPTIONS.map((count) => {
                const selected = count === packs;
                return (
                  <button
                    key={count}
                    type="button"
                    role="radio"
                    aria-checked={selected}
                    disabled={busy}
                    onClick={() => setPacks(count)}
                    className={cn(
                      "flex w-full items-center gap-3 rounded-lg border px-4 py-3 text-left text-body transition-colors",
                      selected
                        ? "border-foreground"
                        : "border-surface-border hover:bg-accent/50",
                    )}
                  >
                    <span
                      aria-hidden="true"
                      className={cn(
                        "flex size-4 shrink-0 items-center justify-center rounded-full border",
                        selected ? "border-foreground" : "border-muted-foreground/50",
                      )}
                    >
                      {selected ? <span className="size-2 rounded-full bg-foreground" /> : null}
                    </span>
                    <span className="flex-1 tabular-nums">
                      {t(($) => $.autopilot_addon.option, {
                        runs: numberFormatter.format(count * price.runsPerPack),
                      })}
                    </span>
                    <span className="tabular-nums">{amountFor(count)}</span>
                  </button>
                );
              })}
            </div>
            {usedLine ? (
              <p className="text-caption leading-5 text-muted-foreground tabular-nums">{usedLine}</p>
            ) : null}
          </div>
        )}

        {error ? (
          <Alert variant="destructive">
            <AlertCircle />
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={() => onOpenChange(false)}>
            {t(($) => $.autopilot_addon.cancel)}
          </Button>
          <Button disabled={busy || !price} onClick={() => void submit()}>
            {busy ? (
              <Loader2 className="animate-spin motion-reduce:animate-none" />
            ) : (
              <ExternalLink />
            )}
            {selectedAmount !== null
              ? t(($) => $.autopilot_addon.pay, { amount: selectedAmount })
              : t(($) => $.autopilot_addon.pay_fallback)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
