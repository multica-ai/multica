"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
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
import { prefersReducedMotion } from "@multica/core/appearance";
import { autopilotQuotaUsageOptions } from "@multica/core/autopilots";
import {
  useChangeWorkspacePlan,
  useCreateWorkspaceSubscriptionCheckout,
  useCreateWorkspaceSubscriptionPortal,
  usePreviewWorkspacePlanChange,
  useReconcileWorkspaceSubscriptionSeats,
  usePreviewWorkspaceSeatPurchase,
  usePurchaseWorkspaceSeats,
  bindUpgradeInviteCheckout,
  clearUpgradeInvite,
  heldInvitesToOffer,
  listCheckoutHeldInvites,
  memberLimitKeys,
  memberLimitOptions,
  appCallUsageKeys,
  appCallUsageOptions,
  resolveAppCallRow,
  formatStorageBytes,
  resolveStorageRow,
  storageUsageKeys,
  storageUsageOptions,
  storageUsageProgress,
  readUpgradeInvite,
  type HeldInvite,
  workspaceFeatureKeys,
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
  WorkspaceSubscriptionSummary,
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
  SETTINGS_ANCHOR_ATTR,
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";
import { AppCallsLimitRow } from "./app-calls-limit-row";
import {
  hasActiveWorkspaceSeatCapacity,
  resolveAutopilotUsage,
  resolvePlanChangeTarget,
  yearlyPriceMonthlyEquivalent,
} from "./billing-state";
import { formatStripeMinorAmount } from "./billing-format";
import { AutopilotAddonDialog } from "../../billing/autopilot-addon-dialog";
import { AutopilotAddonReturnNotice } from "../../billing/autopilot-addon-return-notice";
import { BillingTrialSection } from "./billing-trial-section";
import { settingsHref } from "./settings-navigation";

export { formatStripeMinorAmount } from "./billing-format";

const CHECKOUT_SYNC_TIMEOUT_MS = 30_000;
const HELD_CHECKOUT_POLL_TIMEOUT_MS = 2 * 60_000;
const UPGRADE_SECTION_ANCHOR = "billing-upgrade";

function latestHeldCheckoutAt(invites: HeldInvite[]): number | null {
  return invites.reduce<number | null>(
    (latest, { checkout }) =>
      checkout ? Math.max(latest ?? checkout.boundAt, checkout.boundAt) : latest,
    null,
  );
}

/** Brings the plan cards into view, as a settings deep link would. */
function scrollToUpgrade() {
  const target = document.querySelector<HTMLElement>(
    `[${SETTINGS_ANCHOR_ATTR}="${UPGRADE_SECTION_ANCHOR}"]`,
  );
  target?.scrollIntoView?.({ block: "start", behavior: prefersReducedMotion() ? "auto" : "smooth" });
}
const SEAT_PURCHASE_POLL_TIMEOUT_MS = 2 * 60_000;
const SEAT_PURCHASE_PREVIEW_DEBOUNCE_MS = 800;
const PAID_PLANS = ["pro", "business"] as const;

type WorkspaceBillingReturnResult =
  | "success"
  | "cancel"
  | "portal"
  | "autopilot_addon"
  | "autopilot_addon_cancel";

function parseReturnResult(
  value: string | null,
): WorkspaceBillingReturnResult | null {
  switch (value) {
    case "success":
    case "cancel":
    case "portal":
    case "autopilot_addon":
    case "autopilot_addon_cancel":
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
  const hasReturnParams =
    navigation.searchParams.has("result") ||
    navigation.searchParams.has("session_id");
  // Cloud stamps every Stripe return with the workspace it authorized. A
  // return URL configured with a fixed workspace slug lands here for every
  // workspace, so a return that belongs to another one is handed to
  // /billing/return, which opens the workspace that started it.
  const returnWorkspaceId = navigation.searchParams.get("workspace_id");
  const returnBelongsElsewhere =
    hasReturnParams && !!wsId && !!returnWorkspaceId && returnWorkspaceId !== wsId;
  const returnResultParam = returnBelongsElsewhere
    ? null
    : parseReturnResult(navigation.searchParams.get("result"));
  const returnSessionId = returnBelongsElsewhere
    ? null
    : navigation.searchParams.get("session_id");
  const callbackKey =
    hasReturnParams && !returnBelongsElsewhere
      ? `${navigation.searchParams.get("result") ?? ""}:${returnSessionId ?? ""}`
      : null;
  const [returnState, setReturnState] = useState<{
    workspaceId: string | null;
    result: WorkspaceBillingReturnResult | null;
    observedAt: number | null;
    sessionId: string | null;
  }>(() => ({
    workspaceId: wsId || null,
    result: returnResultParam,
    observedAt: returnResultParam === "success" ? Date.now() : null,
    sessionId: returnSessionId,
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
    heldToken: string | null;
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
  // Set when a purchase was refused because another seat purchase or plan
  // change holds the subscription. The refetched summary would otherwise start
  // a new quote at once and replace that explanation; the user retries by
  // editing the seat count or reopening the dialog.
  const seatPurchaseBlockedRef = useRef(false);
  const blockSeatPurchase = () => {
    seatPurchaseBlockedRef.current = true;
    // A quote already in flight must not clear the explanation either.
    seatPreviewInputRef.current = "";
  };
  const consumedCallbackKeyRef = useRef<string | null>(null);
  const queryClient = useQueryClient();
  // The invitation Members held back for the checkout this page is about to
  // start ("Upgrade and invite"). Only that checkout buys a seat for it.
  const workspaceSlug = workspace?.slug ?? "";
  const [heldInvite, setHeldInvite] = useState<HeldInvite | null>(null);
  const [awaitingHeldCheckoutAt, setAwaitingHeldCheckoutAt] = useState<number | null>(null);
  const [heldCheckoutPollUntil, setHeldCheckoutPollUntil] = useState(0);
  useEffect(() => {
    setHeldInvite(null);
    setAwaitingHeldCheckoutAt(
      workspaceSlug ? latestHeldCheckoutAt(listCheckoutHeldInvites(workspaceSlug)) : null,
    );
  }, [workspaceSlug]);

  // Use the checkout timestamp so reopening Billing does not restart polling.
  // The invitation can still be sent after this window.
  useEffect(() => {
    const deadline =
      awaitingHeldCheckoutAt === null
        ? 0
        : awaitingHeldCheckoutAt + HELD_CHECKOUT_POLL_TIMEOUT_MS;
    const remaining = deadline - Date.now();
    if (remaining <= 0) {
      setHeldCheckoutPollUntil(0);
      return;
    }
    setHeldCheckoutPollUntil(deadline);
    const timeout = window.setTimeout(() => setHeldCheckoutPollUntil(0), remaining);
    return () => window.clearTimeout(timeout);
  }, [awaitingHeldCheckoutAt]);

  // Held invitations the paid plan may have bought a seat for, offered for
  // the person to send or discard.
  const [heldInvitesToConfirm, setHeldInvitesToConfirm] = useState<HeldInvite[]>([]);
  const memberLimitQuery = useQuery(memberLimitOptions(wsId));
  const storageUsageQuery = useQuery(storageUsageOptions(wsId));
  const appCallUsageQuery = useQuery(appCallUsageOptions(wsId));

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

  useEffect(() => {
    if (!returnBelongsElsewhere) return;
    const forward = new URLSearchParams();
    for (const key of ["workspace_id", "result", "session_id"]) {
      const value = navigation.searchParams.get(key);
      if (value) forward.set(key, value);
    }
    const destination = `/billing/return?${forward.toString()}`;
    if (consumedCallbackKeyRef.current === destination) return;
    consumedCallbackKeyRef.current = destination;
    navigation.replace(destination);
  }, [navigation, returnBelongsElsewhere]);

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
      sessionId: returnSessionId,
    });
    if (returnResultParam === "cancel") checkoutIntentRef.current = null;

    const params = new URLSearchParams(navigation.searchParams);
    params.delete("result");
    params.delete("session_id");
    const query = params.toString();
    navigation.replace(
      query ? `${navigation.pathname}?${query}` : navigation.pathname,
    );
  }, [callbackKey, navigation, returnResultParam, returnSessionId, wsId]);

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
    // Stripe webhooks may update the summary after the customer returns.
    refetchInterval: (query) =>
      isSyncingCheckout ||
      (query.state.data?.seatCapacity?.activePurchase &&
        !seatPurchasePollingTimedOut)
        ? 2_000
        : heldCheckoutPollUntil > Date.now()
          ? 5_000
          : false,
  });
  const refetchSummary = summaryQuery.refetch;
  useEffect(() => {
    if (awaitingHeldCheckoutAt === null) return;
    // Electron can regain focus from the system browser without a visibility change.
    const refresh = () => {
      void refetchSummary({ cancelRefetch: false });
    };
    window.addEventListener("focus", refresh);
    // Reopening Billing after the polling window may still hit a fresh cache.
    if (Date.now() >= awaitingHeldCheckoutAt + HELD_CHECKOUT_POLL_TIMEOUT_MS) refresh();
    return () => window.removeEventListener("focus", refresh);
  }, [awaitingHeldCheckoutAt, refetchSummary]);

  const entitlements = summaryQuery.data?.entitlement;
  // Absent from an older Cloud; the row is then left out.
  const dashboardHistoryLimit = entitlements?.limits.dashboardHistoryDays;
  const dashboardHistoryDays =
    dashboardHistoryLimit?.mode === "limited" ? dashboardHistoryLimit.limit : null;
  // A confirmed upgrade or seat purchase changes the storage and app call
  // limits; the entitlement already carries the new ones, and this refreshes
  // the usage reads behind them.
  const subscriptionVersion = summaryQuery.data?.entitlement.version;
  const seenSubscriptionVersionRef = useRef(subscriptionVersion);
  useEffect(() => {
    const seen = seenSubscriptionVersionRef.current;
    seenSubscriptionVersionRef.current = subscriptionVersion;
    if (seen === undefined || subscriptionVersion === undefined || seen === subscriptionVersion) return;
    void queryClient.invalidateQueries({ queryKey: storageUsageKeys.all(wsId) });
    void queryClient.invalidateQueries({ queryKey: appCallUsageKeys.all(wsId) });
  }, [queryClient, subscriptionVersion, wsId]);
  // Checkout is done once paid seats are in effect. Neither the plan nor
  // Checkout going away proves it: a trial already reports Business, and a
  // first payment still incomplete also takes Checkout away.
  const isCheckoutConfirmed =
    returnResult === "success" &&
    returnObservedAt !== null &&
    hasActiveWorkspaceSeatCapacity(summaryQuery.data) &&
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
  const [autopilotAddonOpen, setAutopilotAddonOpen] = useState(false);
  const pricesQuery = useQuery({
    ...workspaceSubscriptionPricesOptions(wsId),
    // Also read for the add-on pack price shown on the Buy button.
    enabled:
      wsId.length > 0 &&
      (canUpgrade ||
        summaryQuery.data?.availableActions.purchaseAutopilotRuns === true),
  });
  const checkoutMutation = useCreateWorkspaceSubscriptionCheckout(wsId);
  const portalMutation = useCreateWorkspaceSubscriptionPortal(wsId);
  const previewSeatPurchaseMutation = usePreviewWorkspaceSeatPurchase();
  const purchaseSeatsMutation = usePurchaseWorkspaceSeats(wsId);
  const previewPlanChangeMutation = usePreviewWorkspacePlanChange();
  const changePlanMutation = useChangeWorkspacePlan(wsId);
  const checkPlanChangeMutation = useReconcileWorkspaceSubscriptionSeats(wsId);
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
    if (!seatPurchaseOpen || seatPurchaseBlockedRef.current) return;
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
      // The paid plan is now recorded: re-read the paid features and plan
      // limits so nothing stays locked under the plan the user just left.
      void queryClient.invalidateQueries({ queryKey: workspaceFeatureKeys.all(wsId) });
    }
  }, [isCheckoutConfirmed, isSyncingCheckout, queryClient, wsId]);

  const sendHeldInvite = (held: HeldInvite) => {
    setHeldInvitesToConfirm([]);
    void queryClient.invalidateQueries({ queryKey: memberLimitKeys.all(wsId) });
    // The Stripe callback params are still in this render's URL; leaving
    // them on would replay the checkout banner the next time Billing opens.
    const params = new URLSearchParams(navigation.searchParams);
    params.delete("result");
    params.delete("session_id");
    navigation.push(
      `${settingsHref(navigation.pathname, params, "members")}&invite=resume&held_invite=${encodeURIComponent(held.token)}`,
    );
  };

  // A paid plan in effect: an incomplete first payment still reads as Free,
  // and a no-card trial is not a purchase.
  const subscriptionForHeldInvites = (summary: WorkspaceSubscriptionSummary) => ({
    version: summary.entitlement.version,
    paid:
      (summary.entitlement.plan === "pro" || summary.entitlement.plan === "business") &&
      summary.trial?.state !== "active",
  });

  // Bumped whenever held invitations change in storage, to re-read them.
  const [heldInvitesRevision, setHeldInvitesRevision] = useState(0);

  // Once the plan is paid, offer every held invitation it may have paid for,
  // for the person to send or discard. None is sent on its own (see
  // heldInvitesToOffer); reading the plan change rather than the Stripe return
  // is what lets Desktop, which pays in the system browser, get here too.
  useEffect(() => {
    const summary = summaryQuery.data;
    if (!summary || !workspaceSlug) return;
    const waiting = listCheckoutHeldInvites(workspaceSlug);
    const offers = heldInvitesToOffer(waiting, subscriptionForHeldInvites(summary));
    setAwaitingHeldCheckoutAt(offers.length === 0 ? latestHeldCheckoutAt(waiting) : null);
    setHeldInvitesToConfirm(offers);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- re-check on each summary read
  }, [summaryQuery.dataUpdatedAt, workspaceSlug, heldInvitesRevision]);

  const discardHeldInvite = (held: HeldInvite) => {
    clearUpgradeInvite(workspaceSlug, held.token);
    setHeldInvitesRevision((revision) => revision + 1);
  };

  // Members links here with `?upgrade=pro` from a full invite dialog: open the
  // Pro checkout confirmation once the summary says checkout is available.
  // `held_invite` names the invitation that checkout is for. The intent waits
  // for a summary that can decide it: a failed read keeps it, and the email
  // and role with it, for the summary retry.
  const upgradeRequest = navigation.searchParams.get("upgrade");
  const heldInviteToken = navigation.searchParams.get("held_invite");
  useEffect(() => {
    const summary = summaryQuery.data;
    if (upgradeRequest === null || !summary) return;
    const params = new URLSearchParams(navigation.searchParams);
    params.delete("upgrade");
    params.delete("held_invite");
    navigation.replace(`${navigation.pathname}?${params.toString()}`);
    const held =
      heldInviteToken && workspaceSlug
        ? readUpgradeInvite(workspaceSlug, heldInviteToken)
        : null;
    if (upgradeRequest === "pro" && summary.availableActions.checkout === true) {
      setHeldInvite(held);
      openCheckoutConfirm("pro");
    } else if (upgradeRequest === "business" && !held && resolvePlanChangeTarget(summary) === "business") {
      // A full workspace's storage notice (MUL-9489): Pro moves up to Business.
      handlePlanChangeOpenChange(true, "business");
    } else if (upgradeRequest === "business" && !held && summary.availableActions.checkout === true) {
      openCheckoutConfirm("business");
    } else if (held) {
      clearUpgradeInvite(workspaceSlug, held.token);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- consume the intent once the summary is known
  }, [upgradeRequest, summaryQuery.dataUpdatedAt]);

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
    // A retry reuses the key only for the same checkout: same plan, interval
    // and held invitation (or none), so a plain upgrade never replays one that
    // bought a seat for an invitation.
    const heldToken = heldInvite?.token ?? null;
    const intent =
      existing?.wsId === wsId &&
      existing.plan === checkoutPlan &&
      existing.interval === interval &&
      existing.heldToken === heldToken
        ? existing
        : {
            wsId,
            plan: checkoutPlan,
            interval,
            heldToken,
            key: createIdempotencyKey("workspace-checkout", wsId),
          };
    checkoutIntentRef.current = intent;
    try {
      const response = await checkoutMutation.mutateAsync({
        plan: intent.plan,
        interval: intent.interval,
        idempotencyKey: intent.key,
        ...(heldInvite ? { additionalSeats: 1 } : {}),
      });
      if (!response?.url) {
        setCheckoutConfirmOpen(false);
        setActionError(t(($) => $.workspace.errors.checkout_response));
        return;
      }
      setCheckoutConfirmOpen(false);
      if (heldInvite && workspaceSlug) {
        bindUpgradeInviteCheckout(workspaceSlug, heldInvite.token, {
          requestId: response.requestId,
          baseVersion: summaryQuery.data?.entitlement.version ?? 0,
        });
        setAwaitingHeldCheckoutAt(Date.now());
        // The invitation now belongs to that checkout. Any later checkout
        // from this page (Desktop returns here after closing the browser) is
        // a plain one and buys no seat for it.
        setHeldInvite(null);
        checkoutIntentRef.current = null;
      }
      openExternal(response.url, { webTarget: "same-tab" });
    } catch (error) {
      setCheckoutConfirmOpen(false);
      // A failed checkout for a held invitation ends it here: the buttons on
      // this page only start plain checkouts, and the invitation is held
      // again from Members if it is still wanted.
      if (heldInvite && workspaceSlug) {
        clearUpgradeInvite(workspaceSlug, heldInvite.token);
        setHeldInvite(null);
        checkoutIntentRef.current = null;
      }
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
    if (open) return;
    checkoutIntentRef.current = null;
    // Cancelled before any checkout: the held invitation goes with it, so a
    // later upgrade from this page does not buy a seat for it.
    if (heldInvite && !checkoutMutation.isPending && workspaceSlug) {
      clearUpgradeInvite(workspaceSlug, heldInvite.token);
      setHeldInvite(null);
    }
  };

  // The plan buttons always start a checkout of their own, never one for a
  // held invitation an earlier checkout from this page was opened for.
  const openPlainCheckoutConfirm = (plan: WorkspaceSubscriptionPaidPlan) => {
    setHeldInvite(null);
    openCheckoutConfirm(plan);
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
          blockSeatPurchase();
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
          blockSeatPurchase();
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
    seatPurchaseBlockedRef.current = false;
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
  const storageRow = resolveStorageRow(
    storageUsageQuery.data,
    storageUsageQuery.isError,
    entitlements?.limits.storageBytes,
  );
  const appCallRow = resolveAppCallRow(
    appCallUsageQuery.data,
    appCallUsageQuery.isError,
    entitlements?.limits.appToolCalls,
  );
  // This month's add-on runs, already part of the limit Cloud reports.
  const autopilotAddon = summaryQuery.data?.autopilotAddon ?? null;
  const canBuyAutopilotAddon =
    summaryQuery.data?.availableActions.purchaseAutopilotRuns === true;
  const autopilotAddonPrice = pricesQuery.data?.autopilotAddon ?? null;
  const formattedAutopilotAddonPrice = autopilotAddonPrice
    ? formatStripeMinorAmount(
        autopilotAddonPrice.unitAmount,
        autopilotAddonPrice.currency,
        locale,
      )
    : null;
  // Members and pending invitations against the plan's member limit, shown
  // whenever Cloud applies one (observed or enforced).
  const memberLimitData = memberLimitQuery.data;
  const memberLimit =
    (memberLimitData?.action === "observe" || memberLimitData?.action === "enforce") &&
    memberLimitData.limit !== null
      ? {
          used: (memberLimitData.members ?? 0) + (memberLimitData.pendingInvitations ?? 0),
          limit: memberLimitData.limit,
          pending: memberLimitData.pendingInvitations ?? 0,
        }
      : null;
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
      {heldInvitesToConfirm.length > 0 ? (
        <Alert>
          <Info />
          <AlertTitle>
            {t(($) => $.workspace.held_invite.title, {
              plan: planLabel(entitlements.plan),
            })}
          </AlertTitle>
          <ul className="col-start-2 mt-1 space-y-3">
            {heldInvitesToConfirm.map((held) => (
              <li key={held.token} className="space-y-2">
                <AlertDescription id={`held-invite-${held.token}`}>
                  {t(($) => $.workspace.held_invite.description, {
                    email: held.email,
                    role:
                      held.role === "admin"
                        ? t(($) => $.workspace.held_invite.role_admin)
                        : t(($) => $.workspace.held_invite.role_member),
                  })}
                </AlertDescription>
                <div className="flex flex-wrap gap-2">
                  <Button
                    size="sm"
                    aria-describedby={`held-invite-${held.token}`}
                    onClick={() => sendHeldInvite(held)}
                  >
                    {t(($) => $.workspace.held_invite.send)}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    aria-describedby={`held-invite-${held.token}`}
                    onClick={() => discardHeldInvite(held)}
                  >
                    {t(($) => $.workspace.held_invite.discard)}
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        </Alert>
      ) : null}

      {returnResult === "cancel" ? (
        <Alert>
          <AlertCircle />
          <AlertTitle>{t(($) => $.workspace.return.cancel_title)}</AlertTitle>
          <AlertDescription>
            {t(($) => $.workspace.return.cancel_description)}
          </AlertDescription>
        </Alert>
      ) : null}

      {returnResult === "autopilot_addon" ? (
        <AutopilotAddonReturnNotice wsId={wsId} sessionId={returnState.sessionId} />
      ) : null}

      {returnResult === "autopilot_addon_cancel" ? (
        <Alert>
          <AlertCircle />
          <AlertTitle>{t(($) => $.autopilot_addon.cancel_title)}</AlertTitle>
          <AlertDescription>
            {t(($) => $.autopilot_addon.cancel_description)}
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

      <BillingTrialSection
        summary={summaryQuery.data}
        onChoosePlan={canUpgrade ? scrollToUpgrade : undefined}
      />

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
        <SettingsSection
          title={t(($) => $.workspace.upgrade.title)}
          anchor={UPGRADE_SECTION_ANCHOR}
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
                        onClick={() => openPlainCheckoutConfirm(plan)}
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
            description={
              quotaUsage.kind === "unlimited"
                ? undefined
                : t(($) => $.workspace.limits.metering)
            }
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
                {autopilotAddon ? (
                  <p className="text-caption text-muted-foreground tabular-nums">
                    {t(($) => $.workspace.limits.addon_included, {
                      runs: numberFormatter.format(autopilotAddon.purchasedRuns),
                    })}
                  </p>
                ) : null}
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
          {storageRow ? (
            <SettingsRow
              label={t(($) => $.workspace.limits.storage)}
              description={t(($) => $.workspace.limits.storage_description)}
            >
              {storageRow.kind === "metered" ? (
                <div className="w-full max-w-72">
                  <Progress
                    value={storageUsageProgress(storageRow.usedBytes, storageRow.limitBytes)}
                    aria-label={t(($) => $.workspace.limits.storage_label)}
                  >
                    <ProgressLabel>
                      {storageRow.reached
                        ? t(($) => $.workspace.limits.reached)
                        : t(($) => $.workspace.limits.current_usage)}
                    </ProgressLabel>
                    <ProgressValue>
                      {() =>
                        t(($) => $.workspace.limits.usage_total, {
                          total: formatStorageBytes(storageRow.usedBytes, locale),
                          limit: formatStorageBytes(storageRow.limitBytes, locale),
                        })
                      }
                    </ProgressValue>
                  </Progress>
                </div>
              ) : (
                <div className="flex flex-col gap-2 sm:items-end">
                  {storageRow.limitBytes !== null ? (
                    <span className="tabular-nums">
                      {formatStorageBytes(storageRow.limitBytes, locale)}
                    </span>
                  ) : null}
                  <span className="text-caption text-muted-foreground">
                    {t(($) => $.workspace.limits.usage_unavailable)}
                  </span>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    aria-label={t(($) => $.workspace.actions.retry_storage)}
                    aria-busy={storageUsageQuery.isFetching}
                    disabled={storageUsageQuery.isFetching}
                    onClick={() => void storageUsageQuery.refetch()}
                  >
                    {storageUsageQuery.isFetching ? (
                      <Loader2 className="animate-spin motion-reduce:animate-none" />
                    ) : (
                      <RefreshCw />
                    )}
                    {t(($) => $.workspace.actions.retry)}
                  </Button>
                </div>
              )}
            </SettingsRow>
          ) : null}
          {appCallRow ? (
            <AppCallsLimitRow
              wsId={wsId}
              row={appCallRow}
              retrying={appCallUsageQuery.isFetching}
              onRetry={() => void appCallUsageQuery.refetch()}
            />
          ) : null}
          {autopilotAddon ? (
            <SettingsRow
              label={t(($) => $.workspace.limits.addon)}
              description={t(($) => $.workspace.limits.addon_description)}
            >
              <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-end">
                <span className="text-body text-muted-foreground tabular-nums">
                  {autopilotAddon.purchasedRuns > 0
                    ? t(($) => $.workspace.limits.addon_purchased, {
                        runs: numberFormatter.format(autopilotAddon.purchasedRuns),
                      })
                    : t(($) => $.workspace.limits.addon_none)}
                </span>
                {canBuyAutopilotAddon ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => setAutopilotAddonOpen(true)}
                  >
                    <Plus />
                    {autopilotAddonPrice && formattedAutopilotAddonPrice
                      ? t(($) => $.workspace.limits.addon_buy, {
                          price: formattedAutopilotAddonPrice,
                          runs: numberFormatter.format(autopilotAddonPrice.runsPerPack),
                        })
                      : t(($) => $.workspace.limits.addon_buy_fallback)}
                  </Button>
                ) : null}
              </div>
            </SettingsRow>
          ) : null}
          {dashboardHistoryDays !== null ? (
            <SettingsRow label={t(($) => $.workspace.limits.dashboard_history)}>
              <span className="tabular-nums">
                {t(($) => $.workspace.limits.dashboard_history_days, {
                  days: dashboardHistoryDays,
                })}
              </span>
            </SettingsRow>
          ) : null}
          {memberLimit ? (
            <SettingsRow label={t(($) => $.workspace.limits.members)}>
              <div className="flex flex-col gap-1 sm:items-end">
                <span className="tabular-nums">
                  {t(($) => $.workspace.limits.usage_total, {
                    total: numberFormatter.format(memberLimit.used),
                    limit: numberFormatter.format(memberLimit.limit),
                  })}
                </span>
                {memberLimit.pending > 0 ? (
                  <span className="text-caption text-muted-foreground tabular-nums">
                    {t(($) => $.workspace.limits.members_pending, {
                      pending: numberFormatter.format(memberLimit.pending),
                    })}
                  </span>
                ) : null}
              </div>
            </SettingsRow>
          ) : null}
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
                  seatPurchaseBlockedRef.current = false;
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
            {planChangePreview &&
            !planChangeIsUpgrade &&
            (autopilotAddon?.purchasedRuns ?? 0) > 0 &&
            planChangePreview.autopilotRunsLimitAfter !== null ? (
              <Alert>
                <AlertCircle />
                <AlertDescription>
                  {quotaUsage.kind === "metered"
                    ? t(($) => $.workspace.plan_change.addon_downgrade_notice, {
                        limit: numberFormatter.format(planChangePreview.autopilotRunsLimitAfter),
                        used: numberFormatter.format(quotaUsage.total),
                      })
                    : t(($) => $.workspace.plan_change.addon_downgrade_notice_limit_only, {
                        limit: numberFormatter.format(planChangePreview.autopilotRunsLimitAfter),
                      })}
                </AlertDescription>
              </Alert>
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
              {heldInvite ? (
                <span className="mt-2 block">
                  {t(($) => $.workspace.confirm.held_invite, {
                    email: heldInvite.email,
                  })}
                </span>
              ) : null}
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
      {autopilotAddonOpen ? (
        <AutopilotAddonDialog
          wsId={wsId}
          open={autopilotAddonOpen}
          onOpenChange={setAutopilotAddonOpen}
        />
      ) : null}
    </SettingsTab>
  );
}
