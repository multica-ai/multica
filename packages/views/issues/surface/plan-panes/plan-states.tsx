"use client";

import type { ReactNode } from "react";
import { AlertTriangle, FileText } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useT } from "../../../i18n";

/** Request in flight — distinct from every other Plan state. */
export function PlanLoadingState() {
  return (
    <div className="flex flex-1 min-h-0 flex-col gap-4 p-6" data-testid="plan-loading-state">
      <Skeleton className="h-6 w-64" />
      <Skeleton className="h-16 w-full" />
      <Skeleton className="h-32 w-full" />
      <Skeleton className="h-32 w-full" />
    </div>
  );
}

/**
 * The read request failed. Rendered instead of PlanEmptyState so a transient
 * failure is never mistaken for "this project has no plan".
 */
export function PlanErrorState({ onRetry }: { onRetry: () => void }) {
  const { t } = useT("issues");
  return (
    <div
      role="alert"
      data-testid="plan-error-state"
      className="flex flex-1 min-h-0 flex-col items-center justify-center gap-3 text-muted-foreground"
    >
      <AlertTriangle className="h-10 w-10 text-destructive" />
      <p className="text-body">{t(($) => $.plan.error.title)}</p>
      <p className="text-caption">{t(($) => $.plan.error.hint)}</p>
      <Button variant="outline" size="sm" className="mt-1" onClick={onRetry}>
        {t(($) => $.plan.error.retry)}
      </Button>
    </div>
  );
}

/**
 * Shown for every Plan view mode when the endpoint confirms a genuine 404 —
 * no active plan for this project. Deliberately the same content for
 * Document, Pipeline, and Coverage: there is exactly one real state to
 * report here, not three fabricated ones.
 *
 * `action` is the manual-authoring entry point — a create-a-plan
 * button when the `project_plans` flag is on. It is a slot rather than a
 * hardcoded button so this state stays the single honest report of "no plan"
 * whether or not authoring is available.
 */
export function PlanNoPlanState({ action }: { action?: ReactNode }) {
  const { t } = useT("issues");
  return (
    <div
      data-testid="plan-no-plan-state"
      className="flex flex-1 min-h-0 flex-col items-center justify-center gap-3 text-muted-foreground"
    >
      <FileText className="h-10 w-10 text-faint-foreground" />
      <p className="text-body">{t(($) => $.plan_empty.title)}</p>
      <p className="text-caption">{t(($) => $.plan_empty.hint)}</p>
      {action}
    </div>
  );
}
