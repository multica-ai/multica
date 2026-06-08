"use client";

import { toast } from "sonner";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useCurrentMember } from "@multica/core/permissions";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  type DisplayCurrency,
  formatCostWithOriginal,
  SUPPORTED_CURRENCIES,
} from "@multica/cerebro-format-cost";
import { useDisplayCurrencyQuery, useSetDisplayCurrencyMutation } from "./api";

const CURRENCY_LABELS: Record<DisplayCurrency, string> = {
  USD: "US Dollar (USD)",
  DKK: "Danish Krone (DKK)",
  EUR: "Euro (EUR)",
};

// Rates older than this are flagged as stale in the UI (the daily job missed a
// run); display still works off the last known rate.
const STALE_AFTER_MS = 48 * 60 * 60 * 1000;

function freshnessLabel(currency: DisplayCurrency, rate: number | undefined, fetchedAt: string | undefined): string {
  if (currency === "USD") {
    return "Cost is stored in USD — no conversion applied.";
  }
  if (rate === undefined) {
    return "No exchange rate cached yet — showing raw USD until the daily rate lands.";
  }
  const base = `1 USD = ${rate.toFixed(4)} ${currency}`;
  if (!fetchedAt) return base;
  const fetched = new Date(fetchedAt);
  const date = Number.isNaN(fetched.getTime()) ? fetchedAt : fetched.toLocaleDateString();
  const stale =
    !Number.isNaN(fetched.getTime()) && Date.now() - fetched.getTime() > STALE_AFTER_MS;
  return `${base} · updated ${date}${stale ? " (rate may be stale)" : ""}`;
}

export function CurrencySettingsTab() {
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const { role } = useCurrentMember(wsId);
  const canManage = role === "owner" || role === "admin";

  const { data } = useDisplayCurrencyQuery(wsId);
  const mutation = useSetDisplayCurrencyMutation(wsId);

  const currency = (data?.currency as DisplayCurrency) ?? "USD";
  const rate = data?.rate ?? undefined;
  const fetchedAt = data?.fetched_at ?? undefined;
  const preview = formatCostWithOriginal(123, { displayCurrency: currency, rate });

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-lg font-medium">Display currency</h2>
        <p className="text-sm text-muted-foreground">
          Cost is always recorded in USD. Choose the currency this workspace sees it
          in — amounts are converted at display time using a daily exchange rate, so
          historical figures are never rewritten.
        </p>
      </div>

      <Card>
        <CardContent className="space-y-4 pt-6">
          <div className="space-y-2">
            <Label htmlFor="display-currency-select">Currency</Label>
            <Select
              value={currency}
              onValueChange={(value) =>
                mutation.mutate(value as DisplayCurrency, {
                  onError: () => toast.error("Failed to update display currency"),
                })
              }
              disabled={!canManage || mutation.isPending}
            >
              <SelectTrigger id="display-currency-select" className="w-64">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SUPPORTED_CURRENCIES.map((c) => (
                  <SelectItem key={c} value={c}>
                    {CURRENCY_LABELS[c]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <p className="text-sm text-muted-foreground">
            {freshnessLabel(currency, rate, fetchedAt)}
          </p>

          <p className="text-sm">
            Example:{" "}
            <span className="font-medium">{preview.display}</span>
            {preview.converted && (
              <span className="text-muted-foreground"> ({preview.originalUsd})</span>
            )}
          </p>

          {!canManage && (
            <p className="text-xs text-muted-foreground">
              Only workspace owners and admins can change the display currency.
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
