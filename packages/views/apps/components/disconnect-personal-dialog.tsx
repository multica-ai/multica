"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Loader2 } from "@multica/ui/icons";
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
import { personalAppImpactOptions, useDisconnectPersonalApp } from "@multica/core/connected-apps";
import { useT } from "../../i18n";

/**
 * Disconnects one of the viewer's own accounts. The account is the same in
 * every workspace they belong to, so the dialog lists the agents in all of
 * them that will ask again, and the runs that stop now.
 */
export function DisconnectPersonalAppDialog({
  connectionId,
  name,
  onClose,
}: {
  connectionId: string;
  name: string;
  onClose: () => void;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const impact = useQuery(personalAppImpactOptions(wsId, connectionId));
  const disconnect = useDisconnectPersonalApp();
  const agents = impact.data?.agents ?? [];

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
      <AlertDialogContent className="sm:max-w-lg">
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.connected_apps.disconnect_personal.title, { app: name })}</AlertDialogTitle>
          <AlertDialogDescription>{t(($) => $.connected_apps.disconnect_personal.description)}</AlertDialogDescription>
        </AlertDialogHeader>
        {impact.isLoading ? (
          <Loader2 className="size-4 animate-spin text-muted-foreground" />
        ) : impact.isError ? (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.connected_apps.disconnect_personal.impact_failed)}
          </p>
        ) : agents.length === 0 ? (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.connected_apps.disconnect_personal.no_agents)}
          </p>
        ) : (
          <div className="space-y-2">
            <p className="text-body font-medium">{t(($) => $.connected_apps.disconnect_personal.agents)}</p>
            <ul className="divide-y rounded-lg border">
              {agents.map((agent) => (
                <li key={`${agent.workspace_id}:${agent.id}`} className="flex items-center gap-2 px-3 py-2 text-body">
                  <span className="min-w-0 truncate font-medium">{agent.name}</span>
                  <span className="min-w-0 flex-1 truncate text-caption text-muted-foreground">{agent.workspace_name}</span>
                  <span className={agent.running ? "shrink-0 text-caption text-warning" : "shrink-0 text-caption text-muted-foreground"}>
                    {agent.running
                      ? t(($) => $.connected_apps.disconnect_personal.running)
                      : t(($) => $.connected_apps.disconnect_personal.uses_it)}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={disconnect.isPending}>
            {t(($) => $.connected_apps.disconnect_personal.cancel)}
          </AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={disconnect.isPending}
            aria-busy={disconnect.isPending}
            onClick={confirm}
          >
            {t(($) => $.connected_apps.disconnect_personal.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
