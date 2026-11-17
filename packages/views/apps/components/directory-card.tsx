"use client";

import { Loader2 } from "@multica/ui/icons";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import type { AppAccountScope } from "@multica/core/types";
import { UpgradeToAddLink } from "../../billing";
import { useT } from "../../i18n";
import type { DirectoryApp } from "../lib/directory";
import { AppCard } from "./app-card";
import { AppStatusLabel } from "./app-status-label";

export interface DirectoryCardHandlers {
  onConnect: (app: DirectoryApp, scope: AppAccountScope) => void;
  onAsk: (app: DirectoryApp) => void;
  /** Slug whose admin request is being sent. */
  asking: string | null;
  /** Whether a team account for this app is one the plan does not include. */
  needsUpgrade: (slug: string) => boolean;
}

/** An app in use: whose account it acts as, where that stands, and who uses it. */
export function DirectoryCard({ app, handlers }: { app: DirectoryApp; handlers: DirectoryCardHandlers }) {
  const { t } = useT("settings");
  const paths = useWorkspacePaths();
  const { getActorName } = useActorName();

  if (!app.inUse && !app.request) return <SetUpCard app={app} />;

  const scopeLine =
    app.scopes.length === 2
      ? t(($) => $.connected_apps.hub.scope_both)
      : app.scopes[0] === "team"
        ? t(($) => $.connected_apps.hub.scope_team)
        : t(($) => $.connected_apps.hub.scope_personal);
  const asked = app.request && app.status === "no_team_account"
    ? t(($) => $.connected_apps.hub.members_asked, { count: app.request.requested_by.length })
    : undefined;
  const usage =
    app.agentIds.length > 0
      ? t(($) => $.connected_apps.hub.used_by, { agents: app.agentIds.map((id) => getActorName("agent", id)).join(", ") })
      : app.request && app.request.agent_ids.length > 0
        ? t(($) => $.connected_apps.hub.wanted_by, { agents: app.request.agent_ids.map((id) => getActorName("agent", id)).join(", ") })
        : app.request
          ? t(($) => $.connected_apps.hub.asked_by, { names: app.request.requested_by.map((id) => getActorName("member", id)).join(", ") })
          : null;

  return (
    <AppCard
      slug={app.slug}
      name={app.name}
      logo={app.entry?.logo}
      href={paths.appDetail(app.slug)}
      subtitle={scopeLine}
      action={<DirectoryAction app={app} handlers={handlers} />}
    >
      {app.status ? <p className="flex min-w-0"><AppStatusLabel status={app.status} detail={asked} /></p> : null}
      {usage ? <p className="truncate">{usage}</p> : null}
    </AppCard>
  );
}

export function SetUpCard({ app }: { app: DirectoryApp }) {
  const { t } = useT("settings");
  const paths = useWorkspacePaths();
  return (
    <AppCard
      slug={app.slug}
      name={app.name}
      logo={app.entry?.logo}
      href={paths.appDetail(app.slug)}
      subtitle={app.entry?.category}
      // The whole card already links to the app; this only labels it.
      action={
        <span aria-hidden="true" className={buttonVariants({ variant: "outline", size: "xs" })}>
          {t(($) => $.connected_apps.hub.set_up)}
        </span>
      }
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
  if (action.kind === "requested") {
    return (
      <Button size="xs" variant="outline" disabled>
        {t(($) => $.connected_apps.hub.requested)}
      </Button>
    );
  }
  const asking = handlers.asking === app.slug;
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
