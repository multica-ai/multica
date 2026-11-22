"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertCircle, Plug, Search } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Tabs, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  appCatalogOptions,
  appsOverviewOptions,
  useDismissTeamAppRequests,
  useRequestTeamApp,
  useRestoreAppAccess,
} from "@multica/core/connected-apps";
import type { AppAccountScope } from "@multica/core/types";
import { ConnectAppDialog } from "../../common/connect-app-dialog";
import { SavedAppRestriction } from "../../common/saved-app-restriction";
import { useComposioAvailable } from "../../common/use-composio-available";
import { CollectionPageState } from "../../layout/collection-page";
import { PageHeader } from "../../layout/page-header";
import { useTeamAppAccess } from "../../common/use-team-app-access";
import { useViewerAppsOverview } from "../../common/use-viewer-apps-overview";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { useAppConnectReturn } from "../hooks/use-app-connect-return";
import { buildAppDirectory, matchesAppSearch, type DirectoryApp } from "../lib/directory";
import { AccountsList, useAccountRows } from "./accounts-list";
import { DirectoryCard, SetUpCard, type DirectoryCardHandlers } from "./directory-card";

const TABS = ["in_use", "attention", "accounts", "all"] as const;
type DirectoryTab = (typeof TABS)[number];

function parseTab(value: string | null): DirectoryTab {
  return (TABS as readonly string[]).includes(value ?? "") ? (value as DirectoryTab) : "in_use";
}

type ConnectTarget = { slug: string; name: string; logo?: string; scope: AppAccountScope };

/** The workspace's apps: what agents use, what needs someone, and the catalog. */
export function AppsPage() {
  const { t } = useT("settings");
  const { t: tl } = useT("layout");
  const available = useComposioAvailable();

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader>
        <h1 className="flex min-w-0 flex-1 items-center gap-2 px-2 text-body font-medium">
          <Plug aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
          <span className="truncate">{tl(($) => $.nav.apps)}</span>
        </h1>
      </PageHeader>
      {available ? (
        <AppsDirectory />
      ) : (
        <CollectionPageState icon={Plug} title={t(($) => $.connected_apps.hub.unavailable)} />
      )}
    </div>
  );
}

