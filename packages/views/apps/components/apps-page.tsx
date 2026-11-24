"use client";

import { useMemo, useState, type ReactNode } from "react";
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
import { useAppCategoryLabel } from "../hooks/use-app-category-label";
import { useAppConnectReturn } from "../hooks/use-app-connect-return";
import { groupAppsByCategory } from "../lib/categories";
import { buildAppDirectory, matchesAppSearch, type DirectoryApp } from "../lib/directory";
import { AccountsList, useAccountRows } from "./accounts-list";
import { DirectoryCard, SetUpCard, type DirectoryCardHandlers } from "./directory-card";

const TABS = ["in_use", "attention", "accounts", "all"] as const;
type DirectoryTab = (typeof TABS)[number];

/** Cards rendered before "Show more"; the full catalog has well over a thousand apps. */
const PAGE_SIZE = 60;
const CURATED = "curated";

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
  const categoryParam = tab === "all" ? navigation.searchParams.get("category") : null;
  const [query, setQuery] = useState("");
  const [connecting, setConnecting] = useState<ConnectTarget | null>(null);
  const [asking, setAsking] = useState<string | null>(null);
  const [page, setPage] = useState({ key: "", limit: PAGE_SIZE });
  const categoryLabel = useAppCategoryLabel();
  useAppConnectReturn();
  const accountRows = useAccountRows(viewerOverview, catalog.data?.apps ?? []);

  // The tab and category live in the URL so a connect flow started here comes back to them.
  const replaceParams = (update: (params: URLSearchParams) => void) => {
    const params = new URLSearchParams(navigation.searchParams);
    update(params);
    const search = params.toString();
    navigation.replace(`${navigation.pathname}${search ? `?${search}` : ""}`);
  };
  const setTab = (next: DirectoryTab) =>
    replaceParams((params) => {
      if (next === "in_use") params.delete("tab");
      else params.set("tab", next);
      params.delete("category");
    });
  const setCategory = (next: string | null) =>
    replaceParams((params) => {
      if (next) params.set("category", next);
      else params.delete("category");
    });

  const directory = useMemo(() => {
    if (!viewerOverview) return null;
    return buildAppDirectory({ overview: viewerOverview, catalog: catalog.data?.apps ?? [], viewerId });
  }, [viewerOverview, catalog.data, viewerId]);
  const categories = useMemo(() => groupAppsByCategory(directory?.all ?? []), [directory]);

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
  const needle = query.trim().toLowerCase();
  const matches = (app: DirectoryApp) =>
    matchesAppSearch(app, query) || !!categoryLabel.entry(app.entry)?.toLowerCase().includes(needle);
  const found = tab === "accounts" ? [] : lists[tab].filter(matches);
  const category =
    categoryParam === CURATED || categories.groups.some((group) => group.key === categoryParam)
      ? categoryParam
      : null;
  const inCategory = (app: DirectoryApp) =>
    category === CURATED ? !!app.entry?.curated : categories.categoryOf.get(app.slug) === category;
  const shown = category ? found.filter(inCategory) : found;
  const pageKey = `${tab}|${category ?? ""}|${needle}`;
  const limit = page.key === pageKey ? page.limit : PAGE_SIZE;
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
        ) : tab === "all" ? (
          <div className="grid gap-6 md:grid-cols-[12rem_minmax(0,1fr)]">
            <nav
              aria-label={t(($) => $.connected_apps.hub.categories_nav)}
              className="-mx-1 flex gap-1 overflow-x-auto px-1 md:sticky md:top-0 md:mx-0 md:flex-col md:gap-px md:self-start md:overflow-visible md:px-0"
            >
              <CategoryItem
                label={t(($) => $.connected_apps.hub.category_all)}
                count={found.length}
                active={category === null}
                onSelect={() => setCategory(null)}
              />
              <CategoryItem
                label={t(($) => $.connected_apps.hub.category_curated)}
                count={found.filter((app) => app.entry?.curated).length}
                active={category === CURATED}
                onSelect={() => setCategory(CURATED)}
              />
              <p className="hidden px-2 pt-3 pb-1 text-caption font-medium text-muted-foreground md:block">
                {t(($) => $.connected_apps.hub.categories_label)}
              </p>
              {categories.groups.map((group) => {
                const count = found.filter((app) => categories.categoryOf.get(app.slug) === group.key).length;
                if (count === 0 && category !== group.key) return null;
                return (
                  <CategoryItem
                    key={group.key}
                    label={categoryLabel.group(group)}
                    count={count}
                    active={category === group.key}
                    onSelect={() => setCategory(group.key)}
                  />
                );
              })}
            </nav>
            <section className="min-w-0 space-y-3">
              {category && (
                <h3 className="flex items-baseline gap-2 text-body font-semibold">
                  {category === CURATED
                    ? t(($) => $.connected_apps.hub.category_curated)
                    : categoryLabel.group(categories.groups.find((group) => group.key === category)!)}
                  <span className="text-caption font-normal text-muted-foreground tabular-nums">{shown.length}</span>
                </h3>
              )}
              <AppGrid
                apps={shown}
                limit={limit}
                onShowMore={() => setPage({ key: pageKey, limit: limit + PAGE_SIZE })}
                emptyText={emptyText}
                columns="sm:grid-cols-2 xl:grid-cols-3"
                render={(app) => (
                  <DirectoryCard key={app.slug} app={app} handlers={handlers} detail={category ? "connect" : "category"} />
                )}
              />
            </section>
          </div>
        ) : (
          <AppGrid
            apps={shown}
            limit={limit}
            onShowMore={() => setPage({ key: pageKey, limit: limit + PAGE_SIZE })}
            emptyText={emptyText}
            columns="sm:grid-cols-2 lg:grid-cols-3"
            render={(app) => <DirectoryCard key={app.slug} app={app} handlers={handlers} />}
          />
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

function AppGrid({
  apps,
  limit,
  onShowMore,
  emptyText,
  columns,
  render,
}: {
  apps: DirectoryApp[];
  limit: number;
  onShowMore: () => void;
  emptyText: string;
  columns: string;
  render: (app: DirectoryApp) => ReactNode;
}) {
  const { t } = useT("settings");
  if (apps.length === 0) {
    return (
      <p className="rounded-lg border border-dashed px-4 py-6 text-center text-caption text-muted-foreground">
        {emptyText}
      </p>
    );
  }
  const hidden = apps.length - limit;
  return (
    <>
      <ul className={`grid gap-3 ${columns}`}>{apps.slice(0, limit).map(render)}</ul>
      {hidden > 0 && (
        <div className="flex justify-center">
          <Button variant="outline" size="sm" onClick={onShowMore}>
            {t(($) => $.connected_apps.hub.show_more)}
            <span className="text-muted-foreground tabular-nums">{hidden}</span>
          </Button>
        </div>
      )}
    </>
  );
}

function CategoryItem({
  label,
  count,
  active,
  onSelect,
}: {
  label: string;
  count: number;
  active: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      aria-current={active ? "true" : undefined}
      onClick={onSelect}
      className="flex h-7 shrink-0 items-center gap-2 rounded-md px-2 text-left text-body whitespace-nowrap outline-none hover:bg-accent/60 focus-visible:ring-2 focus-visible:ring-ring aria-[current=true]:bg-accent aria-[current=true]:font-medium md:w-full"
    >
      <span className="min-w-0 flex-1 truncate">{label}</span>
      <span className="text-caption text-muted-foreground tabular-nums">{count}</span>
    </button>
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
