"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertTriangle, Loader2, Plug, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentAppsOptions,
  appCatalogOptions,
  appsOverviewOptions,
  useUpdateAgentApps,
} from "@multica/core/connected-apps";
import { useWorkspacePaths } from "@multica/core/paths";
import type {
  Agent,
  AgentApp,
  AppAccountScope,
  AppCatalogEntry,
} from "@multica/core/types";
import { ComposioToolkitLogo } from "../../../common/composio-toolkit-logo";
import { AppLink } from "../../../navigation";
import { useT } from "../../../i18n";
import { ObjectSectionEmpty } from "../../../layout/object-page";

const SCOPES: readonly AppAccountScope[] = ["personal", "team"];

/** Apps this agent can use and whose account each acts as.
 * Team account changes require a workspace admin. */
export function AgentAppsTab({ agent }: { agent: Agent }) {
  const { t } = useT("agents");
  const { t: ts } = useT("settings");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const appsQuery = useQuery(agentAppsOptions(wsId, agent.id));
  const overview = useQuery(appsOverviewOptions(wsId));
  const catalog = useQuery(appCatalogOptions(wsId));
  const update = useUpdateAgentApps(agent.id);

  const canManageTeam = overview.data?.can_manage ?? false;
  const apps = useMemo(() => appsQuery.data ?? [], [appsQuery.data]);
  const appBySlug = useMemo(() => {
    const m = new Map<string, AppCatalogEntry>();
    for (const app of catalog.data?.apps ?? []) m.set(app.slug, app);
    return m;
  }, [catalog.data]);
  const connected = useMemo(() => {
    const set = new Set<string>();
    for (const c of overview.data?.team ?? []) if (c.status === "active") set.add(`team:${c.toolkit_slug}`);
    for (const c of overview.data?.personal ?? []) if (c.status === "active") set.add(`personal:${c.toolkit_slug}`);
    return set;
  }, [overview.data]);

  const save = (next: AgentApp[]) => {
    update.mutate(
      {
        apps: next.map((a) => ({
          toolkit_slug: a.toolkit_slug,
          account_scope: a.account_scope,
          level: a.level,
        })),
      },
      { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.tab_body.connected_apps.save_failed_toast)) },
    );
  };
  const patch = (slug: string, change: Partial<AgentApp>) =>
    save(apps.map((a) => (a.toolkit_slug === slug ? { ...a, ...change } : a)));
  const add = (slug: string) => {
    const recommended = appBySlug.get(slug)?.recommended_scope;
    const scope: AppAccountScope = recommended === "team" && canManageTeam ? "team" : "personal";
    save([...apps, { agent_id: agent.id, toolkit_slug: slug, account_scope: scope, level: "write" }]);
  };

  const addable = (catalog.data?.apps ?? []).filter((app) => !apps.some((a) => a.toolkit_slug === app.slug));
  const scopeLabel = (scope: AppAccountScope) => ts(($) => $.connected_apps.scopes[scope]);
  const hasTeamApp = apps.some((a) => a.account_scope === "team");

  if (appsQuery.isLoading) {
    return <p className="text-body text-muted-foreground">{t(($) => $.tab_body.connected_apps.loading)}</p>;
  }
  if (appsQuery.isError) {
    return <p className="text-body text-destructive">{t(($) => $.tab_body.connected_apps.load_failed)}</p>;
  }

  return (
    <div className="space-y-4">
      <p className="text-caption text-muted-foreground">{t(($) => $.tab_body.connected_apps.subtitle)}</p>

      {hasTeamApp && agent.permission_mode !== "private" && (
        <div
          role="alert"
          className="flex items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-caption text-amber-700 dark:text-amber-400"
        >
          <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
          <span>{t(($) => $.tab_body.connected_apps.team_shared_warning)}</span>
        </div>
      )}

      {apps.length === 0 ? (
        <ObjectSectionEmpty icon={Plug} description={t(($) => $.tab_body.connected_apps.empty)} />
      ) : (
        <ul className="divide-y rounded-lg border">
          {apps.map((app) => {
            const entry = appBySlug.get(app.toolkit_slug);
            const name = entry?.name || app.toolkit_slug;
            const isConnected = connected.has(`${app.account_scope}:${app.toolkit_slug}`);
            const status =
              app.account_scope === "team"
                ? isConnected
                  ? t(($) => $.tab_body.connected_apps.team_connected)
                  : t(($) => $.tab_body.connected_apps.team_missing)
                : isConnected
                  ? t(($) => $.tab_body.connected_apps.personal_connected)
                  : t(($) => $.tab_body.connected_apps.personal_missing);
            const lockedTeam = app.account_scope === "team" && !canManageTeam;
            return (
              <li key={app.toolkit_slug} className="flex flex-wrap items-center gap-3 p-3">
                <ComposioToolkitLogo slug={app.toolkit_slug} name={name} fallbackLogo={entry?.logo} />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-body font-medium">{name}</p>
                  <p className={isConnected ? "truncate text-caption text-muted-foreground" : "truncate text-caption text-warning"}>
                    {status}
                  </p>
                </div>
                <Select
                  items={SCOPES.map((value) => ({ value, label: scopeLabel(value) }))}
                  value={app.account_scope}
                  disabled={update.isPending || !canManageTeam}
                  onValueChange={(value) => {
                    if (value && value !== app.account_scope) patch(app.toolkit_slug, { account_scope: value as AppAccountScope });
                  }}
                >
                  <SelectTrigger
                    size="sm"
                    className="w-44"
                    aria-label={t(($) => $.tab_body.connected_apps.account)}
                    title={canManageTeam ? undefined : t(($) => $.tab_body.connected_apps.team_admin_only)}
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent align="end">
                    {SCOPES.map((value) => (
                      <SelectItem key={value} value={value}>
                        {scopeLabel(value)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  disabled={update.isPending || lockedTeam}
                  aria-label={t(($) => $.tab_body.connected_apps.remove, { app: name })}
                  onClick={() => save(apps.filter((a) => a.toolkit_slug !== app.toolkit_slug))}
                >
                  <X />
                </Button>
              </li>
            );
          })}
        </ul>
      )}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <Select
          items={addable.map((app) => ({ value: app.slug, label: app.name || app.slug }))}
          value={null}
          disabled={update.isPending || addable.length === 0}
          onValueChange={(value) => {
            if (value) add(value as string);
          }}
        >
          <SelectTrigger size="sm" className="w-56" aria-label={t(($) => $.tab_body.connected_apps.add_placeholder)}>
            <SelectValue placeholder={t(($) => $.tab_body.connected_apps.add_placeholder)} />
          </SelectTrigger>
          <SelectContent>
            {addable.map((app) => (
              <SelectItem key={app.slug} value={app.slug}>
                {app.name || app.slug}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <AppLink
          href={`${paths.settings()}?tab=apps`}
          className="inline-flex items-center gap-1.5 text-caption font-medium text-primary hover:underline"
        >
          <Plug className="size-3" />
          {t(($) => $.tab_body.connected_apps.manage_link)}
        </AppLink>
      </div>

      {update.isPending && (
        <p className="flex items-center gap-1.5 text-caption text-muted-foreground">
          <Loader2 className="size-3 animate-spin" />
        </p>
      )}
    </div>
  );
}
