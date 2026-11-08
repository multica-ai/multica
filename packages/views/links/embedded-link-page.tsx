"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { linkListOptions, expandLinkUrl, validateLinkUrl } from "@multica/core/links";
import { ExternalLink, Link, FileQuestion } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { PageHeader } from "../layout/page-header";
import { CollectionPageState } from "../layout/collection-page";
import { useNavigation } from "../navigation";
import { openExternal } from "../platform";
import { useT } from "../i18n";
import { LinkIcon } from "./components/link-icon";

export const LINK_SANDBOX = "allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox allow-downloads";

export function EmbeddedLinkPage({ id }: {id: string}) {
  const {t} = useT("settings"); const wsId = useWorkspaceId(); const ws = useCurrentWorkspace();
  const navigation = useNavigation(); const projectId = navigation.searchParams.get("project") ?? undefined;
  const {data: links = [], isLoading, isError} = useQuery(linkListOptions(wsId, projectId));
  if (isLoading) return <CollectionPageState icon={Link} title={t(($) => $.links.loading)} />;
  if (isError) return <CollectionPageState icon={Link} tone="destructive" title={t(($) => $.links.load_failed)} />;
  const link = links.find((entry) => entry.id === id);
  if (!link || !ws || validateLinkUrl(link.urlTemplate, !!link.projectId)) return <CollectionPageState icon={FileQuestion} title={t(($) => $.links.not_found)} />;
  const url = expandLinkUrl(link.urlTemplate, {workspaceSlug: ws.slug, workspaceId: ws.id, projectId: link.projectId ?? undefined});
  return <div className="flex min-h-0 flex-1 flex-col">
    <PageHeader><div className="flex min-w-0 flex-1 items-center gap-2"><LinkIcon icon={link.icon} /><h1 className="truncate text-body font-medium">{link.name}</h1></div><Button variant="ghost" size="icon-sm" aria-label={t(($) => $.links.open)} onClick={() => openExternal(url)}><ExternalLink /></Button></PageHeader>
    <iframe title={link.name} src={url} sandbox={LINK_SANDBOX + (link.allowSameOrigin === true ? " allow-same-origin" : "")} referrerPolicy="no-referrer" className="min-h-0 w-full flex-1 border-0" />
    <div className="flex shrink-0 flex-wrap items-center gap-2 border-t p-3 text-caption text-muted-foreground"><p className="min-w-0 flex-1">{t(($) => $.links.fallback)}</p><Button variant="outline" size="sm" onClick={() => openExternal(url)}>{t(($) => $.links.open)}</Button></div>
  </div>;
}
