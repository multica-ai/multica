"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Radio } from "@base-ui/react/radio";
import { RadioGroup } from "@base-ui/react/radio-group";
import { toast } from "sonner";
import { AlertTriangle, Loader2, Lock, Plug, User, Users } from "@multica/ui/icons";
import { Switch } from "@multica/ui/components/ui/switch";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentAppsOptions,
  appCatalogOptions,
  appsOverviewOptions,
  useUpdateAgentApps,
  useRestoreAppAccess,
} from "@multica/core/connected-apps";
import { useWorkspacePaths } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import type {
  Agent,
  AgentApp,
  AppAccountScope,
  AppCatalogEntry,
} from "@multica/core/types";
import { ComposioToolkitLogo } from "../../../common/composio-toolkit-logo";
import { SavedAppRestriction } from "../../../common/saved-app-restriction";
import { AppLink } from "../../../navigation";
import { useT } from "../../../i18n";
import { ObjectSectionEmpty } from "../../../layout/object-page";

const SCOPES: readonly AppAccountScope[] = ["team", "personal"];

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
  const restore = useRestoreAppAccess();

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
  const visibleSlugs = useMemo(() => {
    const slugs = new Set(apps.map((app) => app.toolkit_slug));
    for (const scope of SCOPES) {
      for (const connection of overview.data?.[scope] ?? []) {
        if (connection.status === "active") {
          slugs.add(connection.toolkit_slug);
        }
      }
    }
    return [...slugs];
  }, [apps, overview.data]);

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
  const scopeLabel = (scope: AppAccountScope) => ts(($) => $.connected_apps.scopes[scope]);

  if (appsQuery.isLoading || overview.isLoading) {
    return <p className="text-body text-muted-foreground">{t(($) => $.tab_body.connected_apps.loading)}</p>;
  }
  if (appsQuery.isError || overview.isError) {
    return <p className="text-body text-destructive">{t(($) => $.tab_body.connected_apps.load_failed)}</p>;
  }

  return (
    <div className="space-y-4">
      <div className="space-y-2 text-caption text-muted-foreground">
        <p className="flex items-start gap-2">
          <Users className="mt-0.5 size-3.5 shrink-0" />
          <span><span className="font-medium text-foreground">{scopeLabel("team")}</span> · {t(($) => $.tab_body.connected_apps.team_help)}</span>
        </p>
        <p className="flex items-start gap-2">
          <User className="mt-0.5 size-3.5 shrink-0" />
          <span><span className="font-medium text-foreground">{scopeLabel("personal")}</span> · {t(($) => $.tab_body.connected_apps.personal_help)}</span>
        </p>
      </div>

      {overview.data && overview.data.catalog_mode !== "all" && (
        <SavedAppRestriction kind="catalog" onRestore={canManageTeam
          ? () => restore.mutateAsync({ catalog: true }) : undefined} />
      )}

      {visibleSlugs.length === 0 ? (
        <ObjectSectionEmpty icon={Plug} description={t(($) => $.tab_body.connected_apps.empty)} />
      ) : (
        <ul className="divide-y rounded-lg border">
          {visibleSlugs.map((slug) => {
            const app = apps.find((item) => item.toolkit_slug === slug);
            const entry = appBySlug.get(slug);
            const name = entry?.name || slug;
            const hasTeam = connected.has(`team:${slug}`);
            const hasPersonal = connected.has(`personal:${slug}`);
            const availableScopes = SCOPES.filter((scope) => connected.has(`${scope}:${slug}`));
            const defaultScope: AppAccountScope = hasTeam && canManageTeam && (entry?.recommended_scope === "team" || !hasPersonal)
              ? "team" : "personal";
            const scope = app?.account_scope ?? defaultScope;
            const isConnected = connected.has(`${scope}:${slug}`);
            const lockedTeam = scope === "team" && !canManageTeam;
            const canEnable = catalog.isSuccess && !!entry && entry.allowed !== false && (hasPersonal || (hasTeam && canManageTeam));
            const AccountIcon = scope === "team" ? Users : User;
            const status =
              scope === "team"
                ? isConnected
                  ? t(($) => $.tab_body.connected_apps.team_connected)
                  : t(($) => $.tab_body.connected_apps.team_missing)
                : isConnected
                  ? t(($) => $.tab_body.connected_apps.personal_connected)
                  : t(($) => $.tab_body.connected_apps.personal_missing);
            return (
              <li key={slug} className="space-y-2 p-3">
                <div className="flex flex-wrap items-center gap-3">
                  {entry ? (
                    <ComposioToolkitLogo slug={slug} name={name} fallbackLogo={entry.logo} className={app ? undefined : "grayscale"} />
                  ) : (
                    <span aria-hidden="true" className="flex size-8 shrink-0 items-center justify-center rounded-xs bg-muted text-caption font-semibold text-muted-foreground">
                      {name.charAt(0).toUpperCase()}
                    </span>
                  )}
                  <div className="min-w-0 flex-1 basis-40">
                    <p className={cn("truncate text-body font-medium", !app && "text-muted-foreground")}>{name}</p>
                    <p className={cn("mt-0.5 flex items-start gap-1 text-caption", app && !isConnected ? "text-warning" : "text-muted-foreground")}>
                      {app ? (
                        <>
                          <AccountIcon className="mt-0.5 size-3 shrink-0" />
                          <span>{scope === "personal" && <>{scopeLabel(scope)} · </>}{status}</span>
                        </>
                      ) : t(($) => $.tab_body.connected_apps.available_accounts, {
                        accounts: availableScopes.map((value) => value === "personal"
                          ? t(($) => $.tab_body.connected_apps.available_personal) : scopeLabel(value)).join(" / "),
                      })}
                    </p>
                    {app?.account_scope === "team" && agent.permission_mode !== "private" && (
                      <p className="mt-1 flex items-start gap-1 text-caption text-muted-foreground">
                        <AlertTriangle className="mt-0.5 size-3 shrink-0 text-warning" />
                        <span>{t(($) => $.tab_body.connected_apps.team_shared_warning)}</span>
                      </p>
                    )}
                  </div>
                  {app && hasTeam && hasPersonal && (
                    <RadioGroup
                      className="inline-flex h-7 shrink-0 items-center rounded-lg bg-muted p-[3px]"
                      aria-label={t(($) => $.tab_body.connected_apps.account_for, { app: name })}
                      value={scope}
                      disabled={update.isPending || lockedTeam || entry?.allowed === false}
                      onValueChange={(value) => {
                        if ((value === "team" || value === "personal") && value !== scope && canManageTeam && entry?.allowed !== false && !update.isPending) patch(slug, { account_scope: value });
                      }}
                    >
                      {SCOPES.map((value) => (
                        <Radio.Root
                          key={value}
                          value={value}
                          className="inline-flex h-[calc(100%-1px)] flex-1 items-center justify-center gap-1.5 rounded-[calc(var(--radius)-3px)] border border-transparent px-1.5 py-0.5 text-caption font-medium whitespace-nowrap text-muted-foreground transition-[color,background-color,border-color,box-shadow] hover:text-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-1 focus-visible:outline-ring data-disabled:pointer-events-none data-disabled:opacity-50 data-checked:bg-background data-checked:text-foreground data-checked:shadow-sm dark:data-checked:bg-input/60"
                          disabled={value === "team" && !canManageTeam}
                          title={!canManageTeam && value === "team" ? t(($) => $.tab_body.connected_apps.team_admin_only) : undefined}
                        >
                          {value === "team" && !canManageTeam && <Lock className="size-3" />}
                          {scopeLabel(value)}
                        </Radio.Root>
                      ))}
                    </RadioGroup>
                  )}
                  <Switch
                    checked={!!app}
                    disabled={update.isPending || (app ? lockedTeam : !canEnable)}
                    aria-label={t(($) => $.tab_body.connected_apps.enable, { app: name })}
                    title={!canManageTeam && (lockedTeam || (!app && hasTeam && !hasPersonal)) ? t(($) => $.tab_body.connected_apps.team_admin_only) : undefined}
                    onCheckedChange={(checked) => {
                      if (checked && canEnable) save([...apps, { agent_id: agent.id, toolkit_slug: slug, account_scope: defaultScope, level: "write" }]);
                      else if (!checked && app && !lockedTeam) save(apps.filter((item) => item.toolkit_slug !== slug));
                    }}
                  />
                </div>
                {app?.level === "read" && (
                  <div className="w-full">
                    <SavedAppRestriction kind="agent" onRestore={lockedTeam ? undefined : () => update.mutateAsync({
                      apps: apps.map((a) => ({
                        toolkit_slug: a.toolkit_slug,
                        account_scope: a.account_scope,
                        level: a.toolkit_slug === slug ? "write" : a.level,
                      })),
                    })} />
                  </div>
                )}
                {app && (overview.data?.[scope]?.some((c) => c.toolkit_slug === slug && c.actions_blocked === true)) && (
                  <p className="w-full text-caption text-warning">{ts(($) => $.connected_apps.saved_access.account_link)}</p>
                )}
              </li>
            );
          })}
        </ul>
      )}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-caption text-muted-foreground">{t(($) => $.tab_body.connected_apps.connect_hint)}</p>
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
