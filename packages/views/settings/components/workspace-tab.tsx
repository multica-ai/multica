"use client";

import { useEffect, useMemo, useState } from "react";
import {
  Save,
  LogOut,
  MessageCircle,
  Rocket,
  Webhook,
  Copy,
  Check,
  RefreshCw,
} from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useLeaveWorkspace, useDeleteWorkspace } from "@multica/core/workspace/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentListOptions,
  memberListOptions,
  workspaceKeys,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import { api } from "@multica/core/api";
import {
  resolvePostAuthDestination,
  useCurrentWorkspace,
  useHasOnboarded,
} from "@multica/core/paths";
import { setCurrentWorkspace } from "@multica/core/platform";
import type { Workspace } from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { DeleteWorkspaceDialog } from "./delete-workspace-dialog";
import { useT } from "../../i18n";

export function WorkspaceTab() {
  const { t } = useT("settings");
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const { data: members = [], isFetched: membersFetched } = useQuery(memberListOptions(wsId));
  const qc = useQueryClient();
  const leaveWorkspace = useLeaveWorkspace();
  const deleteWorkspace = useDeleteWorkspace();
  const navigation = useNavigation();
  const hasOnboarded = useHasOnboarded();

  /**
   * Send the user to a safe URL BEFORE the leave/delete mutation fires.
   * The destination is computed from the current cached workspace list,
   * minus the workspace that's about to go away.
   *
   * Why navigate first, not after:
   *   1. The backend broadcasts `workspace:deleted` / `member:removed` the
   *      moment the mutation lands. If the user is still on the soon-to-
   *      be-deleted workspace's URL when that event arrives, the realtime
   *      handler in `use-realtime-sync.ts` also triggers a relocation —
   *      and both code paths race with the mutation's own
   *      `invalidateQueries` refetch. The loser's in-flight fetch gets
   *      cancelled, surfacing as an unhandled `CancelledError`.
   *   2. Navigating first means by the time the WS event fires, the
   *      active workspace is already something else; the realtime
   *      handler's "current === deleted" check fails and its relocate
   *      branch no-ops.
   *   3. UX: the destructive flow feels instant (dialog closes → new
   *      workspace appears) even though the API hasn't responded yet.
   */
  const navigateAwayFromCurrentWorkspace = () => {
    const cachedList =
      qc.getQueryData<Workspace[]>(workspaceListOptions().queryKey) ?? [];
    const remaining = cachedList.filter((w) => w.id !== workspace?.id);
    // Clear the workspace-context singleton BEFORE navigating and BEFORE
    // the mutation fires. Three downstream consumers read it:
    //  1. Realtime `workspace:deleted` handler's "current === deleted"
    //     check — if the singleton still points at the deleting workspace
    //     when the WS event arrives, it fires a parallel relocate that
    //     races the mutation's invalidate and the settings page's own
    //     navigate, surfacing a CancelledError and a full-page reload.
    //  2. Chrome gating (`{slug && <AppSidebar />}` on desktop) — if the
    //     singleton lingers, the sidebar stays mounted while the deleted
    //     workspace is no longer in the list, and `useWorkspaceId` throws.
    //  3. API client's `X-Workspace-Slug` header — stale header post-
    //     delete is at best a 404, at worst leaks into the next query.
    // WorkspaceRouteLayout re-sets the singleton when a new workspace's
    // route mounts; clearing here is safe — either the next workspace
    // takes over immediately, or the new-workspace overlay takes over
    // (which has no workspace context, so null is correct).
    setCurrentWorkspace(null, null);
    navigation.push(resolvePostAuthDestination(remaining, hasOnboarded));
  };

  const [name, setName] = useState(workspace?.name ?? "");
  const [description, setDescription] = useState(workspace?.description ?? "");
  const [context, setContext] = useState(workspace?.context ?? "");
  // Sentinel "" → "no orchestrator selected" (clears the pointer on save).
  // The Select component disallows actual empty-string values, so we use
  // a NONE sentinel for the rendered <SelectItem> and translate at save time.
  const ORCHESTRATOR_NONE = "__none__";
  const [orchestratorAgentId, setOrchestratorAgentId] = useState<string>(
    workspace?.orchestrator_agent_id ?? ORCHESTRATOR_NONE,
  );
  const [saving, setSaving] = useState(false);
  const [actionId, setActionId] = useState<string | null>(null);
  const [confirmAction, setConfirmAction] = useState<{
    title: string;
    description: string;
    variant?: "destructive";
    onConfirm: () => Promise<void>;
  } | null>(null);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManageWorkspace = currentMember?.role === "owner" || currentMember?.role === "admin";
  const isOwner = currentMember?.role === "owner";
  // Mirror the backend invariant (server/internal/handler/workspace.go:569):
  // a workspace must always have at least one owner, so the sole owner can't
  // leave. Pre-flight here instead of letting the 400 round-trip become a
  // confusing toast — disable Leave and tell the user what they need to do.
  const ownerCount = members.filter((m) => m.role === "owner").length;
  const isSoleOwner = isOwner && ownerCount <= 1;
  const isSoleMember = members.length <= 1;

  useEffect(() => {
    setName(workspace?.name ?? "");
    setDescription(workspace?.description ?? "");
    setContext(workspace?.context ?? "");
    setOrchestratorAgentId(workspace?.orchestrator_agent_id ?? ORCHESTRATOR_NONE);
  }, [workspace]);

  // Agents available to pick as the orchestrator. Filtered to non-archived;
  // the orchestrator field is forced to NULL by the schema's ON DELETE
  // SET NULL when archived/deleted, but offering archived agents in the
  // picker would be misleading.
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const activeAgents = agents.filter((a) => !a.archived_at);

  const handleSave = async () => {
    if (!workspace) return;
    setSaving(true);
    try {
      const orchestratorChanged =
        (workspace.orchestrator_agent_id ?? "") !==
        (orchestratorAgentId === ORCHESTRATOR_NONE ? "" : orchestratorAgentId);
      const updated = await api.updateWorkspace(workspace.id, {
        name,
        description,
        context,
        // Only send the paired orchestrator fields when the user actually
        // changed the picker — avoids sending a no-op write that would
        // otherwise look like an explicit "set" in audit logs.
        ...(orchestratorChanged
          ? {
              orchestrator_agent_id_set: true,
              orchestrator_agent_id:
                orchestratorAgentId === ORCHESTRATOR_NONE ? null : orchestratorAgentId,
            }
          : {}),
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success(t(($) => $.workspace.toast_saved));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.workspace.toast_save_failed));
    } finally {
      setSaving(false);
    }
  };

  const handleLeaveWorkspace = () => {
    if (!workspace) return;
    setConfirmAction({
      title: t(($) => $.workspace.leave_confirm_title),
      description: t(($) => $.workspace.leave_confirm_description, { name: workspace.name }),
      variant: "destructive",
      onConfirm: async () => {
        setActionId("leave");
        navigateAwayFromCurrentWorkspace();
        try {
          await leaveWorkspace.mutateAsync(workspace.id);
        } catch (e) {
          toast.error(e instanceof Error ? e.message : t(($) => $.workspace.toast_leave_failed));
        } finally {
          setActionId(null);
        }
      },
    });
  };

  const handleConfirmDelete = async () => {
    if (!workspace) return;
    setActionId("delete-workspace");
    // Close the dialog and navigate away FIRST. See navigateAwayFromCurrentWorkspace
    // comment for why: keeps the realtime `workspace:deleted` handler out
    // of the race so we don't end up with concurrent refetches cancelling
    // each other and surfacing CancelledError.
    setDeleteDialogOpen(false);
    navigateAwayFromCurrentWorkspace();
    try {
      await deleteWorkspace.mutateAsync(workspace.id);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.workspace.toast_delete_failed));
    } finally {
      setActionId(null);
    }
  };

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      {/* Workspace settings */}
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.workspace.section_general)}</h2>

        <Card>
          <CardContent className="space-y-3">
            <div>
              <Label className="text-xs text-muted-foreground">{t(($) => $.workspace.name_label)}</Label>
              <Input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={!canManageWorkspace}
                className="mt-1"
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">{t(($) => $.workspace.description_label)}</Label>
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={3}
                disabled={!canManageWorkspace}
                className="mt-1 resize-none"
                placeholder={t(($) => $.workspace.description_placeholder)}
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">{t(($) => $.workspace.context_label)}</Label>
              <Textarea
                value={context}
                onChange={(e) => setContext(e.target.value)}
                rows={4}
                disabled={!canManageWorkspace}
                className="mt-1 resize-none"
                placeholder={t(($) => $.workspace.context_placeholder)}
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">{t(($) => $.workspace.slug_label)}</Label>
              <div className="mt-1 rounded-md border bg-muted/50 px-3 py-2 text-sm text-muted-foreground">
                {workspace.slug}
              </div>
            </div>
            {/*
             * Orchestrator agent: when set, the server enqueues a task for
             * this agent on every agent-authored issue comment so it can
             * coordinate cross-agent workflows (acknowledge work, reassign,
             * change status, ping a human). Self-loops and assignee-collisions
             * are skipped server-side. Members and archived agents are
             * excluded from the picker.
             */}
            <div>
              <Label className="text-xs text-muted-foreground">{t(($) => $.workspace.orchestrator_label)}</Label>
              <Select
                value={orchestratorAgentId}
                onValueChange={(v) => { if (v) setOrchestratorAgentId(v); }}
                disabled={!canManageWorkspace}
              >
                <SelectTrigger className="mt-1 w-full">
                  <SelectValue placeholder={t(($) => $.workspace.orchestrator_placeholder)} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={ORCHESTRATOR_NONE}>{t(($) => $.workspace.orchestrator_none)}</SelectItem>
                  {activeAgents.map((a) => (
                    <SelectItem key={a.id} value={a.id}>
                      {a.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="mt-1 text-xs text-muted-foreground">
                {t(($) => $.workspace.orchestrator_hint)}
              </p>
            </div>
            <div className="flex items-center justify-end gap-2 pt-1">
              <Button
                size="sm"
                onClick={handleSave}
                disabled={saving || !name.trim() || !canManageWorkspace}
              >
                <Save className="h-3 w-3" />
                {saving ? t(($) => $.workspace.saving) : t(($) => $.workspace.save)}
              </Button>
            </div>
            {!canManageWorkspace && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.workspace.manage_hint)}
              </p>
            )}
          </CardContent>
        </Card>
      </section>

      {/* Channels feature gate. Hidden from non-admins so we don't tease a
          control they can't toggle. The full Channels feature lives behind
          this single boolean — when off the sidebar entry hides and every
          /api/channels endpoint 404s. See migration 065 + the channels spec
          for the rest of the surface. */}
      {canManageWorkspace && (
        <section className="space-y-4">
          <div className="flex items-center gap-2">
            <MessageCircle className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-sm font-semibold">{t(($) => $.channels.section_title)}</h2>
          </div>
          <Card>
            <CardContent className="space-y-3">
              <ChannelsSettings workspace={workspace} />
            </CardContent>
          </Card>
        </section>
      )}

      {/* Ship Hub settings — admin-only, paired with channels above so the
          two feature flags live next to each other. The token field is
          write-only: the server never echoes it back, only a presence
          flag, so the input renders empty and "Configured" status comes
          from `github_token_set`. */}
      {canManageWorkspace && (
        <section className="space-y-4">
          <div className="flex items-center gap-2">
            <Rocket className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-sm font-semibold">{t(($) => $.ship_hub.section_title)}</h2>
          </div>
          <Card>
            <CardContent className="space-y-3">
              <ShipHubSettings workspace={workspace} />
            </CardContent>
          </Card>
        </section>
      )}

      {/* Danger Zone — gated on the member query settling so the owner-only
          Delete button and the sole-owner Leave guidance don't flash in
          after mount. */}
      {membersFetched && (
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <LogOut className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">{t(($) => $.workspace.danger_zone)}</h2>
        </div>

        <Card>
          <CardContent className="space-y-3">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <p className="text-sm font-medium">{t(($) => $.workspace.leave_title)}</p>
                <p className="text-xs text-muted-foreground">
                  {isSoleOwner
                    ? isSoleMember
                      ? t(($) => $.workspace.leave_sole_member)
                      : t(($) => $.workspace.leave_sole_owner)
                    : t(($) => $.workspace.leave_default)}
                </p>
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={handleLeaveWorkspace}
                disabled={actionId === "leave" || isSoleOwner}
              >
                {actionId === "leave" ? t(($) => $.workspace.leaving) : t(($) => $.workspace.leave_button)}
              </Button>
            </div>

            {isOwner && (
              <div className="flex flex-col gap-2 border-t pt-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <p className="text-sm font-medium text-destructive">{t(($) => $.workspace.delete_title)}</p>
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.workspace.delete_description)}
                  </p>
                </div>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => setDeleteDialogOpen(true)}
                  disabled={actionId === "delete-workspace"}
                >
                  {actionId === "delete-workspace" ? t(($) => $.workspace.deleting) : t(($) => $.workspace.delete_button)}
                </Button>
              </div>
            )}
          </CardContent>
        </Card>
      </section>
      )}

      <AlertDialog open={!!confirmAction} onOpenChange={(v) => { if (!v) setConfirmAction(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirmAction?.title}</AlertDialogTitle>
            <AlertDialogDescription>{confirmAction?.description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.workspace.confirm_cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant={confirmAction?.variant === "destructive" ? "destructive" : "default"}
              onClick={async () => {
                await confirmAction?.onConfirm();
                setConfirmAction(null);
              }}
            >
              {t(($) => $.workspace.confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <DeleteWorkspaceDialog
        workspaceName={workspace.name}
        loading={actionId === "delete-workspace"}
        open={deleteDialogOpen}
        onOpenChange={(open) => {
          // Ignore close requests while the delete mutation is in flight
          // so the user can't accidentally dismiss mid-operation.
          if (actionId === "delete-workspace" && !open) return;
          setDeleteDialogOpen(open);
        }}
        onConfirm={handleConfirmDelete}
      />
    </div>
  );
}

interface ChannelsSettingsProps {
  workspace: Workspace;
}

/**
 * ChannelsSettings — admin toggle for `workspace.channels_enabled`.
 *
 * Optimistic + reconciled: flips the local state immediately on click so
 * the UI feels instant, then sends the PATCH and reconciles on response.
 * On error we revert the toggle and surface a toast — the rest of the
 * workspace settings card uses direct api.* calls (not useMutation) and
 * we follow the same convention rather than introducing a one-off hook.
 */
function ChannelsSettings({ workspace }: ChannelsSettingsProps) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const [enabled, setEnabled] = useState(workspace.channels_enabled);
  const [pending, setPending] = useState(false);

  // Reconcile when the workspace cache updates from elsewhere (WS event,
  // another tab) so the switch never drifts from the source of truth.
  useEffect(() => {
    setEnabled(workspace.channels_enabled);
  }, [workspace.channels_enabled]);

  const handleToggle = async (next: boolean) => {
    setEnabled(next);
    setPending(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        channels_enabled: next,
        channels_enabled_set: true,
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((w) => (w.id === updated.id ? updated : w)),
      );
      toast.success(next ? t(($) => $.channels.toast_enabled) : t(($) => $.channels.toast_disabled));
    } catch (e) {
      setEnabled(!next);
      toast.error(e instanceof Error ? e.message : t(($) => $.channels.toast_toggle_failed));
    } finally {
      setPending(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-sm font-medium">{t(($) => $.channels.enable_label)}</p>
          <p className="text-xs text-muted-foreground">
            {t(($) => $.channels.enable_description)}
          </p>
        </div>
        <Switch
          checked={enabled}
          onCheckedChange={handleToggle}
          disabled={pending}
          aria-label={t(($) => $.channels.enable_aria)}
        />
      </div>
      {enabled && <RetentionSettings workspace={workspace} />}
    </div>
  );
}

interface RetentionSettingsProps {
  workspace: Workspace;
}

/**
 * RetentionSettings — workspace-level default retention window. Per-channel
 * overrides live in the channel settings dialog and beat this default.
 *
 * UX: a "Retain forever" checkbox swaps the integer input in/out. Default
 * suggestion is 90 days (matches the channels spec). Submitting normalizes:
 * checkbox checked → channel_retention_days=null, set=true; checkbox
 * unchecked → channel_retention_days=N, set=true.
 *
 * The "transition to finite" warning is rendered when the user toggles the
 * checkbox off and types a value smaller than the message age that would
 * actually exist — but since we don't have that data on the client, we
 * just show the warning unconditionally when going forever→finite.
 */
function RetentionSettings({ workspace }: RetentionSettingsProps) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const initialForever = workspace.channel_retention_days == null;
  const initialDays = workspace.channel_retention_days ?? 90;
  const [forever, setForever] = useState(initialForever);
  const [days, setDays] = useState<number>(initialDays);
  const [saving, setSaving] = useState(false);

  // Reconcile when the cached workspace updates (e.g. from another tab).
  useEffect(() => {
    setForever(workspace.channel_retention_days == null);
    if (workspace.channel_retention_days != null) {
      setDays(workspace.channel_retention_days);
    }
  }, [workspace.channel_retention_days]);

  const dirty =
    forever !== initialForever ||
    (!forever && days !== initialDays);

  // Warn when the user is about to introduce retention against an existing
  // workspace that previously had none — old messages will start getting
  // hidden by the next sweep.
  const transitioningToFinite = !forever && initialForever;

  // Validation: days must be 1..3650 (10 years) per the channels spec.
  const daysValid = forever || (Number.isInteger(days) && days >= 1 && days <= 3650);

  const handleSave = async () => {
    if (!dirty || !daysValid || saving) return;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        channel_retention_days: forever ? null : days,
        channel_retention_days_set: true,
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((w) => (w.id === updated.id ? updated : w)),
      );
      toast.success(t(($) => $.channels.retention_toast_saved));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.channels.retention_toast_save_failed));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-2 border-t border-border pt-3">
      <div className="text-sm font-medium">{t(($) => $.channels.retention_title)}</div>
      <p className="text-xs text-muted-foreground">
        {t(($) => $.channels.retention_description)}
      </p>
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={forever}
          onChange={(e) => setForever(e.target.checked)}
          disabled={saving}
        />
        {t(($) => $.channels.retain_forever)}
      </label>
      {!forever && (
        <div className="flex items-center gap-2">
          <Input
            id="channel-retention-days"
            type="number"
            min={1}
            max={3650}
            placeholder="90"
            value={days}
            onChange={(e) => setDays(Number(e.target.value))}
            disabled={saving}
            className="w-24"
          />
          <span className="text-xs text-muted-foreground">{t(($) => $.channels.days)}</span>
        </div>
      )}
      {!daysValid && (
        <p className="text-xs text-destructive" role="alert">
          {t(($) => $.channels.retention_invalid)}
        </p>
      )}
      {transitioningToFinite && (
        <p className="text-xs text-muted-foreground">
          {t(($) => $.channels.retention_warning, { days: Number.isFinite(days) ? days : 0 })}
        </p>
      )}
      <div>
        <Button size="sm" onClick={handleSave} disabled={!dirty || !daysValid || saving}>
          <Save className="h-3 w-3" />
          {saving ? t(($) => $.channels.retention_saving) : t(($) => $.channels.retention_save)}
        </Button>
      </div>
    </div>
  );
}

