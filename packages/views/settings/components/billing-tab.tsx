"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AlertCircle,
  ArrowDown,
  ArrowUpCircle,
  Check,
  CheckCircle2,
  CreditCard,
  ExternalLink,
  Info,
  Loader2,
  Plus,
  RefreshCw,
} from "@multica/ui/icons";
import { ApiError, errorCode } from "@multica/core/api";
import { autopilotQuotaUsageOptions } from "@multica/core/autopilots";
import {
  useChangeWorkspacePlan,
  useCreateWorkspaceSubscriptionCheckout,
  useCreateWorkspaceSubscriptionPortal,
  usePreviewWorkspacePlanChange,
  useReconcileWorkspaceSubscriptionSeats,
  usePreviewWorkspaceSeatPurchase,
  usePurchaseWorkspaceSeats,
  workspaceSubscriptionPricesOptions,
  workspaceSubscriptionSummaryOptions,
} from "@multica/core/billing";
import { useFeatureEnabled } from "@multica/core/config";
import { BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG } from "@multica/core/feature-flags";
import { useCurrentWorkspace } from "@multica/core/paths";
import type {
  PurchaseWorkspaceSeatsRequest,
  WorkspacePlanChangePreview,
  WorkspaceSeatPurchasePreview,
  WorkspaceSubscriptionInterval,
  WorkspaceSubscriptionPaidPlan,
} from "@multica/core/types";
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
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
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
  hasActiveWorkspaceSeatCapacity,
  resolveAutopilotUsage,
  resolvePlanChangeTarget,
  yearlyPriceMonthlyEquivalent,
} from "./billing-state";
import { formatStripeMinorAmount } from "./billing-format";

export { formatStripeMinorAmount } from "./billing-format";

const CHECKOUT_SYNC_TIMEOUT_MS = 30_000;
const SEAT_PURCHASE_POLL_TIMEOUT_MS = 2 * 60_000;
const SEAT_PURCHASE_PREVIEW_DEBOUNCE_MS = 800;
const PAID_PLANS = ["pro", "business"] as const;

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

