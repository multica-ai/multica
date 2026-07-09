"use client";

import { useEffect, useRef, useState } from "react";
import { Camera, Loader2, Save, LogOut } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { cn } from "@multica/ui/lib/utils";
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
import { api } from "@multica/core/api";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import {
  paths,
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
  const [slug, setSlug] = useState(workspace?.slug ?? "");
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

  const normalizeSlug = (raw: string) => raw.toLowerCase().replace(/[^a-z0-9-]/g, "");
  const normalizedSlug = normalizeSlug(slug);
  const slugChanged = !!workspace && normalizedSlug !== workspace.slug;
  const slugInvalid = !/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(normalizedSlug);

  // Reset form state only when the user switches to a different workspace.
  // Keying on workspace?.id (not the object ref) avoids wiping unsaved edits
  // when an unrelated mutation — e.g. avatar/logo upload — replaces the
  // cached Workspace object via setQueryData.
  useEffect(() => {
    setName(workspace?.name ?? "");
    setSlug(workspace?.slug ?? "");
    setDescription(workspace?.description ?? "");
    setContext(workspace?.context ?? "");
    // eslint-disable-next-line react-hooks/exhaustive-deps -- intentionally keyed on id only; see comment above
  }, [workspace?.id]);

  const performSave = async () => {
    if (!workspace) return;
    const previousSlug = workspace.slug;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        name,
        slug: normalizedSlug,
        description,
        context,
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      if (updated.slug !== previousSlug) {
        setCurrentWorkspace(updated.slug, updated.id);
        navigation.replace(paths.workspace(updated.slug).settings());
      }
      toast.success(t(($) => $.workspace.toast_saved));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.workspace.toast_save_failed));
    } finally {
      setSaving(false);
    }
  };

  const handleSave = () => {
    if (!workspace || slugInvalid) return;
    if (slugChanged) {
      setConfirmAction({
        title: t(($) => $.workspace.slug_confirm_title),
        description: t(($) => $.workspace.slug_confirm_description, {
          oldSlug: workspace.slug,
          newSlug: normalizedSlug,
        }),
        variant: "destructive",
        onConfirm: () => performSave(),
      });
      return;
    }
    void performSave();
  };

  const fileInputRef = useRef<HTMLInputElement>(null);
  const { upload, uploading } = useFileUpload(api);

  const handleLogoUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    if (!workspace) return;
    const file = e.target.files?.[0];
    if (!file) return;
    // Reset input so the same file can be re-selected
    e.target.value = "";
    try {
      const result = await upload(file);
      if (!result) return;
      const updated = await api.updateWorkspace(workspace.id, { avatar_url: result.link });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success(t(($) => $.workspace.toast_logo_updated));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.workspace.toast_logo_failed));
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

  const logoUrl = resolvePublicFileUrl(workspace.avatar_url);

  return (
    <div className="space-y-8">
      {/* Workspace settings */}
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.workspace.section_general)}</h2>

        <Card>
          <CardContent className="space-y-3">
            <div className="flex items-center gap-4">
              <button
                type="button"
                className="group relative h-16 w-16 shrink-0 overflow-hidden rounded-md bg-muted focus:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
                onClick={() => fileInputRef.current?.click()}
                disabled={uploading || !canManageWorkspace}
                aria-label={t(($) => $.workspace.change_logo_aria)}
              >
                {logoUrl ? (
                  <img
                    src={logoUrl}
                    alt={workspace.name}
                    className="h-full w-full object-cover"
                  />
                ) : (
                  <span className="flex h-full w-full items-center justify-center text-lg font-semibold text-muted-foreground">
                    {workspace.name.charAt(0).toUpperCase()}
                  </span>
                )}
                {canManageWorkspace && (
                  <div className="absolute inset-0 flex items-center justify-center bg-black/40 opacity-0 transition-opacity group-hover:opacity-100">
                    {uploading ? (
                      <Loader2 className="h-5 w-5 animate-spin text-white" />
                    ) : (
                      <Camera className="h-5 w-5 text-white" />
                    )}
                  </div>
                )}
              </button>
              <input
                ref={fileInputRef}
                type="file"
                accept="image/png,image/jpeg,image/webp"
                className="hidden"
                onChange={handleLogoUpload}
              />
              <div className="text-xs text-muted-foreground">
                {t(($) => $.workspace.click_logo_hint)}
              </div>
            </div>
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
              <Input
                type="text"
                value={slug}
                onChange={(e) => setSlug(normalizeSlug(e.target.value))}
                disabled={!canManageWorkspace}
                className={cn("mt-1 font-mono", slugChanged && slugInvalid && "border-destructive")}
                placeholder={workspace.slug}
              />
              <p className="mt-1 text-xs text-muted-foreground">
                {t(($) => $.workspace.slug_hint)}
              </p>
            </div>
            <div className="flex items-center justify-end gap-2 pt-1">
              <Button
                size="sm"
                onClick={handleSave}
                disabled={saving || !name.trim() || !canManageWorkspace || slugInvalid}
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
