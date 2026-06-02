import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { RuntimeDetailPage as SharedRuntimeDetailPage } from "@wallts/views/runtimes";
import { useWorkspaceId } from "@wallts/core/hooks";
import { runtimeListOptions } from "@wallts/core/runtimes/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function RuntimeDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: runtimes } = useQuery(runtimeListOptions(wsId));
  const runtime = runtimes?.find((r) => r.id === id);

  useDocumentTitle(runtime?.name ?? "Runtime");

  if (!id) return null;
  return <SharedRuntimeDetailPage runtimeId={id} />;
}
