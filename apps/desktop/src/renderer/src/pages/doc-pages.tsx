import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { DocDetailPage, DocFolderPage, DocHistoryPage } from "@multica/views/docs";
import { docDetailOptions, docTreeOptions } from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useDocumentTitle } from "@/hooks/use-document-title";

function useDocTitle(id: string | undefined) {
  const wsId = useWorkspaceId();
  const { data: doc } = useQuery({ ...docDetailOptions(wsId, id ?? ""), enabled: !!id });
  useDocumentTitle(doc?.title || "Doc");
}

export function DesktopDocDetailPage() {
  const { id } = useParams<{ id: string }>();
  useDocTitle(id);
  if (!id) return null;
  return <DocDetailPage docId={id} />;
}

export function DesktopDocHistoryPage() {
  const { id } = useParams<{ id: string }>();
  useDocTitle(id);
  if (!id) return null;
  return <DocHistoryPage docId={id} />;
}

export function DesktopDocFolderPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: tree } = useQuery(docTreeOptions(wsId));
  useDocumentTitle(tree?.folders.find((f) => f.id === id)?.title ?? "Folder");
  if (!id) return null;
  return <DocFolderPage folderId={id} />;
}
