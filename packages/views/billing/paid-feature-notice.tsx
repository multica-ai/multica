"use client";

import { useCallback } from "react";
import { Lock } from "@multica/ui/icons";
import { isFeatureLockedError, type PaidFeatureAccessState } from "@multica/core/billing";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../navigation";
import { useT } from "../i18n";

/**
 * The one notice every paid feature shows when the workspace's plan does not
 * include it (MUL-9482): "read_only" where the resource already uses the
 * feature, "unavailable" where it would be turned on. Both link to Billing.
 */
export function PaidFeatureNotice({
  state,
  className,
}: {
  state: Exclude<PaidFeatureAccessState, "available">;
  className?: string;
}) {
  const { t } = useT("billing");
  const paths = useWorkspacePaths();
  return (
    <p className={`flex items-start gap-2 text-caption text-muted-foreground ${className ?? ""}`}>
      <Lock className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
      <span>
        {state === "read_only"
          ? t(($) => $.paid_feature.read_only)
          : t(($) => $.paid_feature.unavailable)}{" "}
        <AppLink href={`${paths.settings()}?tab=billing`} className="font-medium text-foreground underline-offset-4 hover:underline">
          {t(($) => $.paid_feature.view_plans)}
        </AppLink>
      </span>
    </p>
  );
}

/**
 * The message for a failed write: the localized paid-feature copy for a
 * feature_locked refusal, otherwise the caller's fallback.
 */
export function usePaidFeatureErrorMessage() {
  const { t } = useT("billing");
  return useCallback(
    (error: unknown, fallback: string) =>
      isFeatureLockedError(error) ? t(($) => $.paid_feature.locked_error) : fallback,
    [t],
  );
}
