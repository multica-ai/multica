"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
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
import { useWorkspaceId } from "@multica/core/hooks";
import { teamAppImpactOptions, useDisconnectTeamApp } from "@multica/core/connected-apps";
import { useT } from "../../i18n";

/** Disconnects the workspace's team account after showing what stops. */
export function DisconnectTeamAppDialog({
  connectionId,
  slug,
  name,
  onClose,
}: {
  connectionId: string;
  slug: string;
  name: string;
  onClose: () => void;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const impact = useQuery(teamAppImpactOptions(wsId, slug));
  const disconnect = useDisconnectTeamApp();
  const data = impact.data;

  const confirm = () =>
    disconnect.mutateAsync(connectionId).then(
      () => {
        toast.success(t(($) => $.connected_apps.toast_disconnected));
        onClose();
      },
      () => toast.error(t(($) => $.connected_apps.toast_failed)),
    );

  return (
    <AlertDialog open onOpenChange={(open) => !open && !disconnect.isPending && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.connected_apps.disconnect_team.title, { app: name })}</AlertDialogTitle>
          <AlertDialogDescription>{t(($) => $.connected_apps.disconnect_team.description)}</AlertDialogDescription>
        </AlertDialogHeader>
        {data && (
          <div className="space-y-2 text-caption">
            <p className="font-medium">{t(($) => $.connected_apps.disconnect_team.agents)}</p>
            <p className="text-muted-foreground">
              {data.agents.length > 0
                ? data.agents.map((a) => a.name).join(", ")
                : t(($) => $.connected_apps.disconnect_team.no_agents)}
            </p>
            <ul className="space-y-0.5 text-muted-foreground">
              <li>{t(($) => $.connected_apps.disconnect_team.active_tasks, { count: data.active_tasks })}</li>
              <li>{t(($) => $.connected_apps.disconnect_team.active_autopilots, { count: data.active_autopilots })}</li>
              <li>{t(($) => $.connected_apps.disconnect_team.pending, { count: data.pending_requests })}</li>
            </ul>
          </div>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={disconnect.isPending}>
            {t(($) => $.connected_apps.disconnect_team.cancel)}
          </AlertDialogCancel>
          <AlertDialogAction variant="destructive" disabled={disconnect.isPending} onClick={confirm}>
            {t(($) => $.connected_apps.disconnect_team.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
