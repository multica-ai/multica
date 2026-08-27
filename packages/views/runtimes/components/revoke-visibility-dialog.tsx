"use client";

import { useEffect, useState, type ReactNode } from "react";
import { AlertTriangle, Info } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@multica/core/api";
import type { AgentRuntime } from "@multica/core/types";
import { runtimeDisplayLabel } from "@multica/core/runtimes";
import { useRevokeRuntimeAndMakePrivate } from "@multica/core/runtimes/mutations";
import {
  AlertDialog,
  AlertDialogContent,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { useT } from "../../i18n";

// Impact confirmation for taking a shared machine back (public → private,
// MUL-6704). Reclaiming access unbinds other members' agents, cancels their work
// and pauses their Autopilots, so the PATCH refuses with
// `runtime_visibility_has_foreign_agents` + this plan and the user confirms the
// exact set here; the confirm endpoint re-checks it under a lock. Same 409 → plan
// → checkbox → re-confirm flow as DeleteRuntimeDialog's cascade mode, which users
// have already learned.

/**
 * All the server discloses about an affected agent. Deliberately not the full
 * `Agent`: the reader owns the MACHINE, not these agents, and often cannot read
 * them at all, so id + name is the whole surface.
 */
export interface RuntimeRevokeAgent {
  id: string;
  name: string;
}

export interface RuntimeRevokePlan {
  activeAgents: RuntimeRevokeAgent[];
  archivedAgentCount: number;
  retainedAgentCount: number;
  mikaAffected: boolean;
}

export interface RevokeVisibilityDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  runtime: AgentRuntime;
  wsId: string;
  plan: RuntimeRevokePlan;
  // Called after the revoke commits, so the caller can toast and refresh.
  onRevoked: () => void;
}

export function RevokeVisibilityDialog({
  open,
  onOpenChange,
  runtime,
  wsId,
  plan: initialPlan,
  onRevoked,
}: RevokeVisibilityDialogProps) {
  const { t } = useT("runtimes");
  const [plan, setPlan] = useState<RuntimeRevokePlan>(initialPlan);
  const [confirmed, setConfirmed] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [planChangedNotice, setPlanChangedNotice] = useState<string | null>(null);
  const revoke = useRevokeRuntimeAndMakePrivate(wsId);

  // Re-seed on open: a stale notice or ticked checkbox would let the user confirm
  // a plan they never read.
  useEffect(() => {
    if (open) {
      setPlan(initialPlan);
      setConfirmed(false);
      setSubmitting(false);
      setPlanChangedNotice(null);
    }
  }, [open, initialPlan]);

  const handleConfirm = async () => {
    setSubmitting(true);
    setPlanChangedNotice(null);
    try {
      // Every category the user just read, not only the named agents: the
      // archived and retained counts are part of the impact they approved.
      await revoke.mutateAsync({
        runtimeId: runtime.id,
        expectedActiveAgentIds: plan.activeAgents.map((a) => a.id),
        expectedArchivedAgentCount: plan.archivedAgentCount,
        expectedRetainedAgentCount: plan.retainedAgentCount,
      });
      onRevoked();
    } catch (err) {
      // The set moved while the dialog was open; the server wrote nothing.
      const conflict = parseRuntimeRevokeConflict(err);
      if (conflict?.code === "runtime_visibility_plan_changed") {
        setPlan(conflict.plan);
        setConfirmed(false);
        setPlanChangedNotice(
          t(($) => $.detail.revoke_visibility_dialog.notice_plan_changed),
        );
        return;
      }
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.detail.revoke_visibility_dialog.failed_toast),
      );
    } finally {
      setSubmitting(false);
    }
  };

  const handleOpenChange = (next: boolean) => {
    if (submitting) return;
    onOpenChange(next);
  };

  const count = plan.activeAgents.length;

  return (
    <AlertDialog open={open} onOpenChange={handleOpenChange}>
      <AlertDialogContent
        className="w-[calc(100vw-2rem)] !max-w-[560px] gap-0 overflow-hidden rounded-lg p-0"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="px-5 pb-4 pt-5">
          <h2 className="text-title-sm font-semibold">
            {t(($) => $.detail.revoke_visibility_dialog.title, { count })}
          </h2>
          <p className="mt-1 text-body leading-5 text-muted-foreground">
            {t(($) => $.detail.revoke_visibility_dialog.description, {
              name: runtimeDisplayLabel(runtime),
            })}
          </p>

          <Banner tone="destructive">
            {t(($) => $.detail.revoke_visibility_dialog.warning)}
          </Banner>

          {/* One Mika per workspace, so unbinding her stops her for everyone. */}
          {plan.mikaAffected && (
            <Banner tone="destructive">
              {t(($) => $.detail.revoke_visibility_dialog.mika_warning)}
            </Banner>
          )}

          {planChangedNotice && (
            <div
              role="status"
              className="mt-2 rounded-md border bg-muted/40 px-3 py-2 text-caption text-foreground"
            >
              {planChangedNotice}
            </div>
          )}

          {count > 0 && (
            <ul className="mt-3 max-h-48 divide-y overflow-y-auto rounded-md border">
              {plan.activeAgents.map((agent) => (
                <li
                  key={agent.id}
                  className="flex items-center justify-between gap-2 px-3 py-2 text-body"
                >
                  <span className="truncate">{agent.name}</span>
                  <span className="shrink-0 text-caption text-muted-foreground">
                    {t(($) => $.detail.revoke_visibility_dialog.agent_action_unbind)}
                  </span>
                </li>
              ))}
            </ul>
          )}

          {plan.archivedAgentCount > 0 && (
            <p className="mt-2 text-caption text-muted-foreground">
              {t(($) => $.detail.revoke_visibility_dialog.archived_note, {
                count: plan.archivedAgentCount,
              })}
            </p>
          )}

          {/* Carriers keep their binding (unbinding strands them, deleting them
              destroys the conversation) but cannot run here, so point at the fix. */}
          {plan.retainedAgentCount > 0 && (
            <Banner tone="warning">
              {t(($) => $.detail.revoke_visibility_dialog.retained_note, {
                count: plan.retainedAgentCount,
              })}
            </Banner>
          )}

          <label className="mt-4 flex cursor-pointer items-start gap-2 text-body">
            <Checkbox
              checked={confirmed}
              onCheckedChange={(next) => setConfirmed(next === true)}
              disabled={submitting}
            />
            <span>
              {t(($) => $.detail.revoke_visibility_dialog.checkbox, { count })}
            </span>
          </label>
        </div>

        <div className="border-t bg-muted/25 px-5 py-3">
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <Button
              type="button"
              variant="outline"
              className="w-full sm:w-auto"
              onClick={() => handleOpenChange(false)}
              disabled={submitting}
            >
              {t(($) => $.detail.revoke_visibility_dialog.cancel)}
            </Button>
            <Button
              type="button"
              variant="destructive"
              className="w-full sm:w-auto"
              onClick={handleConfirm}
              disabled={submitting || !confirmed}
            >
              {submitting
                ? t(($) => $.detail.revoke_visibility_dialog.submitting)
                : t(($) => $.detail.revoke_visibility_dialog.confirm)}
            </Button>
          </div>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  );
}

