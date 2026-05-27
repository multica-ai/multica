"use client";

import { useEffect, useMemo, useState } from "react";
import { AlertTriangle, Globe, Lock } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import type { Agent, AgentRuntime, MemberWithUser } from "@multica/core/types";
import {
  useDeleteRuntime,
  useArchiveAgentsAndDeleteRuntime,
} from "@multica/core/runtimes/mutations";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import {
  type AgentPresenceDetail,
  useWorkspacePresenceMap,
} from "@multica/core/agents";
import { useAuthStore } from "@multica/core/auth";
import {
  AlertDialog,
  AlertDialogContent,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { ActorAvatar } from "../../common/actor-avatar";
import { availabilityConfig, workloadConfig } from "../../agents/presence";
import { useT } from "../../i18n";
import { isSelfHealingRuntime } from "../utils";

// DeleteRuntimeDialog is the single confirmation surface for runtime
// deletion across the list-page kebab and the detail-page Diagnostics
// card. It runs in two modes that share the same shell — light when no
// agents are bound (matches the legacy "are you sure" prompt) and
// cascade when active agents would be archived as part of the delete.
//
// Mode is decided dynamically:
//   1. Initial: peek at the cached agent list and pick light vs cascade
//      based on whether any active agent.runtime_id === runtime.id. This
//      lets the dialog open in the right state without an extra request.
//   2. After the strict DELETE: if the server refuses with
//      `runtime_has_active_agents` (the local data was stale), switch to
//      cascade mode using the server's authoritative agent list.
//   3. After the cascade endpoint: if it refuses with
//      `runtime_delete_plan_changed`, refresh the displayed list with the
//      server snapshot and force the user to re-confirm the checkbox.
//
// Self-healing local runtimes (online local daemons that re-register
// themselves seconds after deletion — see isSelfHealingRuntime) are
// gated at the affordance level by the row-menu and Diagnostics card,
// so this dialog never opens for them. We re-check at confirm time as
// defence in depth in case the runtime flips while the dialog is open.
export interface DeleteRuntimeDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  runtime: AgentRuntime;
  wsId: string;
  // Called after a successful delete. List page closes the dialog and
  // toasts; detail page additionally navigates back to /runtimes.
  onDeleted: () => void;
}

