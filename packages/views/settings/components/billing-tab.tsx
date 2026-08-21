"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AlertCircle,
  CheckCircle2,
  CreditCard,
  ExternalLink,
  Loader2,
  RefreshCw,
  ShieldCheck,
} from "lucide-react";
import { ApiError } from "@multica/core/api";
import { autopilotQuotaUsageOptions } from "@multica/core/autopilots";
import {
  useCreateWorkspaceSubscriptionCheckout,
  useCreateWorkspaceSubscriptionPortal,
  useReconcileWorkspaceSubscriptionSeats,
  workspaceSubscriptionEntitlementsOptions,
  workspaceSubscriptionPricesOptions,
  workspaceSubscriptionSummaryOptions,
} from "@multica/core/billing";
import { useFeatureEnabled } from "@multica/core/config";
import { BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG } from "@multica/core/feature-flags";
import { useCurrentMember } from "@multica/core/permissions";
import { useCurrentWorkspace } from "@multica/core/paths";
import type { WorkspaceSubscriptionInterval } from "@multica/core/types";
import {
  Alert,
  AlertDescription,
  AlertTitle,
} from "@multica/ui/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Progress,
  ProgressLabel,
  ProgressValue,
} from "@multica/ui/components/ui/progress";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useLocale, useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { openExternal } from "../../platform";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";
import {
  canPurchaseWorkspaceSubscription,
  hasManagedWorkspaceSubscription,
  resolveAutopilotUsage,
} from "./billing-state";

const CHECKOUT_SYNC_TIMEOUT_MS = 30_000;

const STRIPE_ZERO_DECIMAL_CURRENCIES = new Set([
  "BIF",
  "CLP",
  "DJF",
  "GNF",
  "JPY",
  "KMF",
  "KRW",
  "MGA",
  "PYG",
  "RWF",
  "VND",
  "VUV",
  "XAF",
  "XOF",
  "XPF",
]);
const STRIPE_TWO_DECIMAL_COMPAT_CURRENCIES = new Set(["ISK", "UGX"]);
const STRIPE_THREE_DECIMAL_CURRENCIES = new Set([
  "BHD",
  "JOD",
  "KWD",
  "OMR",
  "TND",
]);

type WorkspaceBillingReturnResult = "success" | "cancel" | "portal";

function parseReturnResult(
  value: string | null,
): WorkspaceBillingReturnResult | null {
  switch (value) {
    case "success":
    case "cancel":
    case "portal":
      return value;
    default:
      return null;
  }
}

function createIdempotencyKey(prefix: string, wsId: string): string {
  const suffix =
    globalThis.crypto?.randomUUID?.() ??
    `${Date.now()}-${Math.random().toString(36).slice(2)}`;
  return `${prefix}-${wsId}-${suffix}`.slice(0, 255);
}

function formatDate(value: string | null, locale: string): string | null {
  if (!value) return null;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(date);
}

