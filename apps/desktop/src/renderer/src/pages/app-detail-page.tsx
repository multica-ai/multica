import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { AppDetailPage as SharedAppDetailPage } from "@multica/views/apps";
import { useWorkspaceId } from "@multica/core/hooks";
import { appCatalogOptions } from "@multica/core/connected-apps";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function AppDetailPage() {
  const { slug } = useParams<{ slug: string }>();
  const wsId = useWorkspaceId();
  const { data: catalog } = useQuery({ ...appCatalogOptions(wsId), enabled: false });

  useDocumentTitle(catalog?.apps.find((a) => a.slug === slug)?.name ?? "App");

  if (!slug) return null;
  return <SharedAppDetailPage toolkitSlug={slug} />;
}
