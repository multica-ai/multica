import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { RuntimeDetailPage as SharedRuntimeDetailPage } from "@multica/views/runtimes";
import { useWorkspaceId } from "@multica/core/hooks";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { useT } from "@multica/i18n/react";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function RuntimeDetailPage() {
  const t = useT("desktop");
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: runtimes } = useQuery(runtimeListOptions(wsId));
  const runtime = runtimes?.find((r) => r.id === id);

  useDocumentTitle(runtime?.name ?? t("page_runtime"));

  if (!id) return null;
  return <SharedRuntimeDetailPage runtimeId={id} />;
}
