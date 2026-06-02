import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { IssueDetail } from "@wallts/views/issues/components";
import { useWorkspaceId } from "@wallts/core/hooks";
import { issueDetailOptions } from "@wallts/core/issues/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function IssueDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: issue } = useQuery(issueDetailOptions(wsId, id!));

  useDocumentTitle(issue ? `${issue.identifier}: ${issue.title}` : "Issue");

  if (!id) return null;
  // Render errors bubble to the root route errorElement (DesktopRouteErrorPage),
  // which contains the crash inside the tab content pane. No page-level boundary
  // here — a whole-page wrapper duplicates the route-level error UI.
  return <IssueDetail issueId={id} />;
}
