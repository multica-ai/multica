"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { myPendingAppRequestsOptions } from "@multica/core/connected-apps";
import type { AppAccountScope, AppCatalogEntry, AppConnection, AppRequest, AppsOverview } from "@multica/core/types";
import { appAccountStatus, teamRequestFor, type AppAccountStatus } from "../../common/app-status";
import { ConnectAppDialog } from "../../common/connect-app-dialog";
import { useT } from "../../i18n";
import { AccountOwner, AccountRow, type AccountRowHandlers } from "./account-row";
import { AppAccountDialog } from "./app-account-dialog";
import { DisconnectAccountDialog } from "./disconnect-account-dialog";

type AppRef = { slug: string; name: string; logo?: string };

export type AccountRows = Record<AppAccountScope, AppRef[]> & { waiting: AppRequest[] };

// Rows that need someone to act come first.
const ORDER: Record<AppAccountStatus, number> = {
  needs_reconnecting: 0,
  requested: 1,
  not_connected: 2,
  connected: 3,
};

/**
 * The apps involved in one owner's accounts: those connected, those agents
 * use through that owner, and, for the team, those someone is waiting for;
 * for the viewer, those an agent is waiting on them to connect.
 */
function accountSlugs(overview: AppsOverview, scope: AppAccountScope, waiting: AppRequest[]): string[] {
  const slugs = new Set<string>();
  for (const c of overview[scope]) if (c.status !== "revoked") slugs.add(c.toolkit_slug);
  for (const a of overview.agent_apps) if (a.account_scope === scope) slugs.add(a.toolkit_slug);
  if (scope === "team") for (const r of overview.team_requests ?? []) slugs.add(r.toolkit_slug);
  else for (const r of waiting) slugs.add(r.toolkit_slug);
  return [...slugs];
}

/** The team's and the viewer's account rows, needs-action first. */
export function useAccountRows(overview: AppsOverview | undefined, catalog: AppCatalogEntry[]): AccountRows {
  const wsId = useWorkspaceId();
  const requests = useQuery(myPendingAppRequestsOptions(wsId));
  const waiting = (requests.data ?? []).filter(
    (r) => r.kind === "connect" && r.status === "pending" && r.account_scope === "personal",
  );
  const entries = new Map(catalog.map((a) => [a.slug, a]));
  const rows = (scope: AppAccountScope): AppRef[] => {
    if (!overview) return [];
    return accountSlugs(overview, scope, waiting)
      .map((slug) => {
        const entry = entries.get(slug);
        return { app: { slug, name: entry?.name || slug, logo: entry?.logo }, status: appAccountStatus(overview, scope, slug) };
      })
      .sort((a, b) => ORDER[a.status] - ORDER[b.status] || a.app.name.localeCompare(b.app.name))
      .map(({ app }) => app);
  };
  return { team: rows("team"), personal: rows("personal"), waiting };
}

/** The accounts agents in this workspace act as: the team's and the viewer's. */
export function AccountsList({
  overview,
  rows,
  matches,
}: {
  overview: AppsOverview;
  rows: AccountRows;
  matches: (name: string) => boolean;
}) {
  const { t } = useT("settings");
  const usage = useUsage(overview, rows.waiting);
  const [connecting, setConnecting] = useState<{ scope: AppAccountScope; app: AppRef } | null>(null);
  const [activity, setActivity] = useState<{ scope: AppAccountScope; app: AppRef } | null>(null);
  const [disconnecting, setDisconnecting] = useState<{ scope: AppAccountScope; connection: AppConnection; app: AppRef } | null>(null);

  const handlersFor = (app: AppRef): AccountRowHandlers => ({
    connect: (scope) => setConnecting({ scope, app }),
    activity: (scope) => setActivity({ scope, app }),
    disconnect: (scope, connection) => setDisconnecting({ scope, connection, app }),
  });

  const group = (scope: AppAccountScope) => {
    const list = rows[scope].filter((app) => matches(app.name) || matches(app.slug));
    const title = scope === "team"
      ? t(($) => $.connected_apps.accounts_page.team_title)
      : t(($) => $.connected_apps.accounts_page.yours_title);
    const description = scope === "personal"
      ? t(($) => $.connected_apps.accounts_page.yours_description)
      : overview.can_manage
        ? t(($) => $.connected_apps.accounts_page.team_description_admin)
        : t(($) => $.connected_apps.accounts_page.team_description_member);
    return (
      <section aria-label={title} className="space-y-3">
        <div className="space-y-0.5">
          <h3 className="text-body font-semibold">{title}</h3>
          <p className="text-caption text-muted-foreground">{description}</p>
        </div>
        {list.length === 0 ? (
          <p className="rounded-lg border border-dashed px-4 py-6 text-center text-caption text-muted-foreground">
            {scope === "team"
              ? t(($) => $.connected_apps.accounts_page.team_empty)
              : t(($) => $.connected_apps.accounts_page.empty)}
          </p>
        ) : (
          <div className="divide-y overflow-hidden rounded-lg border bg-card">
            {list.map((app) => (
              <AccountRow
                key={app.slug}
                scope={scope}
                slug={app.slug}
                name={app.name}
                logo={app.logo}
                showLogo
                overview={overview}
                title={app.name}
                subtitle={<AccountOwner scope={scope} detail={usage(scope, app.slug)} />}
                on={handlersFor(app)}
              />
            ))}
          </div>
        )}
      </section>
    );
  };

  return (
    <div className="space-y-8">
      {group("team")}
      {group("personal")}

      {connecting && (
        <ConnectAppDialog
          open
          onOpenChange={(open) => { if (!open) setConnecting(null); }}
          scope={connecting.scope}
          slug={connecting.app.slug}
          name={connecting.app.name}
          logo={connecting.app.logo}
        />
      )}
      {activity && (
        <AppAccountDialog
          scope={activity.scope}
          slug={activity.app.slug}
          name={activity.app.name}
          onClose={() => setActivity(null)}
        />
      )}
      {disconnecting && (
        <DisconnectAccountDialog
          scope={disconnecting.scope}
          connectionId={disconnecting.connection.id}
          slug={disconnecting.app.slug}
          name={disconnecting.app.name}
          logo={disconnecting.app.logo}
          onClose={() => setDisconnecting(null)}
        />
      )}
    </div>
  );
}

/** Who relies on the account: the agents acting as it, or who waits for it. */
function useUsage(overview: AppsOverview, waiting: AppRequest[]) {
  const { t } = useT("settings");
  const { getActorName } = useActorName();
  const names = (ids: string[]) => ids.map((id) => getActorName("agent", id)).join(", ");
  return (scope: AppAccountScope, slug: string): string | undefined => {
    const agentIds = [...new Set(overview.agent_apps.filter((a) => a.toolkit_slug === slug && a.account_scope === scope).map((a) => a.agent_id))];
    const waitingAgents = scope === "team"
      ? (teamRequestFor(overview, slug)?.agent_ids ?? [])
      : waiting.filter((r) => r.toolkit_slug === slug).map((r) => r.agent_id);
    const needing = [...new Set(waitingAgents)].filter(Boolean);
    if (needing.length > 0 && appAccountStatus(overview, scope, slug) !== "connected") {
      return t(($) => $.connected_apps.accounts_page.needed_by, { agents: names(needing) });
    }
    if (agentIds.length === 0) return undefined;
    return t(($) => $.connected_apps.accounts_page.used_by, { agents: names(agentIds) });
  };
}
