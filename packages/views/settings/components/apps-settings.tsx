"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertTriangle, Plus, Trash2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  appCatalogOptions,
  appsOverviewOptions,
  connectedAppsKeys,
  teamAppImpactOptions,
  useDisconnectTeamApp,
  useUpdateAppSettings,
} from "@multica/core/connected-apps";
import { useActorName } from "@multica/core/workspace/hooks";
import type {
  AppAccountScope,
  AppCatalogEntry,
  AppCatalogMode,
  AppConnection,
} from "@multica/core/types";
import { ComposioToolkitLogo } from "../../common/composio-toolkit-logo";
import { useT, useTimeAgo } from "../../i18n";
import { useNavigation } from "../../navigation";
import { AddAppDialog } from "./add-app-dialog";
import { AppAccountDialog } from "./app-account-dialog";
import { CustomCatalogDialog } from "./custom-catalog-dialog";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";

type AccountTarget = { scope: AppAccountScope; connection: AppConnection; name: string };

/**
 * Settings › Connected apps (MUL-8516): the workspace's team accounts, the
 * caller's own accounts, and which apps the workspace allows. Which agent
 * uses which app is set on the agent.
 */
export function AppsSettings() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const navigation = useNavigation();
  const overview = useQuery(appsOverviewOptions(wsId));
  const catalog = useQuery(appCatalogOptions(wsId));
  const updateSettings = useUpdateAppSettings();

  const [adding, setAdding] = useState(false);
  const [detail, setDetail] = useState<AccountTarget | null>(null);
  const [disconnecting, setDisconnecting] = useState<AccountTarget | null>(null);
  const [editingList, setEditingList] = useState(false);

  // The hosted connect flow returns with `?connected=<slug>` or
  // `?error=composio_connect_failed`. Consume it once, then drop the params so
  // a refresh does not toast again.
  const connectedParam = navigation.searchParams.get("connected");
  const errorParam = navigation.searchParams.get("error");
  const consumed = useRef<string | null>(null);
  useEffect(() => {
    const key = connectedParam
      ? `connected:${connectedParam}`
      : errorParam === "composio_connect_failed"
        ? "error"
        : null;
    if (!key || consumed.current === key) return;
    consumed.current = key;
    if (connectedParam) {
      toast.success(t(($) => $.connected_apps.toast_connected));
      void qc.invalidateQueries({ queryKey: connectedAppsKeys.all(wsId) });
    } else {
      toast.error(t(($) => $.connected_apps.toast_connect_failed));
    }
    const params = new URLSearchParams(navigation.searchParams);
    params.set("tab", "apps");
    for (const k of ["integration", "connected", "error", "scope"]) params.delete(k);
    navigation.replace(`${navigation.pathname}?${params.toString()}`);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connectedParam, errorParam]);

  const appBySlug = useMemo(() => {
    const m = new Map<string, AppCatalogEntry>();
    for (const app of catalog.data?.apps ?? []) m.set(app.slug, app);
    return m;
  }, [catalog.data]);

  const agentCount = useMemo(() => {
    const m = new Map<string, number>();
    for (const app of overview.data?.agent_apps ?? []) {
      const key = `${app.account_scope}:${app.toolkit_slug}`;
      m.set(key, (m.get(key) ?? 0) + 1);
    }
    return m;
  }, [overview.data]);

  if (overview.isLoading) {
    return <p className="text-body text-muted-foreground">{t(($) => $.connected_apps.loading)}</p>;
  }
  if (overview.isError || !overview.data) {
    return <p className="text-body text-destructive">{t(($) => $.connected_apps.load_failed)}</p>;
  }
  const data = overview.data;
  const canManage = data.can_manage;
  const nameOf = (slug: string) => appBySlug.get(slug)?.name || slug;
  const catalogModeLabel = (mode: AppCatalogMode) =>
    mode === "curated"
      ? t(($) => $.connected_apps.catalog_curated)
      : mode === "custom"
        ? t(($) => $.connected_apps.catalog_custom)
        : t(($) => $.connected_apps.catalog_all);
  const allowedCount = data.allowed_toolkits?.length ?? 0;

  const rows = (scope: AppAccountScope, connections: AppConnection[]) =>
    connections
      .filter((c) => c.status !== "revoked")
      .map((c) => (
        <AccountRow
          key={c.id}
          scope={scope}
          connection={c}
          app={appBySlug.get(c.toolkit_slug)}
          agents={agentCount.get(`${scope}:${c.toolkit_slug}`) ?? 0}
          canManage={scope === "personal" || canManage}
          onOpen={() => setDetail({ scope, connection: c, name: nameOf(c.toolkit_slug) })}
          onDisconnect={() => setDisconnecting({ scope, connection: c, name: nameOf(c.toolkit_slug) })}
        />
      ));
  const teamRows = rows("team", data.team);
  const personalRows = rows("personal", data.personal);

  return (
    <div className="space-y-8">
      <div className="flex items-start justify-between gap-4">
        <p className="text-caption text-muted-foreground">{t(($) => $.connected_apps.description)}</p>
        <Button size="sm" onClick={() => setAdding(true)}>
          <Plus />
          {t(($) => $.connected_apps.add_app)}
        </Button>
      </div>

      <SettingsSection
        anchor="team-accounts"
        title={t(($) => $.connected_apps.team_title)}
        description={t(($) => $.connected_apps.team_description)}
      >
        {teamRows.length > 0 ? (
          <SettingsCard>{teamRows}</SettingsCard>
        ) : (
          <EmptyCard
            text={
              canManage
                ? t(($) => $.connected_apps.team_empty)
                : t(($) => $.connected_apps.team_empty_member)
            }
          />
        )}
      </SettingsSection>

      <SettingsSection
        anchor="my-accounts"
        title={t(($) => $.connected_apps.personal_title)}
        description={t(($) => $.connected_apps.personal_description)}
      >
        {personalRows.length > 0 ? (
          <SettingsCard>{personalRows}</SettingsCard>
        ) : (
          <EmptyCard text={t(($) => $.connected_apps.personal_empty)} />
        )}
      </SettingsSection>

      <SettingsSection anchor="catalog" title={t(($) => $.connected_apps.catalog_title)}>
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.connected_apps.catalog_label)}
            description={t(($) => $.connected_apps.catalog_description)}
            size="select"
          >
            <Select
              items={(["all", "curated", "custom"] as const).map((value) => ({
                value,
                label: catalogModeLabel(value),
              }))}
              value={data.catalog_mode}
              disabled={!canManage || updateSettings.isPending}
              onValueChange={(value) => {
                if (!value || value === data.catalog_mode) return;
                // A custom catalog is only saved together with its list.
                if (value === "custom") {
                  setEditingList(true);
                  return;
                }
                updateSettings.mutate({ catalog_mode: value as AppCatalogMode }, {
                  onError: () => toast.error(t(($) => $.connected_apps.toast_failed)),
                });
              }}
            >
              <SelectTrigger size="sm" className="w-full" aria-label={t(($) => $.connected_apps.catalog_label)}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent align="end">
                <SelectItem value="all">{catalogModeLabel("all")}</SelectItem>
                <SelectItem value="curated">{catalogModeLabel("curated")}</SelectItem>
                <SelectItem value="custom">{catalogModeLabel("custom")}</SelectItem>
              </SelectContent>
            </Select>
          </SettingsRow>
          {data.catalog_mode === "custom" && (
            <SettingsRow
              label={t(($) => $.connected_apps.custom.row_label)}
              description={t(($) => $.connected_apps.custom.row_description, { count: allowedCount })}
            >
              <Button size="sm" variant="outline" disabled={!canManage} onClick={() => setEditingList(true)}>
                {t(($) => $.connected_apps.custom.edit)}
              </Button>
            </SettingsRow>
          )}
        </SettingsCard>
      </SettingsSection>

      <AddAppDialog
        open={adding}
        onOpenChange={setAdding}
        canManage={canManage}
        overview={data}
        catalog={catalog.data?.apps ?? []}
        catalogLoading={catalog.isLoading}
        catalogError={catalog.isError}
      />
      {detail && (
        <AppAccountDialog
          scope={detail.scope}
          slug={detail.connection.toolkit_slug}
          name={detail.name}
          canEdit={detail.scope === "personal" || canManage}
          canClassify={canManage}
          onClose={() => setDetail(null)}
        />
      )}
      {editingList && (
        <CustomCatalogDialog initial={data.allowed_toolkits ?? []} onClose={() => setEditingList(false)} />
      )}
      {disconnecting?.scope === "team" && (
        <DisconnectTeamDialog target={disconnecting} onClose={() => setDisconnecting(null)} />
      )}
      {disconnecting?.scope === "personal" && (
        <DisconnectPersonalDialog target={disconnecting} onClose={() => setDisconnecting(null)} />
      )}
    </div>
  );
}

