import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { IssueDetail } from "@multica/views/issues/components";
import { IssueReferenceList } from "@multica/cerebro-references/views";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function IssueDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: issue } = useQuery(issueDetailOptions(wsId, id!));
  const referencesEnabled = useFeatureFlag("cerebro_references");

  useDocumentTitle(issue ? `${issue.identifier}: ${issue.title}` : "Issue");

  if (!id) return null;
  return (
    <ErrorBoundary resetKeys={[id]}>
      <IssueDetail
        issueId={id}
        extensions={referencesEnabled ? <IssueReferenceList issueId={id} /> : null}
      />
    </ErrorBoundary>
  );
}