function formatDateTime(value: string | null, locale: string): string | null {
  if (!value) return null;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  return new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

/**
 * Stripe API amounts use its own minor-unit contract: two decimals by default,
 * an explicit zero-decimal list, five three-decimal currencies, and ISK/UGX in
 * a backwards-compatible two-decimal representation. Intl localizes the
 * already-converted major amount; it must not decide the divisor.
 */
export function formatStripeMinorAmount(
  amount: number,
  currency: string,
  locale: string,
): string | null {
  if (!Number.isSafeInteger(amount) || amount < 0) return null;
  const normalizedCurrency = currency.trim().toUpperCase();
  if (!normalizedCurrency) return null;

  try {
    const fractionDigits = STRIPE_TWO_DECIMAL_COMPAT_CURRENCIES.has(
      normalizedCurrency,
    )
      ? 2
      : STRIPE_ZERO_DECIMAL_CURRENCIES.has(normalizedCurrency)
        ? 0
        : STRIPE_THREE_DECIMAL_CURRENCIES.has(normalizedCurrency)
          ? 3
          : 2;
    const majorAmount = amount / 10 ** fractionDigits;
    const showStripeFraction = !Number.isInteger(majorAmount);
    const formatter = new Intl.NumberFormat(locale, {
      style: "currency",
      currency: normalizedCurrency,
      ...(showStripeFraction
        ? {
            minimumFractionDigits: fractionDigits,
            maximumFractionDigits: fractionDigits,
          }
        : {}),
    });
    return formatter.format(majorAmount);
  } catch {
    return null;
  }
}

function planBadgeVariant(plan: string): "default" | "secondary" | "outline" {
  if (plan === "pro") return "default";
  if (plan === "free") return "secondary";
  return "outline";
}

function statusBadgeVariant(
  status: string,
): "default" | "secondary" | "destructive" | "outline" {
  switch (status) {
    case "active":
    case "trialing":
      return "default";
    case "past_due":
    case "incomplete":
    case "unpaid":
      return "destructive";
    case "inactive":
    case "canceled":
    case "incomplete_expired":
    case "paused":
      return "secondary";
    default:
      return "outline";
  }
}

/**
 * A second gate inside the tab keeps direct/test mounts fail-closed. The
 * Settings shell also omits this component and its navigation entry while the
 * flag is absent, so no subscription request is issued in either path.
 */
export function BillingTab() {
  const enabled = useFeatureEnabled(
    BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG,
    false,
  );
  return enabled ? <BillingTabContent /> : null;
}

function BillingTabContent() {
  const { t } = useT("billing");
  const locale = useLocale();
  const navigation = useNavigation();
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const currentMember = useCurrentMember(wsId);
  const canManage =
    currentMember.role === "owner" || currentMember.role === "admin";
  const returnResultParam = parseReturnResult(
    navigation.searchParams.get("result"),
  );
  const returnSessionId = navigation.searchParams.get("session_id");
  const callbackKey =
    navigation.searchParams.has("result") ||
    navigation.searchParams.has("session_id")
      ? `${navigation.searchParams.get("result") ?? ""}:${returnSessionId ?? ""}`
      : null;
  const [returnState, setReturnState] = useState<{
    workspaceId: string | null;
    result: WorkspaceBillingReturnResult | null;
    observedAt: number | null;
  }>(() => ({
    workspaceId: wsId || null,
    result: returnResultParam,
    observedAt: returnResultParam === "success" ? Date.now() : null,
  }));
  const returnStateMatchesWorkspace =
    returnState.workspaceId === null || returnState.workspaceId === wsId;
  const returnResult = returnStateMatchesWorkspace ? returnState.result : null;
  const returnObservedAt = returnStateMatchesWorkspace
    ? returnState.observedAt
    : null;
  const [interval, setInterval] =
    useState<WorkspaceSubscriptionInterval>("month");
  const [checkoutConfirmOpen, setCheckoutConfirmOpen] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [portalUnavailable, setPortalUnavailable] = useState(false);
  const [reconcileMessage, setReconcileMessage] = useState<string | null>(null);
  const [isSyncingCheckout, setIsSyncingCheckout] = useState(
    returnResult === "success",
  );
  const [syncTimedOut, setSyncTimedOut] = useState(false);
  const checkoutIntentRef = useRef<{
    wsId: string;
    interval: WorkspaceSubscriptionInterval;
    key: string;
  } | null>(null);
  const portalIntentKeyRef = useRef<string | null>(null);
  const consumedCallbackKeyRef = useRef<string | null>(null);

  useEffect(() => {
    checkoutIntentRef.current = null;
    portalIntentKeyRef.current = null;
    setPortalUnavailable(false);
    setActionError(null);
    setReconcileMessage(null);
  }, [wsId]);

  // Consume Stripe callback params once, then remove them with replace so a
  // refresh, copied URL, or settings-tab round trip cannot replay a banner or
  // restart subscription polling. Keep unrelated params, especially `tab`.
  useEffect(() => {
    if (!callbackKey || consumedCallbackKeyRef.current === callbackKey) return;
    consumedCallbackKeyRef.current = callbackKey;
    setReturnState({
      workspaceId: wsId || null,
      result: returnResultParam,
      observedAt: returnResultParam === "success" ? Date.now() : null,
    });
    if (returnResultParam === "cancel") checkoutIntentRef.current = null;

    const params = new URLSearchParams(navigation.searchParams);
    params.delete("result");
    params.delete("session_id");
    const query = params.toString();
    navigation.replace(
      query ? `${navigation.pathname}?${query}` : navigation.pathname,
    );
  }, [callbackKey, navigation, returnResultParam, wsId]);

  useEffect(() => {
    if (returnResult !== "success") {
      setIsSyncingCheckout(false);
      setSyncTimedOut(false);
      return;
    }
    setIsSyncingCheckout(true);
    setSyncTimedOut(false);
    const timeout = window.setTimeout(() => {
      setIsSyncingCheckout(false);
      setSyncTimedOut(true);
    }, CHECKOUT_SYNC_TIMEOUT_MS);
    return () => window.clearTimeout(timeout);
  }, [returnResult, wsId]);

  const entitlementQuery = useQuery({
    ...workspaceSubscriptionEntitlementsOptions(wsId),
    refetchInterval: isSyncingCheckout ? 2_000 : false,
  });
  const entitlements = entitlementQuery.data;
  const isCheckoutProConfirmed =
    returnResult === "success" &&
    returnObservedAt !== null &&
    entitlements?.plan === "pro" &&
    entitlementQuery.isFetchedAfterMount &&
    entitlementQuery.dataUpdatedAt >= returnObservedAt;
  const summaryQuery = useQuery({
    ...workspaceSubscriptionSummaryOptions(wsId),
    refetchInterval: isSyncingCheckout ? 2_000 : false,
  });
  const summaryUnavailable =
    summaryQuery.isError ||
    (!summaryQuery.isPending && summaryQuery.data == null);
  const quotaUsageQuery = useQuery(autopilotQuotaUsageOptions(wsId));
  const hasManagedSubscription = entitlements
    ? hasManagedWorkspaceSubscription(entitlements, summaryQuery.data)
    : false;
  const canUpgrade = entitlements
    ? canPurchaseWorkspaceSubscription(entitlements)
    : false;
  const pricesQuery = useQuery({
    ...workspaceSubscriptionPricesOptions(wsId),
    enabled: wsId.length > 0 && canUpgrade,
  });
  const checkoutMutation = useCreateWorkspaceSubscriptionCheckout(wsId);
  const portalMutation = useCreateWorkspaceSubscriptionPortal(wsId);
  const reconcileMutation = useReconcileWorkspaceSubscriptionSeats(wsId);
  const refetchEntitlements = entitlementQuery.refetch;
  const refetchSummary = summaryQuery.refetch;

  useEffect(() => {
    if (isSyncingCheckout && isCheckoutProConfirmed) {
      setIsSyncingCheckout(false);
      setSyncTimedOut(false);
      checkoutIntentRef.current = null;
    }
  }, [isCheckoutProConfirmed, isSyncingCheckout]);

  useEffect(() => {
    const graceUntil = summaryQuery.data?.graceUntil;
    if (!graceUntil) return;
    const graceUntilMs = new Date(graceUntil).getTime();
    if (Number.isNaN(graceUntilMs)) return;
    const delay = Math.max(0, graceUntilMs - Date.now()) + 100;
    const timeout = window.setTimeout(() => {
      void refetchEntitlements();
      void refetchSummary();
    }, Math.min(delay, 2_147_000_000));
    return () => window.clearTimeout(timeout);
  }, [refetchEntitlements, refetchSummary, summaryQuery.data?.graceUntil]);

  const planLabel = (plan: string) => {
    switch (plan) {
      case "free":
        return t(($) => $.workspace.plan.free);
      case "pro":
        return t(($) => $.workspace.plan.pro);
      default:
        return t(($) => $.workspace.plan.unknown);
    }
  };

  const statusLabel = (status: string) => {
    switch (status) {
      case "inactive":
        return t(($) => $.workspace.status.inactive);
      case "active":
        return t(($) => $.workspace.status.active);
      case "trialing":
        return t(($) => $.workspace.status.trialing);
      case "past_due":
        return t(($) => $.workspace.status.past_due);
      case "canceled":
        return t(($) => $.workspace.status.canceled);
      case "incomplete":
        return t(($) => $.workspace.status.incomplete);
      case "incomplete_expired":
        return t(($) => $.workspace.status.incomplete_expired);
      case "paused":
        return t(($) => $.workspace.status.paused);
      case "unpaid":
        return t(($) => $.workspace.status.unpaid);
      default:
        return t(($) => $.workspace.status.unknown);
    }
  };

  const reportActionError = (error: unknown, fallback: string) => {
    if (error instanceof ApiError && error.status === 503) {
      setActionError(t(($) => $.workspace.errors.temporarily_unavailable));
      return;
    }
    if (error instanceof ApiError && error.status === 403) {
      setActionError(t(($) => $.workspace.errors.permission_changed));
      return;
    }
    setActionError(fallback);
  };

  const handleCheckout = async () => {
    setActionError(null);
    const existing = checkoutIntentRef.current;
    const intent =
      existing?.wsId === wsId && existing.interval === interval
        ? existing
        : {
            wsId,
            interval,
            key: createIdempotencyKey("workspace-checkout", wsId),
          };
    checkoutIntentRef.current = intent;
    try {
      const response = await checkoutMutation.mutateAsync({
        interval,
        idempotencyKey: intent.key,
      });
      if (!response?.url) {
        setCheckoutConfirmOpen(false);
        setActionError(t(($) => $.workspace.errors.checkout_response));
        return;
      }
      setCheckoutConfirmOpen(false);
      openExternal(response.url, { webTarget: "same-tab" });
    } catch (error) {
      setCheckoutConfirmOpen(false);
      if (error instanceof ApiError && error.status === 409) {
        checkoutIntentRef.current = null;
        setActionError(t(($) => $.workspace.errors.already_subscribed));
        await Promise.all([entitlementQuery.refetch(), summaryQuery.refetch()]);
        return;
      }
      reportActionError(error, t(($) => $.workspace.errors.checkout_failed));
    }
  };

  const handleCheckoutConfirmOpenChange = (open: boolean) => {
    setCheckoutConfirmOpen(open);
    if (!open) checkoutIntentRef.current = null;
  };

  const handlePortal = async () => {
    setActionError(null);
    const key =
      portalIntentKeyRef.current ??
      createIdempotencyKey("workspace-portal", wsId);
    portalIntentKeyRef.current = key;
    try {
      const response = await portalMutation.mutateAsync(key);
      if (!response?.url) {
        setActionError(t(($) => $.workspace.errors.portal_response));
        return;
      }
      openExternal(response.url, { webTarget: "same-tab" });
      // A Portal URL is single-use. A later click is a new intent, while a
      // network failure above deliberately retains the key for safe retry.
      portalIntentKeyRef.current = null;
    } catch (error) {
      if (error instanceof ApiError && error.status === 404) {
        portalIntentKeyRef.current = null;
        setPortalUnavailable(true);
        setActionError(t(($) => $.workspace.errors.portal_unavailable));
        await Promise.all([entitlementQuery.refetch(), summaryQuery.refetch()]);
        return;
      }
      reportActionError(error, t(($) => $.workspace.errors.portal_failed));
    }
  };

  const handleReconcile = async () => {
    setActionError(null);
    setReconcileMessage(null);
    try {
      const response = await reconcileMutation.mutateAsync();
      if (!response) {
        setActionError(t(($) => $.workspace.errors.reconcile_response));
        return;
      }
      setReconcileMessage(
        t(($) => $.workspace.seats.reconciled, {
          actual: response.actualSeats,
          billed: response.billedSeats,
        }),
      );
      await Promise.all([entitlementQuery.refetch(), summaryQuery.refetch()]);
    } catch (error) {
      reportActionError(error, t(($) => $.workspace.errors.reconcile_failed));
    }
  };

  if (entitlementQuery.isPending) {
    return (
      <SettingsTab
        title={t(($) => $.workspace.title)}
        description={t(($) => $.workspace.description)}
      >
        <SettingsCard>
          <div
            className="space-y-4 p-4 motion-reduce:[&_[data-slot=skeleton]]:animate-none"
            aria-label={t(($) => $.workspace.loading)}
          >
            <Skeleton className="h-5 w-40" />
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-2/3" />
          </div>
        </SettingsCard>
      </SettingsTab>
    );
  }

  if (entitlementQuery.isError || !entitlements) {
    return (
      <SettingsTab
        title={t(($) => $.workspace.title)}
        description={t(($) => $.workspace.description)}
      >
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>{t(($) => $.workspace.load_failed.title)}</AlertTitle>
          <AlertDescription>
            <p>{t(($) => $.workspace.load_failed.description)}</p>
            <Button
              className="mt-3 h-11"
              variant="outline"
              onClick={() => void entitlementQuery.refetch()}
            >
              <RefreshCw />
              {t(($) => $.workspace.actions.retry)}
            </Button>
          </AlertDescription>
        </Alert>
      </SettingsTab>
    );
  }

  const summaryPeriodEnd = formatDate(
    summaryQuery.data?.entitlement.currentPeriodEnd ?? null,
    locale,
  );
  const graceUntilValue = summaryQuery.data?.graceUntil ?? null;
  const graceUntil = formatDate(graceUntilValue, locale);
  const graceUntilMs = graceUntilValue
    ? new Date(graceUntilValue).getTime()
    : Number.NaN;
  const hasActiveProGrace =
    entitlements.plan === "pro" &&
    entitlements.status === "past_due" &&
    Number.isFinite(graceUntilMs) &&
    graceUntilMs > Date.now();
  const canUseEntitlementUnlimited =
    entitlements.plan === "pro" &&
    (entitlements.status !== "past_due" || hasActiveProGrace) &&
    (returnResult !== "success" || isCheckoutProConfirmed);
  const actualSeats = summaryQuery.data?.actualSeats ?? entitlements.seats;
  const billedSeats = summaryQuery.data?.billedSeats;
  const pendingSeatQuantity = summaryQuery.data?.pendingSeatQuantity;
  const quotaUsage = resolveAutopilotUsage(
    entitlements,
    quotaUsageQuery.data,
    quotaUsageQuery.isError,
    canUseEntitlementUnlimited,
  );
  const quotaResetAt =
    quotaUsage.kind === "metered"
      ? formatDateTime(quotaUsage.resetAt, locale)
      : null;
  const numberFormatter = new Intl.NumberFormat(locale);
  const isMutating =
    checkoutMutation.isPending ||
    portalMutation.isPending ||
    reconcileMutation.isPending;
  const selectedPrice = pricesQuery.data?.[interval] ?? null;
  const formattedUnitPrice = selectedPrice
    ? formatStripeMinorAmount(
        selectedPrice.unitAmount,
        selectedPrice.currency,
        locale,
      )
    : null;
  const formattedEstimatedTotal =
    selectedPrice && actualSeats > 0
      ? formatStripeMinorAmount(
        selectedPrice.unitAmount * actualSeats,
        selectedPrice.currency,
        locale,
      )
    : null;
  const hasDisplayableUnitPrice =
    selectedPrice?.intervalCount === 1 && formattedUnitPrice !== null;
  const hasDisplayableEstimatedTotal =
    hasDisplayableUnitPrice && formattedEstimatedTotal !== null;
  const canRetryPrice = !pricesQuery.isLoading && selectedPrice === null;

  return (
    <SettingsTab
      title={t(($) => $.workspace.title)}
      description={t(($) => $.workspace.description)}
    >
      {returnResult === "cancel" ? (
        <Alert>
          <AlertCircle />
          <AlertTitle>{t(($) => $.workspace.return.cancel_title)}</AlertTitle>
          <AlertDescription>
            {t(($) => $.workspace.return.cancel_description)}
          </AlertDescription>
        </Alert>
      ) : null}

      {returnResult === "portal" ? (
        <Alert>
          <CheckCircle2 />
          <AlertTitle>{t(($) => $.workspace.return.portal_title)}</AlertTitle>
          <AlertDescription>
            {t(($) => $.workspace.return.portal_description)}
          </AlertDescription>
        </Alert>
      ) : null}

      {returnResult === "success" ? (
        <Alert>
          {isCheckoutProConfirmed ? (
            <CheckCircle2 />
          ) : (
            <Loader2
              className={
                isSyncingCheckout
                  ? "animate-spin motion-reduce:animate-none"
                  : undefined
              }
            />
          )}
          <AlertTitle>
            {isCheckoutProConfirmed
              ? t(($) => $.workspace.return.active_title)
              : t(($) => $.workspace.return.syncing_title)}
          </AlertTitle>
          <AlertDescription>
            {isCheckoutProConfirmed
              ? t(($) => $.workspace.return.active_description)
              : syncTimedOut
                ? t(($) => $.workspace.return.timeout_description)
                : t(($) => $.workspace.return.syncing_description)}
          </AlertDescription>
        </Alert>
      ) : null}

      {summaryQuery.data?.cancelAtPeriodEnd ? (
        <Alert>
          <AlertCircle />
          <AlertTitle>
            {t(($) => $.workspace.subscription_notice.canceling_title)}
          </AlertTitle>
          <AlertDescription>
            {summaryPeriodEnd
              ? t(
                  ($) =>
                    $.workspace.subscription_notice.canceling_description,
                  { date: summaryPeriodEnd },
                )
              : t(
                  ($) =>
                    $.workspace.subscription_notice
                      .canceling_description_without_date,
                )}
          </AlertDescription>
        </Alert>
      ) : null}

      {entitlements.status === "past_due" ? (
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>{t(($) => $.workspace.past_due.title)}</AlertTitle>
          <AlertDescription>
            {hasActiveProGrace && graceUntil
              ? t(($) => $.workspace.past_due.grace_description, {
                  date: graceUntil,
                })
              : t(($) => $.workspace.past_due.description)}
          </AlertDescription>
        </Alert>
      ) : null}

      {entitlements.status === "incomplete" ? (
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>
            {t(($) => $.workspace.subscription_notice.incomplete_title)}
          </AlertTitle>
          <AlertDescription>
            {t(($) => $.workspace.subscription_notice.incomplete_description)}
          </AlertDescription>
        </Alert>
      ) : null}

      {entitlements.status === "incomplete_expired" ? (
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>
            {t(
              ($) => $.workspace.subscription_notice.incomplete_expired_title,
            )}
          </AlertTitle>
          <AlertDescription>
            {t(
              ($) =>
                $.workspace.subscription_notice
                  .incomplete_expired_description,
            )}
          </AlertDescription>
        </Alert>
      ) : null}

      {entitlements.status === "paused" ? (
        <Alert>
          <AlertCircle />
          <AlertTitle>
            {t(($) => $.workspace.subscription_notice.paused_title)}
          </AlertTitle>
          <AlertDescription>
            {t(($) => $.workspace.subscription_notice.paused_description)}
          </AlertDescription>
        </Alert>
      ) : null}

      {entitlements.status === "unpaid" ? (
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>
            {t(($) => $.workspace.subscription_notice.unpaid_title)}
          </AlertTitle>
          <AlertDescription>
            {t(($) => $.workspace.subscription_notice.unpaid_description)}
          </AlertDescription>
        </Alert>
      ) : null}

      {entitlements.status === "canceled" ? (
        <Alert>
          <AlertCircle />
          <AlertTitle>
            {t(($) => $.workspace.subscription_notice.canceled_title)}
          </AlertTitle>
          <AlertDescription>
            {t(($) => $.workspace.subscription_notice.canceled_description)}
          </AlertDescription>
        </Alert>
      ) : null}

      {!canManage && !currentMember.isLoading ? (
        <Alert>
          <ShieldCheck />
          <AlertTitle>{t(($) => $.workspace.read_only.title)}</AlertTitle>
          <AlertDescription>
            {t(($) => $.workspace.read_only.description)}
          </AlertDescription>
        </Alert>
      ) : null}

      {actionError ? (
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>{t(($) => $.workspace.errors.action_title)}</AlertTitle>
          <AlertDescription>{actionError}</AlertDescription>
        </Alert>
      ) : null}

      <SettingsSection title={t(($) => $.workspace.current.title)}>
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.workspace.current.plan)}
            description={t(($) => $.workspace.current.plan_description)}
          >
            <div className="flex flex-wrap items-center gap-2 sm:justify-end">
              <Badge variant={planBadgeVariant(entitlements.plan)}>
                {planLabel(entitlements.plan)}
              </Badge>
              <Badge variant={statusBadgeVariant(entitlements.status)}>
                {statusLabel(entitlements.status)}
              </Badge>
            </div>
          </SettingsRow>
          <SettingsRow
            label={t(($) => $.workspace.current.members)}
            description={t(($) => $.workspace.current.members_description)}
          >
            <span className="tabular-nums">
              {t(($) => $.workspace.current.member_count, {
                count: actualSeats,
              })}
            </span>
          </SettingsRow>
          {summaryQuery.data?.billingInterval ? (
            <SettingsRow
              label={t(($) => $.workspace.current.billing_interval)}
              description={t(
                ($) => $.workspace.current.billing_interval_description,
              )}
            >
              <span>
                {summaryQuery.data.billingInterval === "month"
                  ? t(($) => $.workspace.upgrade.monthly)
                  : t(($) => $.workspace.upgrade.yearly)}
              </span>
            </SettingsRow>
          ) : null}
          {summaryPeriodEnd ? (
            <SettingsRow
              label={t(($) => $.workspace.current.period_end)}
              description={t(($) => $.workspace.current.period_end_description)}
            >
              <span className="tabular-nums">{summaryPeriodEnd}</span>
            </SettingsRow>
          ) : null}
        </SettingsCard>
      </SettingsSection>

      {canUpgrade ? (
        <SettingsSection
          title={t(($) => $.workspace.upgrade.title)}
          description={t(($) => $.workspace.upgrade.description)}
        >
          <SettingsCard>
            <div className="space-y-5 p-4 sm:p-5">
              <div
                className="inline-flex w-full rounded-lg border border-surface-border p-1 sm:w-auto"
                role="group"
                aria-label={t(($) => $.workspace.upgrade.interval_label)}
              >
                {(["month", "year"] as const).map((value) => (
                  <button
                    key={value}
                    type="button"
                    aria-pressed={interval === value}
                    className="min-h-11 flex-1 rounded-md px-4 text-body font-medium text-muted-foreground transition-[color,background-color,box-shadow] hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring aria-pressed:bg-surface-selected aria-pressed:text-surface-selected-foreground aria-pressed:shadow-sm sm:min-w-32"
                    onClick={() => {
                      setInterval(value);
                      checkoutIntentRef.current = null;
                    }}
                  >
                    {value === "month"
                      ? t(($) => $.workspace.upgrade.monthly)
                      : t(($) => $.workspace.upgrade.yearly)}
                  </button>
                ))}
              </div>
              <div className="space-y-2">
                <p className="text-body font-medium">
                  {t(($) => $.workspace.upgrade.pro_for_team, {
                    count: actualSeats,
                  })}
                </p>
                {pricesQuery.isLoading ? (
                  <div
                    className="space-y-2 motion-reduce:[&_[data-slot=skeleton]]:animate-none"
                    role="status"
                    aria-label={t(($) => $.workspace.upgrade.price_loading)}
                  >
                    <Skeleton className="h-5 w-48" />
                    <Skeleton className="h-4 w-64 max-w-full" />
                  </div>
                ) : hasDisplayableUnitPrice ? (
                  <div className="space-y-1">
                    <p className="text-body font-semibold tabular-nums">
                      {t(($) => $.workspace.upgrade.unit_price, {
                        price: formattedUnitPrice,
                      })}
                    </p>
                    {hasDisplayableEstimatedTotal ? (
                      <p className="text-caption leading-5 text-muted-foreground tabular-nums">
                        {t(
                          interval === "month"
                            ? ($) => $.workspace.upgrade.estimated_monthly_total
                            : ($) => $.workspace.upgrade.estimated_yearly_total,
                          { price: formattedEstimatedTotal },
                        )}
                      </p>
                    ) : null}
                  </div>
                ) : null}
                <p className="max-w-[65ch] text-caption leading-5 text-muted-foreground">
                  {t(($) => $.workspace.upgrade.price_at_checkout)}
                </p>
                {canRetryPrice ? (
                  <>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      aria-busy={pricesQuery.isFetching}
                      disabled={pricesQuery.isFetching}
                      onClick={() => void pricesQuery.refetch()}
                    >
                      {pricesQuery.isFetching ? (
                        <Loader2 className="animate-spin" />
                      ) : (
                        <RefreshCw />
                      )}
                      {t(($) => $.workspace.actions.retry)}
                    </Button>
                    {pricesQuery.isFetching ? (
                      <span
                        className="sr-only"
                        role="status"
                        aria-label={t(
                          ($) => $.workspace.upgrade.price_loading,
                        )}
                      >
                        {t(($) => $.workspace.upgrade.price_loading)}
                      </span>
                    ) : null}
                  </>
                ) : null}
              </div>
              {canManage ? (
                <Button
                  className="h-11 w-full sm:w-auto"
                  disabled={isMutating}
                  onClick={() => setCheckoutConfirmOpen(true)}
                >
                  <CreditCard />
                  {t(($) => $.workspace.actions.upgrade)}
                </Button>
              ) : null}
            </div>
          </SettingsCard>
        </SettingsSection>
      ) : null}

      {hasManagedSubscription && canManage ? (
        <SettingsSection
          title={t(($) => $.workspace.management.title)}
          description={t(($) => $.workspace.management.description)}
        >
          <SettingsCard>
            <SettingsRow
              label={t(($) => $.workspace.management.portal)}
              description={
                portalUnavailable
                  ? t(($) => $.workspace.management.portal_unavailable)
                  : t(($) => $.workspace.management.portal_description)
              }
            >
              {!portalUnavailable ? (
                <Button
                  className="h-11 w-full sm:w-auto"
                  disabled={isMutating}
                  onClick={() => void handlePortal()}
                >
                  {portalMutation.isPending ? (
                    <Loader2 className="animate-spin motion-reduce:animate-none" />
                  ) : (
                    <ExternalLink />
                  )}
                  {t(($) => $.workspace.actions.manage)}
                </Button>
              ) : null}
            </SettingsRow>
          </SettingsCard>
        </SettingsSection>
      ) : null}

      <SettingsSection
        title={t(($) => $.workspace.limits.title)}
        description={t(($) => $.workspace.limits.description)}
      >
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.workspace.limits.issues)}
            description={t(($) => $.workspace.limits.issues_description)}
          >
            <span className="tabular-nums">
              {entitlements.issueWindow === null
                ? canUseEntitlementUnlimited
                  ? t(($) => $.workspace.limits.unlimited)
                  : t(($) => $.workspace.limits.unavailable)
                : numberFormatter.format(entitlements.issueWindow)}
            </span>
          </SettingsRow>
          <SettingsRow
            label={t(($) => $.workspace.limits.autopilots)}
            description={t(($) => $.workspace.limits.autopilots_description)}
          >
            {quotaUsage.kind === "unlimited" ? (
              <span className="tabular-nums">
                {t(($) => $.workspace.limits.unlimited)}
              </span>
            ) : quotaUsageQuery.isPending ? (
              <div
                className="w-full max-w-72 space-y-2 motion-reduce:[&_[data-slot=skeleton]]:animate-none"
                role="status"
                aria-label={t(($) => $.workspace.limits.usage_loading)}
              >
                <Skeleton className="h-5 w-full" />
                <Skeleton className="h-4 w-2/3" />
              </div>
            ) : quotaUsage.kind === "metered" ? (
              <div className="w-full max-w-72 space-y-2">
                <Progress
                  value={quotaUsage.progress}
                  aria-label={t(($) => $.workspace.limits.usage_label)}
                >
                  <ProgressLabel>
                    {quotaUsage.reached
                      ? t(($) => $.workspace.limits.reached)
                      : t(($) => $.workspace.limits.current_usage)}
                  </ProgressLabel>
                  <ProgressValue>
                    {() =>
                      t(($) => $.workspace.limits.usage_total, {
                        total: numberFormatter.format(quotaUsage.total),
                        limit: numberFormatter.format(quotaUsage.limit),
                      })
                    }
                  </ProgressValue>
                </Progress>
                <p className="text-caption text-muted-foreground tabular-nums">
                  {t(($) => $.workspace.limits.usage_breakdown, {
                    used: numberFormatter.format(quotaUsage.used),
                    reserved: numberFormatter.format(quotaUsage.reserved),
                  })}
                </p>
                {quotaResetAt ? (
                  <p className="text-caption text-muted-foreground tabular-nums">
                    {t(($) => $.workspace.limits.resets_at, {
                      date: quotaResetAt,
                    })}
                  </p>
                ) : null}
              </div>
            ) : (
              <div className="flex flex-col gap-2 sm:items-end">
                {entitlements.autopilotRuns !== null ? (
                  <span className="tabular-nums">
                    {t(($) => $.workspace.limits.per_month, {
                      count: entitlements.autopilotRuns,
                    })}
                  </span>
                ) : null}
                <span className="text-caption text-muted-foreground">
                  {t(($) => $.workspace.limits.usage_unavailable)}
                </span>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  aria-label={t(($) => $.workspace.actions.retry_autopilots)}
                  aria-busy={quotaUsageQuery.isFetching}
                  disabled={quotaUsageQuery.isFetching}
                  onClick={() => void quotaUsageQuery.refetch()}
                >
                  {quotaUsageQuery.isFetching ? (
                    <Loader2 className="animate-spin motion-reduce:animate-none" />
                  ) : (
                    <RefreshCw />
                  )}
                  {t(($) => $.workspace.actions.retry)}
                </Button>
              </div>
            )}
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.workspace.seats.title)}
        description={t(($) => $.workspace.seats.description)}
      >
        {summaryUnavailable ? (
          <Alert className="mb-3">
            <AlertCircle />
            <AlertTitle>
              {t(($) => $.workspace.seats.summary_unavailable_title)}
            </AlertTitle>
            <AlertDescription>
              <p>
                {t(($) => $.workspace.seats.summary_unavailable_description)}
              </p>
              <Button
                className="mt-3"
                type="button"
                variant="outline"
                size="sm"
                aria-busy={summaryQuery.isFetching}
                disabled={summaryQuery.isFetching}
                onClick={() => void summaryQuery.refetch()}
              >
                {summaryQuery.isFetching ? (
                  <Loader2 className="animate-spin motion-reduce:animate-none" />
                ) : (
                  <RefreshCw />
                )}
                {t(($) => $.workspace.actions.retry)}
              </Button>
            </AlertDescription>
          </Alert>
        ) : null}
        {reconcileMessage ? (
          <Alert className="mb-3">
            <CheckCircle2 />
            <AlertTitle>{t(($) => $.workspace.seats.updated)}</AlertTitle>
            <AlertDescription>{reconcileMessage}</AlertDescription>
          </Alert>
        ) : null}
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.workspace.seats.human_members)}
            description={t(($) => $.workspace.seats.human_members_description)}
          >
            <div className="flex flex-col gap-2 sm:items-end">
              <span className="tabular-nums">
                {t(($) => $.workspace.current.member_count, {
                  count: actualSeats,
                })}
              </span>
              {canManage && hasManagedSubscription ? (
                <Button
                  className="h-11 w-full sm:w-auto"
                  variant="outline"
                  disabled={isMutating}
                  onClick={() => void handleReconcile()}
                >
                  {reconcileMutation.isPending ? (
                    <Loader2 className="animate-spin motion-reduce:animate-none" />
                  ) : (
                    <RefreshCw />
                  )}
                  {t(($) => $.workspace.actions.refresh_seats)}
                </Button>
              ) : null}
            </div>
          </SettingsRow>
          <SettingsRow
            label={t(($) => $.workspace.seats.billed)}
            description={t(($) => $.workspace.seats.billed_description)}
          >
            {summaryQuery.isPending ? (
              <Skeleton
                className="h-5 w-20 motion-reduce:animate-none"
                aria-label={t(($) => $.workspace.seats.summary_loading)}
              />
            ) : summaryUnavailable ? (
              <span className="text-muted-foreground">
                {t(($) => $.workspace.seats.unavailable)}
              </span>
            ) : billedSeats === null || billedSeats === undefined ? (
              <span className="text-muted-foreground">
                {t(($) => $.workspace.seats.not_subscribed)}
              </span>
            ) : (
              <span className="tabular-nums">
                {t(($) => $.workspace.seats.seat_count, {
                  count: billedSeats,
                })}
              </span>
            )}
          </SettingsRow>
          <SettingsRow
            label={t(($) => $.workspace.seats.pending)}
            description={t(($) => $.workspace.seats.pending_description)}
          >
            {summaryQuery.isPending ? (
              <Skeleton
                className="h-5 w-28 motion-reduce:animate-none"
                aria-label={t(($) => $.workspace.seats.summary_loading)}
              />
            ) : summaryUnavailable ? (
              <span className="text-muted-foreground">
                {t(($) => $.workspace.seats.unavailable)}
              </span>
            ) : pendingSeatQuantity === null ||
              pendingSeatQuantity === undefined ? (
              <span className="text-muted-foreground">
                {t(($) => $.workspace.seats.none_pending)}
              </span>
            ) : summaryPeriodEnd ? (
              <span className="tabular-nums">
                {t(($) => $.workspace.seats.pending_with_date, {
                  count: pendingSeatQuantity,
                  date: summaryPeriodEnd,
                })}
              </span>
            ) : (
              <span className="tabular-nums">
                {t(($) => $.workspace.seats.seat_count, {
                  count: pendingSeatQuantity,
                })}
              </span>
            )}
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>

      <AlertDialog
        open={checkoutConfirmOpen}
        onOpenChange={handleCheckoutConfirmOpenChange}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.workspace.confirm.title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.workspace.confirm.description, {
                interval:
                  interval === "month"
                    ? t(($) => $.workspace.upgrade.monthly)
                    : t(($) => $.workspace.upgrade.yearly),
                count: actualSeats,
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel
              className="h-11"
              disabled={checkoutMutation.isPending}
            >
              {t(($) => $.workspace.actions.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              className="h-11"
              disabled={checkoutMutation.isPending}
              onClick={() => void handleCheckout()}
            >
              {checkoutMutation.isPending ? (
                <Loader2 className="animate-spin motion-reduce:animate-none" />
              ) : (
                <ExternalLink />
              )}
              {t(($) => $.workspace.actions.continue_to_stripe)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsTab>
  );
}