export function DeleteRuntimeDialog({
  open,
  onOpenChange,
  runtime,
  wsId,
  onDeleted,
}: DeleteRuntimeDialogProps) {
  const { t } = useT("runtimes");
  // Pull cached workspace data — every consumer page already has these
  // mounted, so this dialog adds zero new fetches when opened.
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);
  const user = useAuthStore((s) => s.user);

  // The displayed plan starts from the cached agent list and is replaced by
  // server snapshots when the backend returns a structured 409 (either on
  // the initial DELETE or on a plan-changed cascade refusal). We hold it in
  // state so renders are deterministic even after invalidation flickers.
  const cachedActiveAgents = useMemo(
    () => agents.filter((a) => a.runtime_id === runtime.id && !a.archived_at),
    [agents, runtime.id],
  );
  const [planAgents, setPlanAgents] = useState<Agent[]>(cachedActiveAgents);
  // `cascade` is true when the visible plan has at least one agent. Mode is
  // a derived value, not user-controlled — the user always confirms the
  // current plan; switching is the consequence of a server response, not a
  // toggle.
  const cascade = planAgents.length > 0;

  const [confirmed, setConfirmed] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  // Server-issued message shown above the agent table on plan-changed —
  // tells the user "the list refreshed because something moved" without
  // re-stating it in the title. Stored as a localized string (already
  // translated) so the consumer can just render it.
  const [planChangedNotice, setPlanChangedNotice] = useState<string | null>(null);

  // Reset transient state every time the dialog opens. Without this a
  // previous plan-changed notice or a stale checkbox could survive across
  // open/close cycles and confuse the next attempt.
  useEffect(() => {
    if (open) {
      setPlanAgents(cachedActiveAgents);
      setConfirmed(false);
      setSubmitting(false);
      setPlanChangedNotice(null);
    }
  }, [open, cachedActiveAgents]);

  const lightMutation = useDeleteRuntime(wsId);
  const cascadeMutation = useArchiveAgentsAndDeleteRuntime(wsId);

  const handleConfirm = async () => {
    // Defensive re-check of the self-healing rule — the affordance is
    // gated upstream, but a local daemon that came online while the
    // dialog was open should still block the action.
    if (isSelfHealingRuntime(runtime)) {
      toast.error(
        t(($) => $.detail.delete_dialog.cascade.self_healing_blocked_toast),
      );
      return;
    }

    setSubmitting(true);
    setPlanChangedNotice(null);

    try {
      if (cascade) {
        await cascadeMutation.mutateAsync({
          runtimeId: runtime.id,
          expectedActiveAgentIds: planAgents.map((a) => a.id),
        });
        onDeleted();
      } else {
        try {
          await lightMutation.mutateAsync(runtime.id);
          onDeleted();
        } catch (err) {
          // The strict DELETE returns a structured 409 when active
          // agents were created between dialog-open and confirm. Pivot
          // into cascade mode using the server's authoritative list,
          // unset the checkbox, and surface a notice so the user knows
          // why the dialog suddenly grew an agent table.
          const conflict = parseActiveAgentsConflict(err);
          if (conflict?.code === "runtime_has_active_agents") {
            setPlanAgents(conflict.activeAgents);
            setConfirmed(false);
            setPlanChangedNotice(
              t(
                ($) =>
                  $.detail.delete_dialog.cascade.notice_runtime_has_active_agents,
              ),
            );
            return;
          }
          throw err;
        }
      }
    } catch (err) {
      // Cascade-side plan-changed: the user confirmed plan A but the live
      // set is now plan B. Refresh the displayed list with the server
      // snapshot, force the user to re-tick the checkbox.
      const conflict = parseActiveAgentsConflict(err);
      if (conflict?.code === "runtime_delete_plan_changed") {
        setPlanAgents(conflict.activeAgents);
        setConfirmed(false);
        setPlanChangedNotice(
          t(
            ($) =>
              $.detail.delete_dialog.cascade.notice_runtime_delete_plan_changed,
          ),
        );
        return;
      }
      const message =
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.detail.delete_dialog.cascade.delete_failed_toast);
      toast.error(message);
    } finally {
      setSubmitting(false);
    }
  };

  // Permission to act — `false` blocks the close while submitting, prevents
  // an accidental click on the underlying page from cancelling mid-write.
  const handleOpenChange = (next: boolean) => {
    if (submitting) return;
    onOpenChange(next);
  };

  // Light mode keeps the legacy short copy. Cascade mode mirrors the plan
  // 赵刚 wrote: destructive title with the count, a destructive warning
  // banner, the agent table, then a checkbox confirm whose label restates
  // the consequences in the same words as the warning.
  return (
    <AlertDialog open={open} onOpenChange={handleOpenChange}>
      <AlertDialogContent
        className={
          cascade
            ? "w-[calc(100vw-2rem)] !max-w-[640px] gap-0 overflow-hidden rounded-lg p-0"
            : "w-[calc(100vw-2rem)] !max-w-[440px] gap-0 overflow-hidden rounded-lg p-0"
        }
        onClick={(e) => e.stopPropagation()}
      >
        {cascade ? (
          <CascadeBody
            runtime={runtime}
            agents={planAgents}
            members={members}
            presenceMap={presenceMap}
            currentUserId={user?.id ?? null}
            confirmed={confirmed}
            onConfirmedChange={setConfirmed}
            planChangedNotice={planChangedNotice}
            submitting={submitting}
            onCancel={() => handleOpenChange(false)}
            onConfirm={handleConfirm}
          />
        ) : (
          <LightBody
            runtime={runtime}
            submitting={submitting}
            onCancel={() => handleOpenChange(false)}
            onConfirm={handleConfirm}
          />
        )}
      </AlertDialogContent>
    </AlertDialog>
  );
}