// Banner is the dialog's one inline-notice shape; tone picks destructive
// (consequences of confirming) or warning (something to repair afterwards).
function Banner({
  tone,
  children,
}: {
  tone: "destructive" | "warning";
  children: ReactNode;
}) {
  const destructive = tone === "destructive";
  const Icon = destructive ? AlertTriangle : Info;
  return (
    <div
      role={destructive ? "alert" : "status"}
      className={`mt-2 flex items-start gap-2 rounded-md border px-3 py-2 text-caption ${
        destructive
          ? "border-destructive/40 bg-destructive/5 text-destructive"
          : "border-warning/40 bg-warning/5"
      }`}
    >
      <Icon
        className={`mt-0.5 size-3.5 shrink-0 ${destructive ? "" : "text-warning"}`}
      />
      <span>{children}</span>
    </div>
  );
}

export interface RuntimeRevokeConflict {
  code:
    | "runtime_visibility_has_foreign_agents"
    | "runtime_visibility_plan_changed";
  plan: RuntimeRevokePlan;
}

/**
 * Reads the structured 409 strictly, and fails closed: any unexpected status,
 * code, missing field or wrong type returns `null` so the caller shows a plain
 * error and no dialog opens. A tolerant parser is not harmless —
 * `archived_agent_count` / `retained_agent_count` are not in
 * `expected_active_agent_ids`, so the server's set comparison cannot catch a body
 * that under-reports them, and the user would confirm a plan they never saw.
 */
export function parseRuntimeRevokeConflict(
  err: unknown,
): RuntimeRevokeConflict | null {
  if (!(err instanceof ApiError)) return null;
  if (err.status !== 409) return null;
  const body = err.body;
  if (!body || typeof body !== "object" || Array.isArray(body)) return null;
  const record = body as Record<string, unknown>;
  const code = record.code;
  if (
    code !== "runtime_visibility_has_foreign_agents" &&
    code !== "runtime_visibility_plan_changed"
  ) {
    return null;
  }
  if (!Array.isArray(record.active_agents)) return null;
  const activeAgents: RuntimeRevokeAgent[] = [];
  for (const raw of record.active_agents) {
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;
    const entry = raw as Record<string, unknown>;
    if (typeof entry.id !== "string" || entry.id === "") return null;
    if (typeof entry.name !== "string") return null;
    activeAgents.push({ id: entry.id, name: entry.name });
  }
  const archivedAgentCount = strictCount(record.archived_agent_count);
  const retainedAgentCount = strictCount(record.retained_agent_count);
  if (archivedAgentCount === null || retainedAgentCount === null) return null;
  if (typeof record.mika_affected !== "boolean") return null;
  return {
    code,
    plan: {
      activeAgents,
      archivedAgentCount,
      retainedAgentCount,
      mikaAffected: record.mika_affected,
    },
  };
}

/** A count is a non-negative integer or the body is not trustworthy. */
function strictCount(value: unknown): number | null {
  if (typeof value !== "number" || !Number.isInteger(value) || value < 0) {
    return null;
  }
  return value;
}
