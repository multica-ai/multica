"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ArrowUpRight, Bot, Check, Loader2, Lock, Users } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { refreshFeaturesOnLockedError } from "@multica/core/billing";
import { appsOverviewOptions } from "@multica/core/connected-apps";
import { useActorName } from "@multica/core/workspace/hooks";
import type { AppAccountScope, AppActionClass } from "@multica/core/types";
import { ComposioToolkitLogo } from "./composio-toolkit-logo";
import { useViewerAppsOverview } from "./use-viewer-apps-overview";
import { usePaidFeatureErrorMessage } from "../billing";
import { useT } from "../i18n";
import { currentPath, useNavigation } from "../navigation";
import { isDesktopShell, openExternal } from "../platform";

const CLASSES: readonly AppActionClass[] = ["read", "write", "send", "delete", "share"];

/**
 * Says what agents will be able to do with an account before the hosted
 * connect flow starts: agents act without asking, and who the account is
 * visible to. Every place that starts a connection goes through it.
 */
export function ConnectAppDialog({
  open,
  onOpenChange,
  scope,
  slug,
  name,
  logo,
  returnTo,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  scope: AppAccountScope;
  slug: string;
  name: string;
  logo?: string;
  /** App path to come back to; defaults to the current page. */
  returnTo?: string;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const { getActorName } = useActorName();
  const qc = useQueryClient();
  const paidFeatureError = usePaidFeatureErrorMessage();
  const overview = useQuery({ ...appsOverviewOptions(wsId), enabled: open });
  const viewerOverview = useViewerAppsOverview(wsId, overview.data);
  const [pending, setPending] = useState(false);

  const agentNames = (viewerOverview?.agent_apps ?? [])
    .filter((a) => a.toolkit_slug === slug && a.account_scope === scope)
    .map((a) => getActorName("agent", a.agent_id));

  const start = async () => {
    setPending(true);
    try {
      const target = returnTo ?? currentPath({ ...navigation, hash: "" });
      const { redirect_url } = await api.connectApp(scope, slug, target);
      openExternal(redirect_url, { webTarget: "same-tab" });
      // Desktop finishes the flow in the system browser and stays here.
      if (isDesktopShell()) {
        setPending(false);
        onOpenChange(false);
      }
    } catch (e) {
      refreshFeaturesOnLockedError(qc, wsId, e);
      toast.error(paidFeatureError(e, e instanceof Error && e.message ? e.message : t(($) => $.connected_apps.toast_failed)));
      setPending(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !pending && onOpenChange(next)}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ComposioToolkitLogo slug={slug} name={name} fallbackLogo={logo} className="size-5" />
            {t(($) => $.connected_apps.disclosure.title, { app: name })}
          </DialogTitle>
          <DialogDescription>
            {scope === "team"
              ? t(($) => $.connected_apps.disclosure.team_subtitle)
              : t(($) => $.connected_apps.disclosure.personal_subtitle)}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-2 rounded-lg bg-muted/60 p-3">
          <p className="text-body font-medium">{t(($) => $.connected_apps.disclosure.without_asking)}</p>
          <ul className="space-y-1">
            {CLASSES.map((c) => (
              <li key={c} className="flex items-start gap-2 text-body">
                <Check className="mt-0.5 size-3.5 shrink-0 text-success" />
                <span>
                  {t(($) => $.connected_apps.classes[c].label)}
                  <span className="text-muted-foreground"> · {t(($) => $.connected_apps.classes[c].description)}</span>
                </span>
              </li>
            ))}
          </ul>
          <p className="text-caption text-muted-foreground">{t(($) => $.connected_apps.disclosure.logged)}</p>
        </div>

        <ul className="space-y-1.5 text-body">
          <li className="flex items-start gap-2">
            <Bot className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
            <span>
              {agentNames.length > 0
                ? t(($) => $.connected_apps.disclosure.used_by, { agents: agentNames.join(", "), app: name })
                : t(($) => $.connected_apps.disclosure.used_by_none)}
            </span>
          </li>
          <li className="flex items-start gap-2">
            {scope === "team"
              ? <Users className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
              : <Lock className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />}
            <span>
              {scope === "team"
                ? t(($) => $.connected_apps.disclosure.visibility_team)
                : t(($) => $.connected_apps.disclosure.visibility_personal)}
            </span>
          </li>
        </ul>

        <DialogFooter>
          <Button variant="ghost" disabled={pending} onClick={() => onOpenChange(false)}>
            {t(($) => $.connected_apps.disclosure.cancel)}
          </Button>
          <Button disabled={pending} onClick={start}>
            {pending ? <Loader2 className="animate-spin" /> : <ArrowUpRight />}
            {t(($) => $.connected_apps.disclosure.continue)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
