"use client";

import { useMemo, useState, type ComponentType, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertCircle, AlertTriangle, LayoutGrid, Plug, Search, Star, User, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
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
import { InfiniteScrollSentinel } from "../../issues/components/infinite-scroll-sentinel";
import { CollectionPageState } from "../../layout/collection-page";
import { CollapsedNavTrigger, PageHeader } from "../../layout/page-header";
import { PanelBody, PanelCount, PanelHeader, PanelLink, PanelList, PanelSection } from "../../layout/panel-nav";
import { useRestoredScrollRef } from "../../platform/scroll-restoration";
import { useTeamAppAccess } from "../../common/use-team-app-access";
import { useViewerAppsOverview } from "../../common/use-viewer-apps-overview";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { useAppCategoryLabel } from "../hooks/use-app-category-label";
import { useAppConnectReturn } from "../hooks/use-app-connect-return";
import { groupAppsByCategory } from "../lib/categories";
import { buildAppDirectory, matchesAppSearch, type DirectoryApp } from "../lib/directory";
import { AccountsList, useAccountRows } from "./accounts-list";
import { AppCardSkeleton } from "./app-card";
import { DirectoryCard, SetUpCard, type DirectoryCardHandlers } from "./directory-card";

const TABS = ["in_use", "attention", "accounts", "all"] as const;
type DirectoryTab = (typeof TABS)[number];

/** Cards rendered per scroll step; the full catalog has well over a thousand apps. */
const PAGE_SIZE = 60;
const CURATED = "curated";
const GRID_CLASS = "grid gap-3 @lg:grid-cols-2 @3xl:grid-cols-3";

function parseTab(value: string | null): DirectoryTab {
  return (TABS as readonly string[]).includes(value ?? "") ? (value as DirectoryTab) : "in_use";
}

type ConnectTarget = { slug: string; name: string; logo?: string; scope: AppAccountScope };

/** The workspace's apps: what agents use, what needs someone, and the catalog. */
export function AppsPage() {
  const { t } = useT("settings");
  const available = useComposioAvailable();
  if (available) return <AppsDirectory />;
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <AppsPageHeader />
      <CollectionPageState icon={Plug} title={t(($) => $.connected_apps.hub.unavailable)} />
    </div>
  );
}

