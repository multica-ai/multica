import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { MemoryDetailPage as SharedMemoryDetailPage } from "@multica/views/memory/components";
import { useWorkspaceId } from "@multica/core/hooks";
import { memoryDetailOptions } from "@multica/core/memory";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function MemoryDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: artifact } = useQuery(memoryDetailOptions(wsId, id!));

  useDocumentTitle(artifact ? artifact.title : "Memory");

  if (!id) return null;
  return <SharedMemoryDetailPage id={id} />;
}