interface ShipHubSettingsProps {
  workspace: Workspace;
}

/**
 * ShipHubSettings — admin toggle for `workspace.ship_hub_enabled` plus
 * write-only GitHub PAT input.
 *
 * The token field is genuinely write-only: the server returns
 * `github_token_set: bool` instead of the value, so this component renders
 * an empty input and shows "Configured" / "Not configured" derived from
 * that flag. Submitting an empty value while "Configured" is shown clears
 * the token (paired-bool pattern: `github_token: null, github_token_set: true`).
 */
function ShipHubSettings({ workspace }: ShipHubSettingsProps) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const [enabled, setEnabled] = useState(workspace.ship_hub_enabled);
  const [pendingToggle, setPendingToggle] = useState(false);
  const [tokenInput, setTokenInput] = useState("");
  const [tokenSaving, setTokenSaving] = useState(false);

  // Reconcile when the workspace cache updates from elsewhere (WS event,
  // another tab) so the switch never drifts from the source of truth.
  useEffect(() => {
    setEnabled(workspace.ship_hub_enabled);
  }, [workspace.ship_hub_enabled]);

  const handleToggle = async (next: boolean) => {
    setEnabled(next);
    setPendingToggle(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        ship_hub_enabled: next,
        ship_hub_enabled_set: true,
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((w) => (w.id === updated.id ? updated : w)),
      );
      toast.success(
        next
          ? t(($) => $.ship_hub.toast_enabled)
          : t(($) => $.ship_hub.toast_disabled),
      );
    } catch (e) {
      setEnabled(!next);
      toast.error(
        e instanceof Error ? e.message : t(($) => $.ship_hub.toast_toggle_failed),
      );
    } finally {
      setPendingToggle(false);
    }
  };

  const handleTokenSave = async () => {
    setTokenSaving(true);
    try {
      const trimmed = tokenInput.trim();
      const updated = await api.updateWorkspace(workspace.id, {
        // Empty input is a no-op: we only PATCH when the user typed
        // something. To clear, the user clicks "Clear token" below.
        github_token: trimmed,
        github_token_set: true,
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((w) => (w.id === updated.id ? updated : w)),
      );
      setTokenInput("");
      toast.success(t(($) => $.ship_hub.token_toast_saved));
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.ship_hub.token_toast_failed),
      );
    } finally {
      setTokenSaving(false);
    }
  };

  const handleTokenClear = async () => {
    setTokenSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        github_token: null,
        github_token_set: true,
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((w) => (w.id === updated.id ? updated : w)),
      );
      setTokenInput("");
      toast.success(t(($) => $.ship_hub.token_toast_cleared));
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.ship_hub.token_toast_failed),
      );
    } finally {
      setTokenSaving(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-sm font-medium">{t(($) => $.ship_hub.enable_label)}</p>
          <p className="text-xs text-muted-foreground">
            {t(($) => $.ship_hub.enable_description)}
          </p>
        </div>
        <Switch
          checked={enabled}
          onCheckedChange={handleToggle}
          disabled={pendingToggle}
          aria-label={t(($) => $.ship_hub.enable_aria)}
        />
      </div>

      {enabled && (
        <div className="space-y-2 border-t border-border pt-3">
          <div className="text-sm font-medium">
            {t(($) => $.ship_hub.token_title)}
          </div>
          <p className="text-xs text-muted-foreground">
            {t(($) => $.ship_hub.token_description)}
          </p>
          <div className="text-xs text-muted-foreground">
            {workspace.github_token_set
              ? t(($) => $.ship_hub.token_status_set)
              : t(($) => $.ship_hub.token_status_unset)}
          </div>
          <Label className="text-xs text-muted-foreground" htmlFor="ship-hub-token">
            {t(($) => $.ship_hub.token_label)}
          </Label>
          <Input
            id="ship-hub-token"
            type="password"
            autoComplete="off"
            value={tokenInput}
            onChange={(e) => setTokenInput(e.target.value)}
            placeholder={t(($) => $.ship_hub.token_placeholder)}
            disabled={tokenSaving}
          />
          <p className="text-xs text-muted-foreground">
            {t(($) => $.ship_hub.token_help)}
          </p>
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              onClick={handleTokenSave}
              disabled={tokenSaving || !tokenInput.trim()}
            >
              <Save className="h-3 w-3" />
              {tokenSaving
                ? t(($) => $.ship_hub.token_saving)
                : t(($) => $.ship_hub.token_save)}
            </Button>
            {workspace.github_token_set && (
              <Button
                size="sm"
                variant="ghost"
                onClick={handleTokenClear}
                disabled={tokenSaving}
              >
                {t(($) => $.ship_hub.token_clear)}
              </Button>
            )}
          </div>
        </div>
      )}

      {/* Phase 2 — GitHub webhook setup. The webhook URL is computed
          server-side from MULTICA_API_BASE_URL; the secret is generated
          on demand and shown once. The same URL/secret is reused across
          every repo in the workspace, so the user only completes this
          flow once even with many repositories. */}
      {enabled && <WebhookSettings workspace={workspace} />}

      {/* Phase 7d follow-up — configurable approval rules per risk
          tier. Owner-only because the rule directly controls who can
          ship to production; admins can reach everything else but the
          policy decision belongs to ownership. */}
      {enabled && <ApprovalSettings workspace={workspace} />}

      {/* Phase 7d follow-up — auto-detect deploys via GitHub Actions
          polling. When configured, removes the need for the user to
          click "Mark deploy as landed" because the server polls
          completed runs of the workflow on main and auto-links by
          sha. Mirrors the structure of ApprovalSettings — paired-bool
          PATCH that only sends the fields the user actually changed. */}
      {enabled && <DeployWorkflowSettings workspace={workspace} />}
    </div>
  );
}

