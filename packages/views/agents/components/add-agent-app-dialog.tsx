"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Loader2, Plus } from "@multica/ui/icons";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentAppsOptions,
  appCatalogOptions,
  appsOverviewOptions,
  useUpdateAgentApps,
} from "@multica/core/connected-apps";
import type { Agent, AppCatalogEntry } from "@multica/core/types";
import { appAccountStatus } from "../../common/app-status";
import { ComposioToolkitLogo } from "../../common/composio-toolkit-logo";
import { useT } from "../../i18n";
import { useTeamAppAccess } from "../../common/use-team-app-access";
import { scopeForNewApp } from "./agent-app-status";
import { useAppStatusLabel } from "./use-agent-apps";

/**
 * Adds one catalog app to the agent. The account it uses is decided for the
 * viewer (`scopeForNewApp`); a personal-account app needs no account of the
 * viewer's own, since each run uses whoever starts it.
 */
export function AddAgentAppDialog({
  agent,
  open,
  onOpenChange,
}: {
  agent: Agent;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const appsQuery = useQuery({ ...agentAppsOptions(wsId, agent.id), enabled: open });
  const overview = useQuery({ ...appsOverviewOptions(wsId), enabled: open });
  const catalog = useQuery({ ...appCatalogOptions(wsId), enabled: open });
  const update = useUpdateAgentApps(agent.id);
  const statusLabel = useAppStatusLabel();
  const teamAccess = useTeamAppAccess(overview.data);
  const [query, setQuery] = useState("");
  const [adding, setAdding] = useState<string | null>(null);

  const available = useMemo(() => {
    const onAgent = new Set((appsQuery.data ?? []).map((a) => a.toolkit_slug));
    return (catalog.data?.apps ?? []).filter((app) => app.allowed !== false && !onAgent.has(app.slug));
  }, [appsQuery.data, catalog.data]);

  const mixedCatalog = available.some((app) => app.curated) && available.some((app) => !app.curated);
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return available;
    return available.filter(
      (app) =>
        app.name.toLowerCase().includes(q) ||
        app.slug.includes(q) ||
        (app.category ?? "").toLowerCase().includes(q),
    );
  }, [available, query]);

  const ready = appsQuery.isSuccess && overview.isSuccess;

  const add = (app: AppCatalogEntry) => {
    const current = appsQuery.data;
    if (!current || !overview.data || adding) return;
    const name = app.name || app.slug;
    setAdding(app.slug);
    update
      .mutateAsync({
        apps: [
          ...current.map((a) => ({
            toolkit_slug: a.toolkit_slug,
            account_scope: a.account_scope,
            level: a.level,
          })),
          {
            toolkit_slug: app.slug,
            account_scope: scopeForNewApp(overview.data, app.slug, app.recommended_scope, teamAccess.canAdd),
            level: "write",
          },
        ],
      })
      .then(
        () => {
          toast.success(t(($) => $.add_app_dialog.added_toast, { app: name, agent: agent.name }));
          onOpenChange(false);
        },
        (e: unknown) =>
          toast.error(e instanceof Error && e.message ? e.message : t(($) => $.tab_body.connected_apps.save_failed_toast)),
      )
      .finally(() => setAdding(null));
  };

  const hint = (app: AppCatalogEntry) => {
    const scope = scopeForNewApp(overview.data, app.slug, app.recommended_scope, teamAccess.canAdd);
    const status = scope === "team"
      ? t(($) => $.add_app_dialog.team_connected)
      : statusLabel(appAccountStatus(overview.data, "personal", app.slug));
    const usual = app.recommended_scope === "team"
      ? t(($) => $.add_app_dialog.recommended_team)
      : app.recommended_scope === "personal"
        ? t(($) => $.add_app_dialog.recommended_personal)
        : app.category;
    return [usual, overview.isSuccess ? status : null].filter(Boolean).join(" · ");
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setQuery("");
        onOpenChange(next);
      }}
    >
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.add_app_dialog.title, { agent: agent.name })}</DialogTitle>
          <DialogDescription>{t(($) => $.add_app_dialog.description)}</DialogDescription>
        </DialogHeader>
        <Input
          autoFocus
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t(($) => $.add_app_dialog.search, { count: available.length })}
          aria-label={t(($) => $.add_app_dialog.search_label)}
        />
        <div className="max-h-[60vh] min-h-48 overflow-y-auto rounded-lg border">
          {catalog.isLoading || appsQuery.isLoading ? (
            <p className="p-4 text-caption text-muted-foreground">{t(($) => $.tab_body.connected_apps.loading)}</p>
          ) : catalog.isError || appsQuery.isError ? (
            <p className="p-4 text-caption text-destructive">{t(($) => $.add_app_dialog.load_failed)}</p>
          ) : filtered.length === 0 ? (
            <p className="p-4 text-caption text-muted-foreground">{t(($) => $.add_app_dialog.empty)}</p>
          ) : (
            <ul className="divide-y">
              {filtered.map((app) => (
                <li key={app.slug} className="flex items-center gap-3 px-3 py-2.5">
                  <ComposioToolkitLogo slug={app.slug} name={app.name} fallbackLogo={app.logo} />
                  <div className="min-w-0 flex-1">
                    <p className="flex items-center gap-2 truncate text-body font-medium">
                      {app.name || app.slug}
                      {mixedCatalog && app.curated && <Badge variant="secondary">{t(($) => $.add_app_dialog.curated)}</Badge>}
                    </p>
                    <p className="truncate text-caption text-muted-foreground">{hint(app)}</p>
                  </div>
                  <Button
                    size="xs"
                    variant="outline"
                    className="shrink-0"
                    disabled={!ready || adding !== null}
                    aria-busy={adding === app.slug}
                    aria-label={t(($) => $.add_app_dialog.add_aria, { app: app.name || app.slug })}
                    onClick={() => add(app)}
                  >
                    {adding === app.slug ? <Loader2 className="animate-spin" /> : <Plus />}
                    {t(($) => $.add_app_dialog.add)}
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
