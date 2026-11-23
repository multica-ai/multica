import { useParams } from "react-router-dom";
import { DocPage, DocHistoryPage, DocsHomePage } from "@multica/views/docs";
export function DocRoute() {
  return <DocPage docId={useParams().id ?? ""} />;
}
export function DocHistoryRoute() {
  return <DocHistoryPage docId={useParams().id ?? ""} />;
}
export function DocFolderRoute() {
  return <DocsHomePage folderId={useParams().id ?? ""} />;
}