interface WebhookSettingsProps {
  workspace: Workspace;
}

/**
 * WebhookSettings — workspace-scoped GitHub webhook config.
 *
 * Renders three pieces:
 *   1. The Payload URL (copy-on-click), surfaced from
 *      `workspace.ship_hub_webhook_url` so the server controls what to
 *      display (computed from MULTICA_API_BASE_URL with a sensible
 *      fallback for local dev).
 *   2. A status pill driven by `workspace.ship_hub_webhook_secret_set`.
 *   3. A "Generate" / "Rotate" button that calls the regenerate endpoint;
 *      the plaintext secret is shown ONCE in a one-time-display modal,
 *      mirroring the personal-access-token create flow in tokens-tab.tsx.
 *
 * No "Test webhook" affordance — the backend doesn't (yet) expose a
 * webhook_health endpoint. When that lands, surface "Last delivery:
 * <timestamp>" here. Until then, document the gap in code rather than
 * shipping a button that 404s.
 */
function WebhookSettings({ workspace }: WebhookSettingsProps) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const [generating, setGenerating] = useState(false);
  const [revealedSecret, setRevealedSecret] = useState<string | null>(null);
  const [secretCopied, setSecretCopied] = useState(false);
  const [urlCopied, setUrlCopied] = useState(false);

  const isConfigured = workspace.ship_hub_webhook_secret_set;
  const url = workspace.ship_hub_webhook_url;

  const handleGenerate = async () => {
    setGenerating(true);
    try {
      const result = await api.regenerateShipHubWebhookSecret(workspace.id);
      // Schema fallback returns "" when the response is malformed; only
      // celebrate if we actually got a secret back.
      if (!result.webhook_secret) {
        toast.error(t(($) => $.ship_hub.webhook_toast_failed));
        return;
      }
      setRevealedSecret(result.webhook_secret);
      // The workspace cache holds `ship_hub_webhook_secret_set: false` if
      // this is the first generation. The handler returns `true` after a
      // successful write, so optimistically flip the cached row so the
      // status pill updates immediately. WS event traffic will reconcile.
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((w) =>
          w.id === workspace.id
            ? { ...w, ship_hub_webhook_secret_set: true }
            : w,
        ),
      );
      toast.success(
        isConfigured
          ? t(($) => $.ship_hub.webhook_toast_rotated)
          : t(($) => $.ship_hub.webhook_toast_generated),
      );
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.ship_hub.webhook_toast_failed),
      );
    } finally {
      setGenerating(false);
    }
  };

  const handleCopyUrl = async () => {
    if (!url) return;
    await navigator.clipboard.writeText(url);
    setUrlCopied(true);
    setTimeout(() => setUrlCopied(false), 1500);
  };

  const handleCopySecret = async () => {
    if (!revealedSecret) return;
    await navigator.clipboard.writeText(revealedSecret);
    setSecretCopied(true);
    setTimeout(() => setSecretCopied(false), 1500);
  };

  const handleDialogClose = (open: boolean) => {
    if (open) return;
    // Reset both the secret and the copy indicator together so reopening
    // the modal (a second rotation in the same session) doesn't briefly
    // flash "Copied" before the new secret renders.
    setRevealedSecret(null);
    setSecretCopied(false);
  };

  return (
    <div className="space-y-3 border-t border-border pt-3">
      <div className="flex items-center gap-2">
        <Webhook className="h-3.5 w-3.5 text-muted-foreground" />
        <div className="text-sm font-medium">
          {t(($) => $.ship_hub.webhook_section_title)}
        </div>
      </div>
      <p className="text-xs text-muted-foreground">
        {t(($) => $.ship_hub.webhook_description)}
      </p>

      {/* URL row — copy-on-click. Reads from the workspace shape (not a
          local config) so a server-side env-var change automatically
          propagates without a frontend release. */}
      <div className="space-y-1">
        <Label className="text-xs text-muted-foreground">
          {t(($) => $.ship_hub.webhook_url_label)}
        </Label>
        <div className="flex items-center gap-2">
          <code className="flex-1 truncate rounded-md border bg-muted/50 px-3 py-2 text-xs select-all">
            {url || (
              <span className="text-muted-foreground">
                {t(($) => $.ship_hub.webhook_url_unavailable)}
              </span>
            )}
          </code>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="outline"
                  size="icon-sm"
                  onClick={handleCopyUrl}
                  disabled={!url}
                  aria-label={t(($) => $.ship_hub.webhook_copy_url_tooltip)}
                >
                  {urlCopied ? (
                    <Check className="h-3.5 w-3.5" />
                  ) : (
                    <Copy className="h-3.5 w-3.5" />
                  )}
                </Button>
              }
            />
            <TooltipContent>
              {t(($) => $.ship_hub.webhook_copy_url_tooltip)}
            </TooltipContent>
          </Tooltip>
        </div>
      </div>

      {/* Status pill + generate/rotate button */}
      <div className="flex flex-wrap items-center gap-2">
        <span
          className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs ${
            isConfigured
              ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400"
              : "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-400"
          }`}
        >
          <span
            className={`size-1.5 rounded-full ${isConfigured ? "bg-emerald-500" : "bg-amber-500"}`}
            aria-hidden
          />
          {isConfigured
            ? t(($) => $.ship_hub.webhook_secret_status_set)
            : t(($) => $.ship_hub.webhook_secret_status_unset)}
        </span>
        <Button
          size="sm"
          variant={isConfigured ? "outline" : "default"}
          onClick={handleGenerate}
          disabled={generating}
        >
          <RefreshCw className="h-3 w-3" />
          {isConfigured
            ? generating
              ? t(($) => $.ship_hub.webhook_rotating)
              : t(($) => $.ship_hub.webhook_rotate)
            : generating
              ? t(($) => $.ship_hub.webhook_generating)
              : t(($) => $.ship_hub.webhook_generate)}
        </Button>
      </div>

      {/* Setup steps — numbered list with explicit GH navigation cues so
          a user new to webhooks can follow without leaving the page. */}
      <div className="space-y-1.5 rounded-md border bg-muted/30 p-3 text-xs">
        <div className="font-medium">
          {t(($) => $.ship_hub.webhook_instructions_title)}
        </div>
        <ol className="list-decimal space-y-1 pl-4 text-muted-foreground">
          <li>{t(($) => $.ship_hub.webhook_step_repo)}</li>
          <li>{t(($) => $.ship_hub.webhook_step_url)}</li>
          <li>{t(($) => $.ship_hub.webhook_step_secret)}</li>
          <li>{t(($) => $.ship_hub.webhook_step_content_type)}</li>
          <li>{t(($) => $.ship_hub.webhook_step_events)}</li>
          <li>{t(($) => $.ship_hub.webhook_step_save)}</li>
        </ol>
      </div>

      {/* One-time-display modal for the freshly minted secret. Mirrors
          the PAT-create dialog at packages/views/settings/components/
          tokens-tab.tsx — captures the value, lets the user copy, then
          discards on close. The plaintext is never written to localStorage
          or any persisted cache. */}
      <Dialog open={!!revealedSecret} onOpenChange={handleDialogClose}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t(($) => $.ship_hub.webhook_secret_dialog_title)}
            </DialogTitle>
            <DialogDescription>
              {t(($) => $.ship_hub.webhook_secret_dialog_description)}
            </DialogDescription>
          </DialogHeader>
          <div className="flex items-center gap-2">
            <code className="flex-1 rounded-md border bg-muted/50 px-3 py-2 text-sm break-all select-all">
              {revealedSecret}
            </code>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="outline"
                    size="icon"
                    onClick={handleCopySecret}
                  >
                    {secretCopied ? (
                      <Check className="h-4 w-4" />
                    ) : (
                      <Copy className="h-4 w-4" />
                    )}
                  </Button>
                }
              />
              <TooltipContent>
                {t(($) => $.ship_hub.webhook_secret_dialog_copy_tooltip)}
              </TooltipContent>
            </Tooltip>
          </div>
          <DialogFooter>
            <Button
              onClick={() => {
                setRevealedSecret(null);
                setSecretCopied(false);
              }}
            >
              {t(($) => $.ship_hub.webhook_secret_dialog_done)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

/** normalizeApprovalRule clamps an arbitrary string from the wire
 *  to one of the four known rule values. Unknown / undefined values
 *  fall back to the per-tier default so an old server response (or
 *  a malformed PATCH) never leaves the Select with no value. */
function normalizeApprovalRule(
  v: string | undefined,
  fallback: "member" | "admin" | "approver" | "two",
): "member" | "admin" | "approver" | "two" {
  if (v === "member" || v === "admin" || v === "approver" || v === "two") {
    return v;
  }
  return fallback;
}

interface ApprovalSettingsProps {
  workspace: Workspace;
}

/**
 * ApprovalSettings — workspace owner controls for the per-risk-tier
 * verify/promote rule. Phase 7d follow-up.
 *
 * Each of the four risk tiers (low / medium / high / critical) gets
 * its own Select with four rule values. Defaults preserve the legacy
 * hardcoded behavior (low/medium → "member", high → "approver",
 * critical → "two") so a workspace that hasn't touched these
 * controls behaves exactly like it did before the migration.
 *
 * The "Allow PR authors to verify" checkbox flips
 * `ship_hub_approver_can_be_author`; defaults to true (small teams
 * typically self-verify; large teams flip it off).
 *
 * Saving sends paired-bool patches per field — only the rule(s) the
 * user actually changed are sent, so concurrent edits from another
 * tab don't get clobbered by an "echo back" of the cached state.
 */
function ApprovalSettings({ workspace }: ApprovalSettingsProps) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  // Type cast — the field is a free-form string on the wire (per the
  // API drift contract); the UI normalizes to one of the four known
  // rule values for the Select. An unrecognized value rendered before
  // the user touched the picker is replaced by the legacy default
  // for that tier so the dropdown never shows an empty trigger.
  type Rule = "member" | "admin" | "approver" | "two";
  const tiers: ReadonlyArray<{
    risk: "low" | "medium" | "high" | "critical";
    fallback: Rule;
    field: "low" | "medium" | "high" | "critical";
  }> = [
    { risk: "low", fallback: "member", field: "low" },
    { risk: "medium", fallback: "member", field: "medium" },
    { risk: "high", fallback: "approver", field: "high" },
    { risk: "critical", fallback: "two", field: "critical" },
  ];

  // norm is intentionally module-private — pulling it out of the
  // component avoids a dependency-array entry on the useMemo below
  // (and the corresponding identity churn on every render).
  const initialRules = useMemo<Record<typeof tiers[number]["field"], Rule>>(
    () => ({
      low: normalizeApprovalRule(workspace.ship_hub_approval_low, "member"),
      medium: normalizeApprovalRule(workspace.ship_hub_approval_medium, "member"),
      high: normalizeApprovalRule(workspace.ship_hub_approval_high, "approver"),
      critical: normalizeApprovalRule(workspace.ship_hub_approval_critical, "two"),
    }),
    [
      workspace.ship_hub_approval_low,
      workspace.ship_hub_approval_medium,
      workspace.ship_hub_approval_high,
      workspace.ship_hub_approval_critical,
    ],
  );

  const initialAuthor = workspace.ship_hub_approver_can_be_author ?? true;

  const [rules, setRules] = useState(initialRules);
  const [allowAuthor, setAllowAuthor] = useState(initialAuthor);
  const [saving, setSaving] = useState(false);

  // Reconcile when the cached workspace updates (WS event, another tab).
  useEffect(() => {
    setRules(initialRules);
    setAllowAuthor(initialAuthor);
  }, [initialRules, initialAuthor]);

  const dirty =
    rules.low !== initialRules.low ||
    rules.medium !== initialRules.medium ||
    rules.high !== initialRules.high ||
    rules.critical !== initialRules.critical ||
    allowAuthor !== initialAuthor;

  const handleSave = async () => {
    if (!dirty || saving) return;
    setSaving(true);
    try {
      const payload: Parameters<typeof api.updateWorkspace>[1] = {};
      if (rules.low !== initialRules.low) {
        payload.ship_hub_approval_low = rules.low;
        payload.ship_hub_approval_low_set = true;
      }
      if (rules.medium !== initialRules.medium) {
        payload.ship_hub_approval_medium = rules.medium;
        payload.ship_hub_approval_medium_set = true;
      }
      if (rules.high !== initialRules.high) {
        payload.ship_hub_approval_high = rules.high;
        payload.ship_hub_approval_high_set = true;
      }
      if (rules.critical !== initialRules.critical) {
        payload.ship_hub_approval_critical = rules.critical;
        payload.ship_hub_approval_critical_set = true;
      }
      if (allowAuthor !== initialAuthor) {
        payload.ship_hub_approver_can_be_author = allowAuthor;
        payload.ship_hub_approver_can_be_author_set = true;
      }
      const updated = await api.updateWorkspace(workspace.id, payload);
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((w) => (w.id === updated.id ? updated : w)),
      );
      toast.success(t(($) => $.ship_hub.approval.toast_saved));
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.ship_hub.approval.toast_save_failed),
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-3 border-t border-border pt-3">
      <div className="text-sm font-medium">
        {t(($) => $.ship_hub.approval.section_title)}
      </div>
      <p className="text-xs text-muted-foreground">
        {t(($) => $.ship_hub.approval.section_description)}
      </p>

      {tiers.map(({ risk, field }) => (
        <div key={field} className="space-y-1">
          <Label className="text-xs text-muted-foreground" htmlFor={`approval-${field}`}>
            {t(($) => $.ship_hub.approval.tier_label[risk])}
          </Label>
          <Select
            value={rules[field]}
            onValueChange={(v) => {
              if (v === "member" || v === "admin" || v === "approver" || v === "two") {
                setRules((prev) => ({ ...prev, [field]: v }));
              }
            }}
            disabled={saving}
          >
            <SelectTrigger
              id={`approval-${field}`}
              data-testid={`approval-${field}-select`}
              className="w-full"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="member">
                {t(($) => $.ship_hub.approval.rule_options.member)}
              </SelectItem>
              <SelectItem value="admin">
                {t(($) => $.ship_hub.approval.rule_options.admin)}
              </SelectItem>
              <SelectItem value="approver">
                {t(($) => $.ship_hub.approval.rule_options.approver)}
              </SelectItem>
              <SelectItem value="two">
                {t(($) => $.ship_hub.approval.rule_options.two)}
              </SelectItem>
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">
            {t(($) => $.ship_hub.approval.tier_hint[risk])}
          </p>
        </div>
      ))}

      <label className="flex items-center gap-2 pt-2 text-sm">
        <input
          type="checkbox"
          checked={allowAuthor}
          onChange={(e) => setAllowAuthor(e.target.checked)}
          disabled={saving}
          data-testid="approval-allow-author-checkbox"
        />
        <span>{t(($) => $.ship_hub.approval.allow_author_label)}</span>
      </label>
      <p className="text-xs text-muted-foreground">
        {t(($) => $.ship_hub.approval.allow_author_hint)}
      </p>

      <div className="pt-1">
        <Button
          size="sm"
          onClick={handleSave}
          disabled={!dirty || saving}
          data-testid="approval-save-button"
        >
          <Save className="h-3 w-3" />
          {saving ? t(($) => $.ship_hub.approval.saving) : t(($) => $.ship_hub.approval.save)}
        </Button>
      </div>
    </div>
  );
}

interface DeployWorkflowSettingsProps {
  workspace: Workspace;
}

/**
 * DeployWorkflowSettings — workspace owner/admin controls for the
 * Phase 7d follow-up auto-detect-deploys feature. The user sets two
 * optional GitHub Actions workflow filenames (staging / production);
 * when set, the server polls every 2 minutes and auto-links matching
 * deploys by sha, removing the need to click Mark-deploy-as-landed.
 *
 * The on/off pill next to each input mirrors the smoke-workflow
 * status-pill pattern: presence of the field on the server response
 * (`ship_hub_deploy_workflow_*_set`) is the source of truth, NOT the
 * locally-typed input value (which can be midway through a save).
 *
 * Saves via paired-bool PATCH per field — same pattern as
 * ApprovalSettings — so a concurrent edit from another tab can't be
 * clobbered by an "echo back" of cached state.
 */
function DeployWorkflowSettings({ workspace }: DeployWorkflowSettingsProps) {
  const { t } = useT("settings");
  const qc = useQueryClient();

  // The actual filename strings aren't returned by the server (only
  // the *_set boolean), so we don't pre-populate the input when the
  // workspace already has one configured. Empty input + on-pill is
  // the legitimate "configured, leave alone" state; a non-empty input
  // is treated as "rename / overwrite". This mirrors the GitHub
  // token's write-only contract.
  const [staging, setStaging] = useState("");
  const [production, setProduction] = useState("");
  const [saving, setSaving] = useState(false);

  const stagingSet = workspace.ship_hub_deploy_workflow_staging_set === true;
  const productionSet = workspace.ship_hub_deploy_workflow_production_set === true;

  const handleSave = async () => {
    if (saving) return;
    const trimmedStaging = staging.trim();
    const trimmedProduction = production.trim();
    if (!trimmedStaging && !trimmedProduction) return;
    setSaving(true);
    try {
      const payload: Parameters<typeof api.updateWorkspace>[1] = {};
      if (trimmedStaging) {
        payload.ship_hub_deploy_workflow_staging = trimmedStaging;
        payload.ship_hub_deploy_workflow_staging_set = true;
      }
      if (trimmedProduction) {
        payload.ship_hub_deploy_workflow_production = trimmedProduction;
        payload.ship_hub_deploy_workflow_production_set = true;
      }
      const updated = await api.updateWorkspace(workspace.id, payload);
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((w) => (w.id === updated.id ? updated : w)),
      );
      setStaging("");
      setProduction("");
      toast.success(t(($) => $.ship_hub.deploy_workflow.toast_saved));
    } catch (e) {
      toast.error(
        e instanceof Error
          ? e.message
          : t(($) => $.ship_hub.deploy_workflow.toast_save_failed),
      );
    } finally {
      setSaving(false);
    }
  };

  const handleClear = async (field: "staging" | "production") => {
    if (saving) return;
    setSaving(true);
    try {
      const payload: Parameters<typeof api.updateWorkspace>[1] = {};
      if (field === "staging") {
        payload.ship_hub_deploy_workflow_staging = null;
        payload.ship_hub_deploy_workflow_staging_set = true;
      } else {
        payload.ship_hub_deploy_workflow_production = null;
        payload.ship_hub_deploy_workflow_production_set = true;
      }
      const updated = await api.updateWorkspace(workspace.id, payload);
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((w) => (w.id === updated.id ? updated : w)),
      );
      if (field === "staging") setStaging("");
      else setProduction("");
      toast.success(t(($) => $.ship_hub.deploy_workflow.toast_saved));
    } catch (e) {
      toast.error(
        e instanceof Error
          ? e.message
          : t(($) => $.ship_hub.deploy_workflow.toast_save_failed),
      );
    } finally {
      setSaving(false);
    }
  };

  const dirty = staging.trim().length > 0 || production.trim().length > 0;

  return (
    <div className="space-y-3 border-t border-border pt-3">
      <div className="text-sm font-medium">
        {t(($) => $.ship_hub.deploy_workflow.section_title)}
      </div>
      <p className="text-xs text-muted-foreground">
        {t(($) => $.ship_hub.deploy_workflow.section_description)}
      </p>

      {/* Staging field. */}
      <div className="space-y-1">
        <div className="flex items-center justify-between gap-2">
          <Label
            className="text-xs text-muted-foreground"
            htmlFor="deploy-workflow-staging"
          >
            {t(($) => $.ship_hub.deploy_workflow.staging_label)}
          </Label>
          <span
            className={
              stagingSet
                ? "rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:text-emerald-300"
                : "rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground"
            }
            data-testid="deploy-workflow-staging-pill"
          >
            {stagingSet
              ? t(($) => $.ship_hub.deploy_workflow.status_on)
              : t(($) => $.ship_hub.deploy_workflow.status_off)}
          </span>
        </div>
        <div className="flex items-center gap-2">
          <Input
            id="deploy-workflow-staging"
            data-testid="deploy-workflow-staging-input"
            value={staging}
            onChange={(e) => setStaging(e.target.value)}
            placeholder={t(($) => $.ship_hub.deploy_workflow.staging_placeholder)}
            disabled={saving}
          />
          {stagingSet && (
            <Button
              size="sm"
              variant="ghost"
              onClick={() => handleClear("staging")}
              disabled={saving}
              data-testid="deploy-workflow-staging-clear"
            >
              {t(($) => $.ship_hub.token_clear)}
            </Button>
          )}
        </div>
      </div>

      {/* Production field. */}
      <div className="space-y-1">
        <div className="flex items-center justify-between gap-2">
          <Label
            className="text-xs text-muted-foreground"
            htmlFor="deploy-workflow-production"
          >
            {t(($) => $.ship_hub.deploy_workflow.production_label)}
          </Label>
          <span
            className={
              productionSet
                ? "rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:text-emerald-300"
                : "rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground"
            }
            data-testid="deploy-workflow-production-pill"
          >
            {productionSet
              ? t(($) => $.ship_hub.deploy_workflow.status_on)
              : t(($) => $.ship_hub.deploy_workflow.status_off)}
          </span>
        </div>
        <div className="flex items-center gap-2">
          <Input
            id="deploy-workflow-production"
            data-testid="deploy-workflow-production-input"
            value={production}
            onChange={(e) => setProduction(e.target.value)}
            placeholder={t(($) => $.ship_hub.deploy_workflow.production_placeholder)}
            disabled={saving}
          />
          {productionSet && (
            <Button
              size="sm"
              variant="ghost"
              onClick={() => handleClear("production")}
              disabled={saving}
              data-testid="deploy-workflow-production-clear"
            >
              {t(($) => $.ship_hub.token_clear)}
            </Button>
          )}
        </div>
      </div>

      <p className="text-xs text-muted-foreground">
        {t(($) => $.ship_hub.deploy_workflow.help_text)}
      </p>

      <div className="pt-1">
        <Button
          size="sm"
          onClick={handleSave}
          disabled={!dirty || saving}
          data-testid="deploy-workflow-save-button"
        >
          <Save className="h-3 w-3" />
          {saving
            ? t(($) => $.ship_hub.deploy_workflow.saving)
            : t(($) => $.ship_hub.deploy_workflow.save)}
        </Button>
      </div>
    </div>
  );
}