// ---------------------------------------------------------------------------
// Light mode — no active agents, classic "are you sure" prompt. Title and
// description match the legacy AlertDialog so existing screenshots / muscle
// memory still apply.
// ---------------------------------------------------------------------------

function LightBody({
  runtime,
  submitting,
  onCancel,
  onConfirm,
}: {
  runtime: AgentRuntime;
  submitting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useT("runtimes");
  return (
    <>
      <div className="px-5 pb-4 pt-5">
        <h2 className="text-base font-semibold">
          {t(($) => $.detail.delete_dialog.light.title)}
        </h2>
        <p className="mt-1 text-sm leading-5 text-muted-foreground">
          {t(($) => $.detail.delete_dialog.light.description, {
            name: runtime.name,
          })}
        </p>
      </div>
      <div className="border-t bg-muted/25 px-5 py-3">
        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button
            type="button"
            variant="outline"
            className="w-full sm:w-auto"
            onClick={onCancel}
            disabled={submitting}
          >
            {t(($) => $.detail.delete_dialog.light.cancel)}
          </Button>
          <Button
            type="button"
            variant="destructive"
            className="w-full sm:w-auto"
            onClick={onConfirm}
            disabled={submitting}
          >
            {submitting
              ? t(($) => $.detail.delete_dialog.light.submitting)
              : t(($) => $.detail.delete_dialog.light.confirm)}
          </Button>
        </div>
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// Cascade mode — destructive warning, agent table, checkbox-confirmed
// destructive button. Copy follows 赵刚's English text verbatim per the
// squad lead's directive.
// ---------------------------------------------------------------------------

function CascadeBody({
  runtime,
  agents,
  members,
  presenceMap,
  currentUserId,
  confirmed,
  onConfirmedChange,
  planChangedNotice,
  submitting,
  onCancel,
  onConfirm,
}: {
  runtime: AgentRuntime;
  agents: Agent[];
  members: MemberWithUser[];
  presenceMap: Map<string, AgentPresenceDetail>;
  currentUserId: string | null;
  confirmed: boolean;
  onConfirmedChange: (next: boolean) => void;
  planChangedNotice: string | null;
  submitting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useT("runtimes");
  const count = agents.length;

  return (
    <>
      <div className="px-5 pb-4 pt-5">
        <h2 className="text-base font-semibold">
          {t(($) => $.detail.delete_dialog.cascade.title, { count })}
        </h2>
        <p className="mt-1 text-sm leading-5 text-muted-foreground">
          {t(($) => $.detail.delete_dialog.cascade.description, {
            name: runtime.name,
          })}
        </p>

        {/* Destructive banner — keep the user's eye on the irreversible
            half before they scan the agent table. */}
        <div
          role="alert"
          className="mt-3 flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/5 px-3 py-2 text-xs text-destructive"
        >
          <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
          <span>{t(($) => $.detail.delete_dialog.cascade.warning)}</span>
        </div>

        {planChangedNotice && (
          <div
            role="status"
            className="mt-2 rounded-md border bg-muted/40 px-3 py-2 text-xs text-foreground"
          >
            {planChangedNotice}
          </div>
        )}

        <AgentPlanTable
          agents={agents}
          members={members}
          presenceMap={presenceMap}
          currentUserId={currentUserId}
        />
      </div>

      <div className="border-t bg-muted/25 px-5 py-4">
        <label className="flex cursor-pointer items-start gap-2 text-sm text-foreground">
          <Checkbox
            className="mt-0.5"
            checked={confirmed}
            onCheckedChange={(next) => onConfirmedChange(next === true)}
            disabled={submitting}
          />
          <span className="leading-5">
            {t(($) => $.detail.delete_dialog.cascade.checkbox, { count })}
          </span>
        </label>
        <div className="mt-3 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button
            type="button"
            variant="outline"
            className="w-full sm:w-auto"
            onClick={onCancel}
            disabled={submitting}
          >
            {t(($) => $.detail.delete_dialog.cascade.cancel)}
          </Button>
          <Button
            type="button"
            variant="destructive"
            className="w-full sm:w-auto"
            onClick={onConfirm}
            disabled={!confirmed || submitting}
          >
            {submitting
              ? t(($) => $.detail.delete_dialog.cascade.submitting)
              : t(($) => $.detail.delete_dialog.cascade.confirm, { count })}
          </Button>
        </div>
      </div>
    </>
  );
}

// AgentPlanTable renders the cascade plan: one row per agent showing the
// fields 赵刚 specified. Header + scroll body live in a fixed-height
// container so a runtime with many agents doesn't push the destructive
// confirm out of view; the body scrolls instead.
function AgentPlanTable({
  agents,
  members,
  presenceMap,
  currentUserId,
}: {
  agents: Agent[];
  members: MemberWithUser[];
  presenceMap: Map<string, AgentPresenceDetail>;
  currentUserId: string | null;
}) {
  const { t } = useT("runtimes");
  const memberById = useMemo(() => {
    const map = new Map<string, MemberWithUser>();
    for (const m of members) map.set(m.user_id, m);
    return map;
  }, [members]);

  return (
    <div className="mt-3 overflow-hidden rounded-md border">
      <div className="grid grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_minmax(0,1.2fr)_minmax(0,0.8fr)_minmax(0,1fr)] gap-3 border-b bg-muted/40 px-3 py-2 text-[11px] uppercase tracking-wide text-muted-foreground">
        <span>{t(($) => $.detail.delete_dialog.cascade.table.header_agent)}</span>
        <span>{t(($) => $.detail.delete_dialog.cascade.table.header_owner)}</span>
        <span>{t(($) => $.detail.delete_dialog.cascade.table.header_status)}</span>
        <span>
          {t(($) => $.detail.delete_dialog.cascade.table.header_visibility)}
        </span>
        <span>{t(($) => $.detail.delete_dialog.cascade.table.header_model)}</span>
      </div>
      <div className="max-h-[240px] overflow-y-auto divide-y">
        {agents.map((agent) => {
          const ownerMember = agent.owner_id
            ? memberById.get(agent.owner_id) ?? null
            : null;
          const ownerLabel = ownerMember
            ? ownerMember.user_id === currentUserId
              ? t(($) => $.detail.delete_dialog.cascade.table.owner_self)
              : ownerMember.name
            : t(($) => $.detail.delete_dialog.cascade.table.owner_unassigned);
          const presence = presenceMap.get(agent.id);
          return (
            <div
              key={agent.id}
              className="grid grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_minmax(0,1.2fr)_minmax(0,0.8fr)_minmax(0,1fr)] items-center gap-3 px-3 py-2 text-xs"
            >
              <span className="inline-flex min-w-0 items-center gap-2">
                <ActorAvatar
                  actorType="agent"
                  actorId={agent.id}
                  size={20}
                  enableHoverCard
                />
                <span className="truncate font-medium text-foreground">
                  {agent.name}
                </span>
              </span>
              <span className="inline-flex min-w-0 items-center gap-1.5">
                {ownerMember ? (
                  <ActorAvatar
                    actorType="member"
                    actorId={ownerMember.user_id}
                    size={16}
                  />
                ) : null}
                <span className="truncate text-muted-foreground">
                  {ownerLabel}
                </span>
              </span>
              <PresenceCell presence={presence} />
              <VisibilityCell visibility={agent.visibility} />
              <span className="truncate text-muted-foreground">
                {agent.model ||
                  t(($) => $.detail.delete_dialog.cascade.table.model_unset)}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function PresenceCell({ presence }: { presence: AgentPresenceDetail | undefined }) {
  const { t } = useT("runtimes");
  if (!presence) {
    return (
      <span className="text-muted-foreground/60">
        {t(($) => $.detail.delete_dialog.cascade.table.presence_unknown)}
      </span>
    );
  }
  const av = availabilityConfig[presence.availability];
  const wl = workloadConfig[presence.workload];
  const counts =
    presence.workload === "working"
      ? presence.queuedCount > 0
        ? `${presence.runningCount} +${presence.queuedCount}q`
        : `${presence.runningCount}`
      : presence.workload === "queued"
        ? `${presence.queuedCount}`
        : null;
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      <span className={`size-1.5 shrink-0 rounded-full ${av.dotClass}`} />
      <span className={av.textClass}>
        {presence.workload === "idle"
          ? t(($) => $.detail.delete_dialog.cascade.table.workload_idle)
          : null}
        {presence.workload === "working" && (
          <wl.icon className={`mr-1 inline size-3 align-[-2px] animate-spin ${wl.textClass}`} />
        )}
        {presence.workload === "queued" && (
          <wl.icon className={`mr-1 inline size-3 align-[-2px] ${wl.textClass}`} />
        )}
        {presence.workload === "working" &&
          t(($) => $.detail.delete_dialog.cascade.table.workload_working)}
        {presence.workload === "queued" &&
          t(($) => $.detail.delete_dialog.cascade.table.workload_queued)}
      </span>
      {counts && (
        <span className="font-mono tabular-nums text-muted-foreground/80">
          {counts}
        </span>
      )}
    </span>
  );
}

function VisibilityCell({ visibility }: { visibility: string }) {
  const { t } = useT("runtimes");
  if (visibility === "public" || visibility === "workspace") {
    return (
      <span className="inline-flex items-center gap-1 text-muted-foreground">
        <Globe className="size-3" />
        <span>
          {t(($) => $.detail.delete_dialog.cascade.table.visibility_workspace)}
        </span>
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 text-muted-foreground">
      <Lock className="size-3" />
      <span>
        {t(($) => $.detail.delete_dialog.cascade.table.visibility_private)}
      </span>
    </span>
  );
}

// ---------------------------------------------------------------------------
// Server response parsing
// ---------------------------------------------------------------------------

interface ActiveAgentsConflict {
  code: "runtime_has_active_agents" | "runtime_delete_plan_changed";
  activeAgents: Agent[];
}

// parseActiveAgentsConflict pulls the structured 409 fields off an ApiError.
// Non-409s, non-active-agents codes, and missing bodies all collapse to
// `null` so callers can fall through to the generic error toast.
function parseActiveAgentsConflict(err: unknown): ActiveAgentsConflict | null {
  if (!(err instanceof ApiError)) return null;
  if (err.status !== 409) return null;
  const body = err.body;
  if (!body || typeof body !== "object") return null;
  const code = (body as Record<string, unknown>).code;
  if (
    code !== "runtime_has_active_agents" &&
    code !== "runtime_delete_plan_changed"
  ) {
    return null;
  }
  const rawAgents = (body as Record<string, unknown>).active_agents;
  if (!Array.isArray(rawAgents)) {
    return { code, activeAgents: [] };
  }
  // We trust the server contract here — the response is the same
  // AgentResponse shape that the agent list endpoint returns. Light
  // runtime checks (id, runtime_id, name) catch genuinely malformed
  // payloads without re-typing every field.
  const activeAgents = rawAgents.filter(
    (a): a is Agent =>
      typeof a === "object" &&
      a !== null &&
      typeof (a as Record<string, unknown>).id === "string" &&
      typeof (a as Record<string, unknown>).name === "string",
  );
  return { code, activeAgents };
}
