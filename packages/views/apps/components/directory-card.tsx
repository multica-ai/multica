"use client";

import { Loader2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import type { AppAccountScope } from "@multica/core/types";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { UpgradeToAddLink } from "../../billing";
import { useT } from "../../i18n";
import { useAppCategoryLabel } from "../hooks/use-app-category-label";
import type { DirectoryApp } from "../lib/directory";
import { AppCard } from "./app-card";
import { AppStatusLabel } from "./app-status-label";

export interface DirectoryCardHandlers {
  onConnect: (app: DirectoryApp, scope: AppAccountScope) => void;
  onAsk: (app: DirectoryApp) => void;
  onWithdraw: (app: DirectoryApp) => void;
  /** Slug whose admin request is being sent or withdrawn. */
  asking: string | null;
  /** Whether a team account for this app is one the plan does not include. */
  needsUpgrade: (slug: string) => boolean;
}

/** What a card for an unused app says under its name. */
export type SetUpDetail = "category" | "connect";

/** An app in use: where its account stands, whose it is, and what to do or who uses it. */
export function DirectoryCard({
  app,
  handlers,
  detail,
}: {
  app: DirectoryApp;
  handlers: DirectoryCardHandlers;
  detail?: SetUpDetail;
}) {
  const { t } = useT("settings");
  const paths = useWorkspacePaths();
  const { getActorName } = useActorName();

  if (!app.inUse && !app.request) return <SetUpCard app={app} detail={detail} />;

  const scopeLine =
    app.scopes.length === 2
      ? t(($) => $.connected_apps.hub.scope_both)
      : app.scopes[0] === "team"
        ? t(($) => $.connected_apps.hub.scope_team)
        : t(($) => $.connected_apps.hub.scope_personal);
  const asked = app.request && app.status === "requested"
    ? t(($) => $.connected_apps.hub.members_asked, { count: app.request.requested_by.length })
    : undefined;
  const hasAction = !!app.action && app.action.kind !== "set_up";
  // With nothing to do, the trailing slot shows the agents behind the app.
  const using = app.agentIds.length > 0;
  const agentIds = using ? app.agentIds : (app.request?.agent_ids ?? []);
  const agents = agentIds.map((id) => getActorName("agent", id)).join(", ");
  const usage = using
    ? t(($) => $.connected_apps.hub.used_by, { agents })
    : t(($) => $.connected_apps.hub.wanted_by, { agents });

  return (
    <AppCard
      slug={app.slug}
      name={app.name}
      logo={app.entry?.logo}
      href={paths.appDetail(app.slug)}
      meta={
        app.status
          ? <AppStatusLabel status={app.status} detail={asked ?? scopeLine} />
          : <span className="truncate">{scopeLine}</span>
      }
      action={
        hasAction ? (
          <DirectoryAction app={app} handlers={handlers} />
        ) : agentIds.length > 0 ? (
          <Tooltip>
            <TooltipTrigger
              render={
                <span
                  role="img"
                  tabIndex={0}
                  aria-label={usage}
                  className="inline-flex rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring"
                />
              }
            >
              <AgentAvatarStack agentIds={agentIds} />
            </TooltipTrigger>
            <TooltipContent>{usage}</TooltipContent>
          </Tooltip>
        ) : undefined
      }
    />
  );
}

export function SetUpCard({ app, detail = "category" }: { app: DirectoryApp; detail?: SetUpDetail }) {
  const { t } = useT("settings");
  const paths = useWorkspacePaths();
  const categoryLabel = useAppCategoryLabel();
  const method = app.entry?.connect_method;
  const connect =
    method === "key"
      ? t(($) => $.connected_apps.hub.connect_key)
      : method === "oauth"
        ? t(($) => $.connected_apps.hub.connect_oauth)
        : undefined;
  const category = categoryLabel.entry(app.entry);
  const text = detail === "connect" ? (connect ?? category) : (category ?? connect);
  return (
    <AppCard
      slug={app.slug}
      name={app.name}
      logo={app.entry?.logo}
      href={paths.appDetail(app.slug)}
      meta={text ? <span className="truncate">{text}</span> : undefined}
    />
  );
}

function DirectoryAction({ app, handlers }: { app: DirectoryApp; handlers: DirectoryCardHandlers }) {
  const { t } = useT("settings");
  const action = app.action;
  if (!action || action.kind === "set_up") return null;
  if (action.kind === "connect") {
    if (action.scope === "team" && handlers.needsUpgrade(app.slug)) return <UpgradeToAddLink />;
    return (
      <Button
        size="xs"
        variant={action.reconnect ? "default" : "outline"}
        onClick={() => handlers.onConnect(app, action.scope)}
      >
        {action.reconnect ? t(($) => $.connected_apps.hub.reconnect) : t(($) => $.connected_apps.hub.connect)}
      </Button>
    );
  }
  const asking = handlers.asking === app.slug;
  if (action.kind === "withdraw") {
    return (
      <Button
        size="xs"
        variant="ghost"
        disabled={handlers.asking !== null}
        aria-busy={asking}
        onClick={() => handlers.onWithdraw(app)}
      >
        {asking && <Loader2 className="animate-spin" />}
        {t(($) => $.connected_apps.account_row.withdraw)}
      </Button>
    );
  }
  return (
    <Button
      size="xs"
      variant="outline"
      disabled={handlers.asking !== null}
      aria-busy={asking}
      onClick={() => handlers.onAsk(app)}
    >
      {asking && <Loader2 className="animate-spin" />}
      {t(($) => $.connected_apps.hub.ask_admin)}
    </Button>
  );
}
