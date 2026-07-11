"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { LogOut } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Button } from "@multica/ui/components/ui/button";
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
import {
  memberListOptions,
  workspaceKeys,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
import { issueKeys } from "@multica/core/issues/queries";
import { api } from "@multica/core/api";
import {
  resolvePostAuthDestination,
  useCurrentWorkspace,
  useHasOnboarded,
} from "@multica/core/paths";
import { setCurrentWorkspace } from "@multica/core/platform";
import type { Workspace } from "@multica/core/types";
import { AvatarUploadControl } from "../../common/avatar-upload-control";
import { useNavigation } from "../../navigation";
import { DeleteWorkspaceDialog } from "./delete-workspace-dialog";
import { useT } from "../../i18n";
import {
  SettingsCard,
  SettingsRow,
  SettingsSaveState,
  SettingsSection,
  SettingsTab,
  type SettingsSaveStatus,
} from "./settings-layout";
import { useAutoSave } from "./use-auto-save";

interface WorkspaceDetailsDraft {
  name: string;
  description: string;
  context: string;
}

function workspaceDetailsEqual(
  left: WorkspaceDetailsDraft,
  right: WorkspaceDetailsDraft,
) {
  return (
    left.name === right.name &&
    left.description === right.description &&
    left.context === right.context
  );
}

export function WorkspaceTab() {
  const { t } = useT("settings");
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  // Derive the id from useCurrentWorkspace instead of the throwing
  // useWorkspaceId: this component can legitimately render while the
  // workspace is gone from the list cache but the URL slug hasn't changed
  // yet (post-delete invalidation before navigation completes, or an
  // external delete of the workspace we're on). The `!workspace` guard
  // below renders null for that window; a throwing hook would crash first.
  const wsId = workspace?.id;
  const { data: members = [], isFetched: membersFetched } = useQuery({
    ...memberListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const qc = useQueryClient();
  const leaveWorkspace = useLeaveWorkspace();
  const deleteWorkspace = useDeleteWorkspace();
  const navigation = useNavigation();
  const hasOnboarded = useHasOnboarded();

  /**
   * Send the user to a safe URL, computed from the current cached workspace
   * list minus the workspace that's going away.
   *
   * Call ordering differs per flow:
   *   - Delete calls this AFTER the mutation succeeds. The realtime
   *     `workspace:deleted` handler skips self-initiated deletes (see
   *     pending-delete.ts), so nothing races this navigation.
   *   - Leave still calls this BEFORE the mutation fires: `member:removed`
   *     has no self-initiated marker yet, so if the user were still on the
   *     workspace's URL when that event arrives, the realtime handler in
   *     `use-realtime-sync.ts` would trigger a parallel full-page relocate
   *     that races the mutation's `invalidateQueries` refetch — the loser's
   *     in-flight fetch gets cancelled, surfacing as an unhandled
   *     `CancelledError`. Navigating first makes the handler's
   *     "current === lost workspace" check fail and its relocate no-op.
   *     Known debt: give leave the same await-then-navigate shape as delete.
   */
  const navigateAwayFromCurrentWorkspace = () => {
    const cachedList =
      qc.getQueryData<Workspace[]>(workspaceListOptions().queryKey) ?? [];
    const remaining = cachedList.filter((w) => w.id !== workspace?.id);
    // Clear the workspace-context singleton BEFORE navigating. Three
    // downstream consumers read it:
    //  1. Realtime relocate handlers' "current === lost workspace" check
    //     (`member:removed` for leave; also a second line of defense for
    //     delete) — if the singleton still points at the lost workspace
    //     when the WS event arrives, they fire a parallel full-page
    //     relocate that races this navigation.
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
  const [issuePrefix, setIssuePrefix] = useState(workspace?.issue_prefix ?? "");
  const [prefixSaveStatus, setPrefixSaveStatus] =
    useState<SettingsSaveStatus>("idle");
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

  // Reset form state only when the user switches to a different workspace.
  // Keying on workspace?.id (not the object ref) avoids wiping unsaved edits
  // when an unrelated mutation — e.g. avatar/logo upload — replaces the
  // cached Workspace object via setQueryData.
  useEffect(() => {
    setName(workspace?.name ?? "");
    setDescription(workspace?.description ?? "");
    setContext(workspace?.context ?? "");
    setIssuePrefix(workspace?.issue_prefix ?? "");
    // eslint-disable-next-line react-hooks/exhaustive-deps -- intentionally keyed on id only; see comment above
  }, [workspace?.id]);

  // Letters + digits only, uppercase, capped at 10 chars. The backend
  // uppercases and trims on its side too — this is purely a UX guardrail
  // so the value the user sees in the input matches what gets persisted.
  const normalizePrefix = (raw: string) =>
    raw.toUpperCase().replace(/[^A-Z0-9]/g, "").slice(0, 10);

  const normalizedPrefix = normalizePrefix(issuePrefix);
  const prefixChanged =
    !!workspace && normalizedPrefix !== workspace.issue_prefix;
  const prefixInvalid = normalizedPrefix.length === 0;

  const detailsDraft = useMemo(
    () => ({ name, description, context }),
    [context, description, name],
  );
  const savedDetails = useMemo(
    () => ({
      name: workspace?.name ?? "",
      description: workspace?.description ?? "",
      context: workspace?.context ?? "",
    }),
    [workspace?.context, workspace?.description, workspace?.name],
  );
  const saveDetails = useCallback(
    async (next: WorkspaceDetailsDraft) => {
      if (!workspace) return;
      const updated = await api.updateWorkspace(workspace.id, next);
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
    },
    [qc, workspace],
  );
  const detailsAutoSave = useAutoSave({
    value: detailsDraft,
    savedValue: savedDetails,
    onSave: saveDetails,
    onError: (error) =>
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.workspace.toast_save_failed),
      ),
    enabled: !!workspace && canManageWorkspace && !!name.trim(),
    isEqual: workspaceDetailsEqual,
  });

  const performPrefixSave = async (nextPrefix: string) => {
    if (!workspace) return;
    setPrefixSaveStatus("saving");
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        issue_prefix: nextPrefix,
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      // Issue identifiers are computed from the workspace prefix at read time,
      // so every cached issue key is stale after this confirmed change.
      await qc.invalidateQueries({ queryKey: issueKeys.all(updated.id) });
      setPrefixSaveStatus("saved");
    } catch (error) {
      setPrefixSaveStatus("error");
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.workspace.toast_save_failed),
      );
    }
  };

  const handlePrefixBlur = () => {
    if (!workspace || prefixInvalid || !prefixChanged) return;
    const nextPrefix = normalizedPrefix;
    setConfirmAction({
      title: t(($) => $.workspace.prefix_confirm_title),
      description: t(($) => $.workspace.prefix_confirm_description, {
        oldPrefix: workspace.issue_prefix,
        newPrefix: nextPrefix,
      }),
      variant: "destructive",
      onConfirm: () => performPrefixSave(nextPrefix),
    });
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
    // Await the DELETE with the dialog in its loading state, and only
    // navigate on success (CLAUDE.md: flows that navigate must await the
    // server; no optimistic removal). The realtime `workspace:deleted`
    // handler skips self-initiated deletes via the pending-delete registry,
    // so it can't race this navigation with its own full-page relocate.
    // On failure the dialog stays open, the cache was never touched, and
    // the user is exactly where they started.
    try {
      await deleteWorkspace.mutateAsync(workspace.id);
      setDeleteDialogOpen(false);
      navigateAwayFromCurrentWorkspace();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.workspace.toast_delete_failed));
    } finally {
      setActionId(null);
    }
  };

  if (!workspace) return null;

  return (
    <SettingsTab title={t(($) => $.page.tabs.general)}>
      <SettingsSection
        title={t(($) => $.workspace.section_general)}
        action={
          <SettingsSaveState
            status={
              prefixSaveStatus === "saving" || prefixSaveStatus === "error"
                ? prefixSaveStatus
                : detailsAutoSave.status === "idle"
                  ? prefixSaveStatus
                  : detailsAutoSave.status
            }
            savingLabel={t(($) => $.auto_save.saving)}
            savedLabel={t(($) => $.auto_save.saved)}
            errorLabel={t(($) => $.auto_save.failed)}
          />
        }
      >
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.workspace.logo_label)}
            description={t(($) => $.workspace.click_logo_hint)}
            controlClassName="sm:max-w-none"
          >
            <div className="flex justify-start sm:justify-end">
              <AvatarUploadControl
                variant="workspace"
                value={workspace.avatar_url ?? null}
                name={workspace.name}
                size={64}
                disabled={!canManageWorkspace}
                ariaLabel={t(($) => $.workspace.change_logo_aria)}
                onUploaded={async (url) => {
                  const updated = await api.updateWorkspace(workspace.id, {
                    avatar_url: url,
                  });
                  qc.setQueryData(
                    workspaceKeys.list(),
                    (old: Workspace[] | undefined) =>
                      old?.map((ws) => (ws.id === updated.id ? updated : ws)),
                  );
                }}
              />
            </div>
          </SettingsRow>

          <SettingsRow
            label={t(($) => $.workspace.name_label)}
            controlClassName="sm:w-80"
          >
            <Input
              type="text"
              name="workspace-name"
              autoComplete="organization"
              aria-label={t(($) => $.workspace.name_label)}
              value={name}
              onChange={(event) => setName(event.target.value)}
              onBlur={detailsAutoSave.flush}
              disabled={!canManageWorkspace}
            />
          </SettingsRow>

          <SettingsRow
            label={t(($) => $.workspace.description_label)}
            controlClassName="sm:w-96"
            align="start"
          >
            <Textarea
              name="workspace-description"
              autoComplete="off"
              aria-label={t(($) => $.workspace.description_label)}
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              onBlur={detailsAutoSave.flush}
              rows={3}
              disabled={!canManageWorkspace}
              className="resize-none"
              placeholder={t(($) => $.workspace.description_placeholder)}
            />
          </SettingsRow>

          <SettingsRow
            label={t(($) => $.workspace.context_label)}
            controlClassName="sm:w-96"
            align="start"
          >
            <Textarea
              name="workspace-context"
              autoComplete="off"
              aria-label={t(($) => $.workspace.context_label)}
              value={context}
              onChange={(event) => setContext(event.target.value)}
              onBlur={detailsAutoSave.flush}
              rows={4}
              disabled={!canManageWorkspace}
              className="resize-none"
              placeholder={t(($) => $.workspace.context_placeholder)}
            />
          </SettingsRow>

          <SettingsRow
            label={t(($) => $.workspace.slug_label)}
            controlClassName="sm:w-80"
          >
              <div className="rounded-lg border border-input bg-muted/50 px-2.5 py-1.5 font-mono text-xs text-muted-foreground">
                {workspace.slug}
              </div>
          </SettingsRow>

          <SettingsRow
            label={t(($) => $.workspace.issue_prefix_label)}
            description={t(($) => $.workspace.issue_prefix_hint, {
              example: `${normalizedPrefix || workspace.issue_prefix}-123`,
            })}
            controlClassName="sm:w-40"
          >
              <Input
                type="text"
                name="workspace-issue-prefix"
                autoComplete="off"
                autoCapitalize="characters"
                spellCheck={false}
                aria-label={t(($) => $.workspace.issue_prefix_label)}
                value={issuePrefix}
                onChange={(event) => {
                  setPrefixSaveStatus("idle");
                  setIssuePrefix(normalizePrefix(event.target.value));
                }}
                onBlur={handlePrefixBlur}
                disabled={!canManageWorkspace}
                maxLength={10}
                aria-invalid={prefixInvalid}
                className="font-mono uppercase"
                placeholder={workspace.issue_prefix}
              />
          </SettingsRow>

            {!canManageWorkspace && (
              <div className="px-4 py-3 text-xs text-muted-foreground">
                {t(($) => $.workspace.manage_hint)}
              </div>
            )}
        </SettingsCard>
      </SettingsSection>

      {/* Danger Zone — gated on the member query settling so the owner-only
          Delete button and the sole-owner Leave guidance don't flash in
          after mount. */}
      {membersFetched && (
        <SettingsSection
          title={
            <span className="inline-flex items-center gap-2">
              <LogOut className="h-4 w-4 text-muted-foreground" />
              {t(($) => $.workspace.danger_zone)}
            </span>
          }
        >
          <SettingsCard>
            <SettingsRow
              label={t(($) => $.workspace.leave_title)}
              description={
                isSoleOwner
                  ? isSoleMember
                    ? t(($) => $.workspace.leave_sole_member)
                    : t(($) => $.workspace.leave_sole_owner)
                  : t(($) => $.workspace.leave_default)
              }
            >
              <Button
                variant="outline"
                size="sm"
                onClick={handleLeaveWorkspace}
                disabled={actionId === "leave" || isSoleOwner}
              >
                {actionId === "leave" ? t(($) => $.workspace.leaving) : t(($) => $.workspace.leave_button)}
              </Button>
            </SettingsRow>

            {isOwner && (
              <SettingsRow
                label={
                  <span className="text-destructive">
                    {t(($) => $.workspace.delete_title)}
                  </span>
                }
                description={t(($) => $.workspace.delete_description)}
              >
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => setDeleteDialogOpen(true)}
                  disabled={actionId === "delete-workspace"}
                >
                  {actionId === "delete-workspace" ? t(($) => $.workspace.deleting) : t(($) => $.workspace.delete_button)}
                </Button>
              </SettingsRow>
            )}
          </SettingsCard>
        </SettingsSection>
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
    </SettingsTab>
  );
}
