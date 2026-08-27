"use client";

import { useCallback, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  useCreateWorkspaceSubscriptionPortal,
  workspaceSubscriptionSummaryOptions,
} from "@multica/core/billing";
import { useFeatureEnabled } from "@multica/core/config";
import { BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG } from "@multica/core/feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { WorkspaceSubscriptionSummary } from "@multica/core/types";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { openExternal } from "../platform";

interface IssueLimitUpgradePromptOptions {
  onNavigate?: () => void;
}

type BillingActions = WorkspaceSubscriptionSummary["availableActions"];

function createPortalIdempotencyKey(wsId: string): string {
  const suffix =
    globalThis.crypto?.randomUUID?.() ??
    `${Date.now()}-${Math.random().toString(36).slice(2)}`;
  return `issue-limit-portal-${wsId}-${suffix}`.slice(0, 255);
}

/**
 * Immediately shows a persistent issue-limit recovery surface, then enriches
 * it with the complete action set authorized by Cloud for the current caller.
 * No local role, plan, subscription, or quota inference controls an action.
 */
export function useIssueLimitUpgradePrompt(
  options: IssueLimitUpgradePromptOptions = {},
): () => void {
  const { onNavigate } = options;
  const { t } = useT("modals");
  const queryClient = useQueryClient();
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const billingEnabled = useFeatureEnabled(
    BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG,
    false,
  );
  const createPortal = useCreateWorkspaceSubscriptionPortal(wsId).mutateAsync;
  const portalIntentKeyRef = useRef<string | null>(null);
  const dismissedRef = useRef(false);

  return useCallback(() => {
    dismissedRef.current = false;
    const toastId = `issue-limit-recovery:${wsId}`;
    const title = t(($) => $.create_issue.issue_limit.title);
    const markDismissed = () => {
      dismissedRef.current = true;
    };
    const persistentOptions = {
      id: toastId,
      duration: Infinity,
      closeButton: true,
      onDismiss: markDismissed,
      onAutoClose: markDismissed,
    };
    const dismissPrompt = () => {
      markDismissed();
      toast.dismiss(toastId);
    };
    const openBilling = () => {
      dismissPrompt();
      onNavigate?.();
      navigation.push(`${paths.settings()}?tab=billing`);
    };
    const showBillingFallback = () => {
      if (dismissedRef.current) return;
      toast.error(title, {
        ...persistentOptions,
        description: t(
          ($) => $.create_issue.issue_limit.billing_unavailable_description,
        ),
        action: {
          label: t(($) => $.create_issue.issue_limit.billing_action),
          onClick: openBilling,
        },
      });
    };
    const openPortal = async () => {
      const key =
        portalIntentKeyRef.current ?? createPortalIdempotencyKey(wsId);
      portalIntentKeyRef.current = key;
      try {
        const response = await createPortal(key);
        if (!response?.url) {
          showBillingFallback();
          return;
        }
        portalIntentKeyRef.current = null;
        dismissPrompt();
        onNavigate?.();
        openExternal(response.url, { webTarget: "same-tab" });
      } catch {
        showBillingFallback();
      }
    };
    const showAuthorizedActions = (actions: BillingActions) => {
      if (dismissedRef.current) return;
      if (actions.checkout) {
        toast.error(title, {
          ...persistentOptions,
          description: t(
            ($) => $.create_issue.issue_limit.upgrade_description,
          ),
          action: {
            label: t(($) => $.create_issue.issue_limit.upgrade_action),
            onClick: openBilling,
          },
        });
        return;
      }

      if (actions.portal) {
        toast.error(title, {
          ...persistentOptions,
          description: t(($) => $.create_issue.issue_limit.portal_description),
          action: {
            label: t(($) => $.create_issue.issue_limit.portal_action),
            onClick: () => {
              void openPortal();
            },
          },
        });
        return;
      }

      if (actions.purchaseSeats) {
        toast.error(title, {
          ...persistentOptions,
          description: t(
            ($) => $.create_issue.issue_limit.billing_description,
          ),
          action: {
            label: t(($) => $.create_issue.issue_limit.billing_action),
            onClick: openBilling,
          },
        });
        return;
      }

      toast.error(title, {
        ...persistentOptions,
        description: t(($) => $.create_issue.issue_limit.contact_description),
      });
    };

    if (!billingEnabled) {
      toast.error(title, {
        ...persistentOptions,
        description: t(
          ($) => $.create_issue.issue_limit.billing_disabled_description,
        ),
      });
      return;
    }

    const summaryOptions = workspaceSubscriptionSummaryOptions(wsId);
    const cachedSummary = queryClient.getQueryData<
      WorkspaceSubscriptionSummary | null
    >(summaryOptions.queryKey);
    if (cachedSummary) {
      showAuthorizedActions(cachedSummary.availableActions);
    } else {
      toast.error(title, {
        ...persistentOptions,
        description: t(
          ($) => $.create_issue.issue_limit.checking_description,
        ),
      });
    }

    // A quota rejection is a strong signal that cached billing state may be
    // stale. Refresh once in the background; the recovery surface above never
    // waits for Cloud or inherits the application's retry budget.
    void queryClient
      .fetchQuery({
        ...summaryOptions,
        staleTime: 0,
        retry: false,
      })
      .then((summary) => {
        if (summary) {
          showAuthorizedActions(summary.availableActions);
        } else if (!cachedSummary) {
          showBillingFallback();
        }
      })
      .catch(() => {
        if (!cachedSummary) showBillingFallback();
      });
  }, [
    billingEnabled,
    createPortal,
    navigation,
    onNavigate,
    paths,
    queryClient,
    t,
    wsId,
  ]);
}
