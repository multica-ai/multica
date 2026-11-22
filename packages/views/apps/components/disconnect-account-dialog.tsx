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
import {
  personalAppImpactOptions,
  teamAppImpactOptions,
  useDisconnectPersonalApp,
  useDisconnectTeamApp,
} from "@multica/core/connected-apps";
import type { AppAccountScope } from "@multica/core/types";
import { ComposioToolkitLogo } from "../../common/composio-toolkit-logo";
import { useT } from "../../i18n";
import { AccountOwner } from "./account-row";

interface ImpactAgent {
  id: string;
  name: string;
  running?: boolean;
}

/**
 * Disconnecting an account asks the same question for both owners: these
 * agents act as it, and their next step asks again — the admins for a team
 * account, you for yours.
 */
export function DisconnectAccountDialog({
  scope,
  connectionId,
  slug,
  name,
  logo,
  onClose,
}: {
  scope: AppAccountScope;
  connectionId: string;
  slug: string;
  name: string;
  logo?: string;
  onClose: () => void;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const team = scope === "team";
  const teamImpact = useQuery({ ...teamAppImpactOptions(wsId, slug), enabled: team });
  const personalImpact = useQuery({ ...personalAppImpactOptions(wsId, connectionId), enabled: !team });
  const disconnectTeam = useDisconnectTeamApp();
  const disconnectPersonal = useDisconnectPersonalApp();
  const impact = team ? teamImpact : personalImpact;
  const pending = team ? disconnectTeam.isPending : disconnectPersonal.isPending;
  const agents: ImpactAgent[] = team ? (teamImpact.data?.agents ?? []) : (personalImpact.data?.agents ?? []);
  const activeRuns = team ? (teamImpact.data?.active_tasks ?? 0) : 0;

  const confirm = () =>
    (team ? disconnectTeam : disconnectPersonal).mutateAsync(connectionId).then(
      () => {
        toast.success(t(($) => $.connected_apps.toast_disconnected));
        onClose();
      },
      () => toast.error(t(($) => $.connected_apps.toast_failed)),
    );

  return (
    <AlertDialog open onOpenChange={(open) => !open && !pending && onClose()}>
      <AlertDialogContent className="sm:max-w-md">
        <AlertDialogHeader>
          <div className="flex items-center gap-3">
            <ComposioToolkitLogo slug={slug} name={name} fallbackLogo={logo} className="size-8 shrink-0" />
            <div className="min-w-0 space-y-0.5 text-left">
              <AlertDialogTitle>{t(($) => $.connected_apps.disconnect_dialog.title, { app: name })}</AlertDialogTitle>
              <AccountOwner scope={scope} />
            </div>
          </div>
          <AlertDialogDescription>
            {team
              ? t(($) => $.connected_apps.disconnect_dialog.team_body, { app: name })
              : t(($) => $.connected_apps.disconnect_dialog.personal_body, { app: name })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {impact.isLoading ? (
          <Loader2 className="size-4 animate-spin text-muted-foreground" />
        ) : impact.isError ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.connected_apps.disconnect_dialog.impact_failed)}</p>
        ) : agents.length === 0 ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.connected_apps.disconnect_dialog.no_agents)}</p>
        ) : (
          <ul className="divide-y rounded-lg border" aria-label={t(($) => $.connected_apps.disconnect_dialog.agents)}>
            {agents.map((agent) => (
              <li key={agent.id} className="flex items-center gap-2 px-3 py-2 text-body">
                <span className="min-w-0 flex-1 truncate font-medium">{agent.name}</span>
                {agent.running && (
                  <span className="shrink-0 text-caption text-warning">
                    {t(($) => $.connected_apps.disconnect_dialog.running)}
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
        {activeRuns > 0 && (
          <p className="text-caption text-warning">
            {t(($) => $.connected_apps.disconnect_dialog.active_runs, { count: activeRuns })}
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>{t(($) => $.connected_apps.disconnect_dialog.cancel)}</AlertDialogCancel>
          <AlertDialogAction variant="destructive" disabled={pending} aria-busy={pending} onClick={confirm}>
            {t(($) => $.connected_apps.disconnect_dialog.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