function planBadgeVariant(plan: string): "default" | "secondary" | "outline" {
  if (plan === "pro" || plan === "business") return "default";
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
  const [checkoutPlan, setCheckoutPlan] =
    useState<WorkspaceSubscriptionPaidPlan>("pro");
  const [checkoutConfirmOpen, setCheckoutConfirmOpen] = useState(false);
  const [planChangeTarget, setPlanChangeTarget] =
    useState<WorkspaceSubscriptionPaidPlan | null>(null);
  const [planChangePreview, setPlanChangePreview] =
    useState<WorkspacePlanChangePreview | null>(null);
  const [planChangePreviewRevision, setPlanChangePreviewRevision] =
    useState(0);
  const [planChangeError, setPlanChangeError] = useState<string | null>(null);
  const [planChangeQuoteChanged, setPlanChangeQuoteChanged] = useState(false);
  const [planChangeMessage, setPlanChangeMessage] = useState<string | null>(
    null,
  );
  const [seatPurchaseOpen, setSeatPurchaseOpen] = useState(false);
  const [additionalSeatsInput, setAdditionalSeatsInput] = useState("1");
  const [seatPreview, setSeatPreview] =
    useState<WorkspaceSeatPurchasePreview | null>(null);
  const [seatPreviewRevision, setSeatPreviewRevision] = useState(0);
  const [seatPreviewRefreshing, setSeatPreviewRefreshing] = useState(false);
  const [seatPurchaseError, setSeatPurchaseError] = useState<string | null>(
    null,
  );
  const [seatPurchasePollingTimedOut, setSeatPurchasePollingTimedOut] =
    useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [portalUnavailable, setPortalUnavailable] = useState(false);
  const [seatPurchaseMessage, setSeatPurchaseMessage] = useState<string | null>(
    null,
  );
  const [isSyncingCheckout, setIsSyncingCheckout] = useState(
    returnResult === "success",
  );
  const [syncTimedOut, setSyncTimedOut] = useState(false);
  const checkoutIntentRef = useRef<{
    wsId: string;
    plan: WorkspaceSubscriptionPaidPlan;
    interval: WorkspaceSubscriptionInterval;
    key: string;
  } | null>(null);
  const planChangeIntentRef = useRef<{
    wsId: string;
    key: string;
    preview: WorkspacePlanChangePreview;
  } | null>(null);
  const planChangePreviewKeyRef = useRef("");
  const portalIntentKeyRef = useRef<string | null>(null);
  const seatPurchaseIntentRef = useRef<{
    wsId: string;
    key: string;
    preview: WorkspaceSeatPurchasePreview;
    request: PurchaseWorkspaceSeatsRequest;
  } | null>(null);
  const seatPreviewInputRef = useRef("");
  const seatPreviewCapacityRetryRef = useRef<{
    inputKey: string;
    attempts: number;
  } | null>(null);
  const consumedCallbackKeyRef = useRef<string | null>(null);

  useEffect(() => {
    checkoutIntentRef.current = null;
    portalIntentKeyRef.current = null;
    seatPurchaseIntentRef.current = null;
    seatPreviewInputRef.current = "";
    seatPreviewCapacityRetryRef.current = null;
    planChangeIntentRef.current = null;
    planChangePreviewKeyRef.current = "";
    setPlanChangeTarget(null);
    setPlanChangePreview(null);
    setPlanChangeError(null);
    setPlanChangeQuoteChanged(false);
    setPlanChangeMessage(null);
    setSeatPurchaseOpen(false);
    setSeatPreview(null);
    setSeatPreviewRevision(0);
    setSeatPreviewRefreshing(false);
    setSeatPurchaseError(null);
    setSeatPurchasePollingTimedOut(false);
    setPortalUnavailable(false);
    setActionError(null);
    setSeatPurchaseMessage(null);
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

  const summaryQuery = useQuery({
    ...workspaceSubscriptionSummaryOptions(wsId),
    // Checkout activation and seat additions both finish asynchronously in a
    // Stripe webhook. Seat polling is bounded so a lost webhook cannot keep a
    // browser polling forever.
    refetchInterval: (query) =>
      isSyncingCheckout ||
      (query.state.data?.seatCapacity?.activePurchase &&
        !seatPurchasePollingTimedOut)
        ? 2_000
        : false,
  });
  const entitlements = summaryQuery.data?.entitlement;
  const isCheckoutConfirmed =
    returnResult === "success" &&
    returnObservedAt !== null &&
    summaryQuery.data?.availableActions.checkout === false &&
    summaryQuery.isFetchedAfterMount &&
    summaryQuery.dataUpdatedAt >= returnObservedAt;
  const activeSeatPurchaseRequestId =
    summaryQuery.data?.seatCapacity?.activePurchase?.requestId ?? null;

  useEffect(() => {
    if (!activeSeatPurchaseRequestId) {
      setSeatPurchasePollingTimedOut(false);
      return;
    }
    setSeatPurchasePollingTimedOut(false);
    const timeout = window.setTimeout(
      () => setSeatPurchasePollingTimedOut(true),
      SEAT_PURCHASE_POLL_TIMEOUT_MS,
    );
    return () => window.clearTimeout(timeout);
  }, [activeSeatPurchaseRequestId]);
  const summaryUnavailable =
    summaryQuery.isError ||
    (!summaryQuery.isPending && summaryQuery.data == null);
  const quotaUsageQuery = useQuery(autopilotQuotaUsageOptions(wsId));
  const hasSeatCapacity = hasActiveWorkspaceSeatCapacity(summaryQuery.data);
  const canUpgrade = summaryQuery.data?.availableActions.checkout === true;
  const pricesQuery = useQuery({
    ...workspaceSubscriptionPricesOptions(wsId),
    enabled: wsId.length > 0 && canUpgrade,
  });
  const checkoutMutation = useCreateWorkspaceSubscriptionCheckout(wsId);
  const portalMutation = useCreateWorkspaceSubscriptionPortal(wsId);
  const previewSeatPurchaseMutation = usePreviewWorkspaceSeatPurchase();
  const purchaseSeatsMutation = usePurchaseWorkspaceSeats(wsId);
  const previewPlanChangeMutation = usePreviewWorkspacePlanChange();
  const changePlanMutation = useChangeWorkspacePlan(wsId);
  const checkPlanChangeMutation = useReconcileWorkspaceSubscriptionSeats(wsId);
  const refetchSummary = summaryQuery.refetch;
  const previewSeatPurchase = previewSeatPurchaseMutation.mutateAsync;
  const previewPlanChange = previewPlanChangeMutation.mutateAsync;

  // Each open of the plan change dialog, and each explicit refresh, asks Cloud
  // for a new quote. A response for an older request is ignored so a slow
  // quote can never replace the one the user is looking at.
  useEffect(() => {
    if (!planChangeTarget) return;
    const requestKey = `${wsId}:${planChangeTarget}:${planChangePreviewRevision}`;
    planChangePreviewKeyRef.current = requestKey;
    planChangeIntentRef.current = null;
    setPlanChangePreview(null);
    setPlanChangeError(null);
    void previewPlanChange({ plan: planChangeTarget })
      .then((preview) => {
        if (planChangePreviewKeyRef.current !== requestKey) return;
        if (!preview || preview.targetPlan !== planChangeTarget) {
          setPlanChangeError(t(($) => $.workspace.plan_change.preview_failed));
          return;
        }
        setPlanChangePreview(preview);
      })
      .catch((error: unknown) => {
        if (planChangePreviewKeyRef.current !== requestKey) return;
        const status = error instanceof ApiError ? error.status : null;
        const code = errorCode(error);
        if (
          status === 400 ||
          status === 403 ||
          code === "subscription_not_active"
        ) {
          // The plan this dialog was opened for is no longer a valid switch,
          // for example because it already changed in another tab.
          planChangePreviewKeyRef.current = "";
          setPlanChangeTarget(null);
          setActionError(
            status === 403
              ? t(($) => $.workspace.errors.permission_changed)
              : t(($) => $.workspace.plan_change.not_active),
          );
          void refetchSummary();
          return;
        }
        setPlanChangeError(
          code === "plan_change_rate_limited"
            ? t(($) => $.workspace.plan_change.rate_limited)
            : code === "seat_purchase_in_progress"
              ? t(($) => $.workspace.plan_change.in_progress)
              : code === "plan_change_in_progress"
                ? t(($) => $.workspace.plan_change.change_in_progress)
                : code === "plan_change_unresolved"
                  ? t(($) => $.workspace.plan_change.unresolved)
                  : t(($) => $.workspace.plan_change.preview_failed),
        );
      });
  }, [
    planChangeTarget,
    planChangePreviewRevision,
    previewPlanChange,
    refetchSummary,
    t,
    wsId,
  ]);

  useEffect(() => {
    if (!seatPurchaseOpen) return;
    const value = additionalSeatsInput.trim();
    const additionalSeats = /^\d+$/.test(value) ? Number(value) : 0;
    const currentSeats = summaryQuery.data?.seatCapacity?.purchased ?? null;
    const purchaseVersion = summaryQuery.data?.seatCapacity?.version ?? null;
    const retryInputKey = `${wsId}:${value}`;
    if (seatPreviewCapacityRetryRef.current?.inputKey !== retryInputKey) {
      seatPreviewCapacityRetryRef.current = {
        inputKey: retryInputKey,
        attempts: 0,
      };
    }
    const requestKey = `${retryInputKey}:${currentSeats ?? ""}:${purchaseVersion ?? ""}:${seatPreviewRevision}`;
    seatPreviewInputRef.current = requestKey;
    const intent = seatPurchaseIntentRef.current;
    if (
      intent?.wsId === wsId &&
      intent.request.additionalSeats === additionalSeats &&
      intent.request.expectedCurrentSeats === currentSeats &&
      intent.request.expectedPurchaseVersion === purchaseVersion
    ) {
      setSeatPreview(intent.preview);
      setSeatPreviewRefreshing(false);
      setSeatPurchaseError(null);
      return;
    }
    seatPurchaseIntentRef.current = null;
    setSeatPreview(null);
    if (
      !Number.isSafeInteger(additionalSeats) ||
      additionalSeats < 1 ||
      currentSeats === null ||
      purchaseVersion === null
    ) {
      if (
        seatPreviewCapacityRetryRef.current?.inputKey === retryInputKey &&
        seatPreviewCapacityRetryRef.current.attempts > 0
      ) {
        setSeatPurchaseError(
          t(($) => $.workspace.seat_purchase.preview_failed),
        );
      }
      setSeatPreviewRefreshing(false);
      return;
    }

    const timeout = window.setTimeout(() => {
      void previewSeatPurchase({ additionalSeats })
        .then((preview) => {
          if (seatPreviewInputRef.current !== requestKey) return;
          if (
            !preview ||
            preview.additionalSeats !== additionalSeats ||
            preview.currentSeats !== currentSeats ||
            preview.purchaseVersion !== purchaseVersion ||
            preview.resultingSeats !== currentSeats + additionalSeats
          ) {
            setSeatPreviewRefreshing(false);
            setSeatPurchaseError(
              t(($) => $.workspace.seat_purchase.preview_unreadable),
            );
            return;
          }
          setSeatPreviewRefreshing(false);
          setSeatPreview(preview);
          setSeatPurchaseError(null);
        })
        .catch(async (error: unknown) => {
          if (seatPreviewInputRef.current !== requestKey) return;
          const previewErrorCode =
            error instanceof ApiError && error.status === 409
              ? errorCode(error)
              : null;
          if (previewErrorCode === "seat_capacity_changed") {
            const retry = seatPreviewCapacityRetryRef.current;
            if (retry?.inputKey !== retryInputKey || retry.attempts >= 1) {
              setSeatPreviewRefreshing(false);
              setSeatPurchaseError(
                t(($) => $.workspace.seat_purchase.capacity_out_of_sync),
              );
              return;
            }
            retry.attempts += 1;
            setSeatPreviewRefreshing(true);
            setSeatPurchaseError(null);
            try {
              const refreshed = await refetchSummary();
              if (seatPreviewInputRef.current !== requestKey) return;
              if (refreshed.isError === true) {
                setSeatPreviewRefreshing(false);
                setSeatPurchaseError(
                  t(($) => $.workspace.seat_purchase.preview_failed),
                );
                return;
              }
              const refreshedSeats =
                refreshed.data?.seatCapacity?.purchased ?? null;
              const refreshedVersion =
                refreshed.data?.seatCapacity?.version ?? null;
              if (
                refreshedSeats === currentSeats &&
                refreshedVersion === purchaseVersion
              ) {
                setSeatPreviewRefreshing(false);
                setSeatPurchaseError(
                  t(($) => $.workspace.seat_purchase.capacity_out_of_sync),
                );
                return;
              }
            } catch {
              if (seatPreviewInputRef.current === requestKey) {
                setSeatPreviewRefreshing(false);
                setSeatPurchaseError(
                  t(($) => $.workspace.seat_purchase.preview_failed),
                );
              }
              return;
            }
            if (seatPreviewInputRef.current !== requestKey) return;
            setSeatPreviewRevision((revision) => revision + 1);
            return;
          }
          setSeatPreviewRefreshing(false);
          setSeatPurchaseError(
            previewErrorCode === "seat_purchase_in_progress"
              ? t(($) => $.workspace.seat_purchase.in_progress)
              : previewErrorCode === "plan_change_in_progress"
                ? t(($) => $.workspace.seat_purchase.plan_change_in_progress)
                : previewErrorCode === "plan_change_unresolved"
                  ? t(($) => $.workspace.seat_purchase.plan_change_unresolved)
                  : t(($) => $.workspace.seat_purchase.preview_failed),
          );
        });
    }, SEAT_PURCHASE_PREVIEW_DEBOUNCE_MS);
    return () => window.clearTimeout(timeout);
  }, [
    additionalSeatsInput,
    previewSeatPurchase,
    refetchSummary,
    seatPurchaseOpen,
    seatPreviewRevision,
    summaryQuery.data?.seatCapacity?.purchased,
    summaryQuery.data?.seatCapacity?.version,
    t,
    wsId,
  ]);

  useEffect(() => {
    if (isSyncingCheckout && isCheckoutConfirmed) {
      setIsSyncingCheckout(false);
      setSyncTimedOut(false);
      checkoutIntentRef.current = null;
    }
  }, [isCheckoutConfirmed, isSyncingCheckout]);

  const planLabel = (plan: string) => {
    switch (plan) {
      case "free":
        return t(($) => $.workspace.plan.free);
      case "pro":
        return t(($) => $.workspace.plan.pro);
      case "business":
        return t(($) => $.workspace.plan.business);
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
    const code = errorCode(error);
    if (
      code === "workspace_subscriptions_disabled" ||
      code === "cloud_runtime_not_configured"
    ) {
      setActionError(t(($) => $.workspace.errors.not_enabled));
      return;
    }
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
      existing?.wsId === wsId &&
      existing.plan === checkoutPlan &&
      existing.interval === interval
        ? existing
        : {
            wsId,
            plan: checkoutPlan,
            interval,
            key: createIdempotencyKey("workspace-checkout", wsId),
          };
    checkoutIntentRef.current = intent;
    try {
      const response = await checkoutMutation.mutateAsync({
        plan: intent.plan,
        interval: intent.interval,
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
        await summaryQuery.refetch();
        return;
      }
      reportActionError(error, t(($) => $.workspace.errors.checkout_failed));
    }
  };

  const handleCheckoutConfirmOpenChange = (open: boolean) => {
    setCheckoutConfirmOpen(open);
    if (!open) checkoutIntentRef.current = null;
  };

  const openCheckoutConfirm = (plan: WorkspaceSubscriptionPaidPlan) => {
    if (checkoutIntentRef.current?.plan !== plan) {
      checkoutIntentRef.current = null;
    }
    setCheckoutPlan(plan);
    setCheckoutConfirmOpen(true);
  };

  const handlePlanChangeOpenChange = (
    open: boolean,
    target: WorkspaceSubscriptionPaidPlan | null = null,
  ) => {
    if (!open && changePlanMutation.isPending) return;
    planChangeIntentRef.current = null;
    setPlanChangeError(null);
    setPlanChangeQuoteChanged(false);
    setPlanChangePreview(null);
    if (!open) {
      planChangePreviewKeyRef.current = "";
      setPlanChangeTarget(null);
      return;
    }
    setActionError(null);
    setPlanChangeMessage(null);
    setPlanChangePreviewRevision((revision) => revision + 1);
    setPlanChangeTarget(target);
  };

  const handlePlanChange = async () => {
    const preview = planChangePreview;
    if (!preview) return;
    setPlanChangeError(null);
    const existing = planChangeIntentRef.current;
    // A retry of the same quote reuses its key, so an ambiguous failure can
    // never become a second Stripe change.
    const key =
      existing?.wsId === wsId && existing.preview === preview
        ? existing.key
        : createIdempotencyKey("workspace-plan-change", wsId).slice(0, 200);
    planChangeIntentRef.current = { wsId, key, preview };
    try {
      const response = await changePlanMutation.mutateAsync({
        plan: preview.targetPlan,
        expectedSubscriptionVersion: preview.subscriptionVersion,
        quotedAt: preview.quotedAt,
        acceptedProrationAmount: preview.prorationAmount,
        currency: preview.currency,
        idempotencyKey: key,
      });
      if (!response || response.plan !== preview.targetPlan) {
        setPlanChangeError(t(($) => $.workspace.plan_change.unreadable));
        return;
      }
      planChangeIntentRef.current = null;
      planChangePreviewKeyRef.current = "";
      setPlanChangeTarget(null);
      setPlanChangePreview(null);
      setPlanChangeQuoteChanged(false);
      setPlanChangeMessage(
        t(($) => $.workspace.plan_change.success_description, {
          plan: planLabel(response.plan),
        }),
      );
      await summaryQuery.refetch();
    } catch (error) {
      const status = error instanceof ApiError ? error.status : null;
      const code = errorCode(error);
      if (status === 409) {
        planChangeIntentRef.current = null;
        if (code === "subscription_not_active") {
          planChangePreviewKeyRef.current = "";
          setPlanChangeTarget(null);
          setActionError(t(($) => $.workspace.plan_change.not_active));
          await summaryQuery.refetch();
          return;
        }
        if (code === "seat_purchase_in_progress") {
          setPlanChangePreview(null);
          setPlanChangeError(t(($) => $.workspace.plan_change.in_progress));
          return;
        }
        if (
          code === "plan_change_in_progress" ||
          code === "plan_change_unresolved"
        ) {
          setPlanChangePreview(null);
          setPlanChangeError(
            code === "plan_change_unresolved"
              ? t(($) => $.workspace.plan_change.unresolved)
              : t(($) => $.workspace.plan_change.change_in_progress),
          );
          await summaryQuery.refetch();
          return;
        }
        setPlanChangeQuoteChanged(true);
        await summaryQuery.refetch();
        setPlanChangePreviewRevision((revision) => revision + 1);
        return;
      }
      if (status === 402) {
        planChangeIntentRef.current = null;
        setPlanChangePreview(null);
        setPlanChangeError(t(($) => $.workspace.plan_change.payment_failed));
        return;
      }
      if (status === 403) {
        planChangeIntentRef.current = null;
        planChangePreviewKeyRef.current = "";
        setPlanChangeTarget(null);
        reportActionError(error, t(($) => $.workspace.plan_change.failed));
        return;
      }
      setPlanChangeError(t(($) => $.workspace.plan_change.failed));
    }
  };

  // Asks Cloud to settle a plan change that is still being confirmed now,
  // rather than at its next reconcile pass, then shows the result.
  const handleCheckPlanChange = async () => {
    setActionError(null);
    try {
      await checkPlanChangeMutation.mutateAsync();
    } catch (error) {
      reportActionError(error, t(($) => $.workspace.plan_change.check_failed));
    }
    await summaryQuery.refetch();
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
        await summaryQuery.refetch();
        return;
      }
      reportActionError(error, t(($) => $.workspace.errors.portal_failed));
    }
  };

  const handleSeatPurchase = async () => {
    const confirmedPreview = seatPreview;
    if (!confirmedPreview) return;
    setSeatPurchaseError(null);
    const request: PurchaseWorkspaceSeatsRequest = {
      additionalSeats: confirmedPreview.additionalSeats,
      expectedCurrentSeats: confirmedPreview.currentSeats,
      expectedPurchaseVersion: confirmedPreview.purchaseVersion,
      acceptedProrationAmount: confirmedPreview.prorationAmount,
      currency: confirmedPreview.currency,
      idempotencyKey: "",
    };
    const existing = seatPurchaseIntentRef.current;
    const key =
      existing?.wsId === wsId &&
      existing.request.additionalSeats === request.additionalSeats &&
      existing.request.expectedCurrentSeats === request.expectedCurrentSeats &&
      existing.request.expectedPurchaseVersion ===
        request.expectedPurchaseVersion &&
      existing.request.acceptedProrationAmount ===
        request.acceptedProrationAmount &&
      existing.request.currency === request.currency
        ? existing.key
        : createIdempotencyKey("workspace-seat-purchase", wsId).slice(0, 200);
    request.idempotencyKey = key;
    seatPurchaseIntentRef.current = {
      wsId,
      key,
      preview: confirmedPreview,
      request,
    };
    try {
      const response = await purchaseSeatsMutation.mutateAsync(request);
      if (
        !response ||
        response.currentSeats !== confirmedPreview.currentSeats ||
        response.additionalSeats !== confirmedPreview.additionalSeats ||
        response.resultingSeats !== confirmedPreview.resultingSeats ||
        response.currency !== confirmedPreview.currency
      ) {
        setSeatPurchaseError(
          t(($) => $.workspace.seat_purchase.purchase_unreadable),
        );
        return;
      }
      seatPurchaseIntentRef.current = null;
      seatPreviewInputRef.current = "";
      setSeatPurchaseOpen(false);
      setSeatPreview(null);
      setSeatPurchaseMessage(
        t(($) => $.workspace.seat_purchase.submitted, {
          count: response.resultingSeats,
        }),
      );
      await summaryQuery.refetch();
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        const purchaseErrorCode = errorCode(error);
        seatPurchaseIntentRef.current = null;
        setSeatPreview(null);
        if (purchaseErrorCode === "seat_purchase_in_progress") {
          setSeatPurchaseError(
            t(($) => $.workspace.seat_purchase.in_progress),
          );
          await summaryQuery.refetch();
          return;
        }
        if (
          purchaseErrorCode === "plan_change_in_progress" ||
          purchaseErrorCode === "plan_change_unresolved"
        ) {
          setSeatPurchaseError(
            purchaseErrorCode === "plan_change_unresolved"
              ? t(($) => $.workspace.seat_purchase.plan_change_unresolved)
              : t(($) => $.workspace.seat_purchase.plan_change_in_progress),
          );
          await summaryQuery.refetch();
          return;
        }
        setSeatPurchaseError(t(($) => $.workspace.seat_purchase.quote_changed));
        await summaryQuery.refetch();
        setSeatPreviewRevision((revision) => revision + 1);
        return;
      }
      if (error instanceof ApiError && error.status === 402) {
        seatPurchaseIntentRef.current = null;
        setSeatPreview(null);
        setSeatPurchaseError(
          t(($) => $.workspace.seat_purchase.payment_failed),
        );
        return;
      }
      if (error instanceof ApiError && error.status === 403) {
        seatPurchaseIntentRef.current = null;
        reportActionError(
          error,
          t(($) => $.workspace.seat_purchase.purchase_failed),
        );
        return;
      }
      setSeatPurchaseError(
        t(($) => $.workspace.seat_purchase.purchase_failed),
      );
    }
  };

  const handleSeatPurchaseOpenChange = (open: boolean) => {
    if (!open && purchaseSeatsMutation.isPending) return;
    setSeatPurchaseOpen(open);
    seatPreviewCapacityRetryRef.current = null;
    setSeatPreviewRefreshing(false);
    if (!open) {
      seatPreviewInputRef.current = "";
      return;
    }
    setSeatPurchaseError(null);
    const intent = seatPurchaseIntentRef.current;
    if (intent?.wsId === wsId) {
      setAdditionalSeatsInput(String(intent.request.additionalSeats));
      setSeatPreview(intent.preview);
      return;
    }
    setAdditionalSeatsInput("1");
    setSeatPreview(null);
  };

  if (summaryQuery.isPending) {
    return (
      <SettingsTab
        title={t(($) => $.workspace.title)}
        scope="workspace"
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

  if (summaryQuery.isError || !entitlements) {
    return (
      <SettingsTab
        title={t(($) => $.workspace.title)}
        scope="workspace"
      >
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>{t(($) => $.workspace.load_failed.title)}</AlertTitle>
          <AlertDescription>
            <p>{t(($) => $.workspace.load_failed.description)}</p>
            <Button
              className="mt-3 h-11"
              variant="outline"
              onClick={() => void summaryQuery.refetch()}
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
  const actualSeats = summaryQuery.data?.humanMembers ?? entitlements.seats;
  const seatCapacity = summaryQuery.data?.seatCapacity ?? null;
  const usedSeats = seatCapacity?.used ?? actualSeats;
  const billedSeats = seatCapacity?.purchased ?? null;
  const pendingSeatQuantity = seatCapacity?.pendingQuantity ?? null;
  const reservedSeats = seatCapacity?.reserved ?? 0;
  const membersExceedPurchasedSeats = seatCapacity?.overcommitted === true;
  const availableSeats = seatCapacity?.available ?? null;
  const activeSeatPurchase = seatCapacity?.activePurchase ?? null;
  const activeSeatPurchaseExpiry = formatDateTime(
    activeSeatPurchase?.expiresAt ?? null,
    locale,
  );
  const canAddSeats =
    summaryQuery.data?.availableActions.purchaseSeats === true;
  const quotaUsage = resolveAutopilotUsage(
    entitlements,
    quotaUsageQuery.data,
    quotaUsageQuery.isError,
  );
  const quotaResetAt =
    quotaUsage.kind === "metered"
      ? formatDateTime(quotaUsage.resetAt, locale)
      : null;
  const numberFormatter = new Intl.NumberFormat(locale);
  const isMutating =
    checkoutMutation.isPending ||
    portalMutation.isPending ||
    purchaseSeatsMutation.isPending ||
    changePlanMutation.isPending;
  const planUnitPrice = (plan: WorkspaceSubscriptionPaidPlan) => {
    const price = pricesQuery.data?.[plan][interval] ?? null;
    if (!price || price.intervalCount !== 1) return null;
    if (price.interval === "year") {
      const monthly = yearlyPriceMonthlyEquivalent(price);
      if (monthly !== null) {
        const formatted = formatStripeMinorAmount(
          monthly,
          price.currency,
          locale,
        );
        if (formatted !== null) {
          return t(($) => $.workspace.upgrade.unit_price_year_monthly, {
            price: formatted,
          });
        }
      }
      const formatted = formatStripeMinorAmount(
        price.unitAmount,
        price.currency,
        locale,
      );
      return formatted === null
        ? null
        : t(($) => $.workspace.upgrade.unit_price_year, { price: formatted });
    }
    const formatted = formatStripeMinorAmount(
      price.unitAmount,
      price.currency,
      locale,
    );
    return formatted === null
      ? null
      : t(($) => $.workspace.upgrade.unit_price_month, { price: formatted });
  };
  const canRetryPrice =
    !pricesQuery.isLoading && (pricesQuery.data ?? null) === null;
  const planFeatures: Record<WorkspaceSubscriptionPaidPlan, string[]> = {
    pro: [
      t(($) => $.workspace.upgrade.features.pro_members),
      t(($) => $.workspace.upgrade.features.pro_autopilots),
      t(($) => $.workspace.upgrade.features.pro_storage),
      t(($) => $.workspace.upgrade.features.pro_dashboard),
    ],
    business: [
      t(($) => $.workspace.upgrade.features.business_everything),
      t(($) => $.workspace.upgrade.features.business_access),
      t(($) => $.workspace.upgrade.features.business_apps),
      t(($) => $.workspace.upgrade.features.business_autopilots),
      t(($) => $.workspace.upgrade.features.business_reports),
    ],
  };
  const availablePlanChange = resolvePlanChangeTarget(summaryQuery.data);
  const planChangeHold = summaryQuery.data?.planChange ?? null;
  const planChangeIsUpgrade = planChangeTarget === "business";
  const formattedPlanChangeProration = planChangePreview
    ? formatStripeMinorAmount(
        Math.abs(planChangePreview.prorationAmount),
        planChangePreview.currency,
        locale,
      )
    : null;
  const formattedPlanChangeNextInvoice = planChangePreview
    ? formatStripeMinorAmount(
        planChangePreview.nextInvoiceAmount,
        planChangePreview.currency,
        locale,
      )
    : null;
  const formattedSeatProration = seatPreview
    ? formatStripeMinorAmount(
        seatPreview.prorationAmount,
        seatPreview.currency,
        locale,
      )
    : null;
  const formattedNextSeatInvoice = seatPreview
    ? formatStripeMinorAmount(
        seatPreview.nextInvoiceAmount,
        seatPreview.currency,
        locale,
      )
    : null;

  return (
    <SettingsTab
      title={t(($) => $.workspace.title)}
      scope="workspace"
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
        </Alert>
      ) : null}

      {returnResult === "success" ? (
        <Alert>
          {isCheckoutConfirmed ? (
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
            {isCheckoutConfirmed
              ? t(($) => $.workspace.return.active_title, {
                  plan: planLabel(entitlements.plan),
                })
              : t(($) => $.workspace.return.syncing_title)}
          </AlertTitle>
          <AlertDescription>
            {isCheckoutConfirmed
              ? t(($) => $.workspace.return.active_description, {
                  plan: planLabel(entitlements.plan),
                })
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
            {graceUntil
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
        </Alert>
      ) : null}

      {actionError ? (
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>{t(($) => $.workspace.errors.action_title)}</AlertTitle>
          <AlertDescription>{actionError}</AlertDescription>
        </Alert>
      ) : null}

      {planChangeMessage ? (
        <Alert>
          <CheckCircle2 />
          <AlertTitle>
            {t(($) => $.workspace.plan_change.success_title)}
          </AlertTitle>
          <AlertDescription>{planChangeMessage}</AlertDescription>
        </Alert>
      ) : null}

      <SettingsSection title={t(($) => $.workspace.current.title)}>
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.workspace.current.plan)}
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
            >
              <span className="tabular-nums">{summaryPeriodEnd}</span>
            </SettingsRow>
          ) : null}
          {availablePlanChange ? (
            <SettingsRow
              label={t(($) => $.workspace.current.change_plan)}
              description={
                availablePlanChange === "business"
                  ? t(($) => $.workspace.current.business_adds)
                  : undefined
              }
            >
              <Button
                className="h-11 w-full sm:w-auto"
                variant={
                  availablePlanChange === "business" ? "default" : "outline"
                }
                disabled={isMutating}
                onClick={() =>
                  handlePlanChangeOpenChange(true, availablePlanChange)
                }
              >
                {availablePlanChange === "business" ? (
                  <ArrowUpCircle />
                ) : (
                  <ArrowDown />
                )}
                {availablePlanChange === "business"
                  ? t(($) => $.workspace.actions.upgrade_to, {
                      plan: planLabel("business"),
                    })
                  : t(($) => $.workspace.actions.switch_to, {
                      plan: planLabel("pro"),
                    })}
              </Button>
            </SettingsRow>
          ) : null}
          {planChangeHold ? (
            <SettingsRow
              label={t(($) => $.workspace.current.plan_change_hold)}
              description={
                planChangeHold.state === "unresolved"
                  ? t(($) => $.workspace.current.plan_change_unresolved, {
                      plan: planLabel(planChangeHold.targetPlan),
                    })
                  : t(($) => $.workspace.current.plan_change_confirming, {
                      plan: planLabel(planChangeHold.targetPlan),
                    })
              }
            >
              {planChangeHold.state === "confirming" &&
              summaryQuery.data?.availableActions.checkPlanChange === true ? (
                <Button
                  className="h-11 w-full sm:w-auto"
                  variant="outline"
                  aria-busy={checkPlanChangeMutation.isPending}
                  disabled={checkPlanChangeMutation.isPending || isMutating}
                  onClick={() => void handleCheckPlanChange()}
                >
                  {checkPlanChangeMutation.isPending ? (
                    <Loader2 className="animate-spin" />
                  ) : (
                    <RefreshCw />
                  )}
                  {t(($) => $.workspace.actions.check_status)}
                </Button>
              ) : (
                <Badge
                  variant={
                    planChangeHold.state === "unresolved"
                      ? "destructive"
                      : "secondary"
                  }
                >
                  {planChangeHold.state === "unresolved"
                    ? t(($) => $.workspace.current.plan_change_state_unresolved)
                    : t(($) => $.workspace.current.plan_change_state_confirming)}
                </Badge>
              )}
            </SettingsRow>
          ) : null}
        </SettingsCard>
      </SettingsSection>

      {canUpgrade ? (
        <SettingsSection title={t(($) => $.workspace.upgrade.title)}>
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
              <div className="grid gap-3 md:grid-cols-2">
                {PAID_PLANS.map((plan) => {
                  const recommended = plan === "business";
                  const unitPrice = planUnitPrice(plan);
                  return (
                    <div
                      key={plan}
                      className={
                        recommended
                          ? "flex flex-col gap-4 rounded-lg border border-foreground p-4"
                          : "flex flex-col gap-4 rounded-lg border border-surface-border p-4"
                      }
                    >
                      <div className="space-y-1">
                        <div className="flex items-center gap-2">
                          <h3 className="text-title-sm font-semibold">
                            {planLabel(plan)}
                          </h3>
                          {recommended ? (
                            <Badge>
                              {t(($) => $.workspace.upgrade.recommended)}
                            </Badge>
                          ) : null}
                        </div>
                        {pricesQuery.isLoading ? (
                          <Skeleton
                            aria-hidden="true"
                            className="h-5 w-48 max-w-full motion-reduce:animate-none"
                          />
                        ) : unitPrice ? (
                          <p className="text-body font-semibold tabular-nums">
                            {unitPrice}
                          </p>
                        ) : null}
                      </div>
                      <ul className="flex-1 space-y-2">
                        {planFeatures[plan].map((feature) => (
                          <li
                            key={feature}
                            className="flex items-start gap-2 text-body"
                          >
                            <Check
                              aria-hidden="true"
                              className="mt-0.5 size-4 shrink-0 text-muted-foreground"
                            />
                            <span>{feature}</span>
                          </li>
                        ))}
                      </ul>
                      <Button
                        className="h-11 w-full"
                        variant={recommended ? "default" : "outline"}
                        disabled={isMutating}
                        onClick={() => openCheckoutConfirm(plan)}
                      >
                        <CreditCard />
                        {t(($) => $.workspace.actions.upgrade_to, {
                          plan: planLabel(plan),
                        })}
                      </Button>
                    </div>
                  );
                })}
              </div>
              <div className="space-y-2">
                {pricesQuery.isLoading ? (
                  <span
                    className="sr-only"
                    role="status"
                    aria-label={t(($) => $.workspace.upgrade.price_loading)}
                  >
                    {t(($) => $.workspace.upgrade.price_loading)}
                  </span>
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
            </div>
          </SettingsCard>
        </SettingsSection>
      ) : null}

      {summaryQuery.data?.availableActions.portal === true ? (
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
                  : undefined
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
      >
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.workspace.limits.autopilots)}
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
                {entitlements.limits.autopilotRuns.mode === "limited" &&
                entitlements.limits.autopilotRuns.limit !== null ? (
                  <span className="tabular-nums">
                    {t(($) => $.workspace.limits.per_month, {
                      count: entitlements.limits.autopilotRuns.limit,
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

      {hasSeatCapacity || summaryUnavailable ? (
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
          {seatPurchaseMessage ? (
            <Alert className="mb-3">
              <CheckCircle2 />
              <AlertTitle>{t(($) => $.workspace.seats.updated)}</AlertTitle>
              <AlertDescription>{seatPurchaseMessage}</AlertDescription>
            </Alert>
          ) : null}
          {membersExceedPurchasedSeats ? (
            <Alert className="mb-3">
              <AlertCircle />
              <AlertTitle>
                {t(($) => $.workspace.seats.members_over_capacity_title)}
              </AlertTitle>
              <AlertDescription>
                <p>
                  {t(($) => $.workspace.seats.members_over_capacity_description, {
                    actual: actualSeats,
                    purchased: billedSeats,
                  })}
                </p>
                {canAddSeats ? (
                  <Button
                    className="mt-3"
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={isMutating}
                    onClick={() => handleSeatPurchaseOpenChange(true)}
                  >
                    <Plus />
                    {t(($) => $.workspace.actions.add_seats)}
                  </Button>
                ) : null}
              </AlertDescription>
            </Alert>
          ) : null}
          {activeSeatPurchase ? (
            <Alert className="mb-3">
              {seatPurchasePollingTimedOut ? (
                <AlertCircle />
              ) : (
                <Loader2 className="animate-spin motion-reduce:animate-none" />
              )}
              <AlertTitle>
                {seatPurchasePollingTimedOut
                  ? t(($) => $.workspace.seat_purchase.delayed_title)
                  : t(($) => $.workspace.seat_purchase.pending_title)}
              </AlertTitle>
              <AlertDescription>
                {seatPurchasePollingTimedOut
                  ? activeSeatPurchaseExpiry
                    ? t(
                        ($) =>
                          $.workspace.seat_purchase
                            .delayed_description_with_expiry,
                        { date: activeSeatPurchaseExpiry },
                      )
                    : t(($) => $.workspace.seat_purchase.delayed_description)
                  : t(($) => $.workspace.seat_purchase.pending_description, {
                      count: activeSeatPurchase.targetSeats,
                    })}
              </AlertDescription>
            </Alert>
          ) : null}
          <SettingsCard>
            <SettingsRow
              label={t(($) => $.workspace.seats.human_members)}
            >
              <span className="tabular-nums">
                {t(($) => $.workspace.seats.seat_count, {
                  count: usedSeats,
                })}
              </span>
            </SettingsRow>
            <SettingsRow
              label={t(($) => $.workspace.seats.billed)}
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
              ) : billedSeats === null ? (
                <span className="text-muted-foreground">
                  {t(($) => $.workspace.seats.not_subscribed)}
                </span>
              ) : (
                <div className="flex flex-col gap-2 sm:items-end">
                  <span className="tabular-nums">
                    {t(($) => $.workspace.seats.seat_count, {
                      count: billedSeats,
                    })}
                  </span>
                  {canAddSeats ? (
                    <Button
                      className="h-11 w-full sm:w-auto"
                      type="button"
                      disabled={isMutating}
                      onClick={() => handleSeatPurchaseOpenChange(true)}
                    >
                      <Plus />
                      {t(($) => $.workspace.actions.add_seats)}
                    </Button>
                  ) : null}
                </div>
              )}
            </SettingsRow>
            <SettingsRow
              label={t(($) => $.workspace.seats.pending_invitations)}
              description={t(
                ($) => $.workspace.seats.pending_invitations_description,
              )}
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
              ) : (
                <span className="tabular-nums">
                  {t(($) => $.workspace.seats.seat_count, {
                    count: reservedSeats,
                  })}
                </span>
              )}
            </SettingsRow>
            <SettingsRow
              label={
                <div className="flex items-center gap-1.5">
                  <span>{t(($) => $.workspace.seats.available)}</span>
                  <Popover>
                    <PopoverTrigger
                      render={
                        <IconButton
                          variant="ghost"
                          size="icon-xs"
                          label={t(($) => $.workspace.seats.available_help)}
                        >
                          <Info className="size-3.5" />
                        </IconButton>
                      }
                    />
                    <PopoverContent align="start">
                      <PopoverDescription>
                        {t(($) => $.workspace.seats.available_description)}
                      </PopoverDescription>
                    </PopoverContent>
                  </Popover>
                </div>
              }
            >
              {summaryQuery.isPending ? (
                <Skeleton
                  className="h-5 w-20 motion-reduce:animate-none"
                  aria-label={t(($) => $.workspace.seats.summary_loading)}
                />
              ) : summaryUnavailable || availableSeats === null ? (
                <span className="text-muted-foreground">
                  {t(($) => $.workspace.seats.unavailable)}
                </span>
              ) : (
                <span className="tabular-nums">
                  {t(($) => $.workspace.seats.seat_count, {
                    count: availableSeats,
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
              ) : pendingSeatQuantity === null ? (
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
      ) : null}

      <Dialog
        open={seatPurchaseOpen}
        onOpenChange={handleSeatPurchaseOpenChange}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t(($) => $.workspace.seat_purchase.title)}
            </DialogTitle>
            <DialogDescription>
              {t(($) => $.workspace.seat_purchase.description)}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <label
                className="text-body font-medium"
                htmlFor="workspace-additional-seats"
              >
                {t(($) => $.workspace.seat_purchase.additional_label)}
              </label>
              <Input
                id="workspace-additional-seats"
                className="h-11"
                type="number"
                inputMode="numeric"
                min={1}
                step={1}
                value={additionalSeatsInput}
                disabled={purchaseSeatsMutation.isPending}
                onChange={(event) => {
                  seatPreviewCapacityRetryRef.current = null;
                  setSeatPreviewRefreshing(false);
                  setAdditionalSeatsInput(event.currentTarget.value);
                  setSeatPurchaseError(null);
                }}
              />
              <p className="text-caption text-muted-foreground">
                {t(($) => $.workspace.seat_purchase.additional_hint)}
              </p>
            </div>
            {previewSeatPurchaseMutation.isPending ||
            seatPreviewRefreshing ? (
              <div
                className="flex items-center gap-2 text-body text-muted-foreground"
                role="status"
              >
                <Loader2 className="animate-spin motion-reduce:animate-none" />
                {t(($) => $.workspace.seat_purchase.preview_loading)}
              </div>
            ) : null}
            {seatPurchaseError ? (
              <Alert variant="destructive">
                <AlertCircle />
                <AlertTitle>
                  {t(($) => $.workspace.seat_purchase.error_title)}
                </AlertTitle>
                <AlertDescription>{seatPurchaseError}</AlertDescription>
              </Alert>
            ) : null}
            {seatPreview &&
            formattedSeatProration !== null &&
            formattedNextSeatInvoice !== null ? (
              <div className="space-y-3 rounded-lg border border-surface-border p-4">
                <div className="flex items-center justify-between gap-4 text-body">
                  <span className="text-muted-foreground">
                    {t(($) => $.workspace.seat_purchase.seats_after)}
                  </span>
                  <span className="font-medium tabular-nums">
                    {t(($) => $.workspace.seats.seat_count, {
                      count: seatPreview.resultingSeats,
                    })}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-4 text-body">
                  <span className="text-muted-foreground">
                    {t(($) => $.workspace.seat_purchase.charge_today)}
                  </span>
                  <span className="font-medium tabular-nums">
                    {formattedSeatProration}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-4 text-body">
                  <span className="text-muted-foreground">
                    {t(($) => $.workspace.seat_purchase.next_invoice)}
                  </span>
                  <span className="font-medium tabular-nums">
                    {formattedNextSeatInvoice}
                  </span>
                </div>
                <p className="text-caption leading-5 text-muted-foreground">
                  {t(($) => $.workspace.seat_purchase.tax_notice)}
                </p>
              </div>
            ) : null}
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              className="h-11"
              disabled={purchaseSeatsMutation.isPending}
              onClick={() => handleSeatPurchaseOpenChange(false)}
            >
              {t(($) => $.workspace.actions.cancel)}
            </Button>
            <Button
              type="button"
              className="h-11"
              disabled={
                !seatPreview ||
                formattedSeatProration === null ||
                formattedNextSeatInvoice === null ||
                previewSeatPurchaseMutation.isPending ||
                seatPreviewRefreshing ||
                purchaseSeatsMutation.isPending
              }
              onClick={() => void handleSeatPurchase()}
            >
              {purchaseSeatsMutation.isPending ? (
                <Loader2 className="animate-spin motion-reduce:animate-none" />
              ) : (
                <Plus />
              )}
              {seatPreview
                ? t(($) => $.workspace.seat_purchase.confirm, {
                    count: seatPreview.additionalSeats,
                  })
                : t(($) => $.workspace.actions.add_seats)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={planChangeTarget !== null}
        onOpenChange={(open) => handlePlanChangeOpenChange(open)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {planChangeTarget === null
                ? null
                : planChangeIsUpgrade
                  ? t(($) => $.workspace.actions.upgrade_to, {
                      plan: planLabel(planChangeTarget),
                    })
                  : t(($) => $.workspace.actions.switch_to, {
                      plan: planLabel(planChangeTarget),
                    })}
            </DialogTitle>
            <DialogDescription>
              {planChangeIsUpgrade
                ? t(($) => $.workspace.plan_change.upgrade_description)
                : t(($) => $.workspace.plan_change.downgrade_description)}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            {planChangeQuoteChanged ? (
              <Alert>
                <AlertCircle />
                <AlertDescription>
                  {t(($) => $.workspace.plan_change.quote_changed)}
                </AlertDescription>
              </Alert>
            ) : null}
            {previewPlanChangeMutation.isPending ? (
              <div
                className="flex items-center gap-2 text-body text-muted-foreground"
                role="status"
              >
                <Loader2 className="animate-spin motion-reduce:animate-none" />
                {t(($) => $.workspace.plan_change.preview_loading)}
              </div>
            ) : null}
            {planChangeError ? (
              <Alert variant="destructive">
                <AlertCircle />
                <AlertTitle>
                  {t(($) => $.workspace.plan_change.error_title)}
                </AlertTitle>
                <AlertDescription>
                  <p>{planChangeError}</p>
                  {!planChangePreview ? (
                    <Button
                      className="mt-3"
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={previewPlanChangeMutation.isPending}
                      onClick={() =>
                        setPlanChangePreviewRevision(
                          (revision) => revision + 1,
                        )
                      }
                    >
                      <RefreshCw />
                      {t(($) => $.workspace.actions.retry)}
                    </Button>
                  ) : null}
                </AlertDescription>
              </Alert>
            ) : null}
            {planChangePreview &&
            formattedPlanChangeProration !== null &&
            formattedPlanChangeNextInvoice !== null ? (
              <div className="space-y-3 rounded-lg border border-surface-border p-4">
                <div className="flex items-center justify-between gap-4 text-body">
                  <span className="text-muted-foreground">
                    {t(($) => $.workspace.plan_change.seats)}
                  </span>
                  <span className="font-medium tabular-nums">
                    {t(($) => $.workspace.seats.seat_count, {
                      count: planChangePreview.seats,
                    })}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-4 text-body">
                  <span className="text-muted-foreground">
                    {planChangePreview.prorationAmount < 0
                      ? t(($) => $.workspace.plan_change.credit)
                      : t(($) => $.workspace.plan_change.charge_today)}
                  </span>
                  <span className="font-medium tabular-nums">
                    {formattedPlanChangeProration}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-4 text-body">
                  <span className="text-muted-foreground">
                    {t(($) => $.workspace.plan_change.next_invoice)}
                  </span>
                  <span className="font-medium tabular-nums">
                    {formattedPlanChangeNextInvoice}
                  </span>
                </div>
                <p className="text-caption leading-5 text-muted-foreground">
                  {t(($) => $.workspace.plan_change.tax_notice)}
                </p>
              </div>
            ) : null}
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              className="h-11"
              disabled={changePlanMutation.isPending}
              onClick={() => handlePlanChangeOpenChange(false)}
            >
              {t(($) => $.workspace.actions.cancel)}
            </Button>
            <Button
              type="button"
              className="h-11"
              disabled={
                !planChangePreview ||
                formattedPlanChangeProration === null ||
                formattedPlanChangeNextInvoice === null ||
                previewPlanChangeMutation.isPending ||
                changePlanMutation.isPending
              }
              onClick={() => void handlePlanChange()}
            >
              {changePlanMutation.isPending ? (
                <Loader2 className="animate-spin motion-reduce:animate-none" />
              ) : null}
              {planChangeTarget === null
                ? null
                : planChangeIsUpgrade
                  ? t(($) => $.workspace.actions.upgrade_to, {
                      plan: planLabel(planChangeTarget),
                    })
                  : t(($) => $.workspace.actions.switch_to, {
                      plan: planLabel(planChangeTarget),
                    })}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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
                plan: planLabel(checkoutPlan),
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
