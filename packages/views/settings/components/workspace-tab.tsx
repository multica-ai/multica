"use client";

import { useEffect, useState } from "react";
import { Save, LogOut, MessageCircle } from "lucide-react";
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
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useLeaveWorkspace, useDeleteWorkspace } from "@multica/core/workspace/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  memberListOptions,
  workspaceKeys,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
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

export function WorkspaceTab() {
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
  }, [workspace]);

  const handleSave = async () => {
    if (!workspace) return;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        name,
        description,
        context,
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success("Workspace settings saved");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to save workspace settings");
    } finally {
      setSaving(false);
    }
  };

  const handleLeaveWorkspace = () => {
    if (!workspace) return;
    setConfirmAction({
      title: "Leave workspace",
      description: `Leave ${workspace.name}? You will lose access until re-invited.`,
      variant: "destructive",
      onConfirm: async () => {
        setActionId("leave");
        // Navigate away FIRST so the realtime handler's
        // "current-workspace-deleted" branch doesn't race the mutation.
        navigateAwayFromCurrentWorkspace();
        try {
          await leaveWorkspace.mutateAsync(workspace.id);
        } catch (e) {
          toast.error(e instanceof Error ? e.message : "Failed to leave workspace");
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
      toast.error(e instanceof Error ? e.message : "Failed to delete workspace");
    } finally {
      setActionId(null);
    }
  };

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      {/* Workspace settings */}
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">General</h2>

        <Card>
          <CardContent className="space-y-3">
            <div>
              <Label className="text-xs text-muted-foreground">Name</Label>
              <Input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={!canManageWorkspace}
                className="mt-1"
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Description</Label>
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={3}
                disabled={!canManageWorkspace}
                className="mt-1 resize-none"
                placeholder="What does this workspace focus on?"
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Context</Label>
              <Textarea
                value={context}
                onChange={(e) => setContext(e.target.value)}
                rows={4}
                disabled={!canManageWorkspace}
                className="mt-1 resize-none"
                placeholder="Background information and context for AI agents working in this workspace"
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Slug</Label>
              <div className="mt-1 rounded-md border bg-muted/50 px-3 py-2 text-sm text-muted-foreground">
                {workspace.slug}
              </div>
            </div>
            <div className="flex items-center justify-end gap-2 pt-1">
              <Button
                size="sm"
                onClick={handleSave}
                disabled={saving || !name.trim() || !canManageWorkspace}
              >
                <Save className="h-3 w-3" />
                {saving ? "Saving..." : "Save"}
              </Button>
            </div>
            {!canManageWorkspace && (
              <p className="text-xs text-muted-foreground">
                Only admins and owners can update workspace settings.
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
            <h2 className="text-sm font-semibold">Channels</h2>
          </div>
          <Card>
            <CardContent className="space-y-3">
              <ChannelsSettings workspace={workspace} />
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
          <h2 className="text-sm font-semibold">Danger Zone</h2>
        </div>

        <Card>
          <CardContent className="space-y-3">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <p className="text-sm font-medium">Leave workspace</p>
                <p className="text-xs text-muted-foreground">
                  {isSoleOwner
                    ? isSoleMember
                      ? "You're the only member. Delete the workspace to leave."
                      : "You're the only owner. Promote another member to owner first, or delete the workspace."
                    : "Remove yourself from this workspace."}
                </p>
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={handleLeaveWorkspace}
                disabled={actionId === "leave" || isSoleOwner}
              >
                {actionId === "leave" ? "Leaving..." : "Leave workspace"}
              </Button>
            </div>

            {isOwner && (
              <div className="flex flex-col gap-2 border-t pt-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <p className="text-sm font-medium text-destructive">Delete workspace</p>
                  <p className="text-xs text-muted-foreground">
                    Permanently delete this workspace and its data.
                  </p>
                </div>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => setDeleteDialogOpen(true)}
                  disabled={actionId === "delete-workspace"}
                >
                  {actionId === "delete-workspace" ? "Deleting..." : "Delete workspace"}
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
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant={confirmAction?.variant === "destructive" ? "destructive" : "default"}
              onClick={async () => {
                await confirmAction?.onConfirm();
                setConfirmAction(null);
              }}
            >
              Confirm
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
      toast.success(next ? "Channels enabled" : "Channels disabled");
    } catch (e) {
      setEnabled(!next);
      toast.error(e instanceof Error ? e.message : "Failed to update channels setting");
    } finally {
      setPending(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-sm font-medium">Enable Channels</p>
          <p className="text-xs text-muted-foreground">
            Multi-participant chat alongside the issue board, with public
            channels, private channels, and direct messages. When off, the
            sidebar entry hides and Channels endpoints return 404.
          </p>
        </div>
        <Switch
          checked={enabled}
          onCheckedChange={handleToggle}
          disabled={pending}
          aria-label="Enable Channels"
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
      toast.success("Retention setting saved");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to save retention setting");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-2 border-t border-border pt-3">
      <div className="text-sm font-medium">Message retention</div>
      <p className="text-xs text-muted-foreground">
        Messages older than this are hidden from view. Default: 90 days.
        Per-channel overrides take precedence.
      </p>
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={forever}
          onChange={(e) => setForever(e.target.checked)}
          disabled={saving}
        />
        Retain messages forever
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
          <span className="text-xs text-muted-foreground">days</span>
        </div>
      )}
      {!daysValid && (
        <p className="text-xs text-destructive" role="alert">
          Retention must be between 1 and 3650 days.
        </p>
      )}
      {transitioningToFinite && (
        <p className="text-xs text-muted-foreground">
          Heads up: existing messages older than {Number.isFinite(days) ? days : 0} days will be hidden after the next cleanup run.
        </p>
      )}
      <div>
        <Button size="sm" onClick={handleSave} disabled={!dirty || !daysValid || saving}>
          <Save className="h-3 w-3" />
          {saving ? "Saving…" : "Save"}
        </Button>
      </div>
    </div>
  );
}