/** The page header for states that have no directory to navigate. */
function AppsPageHeader() {
  const { t: tl } = useT("layout");
  return (
    <PageHeader>
      <h1 className="flex min-w-0 flex-1 items-center gap-2 px-2 text-body font-medium">
        <Plug aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
        <span className="truncate">{tl(($) => $.nav.apps)}</span>
      </h1>
    </PageHeader>
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
  // Until the catalog arrives the directory only knows the apps in use, so
  // anything counted over the whole catalog would be wrong, not just early.
  // A failed refresh keeps the catalog already loaded.
  const catalogReady = !!catalog.data;
  const catalogPending = catalog.isPending;

  // The view lives in the URL so a connect flow started here comes back to it.
  const viewHref = (next: DirectoryTab, category: string | null = null) => {
    const params = new URLSearchParams(navigation.searchParams);
    if (next === "in_use") params.delete("tab");
    else params.set("tab", next);
    if (category) params.set("category", category);
    else params.delete("category");
    const search = params.toString();
    return `${navigation.pathname}${search ? `?${search}` : ""}`;
  };

  const directory = useMemo(() => {
    if (!viewerOverview) return null;
    return buildAppDirectory({ overview: viewerOverview, catalog: catalog.data?.apps ?? [], viewerId });
  }, [viewerOverview, catalog.data, viewerId]);
  const categories = useMemo(
    () => groupAppsByCategory(catalogReady ? (directory?.all ?? []) : []),
    [catalogReady, directory],
  );

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

  const restoreScrollRef = useRestoredScrollRef(`apps:${tab}:${categoryParam ?? ""}`);

  if (overview.isError) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <AppsPageHeader />
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
      </div>
    );
  }

  const loaded = directory && viewerOverview ? { directory, viewerOverview } : null;
  const needle = query.trim().toLowerCase();
  const matches = (app: DirectoryApp) =>
    matchesAppSearch(app, query) || !!categoryLabel.entry(app.entry)?.toLowerCase().includes(needle);
  const matchesAccount = (name: string) => !needle || name.toLowerCase().includes(needle);
  const inUse = loaded?.directory.inUse.filter(matches) ?? [];
  const attention = loaded?.directory.attention.filter(matches) ?? [];
  const all = catalogReady ? (loaded?.directory.all.filter(matches) ?? []) : [];
  const accountCount =
    accountRows.team.filter((row) => matchesAccount(row.name) || matchesAccount(row.slug)).length +
    accountRows.personal.filter((row) => matchesAccount(row.name) || matchesAccount(row.slug)).length;

  // An unknown category falls back to all apps once the catalog can tell;
  // until then the URL's choice stands so the view doesn't flip on arrival.
  const category = catalogReady
    ? categoryParam === CURATED || categories.groups.some((group) => group.key === categoryParam)
      ? categoryParam
      : null
    : categoryParam;
  const categoryGroup = categories.groups.find((group) => group.key === category);
  const inCategory = (app: DirectoryApp) =>
    category === CURATED ? !!app.entry?.curated : categories.categoryOf.get(app.slug) === category;
  const curatedCount = all.filter((app) => app.entry?.curated).length;
  const list = tab === "in_use" ? inUse : tab === "attention" ? attention : category ? all.filter(inCategory) : all;
  const pageKey = `${tab}|${category ?? ""}|${needle}`;
  const limit = page.key === pageKey ? page.limit : PAGE_SIZE;

  const catalogCount = (value: number) =>
    catalogReady ? (
      <PanelCount value={value} />
    ) : catalogPending ? (
      <Skeleton aria-hidden="true" className="h-3 w-5 shrink-0" />
    ) : null;

  const views: {
    key: string;
    tab: DirectoryTab;
    category: string | null;
    label: string;
    icon?: ComponentType<{ className?: string }>;
    count: ReactNode;
  }[] = [
    { key: "in_use", tab: "in_use", category: null, label: t(($) => $.connected_apps.hub.tabs.in_use), icon: Plug, count: <PanelCount value={inUse.length} /> },
    {
      key: "attention",
      tab: "attention",
      category: null,
      label: t(($) => $.connected_apps.hub.tabs.attention),
      icon: AlertTriangle,
      count: <PanelCount value={attention.length} className="text-warning" />,
    },
    { key: "accounts", tab: "accounts", category: null, label: t(($) => $.connected_apps.hub.tabs.accounts), icon: User, count: <PanelCount value={accountCount} /> },
  ];
  const catalogViews: typeof views = [
    { key: "all", tab: "all", category: null, label: t(($) => $.connected_apps.hub.tabs.all), icon: LayoutGrid, count: catalogCount(all.length) },
    { key: CURATED, tab: "all", category: CURATED, label: t(($) => $.connected_apps.hub.category_curated), icon: Star, count: catalogCount(curatedCount) },
  ];
  const categoryViews: typeof views = categories.groups.flatMap((group) => {
    const count = all.filter((app) => categories.categoryOf.get(app.slug) === group.key).length;
    if (count === 0 && category !== group.key) return [];
    return [{ key: group.key, tab: "all" as const, category: group.key, label: categoryLabel.group(group), count: <PanelCount value={count} /> }];
  });
  const isActive = (view: (typeof views)[number]) => view.tab === tab && (tab !== "all" || view.category === category);
  const activeView = [...views, ...catalogViews, ...categoryViews].find(isActive);

  const viewTitle =
    activeView?.label ??
    (category && catalogPending ? (
      <Skeleton aria-hidden="true" className="h-3.5 w-24" />
    ) : categoryGroup ? (
      categoryLabel.group(categoryGroup)
    ) : (
      t(($) => $.connected_apps.hub.tabs.all)
    ));
  const ViewIcon = activeView?.icon ?? LayoutGrid;
  const viewCount =
    !loaded || tab === "accounts" ? null : tab === "all" && !catalogReady ? (
      catalogPending ? <Skeleton aria-hidden="true" className="h-3 w-6" /> : null
    ) : (
      <span className="text-caption text-muted-foreground tabular-nums">{list.length}</span>
    );

  const navRow = (view: (typeof views)[number]) => {
    const Icon = view.icon;
    return (
      <PanelLink
        key={view.key}
        href={viewHref(view.tab, view.category)}
        label={view.label}
        icon={Icon ? <Icon /> : undefined}
        active={isActive(view)}
        trailing={view.count}
      />
    );
  };

  const emptyText = needle
    ? t(($) => $.connected_apps.hub.no_matches)
    : tab === "attention"
      ? t(($) => $.connected_apps.hub.empty_attention)
      : tab === "in_use"
        ? t(($) => $.connected_apps.hub.empty_in_use)
        : t(($) => $.connected_apps.add.empty);

  let content: ReactNode;
  if (!loaded) {
    content = <CardGridSkeleton count={6} />;
  } else if (tab === "accounts") {
    content = <AccountsList overview={loaded.viewerOverview} rows={accountRows} matches={matchesAccount} />;
  } else if (tab === "all" && catalog.isError && !catalogReady) {
    content = <CatalogFailed onRetry={() => catalog.refetch()} />;
  } else if (tab === "all" && !catalogReady) {
    content = <CardGridSkeleton count={12} />;
  } else {
    const namePending = (app: DirectoryApp) => catalogPending && !app.entry;
    content = (
      <AppGrid
        apps={list}
        limit={limit}
        onShowMore={() => setPage({ key: pageKey, limit: limit + PAGE_SIZE })}
        emptyText={emptyText}
        render={(app) => (
          <DirectoryCard
            key={app.slug}
            app={app}
            handlers={handlers}
            detail={tab === "all" && category ? "connect" : "category"}
            namePending={namePending(app)}
          />
        )}
      />
    );
  }

  const showPopular = loaded && tab === "in_use" && !needle;

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden md:flex-row">
      <aside className="flex shrink-0 flex-col border-surface-border max-md:border-b md:w-60 md:border-r">
        <PanelHeader as="h1" title={tl(($) => $.nav.apps)} leading={<CollapsedNavTrigger />} />
        <div className="px-4 pb-3 md:hidden">
          <label className="sr-only" htmlFor="apps-navigation">
            {tl(($) => $.nav.apps)}
          </label>
          <select
            id="apps-navigation"
            className="h-10 w-full rounded-lg border border-input bg-background px-3 text-body text-foreground focus-visible:outline-2 focus-visible:outline-ring"
            value={viewHref(tab, category)}
            onChange={(event) => navigation.push(event.target.value)}
          >
            {views.map((view) => (
              <option key={view.key} value={viewHref(view.tab, view.category)}>
                {view.label}
              </option>
            ))}
            <optgroup label={t(($) => $.connected_apps.hub.catalog)}>
              {catalogViews.map((view) => (
                <option key={view.key} value={viewHref(view.tab, view.category)}>
                  {view.label}
                </option>
              ))}
            </optgroup>
            {categoryViews.length > 0 && (
              <optgroup label={t(($) => $.connected_apps.hub.categories_label)}>
                {categoryViews.map((view) => (
                  <option key={view.key} value={viewHref(view.tab, view.category)}>
                    {view.label}
                  </option>
                ))}
              </optgroup>
            )}
          </select>
        </div>
        <div className="relative mx-4 mb-3 shrink-0 md:mx-2">
          <Search
            aria-hidden="true"
            className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
          />
          <input
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Escape" && query && !event.nativeEvent.isComposing) {
                event.preventDefault();
                setQuery("");
              }
            }}
            placeholder={t(($) => $.connected_apps.hub.search)}
            aria-label={t(($) => $.connected_apps.hub.search)}
            className="h-8 w-full rounded-lg border border-input bg-transparent pl-[37px] pr-8 text-body outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30 [&::-webkit-search-cancel-button]:hidden"
          />
          {query ? (
            <button
              type="button"
              onClick={() => setQuery("")}
              aria-label={t(($) => $.page.search_clear)}
              className="absolute right-1.5 top-1/2 flex size-6 -translate-y-1/2 items-center justify-center rounded-md text-muted-foreground hover:bg-surface-hover hover:text-foreground"
            >
              <X className="size-3.5" aria-hidden="true" />
            </button>
          ) : null}
        </div>
        <nav aria-label={tl(($) => $.nav.apps)} className="hidden min-h-0 flex-1 flex-col md:flex">
          <PanelBody>
            <PanelList>{views.map(navRow)}</PanelList>
            <PanelSection label={t(($) => $.connected_apps.hub.catalog)}>
              <PanelList>{catalogViews.map(navRow)}</PanelList>
            </PanelSection>
            {catalogPending ? (
              <PanelSection label={t(($) => $.connected_apps.hub.categories_label)}>
                <NavSkeleton rows={8} />
              </PanelSection>
            ) : categoryViews.length > 0 ? (
              <PanelSection label={t(($) => $.connected_apps.hub.categories_label)}>
                <PanelList>{categoryViews.map(navRow)}</PanelList>
              </PanelSection>
            ) : null}
          </PanelBody>
        </nav>
      </aside>

      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <PageHeader leading={false}>
          <h2 className="flex min-w-0 flex-1 items-center gap-2 px-2 text-body font-medium">
            <ViewIcon aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
            <span className="truncate">{viewTitle}</span>
            {viewCount}
          </h2>
        </PageHeader>
        <div
          ref={restoreScrollRef}
          key={`${tab}:${categoryParam ?? ""}`}
          data-tab-scroll-root={`apps:${tab}:${categoryParam ?? ""}`}
          className="@container min-h-0 min-w-0 flex-1 overflow-y-auto overscroll-contain"
        >
          <div className="mx-auto w-full max-w-5xl space-y-6 px-4 py-6 sm:px-6">
            {loaded && loaded.viewerOverview.catalog_mode !== "all" && (
              <SavedAppRestriction
                kind="catalog"
                onRestore={loaded.viewerOverview.can_manage ? () => restore.mutateAsync({ catalog: true }) : undefined}
              />
            )}
            {tab === "in_use" && (
              <p className="text-body text-muted-foreground">
                {t(($) => $.connected_apps.hub.description, { workspace: workspaceName })}
              </p>
            )}
            {content}
            {showPopular && (catalogPending || loaded.directory.popular.length > 0) && (
              <section className="space-y-3">
                <h3 className="text-body font-semibold">
                  {t(($) => $.connected_apps.hub.popular, { workspace: workspaceName })}
                </h3>
                {catalogPending ? (
                  <CardGridSkeleton count={6} />
                ) : (
                  <ul className={GRID_CLASS}>
                    {loaded.directory.popular.map((app) => <SetUpCard key={app.slug} app={app} />)}
                  </ul>
                )}
              </section>
            )}
          </div>
        </div>
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
  render,
}: {
  apps: DirectoryApp[];
  limit: number;
  onShowMore: () => void;
  emptyText: string;
  render: (app: DirectoryApp) => ReactNode;
}) {
  if (apps.length === 0) {
    return (
      <p className="rounded-lg border border-dashed px-4 py-6 text-center text-caption text-muted-foreground">
        {emptyText}
      </p>
    );
  }
  return (
    <>
      <ul className={GRID_CLASS}>{apps.slice(0, limit).map(render)}</ul>
      {apps.length > limit && <InfiniteScrollSentinel onVisible={onShowMore} loading={false} rootMargin="400px" />}
    </>
  );
}

function CatalogFailed({ onRetry }: { onRetry: () => void }) {
  const { t } = useT("settings");
  return (
    <CollectionPageState
      role="alert"
      tone="destructive"
      size="compact"
      icon={AlertCircle}
      title={t(($) => $.connected_apps.hub.catalog_failed)}
      actions={
        <Button variant="outline" size="sm" onClick={onRetry}>
          {t(($) => $.connected_apps.hub.retry)}
        </Button>
      }
    />
  );
}

function CardGridSkeleton({ count }: { count: number }) {
  return (
    <ul aria-hidden="true" className={GRID_CLASS}>
      {Array.from({ length: count }, (_, i) => <AppCardSkeleton key={i} />)}
    </ul>
  );
}

function NavSkeleton({ rows }: { rows: number }) {
  return (
    <div aria-hidden="true" className="space-y-px">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex h-(--nav-item-height,2rem) items-center gap-2.5 px-3">
          <Skeleton className="h-3 flex-1" style={{ maxWidth: `${55 + ((i * 37) % 35)}%` }} />
          <Skeleton className="h-3 w-5" />
        </div>
      ))}
    </div>
  );
}