function EmptyCard({ text }: { text: string }) {
  return (
    <div className="rounded-lg border border-dashed px-4 py-6 text-center text-caption text-muted-foreground">
      {text}
    </div>
  );
}

function AccountRow({
  scope,
  connection,
  app,
  agents,
  canManage,
  onOpen,
  onDisconnect,
}: {
  scope: AppAccountScope;
  connection: AppConnection;
  app?: AppCatalogEntry;
  agents: number;
  canManage: boolean;
  onOpen: () => void;
  onDisconnect: () => void;
}) {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const name = app?.name || connection.toolkit_slug;
  const expired = connection.status === "expired";
  const meta = [
    scope === "team" && connection.connected_by
      ? t(($) => $.connected_apps.connected_by, { name: getActorName("member", connection.connected_by) })
      : null,
    connection.last_used_at
      ? t(($) => $.connected_apps.last_used, { when: timeAgo(connection.last_used_at) })
      : t(($) => $.connected_apps.last_used_never),
    agents > 0 ? t(($) => $.connected_apps.used_by, { count: agents }) : null,
  ].filter(Boolean);

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <ComposioToolkitLogo slug={connection.toolkit_slug} name={name} fallbackLogo={app?.logo} />
      <div className="min-w-0 flex-1">
        <p className="truncate text-body font-medium">{name}</p>
        <p className="truncate text-caption text-muted-foreground">
          {expired ? (
            <span className="inline-flex items-center gap-1 text-warning">
              <AlertTriangle className="size-3" />
              {t(($) => $.connected_apps.expired)}
            </span>
          ) : (
            meta.join(" · ")
          )}
        </p>
      </div>
      <Button variant="ghost" size="sm" onClick={onOpen}>
        {t(($) => $.connected_apps.rules)}
      </Button>
      {canManage && (
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={t(($) => $.connected_apps.disconnect)}
          onClick={onDisconnect}
        >
          <Trash2 />
        </Button>
      )}
    </div>
  );
}