function AppsDirectory() {
  const { t } = useT("settings");
  const { t: tl } = useT("layout");
  const wsId = useWorkspaceId();
  const workspaceName = useCurrentWorkspace()?.name ?? t(($) => $.page.workspace_fallback);
  const viewerId = useAuthStore((s) => s.user?.id ?? null);
  const overview = useQuery(appsOverviewOptions(wsId));
  const teamAccess = useTeamAppAccess(overview.data);
  const catalog = useQuery(appCatalogOptions(wsId));
  const viewerOverview = useViewerAppsOverview(wsId, overview.data);
  const restore = useRestoreAppAccess();
  const requestTeam = useRequestTeamApp();
  const withdrawTeam = useDismissTeamAppRequests();
  const navigation = useNavigation();
  const tab = parseTab(navigation.searchParams.get("tab"));
  const [query, setQuery] = useState("");
  const [connecting, setConnecting] = useState<ConnectTarget | null>(null);
  const [asking, setAsking] = useState<string | null>(null);
  useAppConnectReturn();
  const accountRows = useAccountRows(viewerOverview, catalog.data?.apps ?? []);

  // The tab lives in the URL so a connect flow started here comes back to it.
  const setTab = (next: DirectoryTab) => {
    const params = new URLSearchParams(navigation.searchParams);
    if (next === "in_use") params.delete("tab");
    else params.set("tab", next);
    const search = params.toString();
    navigation.replace(`${navigation.pathname}${search ? `?${search}` : ""}`);
  };

  const directory = useMemo(() => {
    if (!viewerOverview) return null;
    return buildAppDirectory({ overview: viewerOverview, catalog: catalog.data?.apps ?? [], viewerId });
  }, [viewerOverview, catalog.data, viewerId]);

  const handlers: DirectoryCardHandlers = {
    asking,
    needsUpgrade: teamAccess.needsUpgrade,
    onConnect: (app, scope) => setConnecting({ slug: app.slug, name: app.name, logo: app.entry?.logo, scope }),
    onAsk: (app) => {
      setAsking(app.slug);
      requestTeam.mutateAsync({ slug: app.slug }).then(
        () => toast.success(t(($) => $.connected_apps.hub.toast_requested, { app: app.name })),
        (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.connected_apps.toast_failed)),
      ).finally(() => setAsking(null));
    },
    onWithdraw: (app) => {
      setAsking(app.slug);
      withdrawTeam.mutateAsync(app.slug).then(
        () => toast.success(t(($) => $.connected_apps.account_row.toast_withdrawn)),
        (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.connected_apps.toast_failed)),
      ).finally(() => setAsking(null));
    },
  };

  if (overview.isLoading) {
    return <p className="px-6 py-8 text-body text-muted-foreground">{t(($) => $.connected_apps.loading)}</p>;
  }
  if (overview.isError || !directory || !viewerOverview) {
    return (
      <CollectionPageState
        role="alert"
        tone="destructive"
        icon={AlertCircle}
        title={t(($) => $.connected_apps.load_failed)}
        actions={
          <Button variant="outline" size="sm" onClick={() => overview.refetch()}>
            {t(($) => $.connected_apps.hub.retry)}
          </Button>
        }
      />
    );
  }

  const lists: Record<Exclude<DirectoryTab, "accounts">, DirectoryApp[]> = {
    in_use: directory.inUse,
    attention: directory.attention,
    all: directory.all,
  };
  const shown = tab === "accounts" ? [] : lists[tab].filter((app) => matchesAppSearch(app, query));
  const needle = query.trim().toLowerCase();
  const matchesAccount = (name: string) => !needle || name.toLowerCase().includes(needle);
  const emptyText = query.trim()
    ? t(($) => $.connected_apps.hub.no_matches)
    : tab === "attention"
      ? t(($) => $.connected_apps.hub.empty_attention)
      : tab === "in_use"
        ? t(($) => $.connected_apps.hub.empty_in_use)
        : catalog.isLoading
          ? t(($) => $.connected_apps.loading)
          : t(($) => $.connected_apps.add.empty);
  const popular = tab === "in_use" && !query.trim() ? directory.popular : [];

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-5xl space-y-6 px-6 py-8">
        <div className="space-y-1">
          <h2 className="text-title font-semibold">{tl(($) => $.nav.apps)}</h2>
          <p className="text-body text-muted-foreground">
            {t(($) => $.connected_apps.hub.description, { workspace: workspaceName })}
          </p>
        </div>

        {viewerOverview.catalog_mode !== "all" && (
          <SavedAppRestriction
            kind="catalog"
            onRestore={viewerOverview.can_manage ? () => restore.mutateAsync({ catalog: true }) : undefined}
          />
        )}

        <div className="flex flex-wrap items-center justify-between gap-3">
          <Tabs value={tab} onValueChange={(value) => setTab(value as DirectoryTab)}>
            <TabsList>
              <TabTrigger value="in_use" label={t(($) => $.connected_apps.hub.tabs.in_use)} count={directory.inUse.length} />
              <TabTrigger value="attention" label={t(($) => $.connected_apps.hub.tabs.attention)} count={directory.attention.length} />
              <TabTrigger value="accounts" label={t(($) => $.connected_apps.hub.tabs.accounts)} count={accountRows.team.length + accountRows.personal.length} />
              <TabTrigger value="all" label={t(($) => $.connected_apps.hub.tabs.all)} count={directory.all.length} />
            </TabsList>
          </Tabs>
          <div className="relative w-full sm:w-60">
            <Search aria-hidden="true" className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t(($) => $.connected_apps.hub.search)}
              aria-label={t(($) => $.connected_apps.hub.search)}
              className="pl-8"
            />
          </div>
        </div>

        {tab === "accounts" ? (
          <AccountsList overview={viewerOverview} rows={accountRows} matches={matchesAccount} />
        ) : shown.length > 0 ? (
          <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {shown.map((app) => <DirectoryCard key={app.slug} app={app} handlers={handlers} />)}
          </ul>
        ) : (
          <p className="rounded-lg border border-dashed px-4 py-6 text-center text-caption text-muted-foreground">
            {emptyText}
          </p>
        )}

        {popular.length > 0 && (
          <section className="space-y-3">
            <h3 className="text-body font-semibold">
              {t(($) => $.connected_apps.hub.popular, { workspace: workspaceName })}
            </h3>
            <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {popular.map((app) => <SetUpCard key={app.slug} app={app} />)}
            </ul>
          </section>
        )}
      </div>

      {connecting && (
        <ConnectAppDialog
          open
          onOpenChange={(open) => { if (!open) setConnecting(null); }}
          scope={connecting.scope}
          slug={connecting.slug}
          name={connecting.name}
          logo={connecting.logo}
        />
      )}
    </div>
  );
}

function TabTrigger({ value, label, count }: { value: DirectoryTab; label: string; count: number }) {
  return (
    <TabsTrigger value={value} className="px-2.5">
      {label}
      <span className="text-caption text-muted-foreground tabular-nums">{count}</span>
    </TabsTrigger>
  );
}
