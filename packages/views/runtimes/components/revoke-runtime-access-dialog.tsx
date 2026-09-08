"use client";

import { useEffect, useState } from "react";
import { AlertTriangle } from "lucide-react";
import { toast } from "sonner";
import type { AgentRuntime } from "@multica/core/types";
import { ApiError, parseWithFallback } from "@multica/core/api";
import { RuntimeAccessRevocationPlanSchema } from "@multica/core/api/schemas";
import { runtimeDisplayLabel } from "@multica/core/runtimes";
import { useRevokeRuntimeWorkspaceAccess } from "@multica/core/runtimes/mutations";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { useT } from "../../i18n";

export interface RuntimeAccessRevocationPlan {
  agents: Array<{ id: string; name: string }>;
  activeTaskIds: string[];
}

// The 409 body is an API error payload, so parse only the fields this dialog
// consumes. Older servers do not send this code and simply retain the existing
// direct visibility-switch experience.
export function parseRuntimeAccessRevocationPlan(
  error: unknown,
): RuntimeAccessRevocationPlan | null {
  if (!(error instanceof ApiError) || error.status !== 409) {
    return null;
  }
  return parseWithFallback<RuntimeAccessRevocationPlan | null>(
    error.body, RuntimeAccessRevocationPlanSchema, null,
    { endpoint: "runtime-access-revocation-plan" },
  );
}

export function RevokeRuntimeAccessDialog({
  open,
  onOpenChange,
  runtime,
  wsId,
  initialPlan,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  runtime: AgentRuntime;
  wsId: string;
  initialPlan: RuntimeAccessRevocationPlan | null;
}) {
  const { t } = useT("runtimes");
  const revokeAccess = useRevokeRuntimeWorkspaceAccess(wsId);
  const [plan, setPlan] = useState<RuntimeAccessRevocationPlan | null>(initialPlan);
  const [confirmed, setConfirmed] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setPlan(initialPlan);
    setConfirmed(false);
    setNotice(null);
  }, [open, initialPlan]);

  const submitting = revokeAccess.isPending;
  const handleConfirm = () => {
    if (!plan) return;
    revokeAccess.mutate(
      {
        runtimeId: runtime.id,
        expectedNonownerAgentIds: plan.agents.map((agent) => agent.id),
        expectedTaskIds: plan.activeTaskIds,
      },
      {
        onSuccess: () => {
          onOpenChange(false);
          toast.success(t(($) => $.detail.visibility_revoke_dialog.toast_success));
        },
        onError: (error) => {
          const updatedPlan = parseRuntimeAccessRevocationPlan(error);
          if (updatedPlan) {
            setPlan(updatedPlan);
            setConfirmed(false);
            setNotice(t(($) => $.detail.visibility_revoke_dialog.plan_changed));
            return;
          }
          toast.error(
            error instanceof Error && error.message
              ? error.message
              : t(($) => $.detail.visibility_revoke_dialog.toast_failed),
          );
        },
      },
    );
  };

  return (
    <AlertDialog open={open} onOpenChange={(next) => !submitting && onOpenChange(next)}>
      <AlertDialogContent className="w-[calc(100vw-2rem)] !max-w-[560px] gap-0 overflow-hidden rounded-lg p-0">
        <div className="px-5 pb-4 pt-5">
          <AlertDialogTitle className="text-title-sm font-semibold">
            {t(($) => $.detail.visibility_revoke_dialog.title)}
          </AlertDialogTitle>
          <AlertDialogDescription className="mt-1 text-body leading-5 text-muted-foreground">
            {t(($) => $.detail.visibility_revoke_dialog.description, {
              name: runtimeDisplayLabel(runtime),
              count: plan?.agents.length ?? 0,
              taskCount: plan?.activeTaskIds.length ?? 0,
            })}
          </AlertDialogDescription>
          <div
            role="alert"
            className="mt-3 flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/5 px-3 py-2 text-caption text-destructive"
          >
            <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
            <span>{t(($) => $.detail.visibility_revoke_dialog.warning)}</span>
          </div>
          {notice && (
            <div role="status" className="mt-2 rounded-md border bg-muted/40 px-3 py-2 text-caption">
              {notice}
            </div>
          )}
          {plan && plan.agents.length > 0 && (
            <ul className="mt-3 max-h-40 space-y-1 overflow-y-auto rounded-md border bg-muted/20 p-2 text-body">
              {plan.agents.map((agent) => <li key={agent.id}>{agent.name}</li>)}
            </ul>
          )}
        </div>
        <div className="border-t bg-muted/25 px-5 py-4">
          <label className="flex cursor-pointer items-start gap-2 text-body text-foreground">
            <Checkbox
              className="mt-0.5"
              checked={confirmed}
              onCheckedChange={(next) => setConfirmed(next === true)}
              disabled={submitting}
            />
            <span className="leading-5">
              {t(($) => $.detail.visibility_revoke_dialog.checkbox, {
                count: plan?.agents.length ?? 0,
                taskCount: plan?.activeTaskIds.length ?? 0,
              })}
            </span>
          </label>
          <div className="mt-3 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <Button type="button" variant="outline" className="w-full sm:w-auto" onClick={() => onOpenChange(false)} disabled={submitting}>
              {t(($) => $.detail.visibility_revoke_dialog.cancel)}
            </Button>
            <Button type="button" variant="destructive" className="w-full sm:w-auto" onClick={handleConfirm} disabled={!confirmed || submitting || !plan}>
              {submitting
                ? t(($) => $.detail.visibility_revoke_dialog.submitting)
                : t(($) => $.detail.visibility_revoke_dialog.confirm)}
            </Button>
          </div>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  );
}
