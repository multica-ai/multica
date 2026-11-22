"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertCircle, Plug } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { appCatalogOptions, appsOverviewOptions } from "@multica/core/connected-apps";
import { ComposioToolkitLogo } from "../../common/composio-toolkit-logo";
import { useComposioAvailable } from "../../common/use-composio-available";
import { CollectionPageState } from "../../layout/collection-page";
import { OBJECT_PAGE_BODY, ObjectIdentity, ObjectPage } from "../../layout/object-page";
import { useT } from "../../i18n";
import { useAppConnectReturn } from "../hooks/use-app-connect-return";
import { AppAccountsSection } from "./app-accounts-card";
import { AppActivityCard } from "./app-activity-card";
import { AppAgentsCard } from "./app-agents-card";
import { AppCapabilitiesCard } from "./app-capabilities-card";

const LAYOUT_ID = "multica_app_detail_layout";

/** One app: its accounts, which agents use it and as whom, and what they did. */
export function AppDetailPage({ toolkitSlug }: { toolkitSlug: string }) {
  const { t } = useT("settings");
  const { t: tl } = useT("layout");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const available = useComposioAvailable();
  const overview = useQuery({ ...appsOverviewOptions(wsId), enabled: available });
  const catalog = useQuery({ ...appCatalogOptions(wsId), enabled: available });
  useAppConnectReturn();

  const entry = catalog.data?.apps.find((a) => a.slug === toolkitSlug);
  const name = entry?.name ?? toolkitSlug;
  const listSegment = { href: paths.apps(), label: tl(($) => $.nav.apps) };
  const leaf = overview.data || catalog.data
    ? <span className="truncate text-foreground">{name}</span>
    : <Skeleton className="h-4 w-32" />;

  if (!available) {
    return (
      <ObjectPage type="app" layoutId={LAYOUT_ID} segments={[listSegment]} leaf={leaf}>
        <CollectionPageState icon={Plug} title={t(($) => $.connected_apps.hub.unavailable)} />
      </ObjectPage>
    );
  }

  if (overview.isError) {
    return (
      <ObjectPage type="app" layoutId={LAYOUT_ID} segments={[listSegment]} leaf={leaf}>
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
      </ObjectPage>
    );
  }

  const data = overview.data;
  return (
    <ObjectPage
      type="app"
      layoutId={LAYOUT_ID}
      segments={[listSegment]}
      leaf={leaf}
      panel={
        data ? <AppCapabilitiesCard /> : undefined
      }
    >
      <div className={`${OBJECT_PAGE_BODY} space-y-8`}>
        <ObjectIdentity
          media={
            <ComposioToolkitLogo
              slug={toolkitSlug}
              name={name}
              fallbackLogo={entry?.logo}
              className="size-12 rounded-xl"
            />
          }
          title={name}
          description={entry?.category}
        />
        {data ? (
          <>
            <AppAccountsSection slug={toolkitSlug} name={name} logo={entry?.logo} overview={data} />
            <AppAgentsCard slug={toolkitSlug} name={name} entry={entry} overview={data} />
            <AppActivityCard slug={toolkitSlug} name={name} canManage={data.can_manage} />
          </>
        ) : (
          <p className="text-body text-muted-foreground">{t(($) => $.connected_apps.loading)}</p>
        )}
      </div>
    </ObjectPage>
  );
}
