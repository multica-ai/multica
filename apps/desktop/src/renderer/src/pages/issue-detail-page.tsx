import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { IssueDetail } from "@multica/views/issues/components";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useT } from "@multica/i18n/react";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function IssueDetailPage() {
  const t = useT("desktop");
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: issue } = useQuery(issueDetailOptions(wsId, id!));

  useDocumentTitle(issue ? `${issue.identifier}: ${issue.title}` : t("page_issue"));

  if (!id) return null;
  return <IssueDetail issueId={id} />;
}
