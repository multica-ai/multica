"use client";

import type { ReactNode } from "react";
import { toast } from "sonner";
import { Building2, UserRound } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import { useActorName } from "@multica/core/workspace/hooks";
import {
  useDismissTeamAppRequests,
  useRequestTeamApp,
  useRestoreAppAccess,
} from "@multica/core/connected-apps";
import type { AppAccountScope, AppConnection, AppsOverview } from "@multica/core/types";
import {
  accountAction,
  accountFor,
  appAccountStatus,
  teamRequestFor,
  viewerTeamAsk,
} from "../../common/app-status";
import { ComposioToolkitLogo } from "../../common/composio-toolkit-logo";
import { SavedAppRestriction } from "../../common/saved-app-restriction";
import { useTeamAppAccess } from "../../common/use-team-app-access";
import { UpgradeToAddLink } from "../../billing";
import { useT } from "../../i18n";
import { AccountMenu } from "./account-menu";
import { AppStatusLabel } from "./app-status-label";

function errorMessage(e: unknown, fallback: string) {
  return e instanceof Error && e.message ? e.message : fallback;
}

/** "Team account" / "Your account" (or `label`), with the owner's icon. */
export function AccountOwner({ scope, label, detail }: { scope: AppAccountScope; label?: string; detail?: ReactNode }) {
  const { t } = useT("settings");
  const Icon = scope === "team" ? Building2 : UserRound;
  return (
    <span className="flex min-w-0 items-center gap-1 text-caption text-muted-foreground">
      <Icon aria-hidden="true" className="size-3 shrink-0" />
      <span className="shrink-0">
        {label ?? (scope === "team" ? t(($) => $.connected_apps.owner.team) : t(($) => $.connected_apps.owner.personal))}
      </span>
      {detail ? <span className="truncate">· {detail}</span> : null}
    </span>
  );
}

export interface AccountRowHandlers {
  connect: (scope: AppAccountScope) => void;
  activity?: (scope: AppAccountScope) => void;
  disconnect?: (scope: AppAccountScope, connection: AppConnection) => void;
}

/**
 * One account of one app, the same everywhere it is listed: whose it is,
 * where it stands, and the one thing the viewer can do about it. Who may do
 * what follows from the owner and the viewer's role (see `accountAction`).
 */
export function AccountRow({
  scope,
  slug,
  name,
  logo,
  showLogo = false,
  overview,
  title,
  subtitle,
  agentId,
  on,
  className,
}: {
  scope: AppAccountScope;
  slug: string;
  name: string;
  logo?: string;
  /** Lists of apps show the app's logo; the app's own page does not. */
  showLogo?: boolean;
  overview: AppsOverview;
  title: ReactNode;
  subtitle: ReactNode;
  /** The agent a member's ask is for. */
  agentId?: string;
  on: AccountRowHandlers;
  className?: string;
}) {
  const { t } = useT("settings");
  const { getActorName } = useActorName();
  const viewerId = useAuthStore((s) => s.user?.id ?? null);
  const restore = useRestoreAppAccess();
  const requestTeam = useRequestTeamApp();
  const dismiss = useDismissTeamAppRequests();
  const teamAccess = useTeamAppAccess(overview);
  const canManage = overview.can_manage;
  const account = accountFor(overview, scope, slug);
  const status = appAccountStatus(overview, scope, slug);
  const request = scope === "team" ? teamRequestFor(overview, slug) : undefined;
  const ask = scope === "team" ? viewerTeamAsk(overview, slug, viewerId) : null;
  const action = accountAction({
    scope,
    status,
    canManage,
    needsUpgrade: scope === "team" && teamAccess.needsUpgrade(slug),
    viewerAsk: ask,
  });

  let detail: string | undefined;
  if (status === "requested" && request) {
    detail = canManage
      ? [
          ...request.agent_ids.map((id) => getActorName("agent", id)),
          ...(request.asked_by ?? request.requested_by).map((id) => getActorName("member", id)),
        ].join(", ")
      : t(($) => $.connected_apps.account_row.admins_notified);
  }

  const askAdmin = () =>
    requestTeam.mutateAsync({ slug, agentId }).then(
      () => toast.success(t(($) => $.connected_apps.hub.toast_requested, { app: name })),
      (e: unknown) => toast.error(errorMessage(e, t(($) => $.connected_apps.toast_failed))),
    );
  const dismissRequests = () =>
    dismiss.mutateAsync(slug).then(
      () =>
        toast.success(
          canManage
            ? t(($) => $.connected_apps.account_row.toast_declined)
            : t(($) => $.connected_apps.account_row.toast_withdrawn),
        ),
      (e: unknown) => toast.error(errorMessage(e, t(($) => $.connected_apps.toast_failed))),
    );

  let button: ReactNode = null;
  switch (action) {
    case "connect":
    case "reconnect":
      button = (
        <Button size="xs" variant={status === "requested" || action === "reconnect" ? "default" : "outline"} onClick={() => on.connect(scope)}>
          {action === "reconnect" ? t(($) => $.connected_apps.hub.reconnect) : t(($) => $.connected_apps.hub.connect)}
        </Button>
      );
      break;
    case "upgrade":
      button = <UpgradeToAddLink />;
      break;
    case "ask_admin":
      button = (
        <Button size="xs" variant="outline" disabled={requestTeam.isPending} onClick={askAdmin}>
          {t(($) => $.connected_apps.hub.ask_admin)}
        </Button>
      );
      break;
    case "withdraw":
      button = (
        <Button size="xs" variant="ghost" disabled={dismiss.isPending} onClick={dismissRequests}>
          {t(($) => $.connected_apps.account_row.withdraw)}
        </Button>
      );
      break;
  }
  const canOpenMenu = !!account && (scope === "personal" || canManage);

  return (
    <div className={cn("space-y-2 px-4 py-3", className)} data-account-row={`${scope}:${slug}`}>
      <div className="flex items-center gap-3">
        {showLogo && <ComposioToolkitLogo slug={slug} name={name} fallbackLogo={logo} className="size-8 shrink-0" />}
        <div className="min-w-0 flex-1 space-y-0.5">
          <div className="truncate text-body font-medium">{title}</div>
          {subtitle}
        </div>
        <div className="hidden min-w-0 max-w-[45%] shrink text-caption sm:flex">
          <AppStatusLabel status={status} detail={detail} />
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {canManage && status === "requested" && (
            <Button size="xs" variant="ghost" disabled={dismiss.isPending} onClick={dismissRequests}>
              {t(($) => $.connected_apps.account_row.decline)}
            </Button>
          )}
          {button}
          {canOpenMenu && on.activity && on.disconnect && (
            <AccountMenu
              label={t(($) => $.connected_apps.app_page.account_actions, { account: name })}
              onActivity={() => on.activity?.(scope)}
              onDisconnect={() => on.disconnect?.(scope, account)}
            />
          )}
        </div>
      </div>
      <div className="text-caption sm:hidden">
        <AppStatusLabel status={status} detail={detail} />
      </div>
      {account?.actions_blocked && (
        <SavedAppRestriction
          kind="account"
          onRestore={scope === "personal" || canManage ? () => restore.mutateAsync({ slug, scope }) : undefined}
        />
      )}
    </div>
  );
}