function DisconnectTeamDialog({ target, onClose }: { target: AccountTarget; onClose: () => void }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const impact = useQuery(teamAppImpactOptions(wsId, target.connection.toolkit_slug));
  const disconnect = useDisconnectTeamApp();
  const data = impact.data;

  return (
    <AlertDialog open onOpenChange={(open) => !open && !disconnect.isPending && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.connected_apps.disconnect_team.title, { app: target.name })}</AlertDialogTitle>
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
          <AlertDialogAction
            variant="destructive"
            disabled={disconnect.isPending}
            onClick={() =>
              disconnect.mutate(target.connection.id, {
                onSuccess: () => {
                  toast.success(t(($) => $.connected_apps.toast_disconnected));
                  onClose();
                },
                onError: () => toast.error(t(($) => $.connected_apps.toast_failed)),
              })
            }
          >
            {t(($) => $.connected_apps.disconnect_team.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function DisconnectPersonalDialog({ target, onClose }: { target: AccountTarget; onClose: () => void }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [pending, setPending] = useState(false);

  return (
    <AlertDialog open onOpenChange={(open) => !open && !pending && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.connected_apps.disconnect_personal.title, { app: target.name })}</AlertDialogTitle>
          <AlertDialogDescription>{t(($) => $.connected_apps.disconnect_personal.description)}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>{t(($) => $.connected_apps.disconnect_personal.cancel)}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={pending}
            onClick={async () => {
              setPending(true);
              try {
                await api.deleteComposioConnection(target.connection.id);
                await qc.invalidateQueries({ queryKey: connectedAppsKeys.all(wsId) });
                toast.success(t(($) => $.connected_apps.toast_disconnected));
                onClose();
              } catch {
                toast.error(t(($) => $.connected_apps.toast_failed));
              } finally {
                setPending(false);
              }
            }}
          >
            {t(($) => $.connected_apps.disconnect_personal.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
