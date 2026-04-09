"use client";

import { useEffect, useState } from "react";
import { Save, LogOut } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
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
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@/platform/auth";
import { useWorkspaceStore } from "@/platform/workspace";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { api } from "@/platform/api";
import { useDashboardLocale } from "@/features/dashboard/i18n";

export function WorkspaceTab() {
  const user = useAuthStore((s) => s.user);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const updateWorkspace = useWorkspaceStore((s) => s.updateWorkspace);
  const leaveWorkspace = useWorkspaceStore((s) => s.leaveWorkspace);
  const deleteWorkspace = useWorkspaceStore((s) => s.deleteWorkspace);
  const { t } = useDashboardLocale();

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

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManageWorkspace = currentMember?.role === "owner" || currentMember?.role === "admin";
  const isOwner = currentMember?.role === "owner";

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
      updateWorkspace(updated);
      toast.success(t.workspace.workspaceSaved);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t.workspace.failedSave);
    } finally {
      setSaving(false);
    }
  };

  const handleLeaveWorkspace = () => {
    if (!workspace) return;
    setConfirmAction({
      title: t.workspace.leaveWorkspace,
      description: t.workspace.leaveDescription,
      variant: "destructive",
      onConfirm: async () => {
        setActionId("leave");
        try {
          await leaveWorkspace(workspace.id);
        } catch (e) {
          toast.error(e instanceof Error ? e.message : t.workspace.failedLeave);
        } finally {
          setActionId(null);
        }
      },
    });
  };

  const handleDeleteWorkspace = () => {
    if (!workspace) return;
    setConfirmAction({
      title: t.workspace.deleteWorkspace,
      description: t.workspace.deleteDescription,
      variant: "destructive",
      onConfirm: async () => {
        setActionId("delete-workspace");
        try {
          await deleteWorkspace(workspace.id);
        } catch (e) {
          toast.error(e instanceof Error ? e.message : t.workspace.failedDelete);
        } finally {
          setActionId(null);
        }
      },
    });
  };

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      {/* Workspace settings */}
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t.settings.general}</h2>

        <Card>
          <CardContent className="space-y-3">
            <div>
              <Label className="text-xs text-muted-foreground">{t.workspace.workspaceName}</Label>
              <Input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={!canManageWorkspace}
                className="mt-1"
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">{t.workspace.description}</Label>
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={3}
                disabled={!canManageWorkspace}
                className="mt-1 resize-none"
                placeholder={t.workspace.description}
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">{t.workspace.aiContext}</Label>
              <Textarea
                value={context}
                onChange={(e) => setContext(e.target.value)}
                rows={4}
                disabled={!canManageWorkspace}
                className="mt-1 resize-none"
                placeholder={t.workspace.aiContextPlaceholder}
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
                {saving ? t.workspace.saving : t.workspace.save}
              </Button>
            </div>
          </CardContent>
        </Card>
      </section>

      {/* Danger Zone */}
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <LogOut className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">{t.workspace.dangerZone}</h2>
        </div>

        <Card>
          <CardContent className="space-y-3">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <p className="text-sm font-medium">{t.workspace.leaveWorkspace}</p>
                <p className="text-xs text-muted-foreground">
                  {t.workspace.leaveDescription}
                </p>
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={handleLeaveWorkspace}
                disabled={actionId === "leave"}
              >
                {actionId === "leave" ? t.workspace.leaving : t.workspace.leave}
              </Button>
            </div>

            {isOwner && (
              <div className="flex flex-col gap-2 border-t pt-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <p className="text-sm font-medium text-destructive">{t.workspace.deleteWorkspace}</p>
                  <p className="text-xs text-muted-foreground">
                    {t.workspace.deleteDescription}
                  </p>
                </div>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={handleDeleteWorkspace}
                  disabled={actionId === "delete-workspace"}
                >
                  {actionId === "delete-workspace" ? t.workspace.deleting : t.workspace.delete}
                </Button>
              </div>
            )}
          </CardContent>
        </Card>
      </section>

      <AlertDialog open={!!confirmAction} onOpenChange={(v) => { if (!v) setConfirmAction(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirmAction?.title}</AlertDialogTitle>
            <AlertDialogDescription>{confirmAction?.description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t.workspace.cancel}</AlertDialogCancel>
            <AlertDialogAction
              variant={confirmAction?.variant === "destructive" ? "destructive" : "default"}
              onClick={async () => {
                await confirmAction?.onConfirm();
                setConfirmAction(null);
              }}
            >
              {t.workspace.confirm}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
