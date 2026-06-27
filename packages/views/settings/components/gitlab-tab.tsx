"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { gitlabConnectionsOptions, useDeleteGitLabConnection } from "@multica/core/gitlab";
import { useT } from "../../i18n";
import { GitLabMark } from "./gitlab-mark";

export function GitLabTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const [connecting, setConnecting] = useState(false);
  const [disconnectTarget, setDisconnectTarget] = useState<string | null>(null);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canView = !!currentMember;

  const { data: connectionData } = useQuery({
    ...gitlabConnectionsOptions(wsId),
    enabled: !!wsId && canView,
  });
  const connections = connectionData?.connections ?? [];
  const configured = connectionData?.configured ?? false;
  const canManage = connectionData?.can_manage === true;

  const deleteMutation = useDeleteGitLabConnection(wsId);

  function handleConnect() {
    setConnecting(true);
    window.location.href = `/api/workspaces/${wsId}/gitlab/connect`;
  }

  async function handleDisconnect(connectionId: string) {
    try {
      await deleteMutation.mutateAsync(connectionId);
      toast.success("GitLab disconnected");
    } catch {
      toast.error("Failed to disconnect GitLab");
    } finally {
      setDisconnectTarget(null);
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start gap-3">
        <GitLabMark className="h-8 w-8 shrink-0" />
        <div>
          <h3 className="font-medium">{t(($) => $.gitlab.title)}</h3>
          <p className="text-sm text-muted-foreground">{t(($) => $.gitlab.description)}</p>
        </div>
      </div>

      {!configured && (
        <p className="text-sm text-muted-foreground">{t(($) => $.gitlab.not_configured)}</p>
      )}

      {configured && connections.length === 0 && canManage && (
        <Button onClick={handleConnect} disabled={connecting} variant="outline">
          {t(($) => $.gitlab.connect)}
        </Button>
      )}

      {connections.map((conn) => (
        <div key={conn.id} className="flex items-center justify-between rounded-md border p-3">
          <div className="flex items-center gap-2">
            {conn.avatar_url && (
              <img src={conn.avatar_url} alt="" className="h-6 w-6 rounded-full" />
            )}
            <span className="text-sm">
              {t(($) => $.gitlab.connected_as, { namespace: conn.namespace })}
            </span>
          </div>
          {canManage && (
            <Button
              variant="ghost"
              size="sm"
              className="text-destructive hover:text-destructive"
              onClick={() => setDisconnectTarget(conn.id)}
            >
              {t(($) => $.gitlab.disconnect)}
            </Button>
          )}
        </div>
      ))}

      <AlertDialog open={!!disconnectTarget} onOpenChange={() => setDisconnectTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.gitlab.disconnect_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.gitlab.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.gitlab.disconnect_confirm_cancel)}</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive hover:bg-destructive/90"
              onClick={() => disconnectTarget && handleDisconnect(disconnectTarget)}
            >
              {t(($) => $.gitlab.disconnect_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
